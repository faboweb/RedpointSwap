package osmosis

import (
	"context"
	"os"
	"time"

	"github.com/avast/retry-go"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authTypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	osmosisApp "github.com/osmosis-labs/osmosis/v12/app"
	osmosisParams "github.com/osmosis-labs/osmosis/v12/app/params"
)

var encodingConfig osmosisParams.EncodingConfig

func Initialize() {
	encodingConfig = osmosisApp.MakeEncodingConfig()
}

func GetOsmosisTxClient(chain string, node string, osmosisHomeDir string, keyringBackend string, fromFlag string) (client.Context, error) {
	clientCtx := client.Context{
		ChainID:      chain,
		NodeURI:      node,
		KeyringDir:   osmosisHomeDir,
		GenerateOnly: false,
	}

	ctxKeyring, krErr := client.NewKeyringFromBackend(clientCtx, keyringBackend)
	if krErr != nil {
		return clientCtx, krErr
	}

	clientCtx = clientCtx.WithKeyring(ctxKeyring)

	//Where node is the node RPC URI
	rpcClient, rpcErr := client.NewClientFromNode(node)

	if rpcErr != nil {
		return clientCtx, rpcErr
	}

	fromAddr, fromName, _, err := client.GetFromFields(clientCtx.Keyring, fromFlag, clientCtx.GenerateOnly)
	if err != nil {
		return clientCtx, err
	}

	clientCtx = clientCtx.WithCodec(encodingConfig.Marshaler).
		WithChainID(chain).
		WithFrom(fromFlag).
		WithFromAddress(fromAddr).
		WithFromName(fromName).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithAccountRetriever(authTypes.AccountRetriever{}).
		WithBroadcastMode(flags.BroadcastAsync).
		WithHomeDir(osmosisHomeDir).
		WithViper("OSMOSIS").
		WithNodeURI(node).
		WithClient(rpcClient).
		WithSkipConfirmation(true)

	return clientCtx, nil
}

func SubmitTxAwaitResponse(clientCtx client.Context, msgs []sdk.Msg, gas uint64, gasPrices string, fees string) (*txTypes.GetTxResponse, error) {
	txf := BuildTxFactory(clientCtx, gas)
	txf, txfErr := PrepareFactory(clientCtx, clientCtx.GetFromName(), txf)
	if txfErr != nil {
		return nil, txfErr
	}

	txBuilder, err := tx.BuildUnsignedTx(txf, msgs...)
	if err != nil {
		return nil, err
	}

	txBuilder.SetFeeGranter(clientCtx.GetFeeGranterAddress())

	err = tx.Sign(txf, clientCtx.GetFromName(), txBuilder, true)
	if err != nil {
		return nil, err
	}

	txBytes, err := clientCtx.TxConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, err
	}

	resp, err := clientCtx.BroadcastTxSync(txBytes)
	if err != nil {
		return nil, err
	}

	tx1resp, err := AwaitTx(clientCtx, resp.TxHash, 15*time.Second)
	if err != nil {
		return nil, err
	}
	return tx1resp, nil
}

// Get the TX by hash, waiting for it to be included in a block
func AwaitTx(clientCtx client.Context, txHash string, timeout time.Duration) (*txTypes.GetTxResponse, error) {
	var txByHash *txTypes.GetTxResponse
	var txLookupErr error
	startTime := time.Now()
	timeBetweenQueries := 100

	txClient := txTypes.NewServiceClient(clientCtx)

	for txByHash == nil {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		if time.Since(startTime) > timeout {
			cancel()
			return nil, txLookupErr
		}

		txByHash, txLookupErr = txClient.GetTx(ctx, &txTypes.GetTxRequest{Hash: txHash})
		if txLookupErr != nil {
			time.Sleep(time.Duration(timeBetweenQueries) * time.Millisecond)
		}
		cancel()
	}

	return txByHash, nil
}

func GetGasFee(numRoutes int) uint64 {
	return uint64(numRoutes * 200000)
}

var (
	// Variables used for retries
	RtyAttNum = uint(5)
	RtyAtt    = retry.Attempts(RtyAttNum)
	RtyDel    = retry.Delay(time.Millisecond * 400)
	RtyErr    = retry.LastErrorOnly(true)
)

func GetKeyAddress(clientCtx client.Context, keyName string) (sdk.AccAddress, error) {
	info, err := clientCtx.Keyring.Key(keyName)
	if err != nil {
		return nil, err
	}
	return info.GetAddress(), nil
}

func PrepareFactory(clientCtx client.Context, keyName string, txf tx.Factory) (tx.Factory, error) {
	var (
		err      error
		from     sdk.AccAddress
		num, seq uint64
	)

	// Get key address and retry if fail
	if err = retry.Do(func() error {
		from, err = GetKeyAddress(clientCtx, keyName)
		if err != nil {
			return err
		}
		return err
	}, RtyAtt, RtyDel, RtyErr); err != nil {
		return tx.Factory{}, err
	}

	// Set the account number and sequence on the transaction factory and retry if fail
	if err = retry.Do(func() error {
		if err = txf.AccountRetriever().EnsureExists(clientCtx, from); err != nil {
			return err
		}
		return err
	}, RtyAtt, RtyDel, RtyErr); err != nil {
		return txf, err
	}

	initNum, initSeq := txf.AccountNumber(), txf.Sequence()

	if initNum == 0 || initSeq == 0 {
		if err = retry.Do(func() error {
			num, seq, err = txf.AccountRetriever().GetAccountNumberSequence(clientCtx, from)
			if err != nil {
				return err
			}
			return err
		}, RtyAtt, RtyDel, RtyErr); err != nil {
			return txf, err
		}

		if initNum == 0 {
			txf = txf.WithAccountNumber(num)
		}

		if initSeq == 0 {
			txf = txf.WithSequence(seq)
		}
	}

	return txf, nil
}

func BuildTxFactory(clientContext client.Context, gas uint64) tx.Factory {
	gasPrices := "0.005uosmo"
	txf := newFactoryCLI(clientContext, gasPrices, gas)
	return txf
}

// NewFactoryCLI creates a new Factory.
func newFactoryCLI(clientCtx client.Context, gasPrices string, gas uint64) tx.Factory {
	f := tx.Factory{}

	f = f.WithChainID(clientCtx.ChainID)
	f = f.WithKeybase(clientCtx.Keyring)
	f = f.WithAccountRetriever(clientCtx.AccountRetriever)
	f = f.WithTxConfig(clientCtx.TxConfig)
	f = f.WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)
	f = f.WithGas(gas)
	f = f.WithGasPrices(gasPrices)

	if clientCtx.SignModeStr == flags.SignModeLegacyAminoJSON {
		//fmt.Println("Default sign-mode 'direct' not supported by Ledger, using sign-mode 'amino-json'.")
		f = f.WithSignMode(signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON)
	}

	return f
}
