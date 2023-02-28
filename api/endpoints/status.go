package endpoints

import (
	"net/http"
	"strconv"

	"github.com/DefiantLabs/RedpointSwap/api"
	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/simulator"
	"github.com/DefiantLabs/RedpointSwap/zenith"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gin-gonic/gin"
)

type AuthzTradeStatus struct {
	UserArbitrage    UserArbitrageEarnings
	ChainHeight      int64      //The last known height of the chain
	TxsCommitted     bool       //True if our TXs were included in the block
	UserSwaps        []api.Swap //The user's swaps (for their 'normal' trade)
	ErrorCheckStatus string     //if some error occurred checking the status (just query the status endpoint again)
	TxError          string     //if some error occurred processing the user's TXs
}

type ZenithTradeStatus struct {
	WaitingForBlock  bool  //True if we are waiting for an available zenith block
	ZenithBlockBid   int64 //Will be non-zero if we bid on an auction block
	ChainHeight      int64 //The last known height of the chain
	TxsCommitted     bool  //True if our TXs were included in the block (only makes sense if ChainHeight >= ZenithBlockBid)
	UserArbitrage    UserArbitrageEarnings
	UserSwaps        []api.Swap //The user's swaps (for their 'normal' trade)
	ErrorCheckStatus string     //if some error occurred checking the status (just query the status endpoint again)
	TxError          string     //if some error occurred processing the user's TXs
	Simulation       simulator.SimulatedSwapResult
}

type UserArbitrageEarnings struct {
	ZenithArbitrageTxHash string     //The Zenith TX that captures arbitrage for the hot wallet
	SendUserFundsTxHash   string     //The hash of the TX that sends the user their arbitrage earnings
	HasArbitrage          bool       //Whether or not the user is owed any arbitrage
	EstimatedEarnings     []sdk.Coin //What we think the user will receive, based on the simulation and fees the hot wallet paid
	AmountInProgress      []sdk.Coin //Arb we owe to the user, we are working on sending (e.g. we submitted a TX to the chain)
	AmountReceived        []sdk.Coin //If the user received the arbitrage (e.g. the TX we submitted succeeded)
	Error                 string     //If there was some issue sending the user tokens or looking up the status
}

func GetTradeStatus(context *gin.Context) {
	id := context.Request.URL.Query().Get("id")
	if id == "" {
		context.JSON(http.StatusOK, gin.H{"error": "empty id provided"})
		return
	}

	var err error
	var zenithTxSet *api.ZenithArbitrageTxSet
	var authzTxSet *api.AuthzArbitrageTxSet

	zenithTxSet, err = api.GetQueuedZenithTxSet(id)
	if zenithTxSet == nil || err != nil {
		authzTxSet, err = api.GetQueuedAuthzTxSet(id)
		if authzTxSet == nil || err != nil {
			context.JSON(http.StatusOK, gin.H{"error": "invalid ID (not found)"})
			return
		}
	}

	if zenithTxSet != nil {
		ts := convertToZenithStatus(zenithTxSet)
		context.JSON(http.StatusOK, ts)
		return
	} else {
		ts := convertToAuthzStatus(authzTxSet)
		context.JSON(http.StatusOK, ts)
		return
	}
}

func convertToZenithStatus(userTrade *api.ZenithArbitrageTxSet) ZenithTradeStatus {
	ts := ZenithTradeStatus{
		UserArbitrage: UserArbitrageEarnings{},
	}

	if len(userTrade.TradeTxs) > 1 {
		ts.UserArbitrage.ZenithArbitrageTxHash = userTrade.TradeTxs[1].TxHash
	}

	estimatedAmountOut := userTrade.Simulation.ArbitrageSwap.SimulatedSwap.TokenOutAmount.ToDec()
	estimatedArbRevenue := estimatedAmountOut.Sub(userTrade.Simulation.ArbitrageSwap.SimulatedSwap.TokenIn.Amount.ToDec())
	totalArbFees, err := zenith.EstimateArbFees(*userTrade.Simulation)

	if err == nil {
		conf := config.Conf
		userProfitShare := 0.85
		if conf.Api.UserProfitSharePercentage <= .85 {
			userProfitShare = conf.Api.UserProfitSharePercentage
		}
		userProfitShareStr := strconv.FormatFloat(userProfitShare, 'f', 6, 64)
		userProfitShareDec, err := sdk.NewDecFromStr(userProfitShareStr)

		if err == nil {
			expectedProfit := estimatedArbRevenue.TruncateInt().Sub(totalArbFees)
			if expectedProfit.IsPositive() {
				expectedProfit = expectedProfit.ToDec().Mul(userProfitShareDec).TruncateInt()
				ts.UserArbitrage.EstimatedEarnings = sdk.NewCoins(sdk.NewCoin("uosmo", expectedProfit))
			} else {
				ts.UserArbitrage.HasArbitrage = false
			}
		}
	} else {
		ts.UserArbitrage.Error = "Problem estimating arbitrage earnings, check back for on-chain results"
	}

	// TXs are awaiting submission to a Zenith auction if:
	// 1) They have not been submitted to an auction before, OR
	// 2) They have been submitted before but didn't win the auction
	awaitingZenithBlock := userTrade.IsAwaitingZenithBlock()

	if userTrade.SubmittedAuctionBid != nil {
		ts.ZenithBlockBid = userTrade.SubmittedAuctionBid.Height
	}
	if userTrade.ErrorPlacingBid {
		ts.TxError = "Error placing bid, will reattempt"
	}

	ts.WaitingForBlock = awaitingZenithBlock
	ts.ChainHeight = userTrade.LastChainHeight
	ts.UserSwaps = getUserSwaps(userTrade.TradeTxs)
	ts.TxsCommitted = userTrade.Committed

	if !userTrade.UserProfitShareTx.ArbitrageProfitsPending.IsZero() || !userTrade.UserProfitShareTx.ArbitrageProfitsReceived.IsZero() {
		ts.UserArbitrage.HasArbitrage = true
		ts.UserArbitrage.SendUserFundsTxHash = userTrade.UserProfitShareTx.TxHash
	}

	if !userTrade.UserProfitShareTx.ArbitrageProfitsPending.IsZero() && userTrade.UserProfitShareTx.ArbitrageProfitsReceived.IsZero() {
		ts.UserArbitrage.AmountInProgress = userTrade.UserProfitShareTx.ArbitrageProfitsPending
	} else if !userTrade.UserProfitShareTx.ArbitrageProfitsReceived.IsZero() {
		ts.UserArbitrage.AmountReceived = userTrade.UserProfitShareTx.ArbitrageProfitsReceived
	}

	if userTrade.UserProfitShareTx.Initiated && userTrade.UserProfitShareTx.Committed && !userTrade.UserProfitShareTx.Succeeded {
		ts.UserArbitrage.AmountReceived = sdk.Coins{}
		ts.UserArbitrage.Error = "Problem sending user arbitrage (will not reattempt, please report address and time of trade)"
	}

	return ts
}

func convertToAuthzStatus(userTrade *api.AuthzArbitrageTxSet) AuthzTradeStatus {
	ts := AuthzTradeStatus{
		UserArbitrage: UserArbitrageEarnings{},
	}

	ts.ChainHeight = userTrade.LastChainHeight
	ts.UserSwaps = getUserSwaps(userTrade.TradeTxs)
	ts.TxsCommitted = userTrade.Committed

	if !userTrade.UserProfitShareTx.ArbitrageProfitsPending.IsZero() || !userTrade.UserProfitShareTx.ArbitrageProfitsReceived.IsZero() {
		ts.UserArbitrage.HasArbitrage = true
	}

	if !userTrade.UserProfitShareTx.ArbitrageProfitsPending.IsZero() && userTrade.UserProfitShareTx.ArbitrageProfitsReceived.IsZero() {
		ts.UserArbitrage.AmountInProgress = userTrade.UserProfitShareTx.ArbitrageProfitsPending
	} else if !userTrade.UserProfitShareTx.ArbitrageProfitsReceived.IsZero() {
		ts.UserArbitrage.AmountReceived = userTrade.UserProfitShareTx.ArbitrageProfitsReceived
	}

	if userTrade.UserProfitShareTx.Initiated && userTrade.UserProfitShareTx.Committed && !userTrade.UserProfitShareTx.Succeeded {
		ts.UserArbitrage.AmountReceived = sdk.Coins{}
		ts.UserArbitrage.Error = "Problem sending user arbitrage (will not reattempt, please report address and time of trade)"
	}

	return ts
}

func getUserSwaps(tradeTxs []api.SubmittedTx) []api.Swap {
	swaps := []api.Swap{}
	for _, t := range tradeTxs {
		for _, swap := range t.Swaps {
			if swap.IsUserSwap {
				swaps = append(swaps, swap)
			}
		}
	}

	return swaps
}
