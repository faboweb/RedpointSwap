package zenith

import (
	b64 "encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/DefiantLabs/RedpointSwap/api"
	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/osmosis"
	cosmosClient "github.com/cosmos/cosmos-sdk/client"
	cosmosSdk "github.com/cosmos/cosmos-sdk/types"
	bankTypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	gamm "github.com/osmosis-labs/osmosis/v13/x/gamm/types"
)

// Will either return an error with a reason the simulation shouldn't be submitted to Zenith,
// or the gas fee, zenith fee, and minimum arb amount to submit the arb to Mekatek Zenith API
func IsZenithEligible(simResult api.SimulatedSwapResult, txClient cosmosClient.Context) (
	arbSwaps []cosmosSdk.Msg,
	gasFeeInt cosmosSdk.Int,
	zenithFeeInt cosmosSdk.Int,
	err error,
) {
	conf := config.Conf

	arbTokenIn := simResult.ArbitrageSwap.SimulatedSwap.TokenIn
	maxBid, err := cosmosSdk.ParseCoinNormalized(conf.Zenith.MaximumBidAmount)
	if err != nil {
		err = errors.New("server misconfiguration (zenith MaximumBidAmount), please notify administrator")
		return
	} else if arbTokenIn.Denom != maxBid.Denom {
		err = fmt.Errorf("request arb denom is %s, but max bid denom configured as %s", arbTokenIn.Denom, maxBid.Denom)
		return
	}

	estimatedAmountOut := simResult.ArbitrageSwap.SimulatedSwap.TokenOutAmount.ToDec()
	estimatedArbProfit := estimatedAmountOut.Sub(arbTokenIn.Amount.ToDec())
	asF := strconv.FormatFloat(conf.Zenith.BidPercentage, 'f', 6, 64)
	zenithBidPercent, err := cosmosSdk.NewDecFromStr(asF)
	if err != nil {
		err = errors.New("server misconfiguration (zenith bid percentage), please notify administrator")
		return
	} else if estimatedArbProfit.LTE(cosmosSdk.ZeroDec()) {
		err = errors.New("arbitrage not profitable")
		return
	}

	zenithFee := estimatedArbProfit.Mul(zenithBidPercent)
	if zenithFee.GT(maxBid.Amount.ToDec()) {
		zenithFee = maxBid.Amount.ToDec()
	}

	if zenithFee.LTE(cosmosSdk.ZeroDec()) {
		err = errors.New("zenith fee calculation error")
		return
	}

	arbSwaps, err = osmosis.BuildArbitrageSwap(txClient, simResult.ArbitrageSwap.SimulatedSwap)
	if err != nil {
		err = errors.New("issue building arbitrage swap")
		return
	}

	gasFee := osmosis.GetGasFee(len(simResult.ArbitrageSwap.SimulatedSwap.Routes) * len(arbSwaps))
	gasFeeInt = cosmosSdk.NewIntFromUint64(gasFee)
	gasFeeUosmo := gasFeeInt.Quo(cosmosSdk.NewInt(200)) //equivalent of dividing by .005, which is the gasPrice amount
	if gasFeeInt.Equal(cosmosSdk.ZeroInt()) {
		err = errors.New("arbitrage swap must have 2-5 routes")
		return
	}

	zenithFeeInt = zenithFee.TruncateInt()
	totalArbFees := gasFeeUosmo.Add(zenithFeeInt)

	//Make sure the arbitrage TX will profit when we consider the zenith fee and TX fee
	arbMinOutDec := arbTokenIn.Amount.ToDec().Add(totalArbFees.ToDec())
	if arbMinOutDec.GT(estimatedAmountOut) {
		err = errors.New("not zenith eligible (unprofitable)")
	}

	//arbMinAmountOut = arbMinOutDec.TruncateInt()
	return
}

func PlaceBid(req BidRequest) ([]string, error) {
	conf := config.Conf

	txClient, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSubmitTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		return nil, errors.New("server misconfiguration (query client error), please notify administrator")
	}

	// The hot wallet will protect itself by only submitting bids in a way that guarantees profits (e.g. arb profits > bid amount)
	// This also considers many other factors such as gas fees
	arbSwaps, gasFeeInt, zenithFeeInt, err := IsZenithEligible(req.SimulatedSwap, txClient)
	if err != nil {
		return nil, err
	}

	userTxBytes, err := b64.StdEncoding.DecodeString(req.SwapTx)
	if err != nil {
		return nil, errors.New("provided user tx must be base 64 encoded")
	}

	decoder := txClient.TxConfig.TxDecoder()
	osmosisTx, err := decoder(userTxBytes)
	if err != nil {
		return nil, errors.New("TX must be a valid Osmosis TX")
	}

	//Whether or not the Bid was submitted with a signed user TX that matches the arbitrage simulator (e.g. the simulator simulated this TX).
	//This isn't intended to be a security check, it is just a sanity check so we don't accidentally place stupid bids.
	userTxMatchesSimulation := false
	msgs := osmosisTx.GetMsgs()
	for _, msg := range msgs {
		swapIn, ok := msg.(*gamm.MsgSwapExactAmountIn)
		if ok {
			if req.SimulatedSwap.SimulatedUserSwap.TokenIn.Denom == swapIn.TokenIn.Denom {
				diff := swapIn.TokenIn.Amount.Sub(req.SimulatedSwap.SimulatedUserSwap.TokenIn.Amount)
				absDiff := diff.Abs().ToDec()
				percentageDiff := absDiff.Quo(req.SimulatedSwap.SimulatedUserSwap.TokenIn.Amount.ToDec())
				percentDiffFloat, err := percentageDiff.Float64()
				if err == nil {
					if percentDiffFloat <= 0.005 { //tolerate .5% difference in case of conversion errors on client
						userTxMatchesSimulation = true
					}
				}
			}
		}
	}

	if !userTxMatchesSimulation {
		return nil, errors.New("TX ineligible for arbitrage (will not be submitted to Zenith)")
	}

	bidTxs := []string{req.SwapTx}
	zenithFeeUosmo, err := zenithFeeInt.ToDec().Float64()
	if err != nil {
		return nil, errors.New("unexpected zenith fee value")
	}

	hotWalletTxMsgs := []cosmosSdk.Msg{}
	hotWalletTxMsgs = append(hotWalletTxMsgs, arbSwaps...)

	total := 0.0
	for _, payment := range req.Payments {
		if payment.Denom != "uosmo" {
			return nil, fmt.Errorf("app only supports uosmo payments, but zenith auction requires %s", payment.Denom)
		}

		total += payment.Allocation
		fee := zenithFeeUosmo * payment.Allocation
		feeCoin := cosmosSdk.NewCoin("uosmo", cosmosSdk.NewInt(int64(math.Trunc(fee))))
		msgZenithPayment := &bankTypes.MsgSend{FromAddress: api.HotWalletAddress, ToAddress: payment.Address, Amount: []cosmosSdk.Coin{feeCoin}}
		hotWalletTxMsgs = append(hotWalletTxMsgs, msgZenithPayment)
	}

	if total != 1.0 {
		return nil, errors.New("zenith auction payments don't equal 1.0")
	}

	zenithTxBytes, err := osmosis.GetSignedTx(txClient, hotWalletTxMsgs, gasFeeInt.Uint64())
	if err != nil {
		return nil, errors.New("problem signing zenith arbitrage & payments TXs")
	}

	zenithTxB64 := b64.StdEncoding.EncodeToString(zenithTxBytes)
	bidTxs = append(bidTxs, zenithTxB64)
	return bidTxs, nil
}
