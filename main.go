package main

import (
	"fmt"
	"os"

	"github.com/DefiantLabs/RedpointSwap/api/middleware"
	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/osmosis"
	"github.com/DefiantLabs/RedpointSwap/zenith"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"go.uber.org/zap"
)

func main() {
	conf := "config.toml"
	if len(os.Args) > 1 {
		conf = os.Args[1]
	}

	var err error
	config.Conf, err = config.GetConfig(conf)
	if err != nil {
		fmt.Println("Error getting config file. Err: ", err)
		os.Exit(1)
	}

	logLevel := config.Conf.Api.LogLevel
	logPath := config.Conf.Api.LogPath
	config.DoConfigureLogger([]string{logPath, "stdout"}, logLevel)

	disallowedExampleSecretKey := "example-key-do-not-use-this-k3y-1n-producti0n"
	//It is insecure to configure a SHA256 key with less than a 32 byte secret key
	if len(config.Conf.JWT.SecretKey) < 32 || string(config.Conf.JWT.SecretKey) == disallowedExampleSecretKey {
		config.Logger.Fatal("Insecure JWT configuration", zap.Int("Secret key length", len(config.Conf.JWT.SecretKey)))
	}

	//Initialize the codecs for Osmosis
	osmosis.Initialize()

	//Chain tx and query client
	txClient, err := osmosis.GetOsmosisTxClient(config.Conf.Api.ChainID, config.Conf.GetApiRpcSearchTxEndpoint(),
		config.Conf.Api.KeyringHomeDir, config.Conf.Api.KeyringBackend, config.Conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Fatal("GetOsmosisTxClient", zap.Error(err))
	}

	//get the bech32 address for the given key
	addr, err := osmosis.GetKeyAddressForKey(config.Conf.Api.ChainID, config.Conf.GetApiRpcSearchTxEndpoint(),
		config.Conf.Api.KeyringHomeDir, config.Conf.Api.KeyringBackend, config.Conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Fatal("GetKeyAddressForKey", zap.Error(err))
	}

	config.HotWalletAddress = addr

	//Make sure the hot wallet has funds
	hotWalletBalances, err := osmosis.GetAccountBalances(txClient, config.HotWalletAddress)
	if err != nil {
		config.Logger.Fatal("GetAccountBalances", zap.Error(err))
	}

	arbWalletBalanceRequired := sdk.NewCoin(config.Conf.Api.ArbitrageDenom, sdk.NewInt(config.Conf.Api.ArbitrageDenomMinAmount))
	arbWalletBalanceActual := osmosis.GetTokenBalance(config.Conf.Api.ArbitrageDenom, hotWalletBalances)
	if !arbWalletBalanceActual.GTE(arbWalletBalanceRequired.Amount) {
		config.Logger.Fatal("Hot wallet insufficient balance", zap.String("Required balance", arbWalletBalanceRequired.String()))
	}

	config.HotWalletArbBalance = arbWalletBalanceActual
	newBlocks := make(chan int64)
	done := make(chan struct{})

	//Detect when new blocks are produced on the chain
	go func() {
		defer close(done)
		osmosis.TrackBlocks(config.Conf.GetApiWebsocketEndpoint(), newBlocks)
	}()

	//Track average time between blocks and notify Zenith when a new block is available
	go func() {
		defer close(done)
		osmosis.ProcessNewBlock(newBlocks, []func(int64, int64){zenith.ZenithBlockNotificationHandler})
	}()

	go func() {
		defer close(done)
		middleware.InitializeRestApi()
	}()

	<-done
}
