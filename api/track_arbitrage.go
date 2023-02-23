package api

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/osmosis"
	"github.com/DefiantLabs/RedpointSwap/simulator"
	"github.com/DefiantLabs/RedpointSwap/zenith"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	tmtypes "github.com/tendermint/tendermint/types"
	"go.uber.org/zap"
)

// Tracks arbitrage TX sets from request all the way through submission on-chain
var txqueue sync.Map

type UserProfitShareTx struct {
	TxHash                   string
	Initiated                bool      //We sent the user profit share TX to the node (e.g. we're waiting for inclusion in a block)
	Committed                bool      //If the TX was committed in a block on chain (e.g. finished)
	Succeeded                bool      //If the TX succeeded or failed
	ArbitrageProfitsPending  sdk.Coins //We submitted a TX, waiting for block inclusion...
	ArbitrageProfitsReceived sdk.Coins //Amount of arbitrage we sent to the user
}

type SubmittedTx struct {
	TxHash    string
	Committed bool //If the TX was committed in a block on chain (e.g. finished)
	Succeeded bool //If the TX succeeded or failed
	Swaps     []Swap
}

type Swap struct {
	TxHash          string
	Succeeded       bool
	IsArbitrageSwap bool //token in denom == token out denom
	IsUserSwap      bool //if this trade was performed using the user's funds
	IsHotWalletSwap bool //if this trade was performed using the hot wallet's funds
	TokenIn         sdk.Coin
	TokenOut        sdk.Coin
}

func GetQueuedAuthzTxSet(id string) (*AuthzArbitrageTxSet, error) {
	val, ok := txqueue.Load(id)
	if ok {
		ats, ok := val.(*AuthzArbitrageTxSet)
		if ok {
			return ats, nil
		}
	}

	return nil, fmt.Errorf("no TXs found for ID %s", id)
}

func GetQueuedZenithTxSet(id string) (*ZenithArbitrageTxSet, error) {
	val, ok := txqueue.Load(id)
	if ok {
		ats, ok := val.(*ZenithArbitrageTxSet)
		if ok {
			return ats, nil
		}
	}

	return nil, fmt.Errorf("no TXs found for ID %s", id)
}

func QueueZenithRequest(zenithBid zenith.UserZenithRequest) (requestId string) {
	requestId = randSeq(10)
	zenithTx := &ZenithArbitrageTxSet{
		UserBidRequest: &zenithBid,
		SubmittedTxSet: SubmittedTxSet{
			Simulation: &zenithBid.SimulatedSwap,
		},
	}

	txqueue.Store(requestId, zenithTx)
	return requestId
}

func ExecuteQueuedZenith(lastChainHeight int64, _ int64) {
	nextBlock := lastChainHeight + 1
	pendingZBlocks := zenith.GetZenithBlocks()

	conf := config.Conf
	txClientSubmit, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSubmitTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
		return
	}

	for _, zBlock := range pendingZBlocks {
		if zBlock.Height == nextBlock && zBlock.IsZenithBlock {
			rmList := []string{}

			txqueue.Range(func(key any, val any) bool {
				ok := false
				zenithTxSet, ok := val.(*ZenithArbitrageTxSet)
				if ok {
					// Submit the TXs to a Zenith auction if:
					// 1) They have not been submitted to an auction before, OR
					// 2) They have been submitted before but didn't win the auction
					zenithTxSet.LastChainHeight = lastChainHeight
					awaitingZenithBlock := zenithTxSet.IsAwaitingZenithBlock()

					if !awaitingZenithBlock {
						return true
					}

					zenithBid := zenithTxSet.UserBidRequest
					reqExpiration, _ := time.Parse(time.RFC3339, zenithBid.Expiration)
					if reqExpiration.Before(zBlock.ProjectedBlocktime) {
						fmt.Printf("Zenith request %+v expired, projected blocktime for the next Zenith block is %s\n", zenithBid, zBlock.ProjectedBlocktime)
						return true
					}

					b64ZenithTxs, txs, err := zenith.GetZenithBid(zBlock, *zenithBid, txClientSubmit)
					if err == nil {
						bidReq := &zenith.ZenithBidRequest{
							ChainID: zBlock.Auction.ChainID,
							Height:  zBlock.Height,
							Txs:     b64ZenithTxs,
						}

						err = zenith.PlaceBid(bidReq)
						zenithTxSet.ErrorPlacingBid = err != nil

						if !zenithTxSet.ErrorPlacingBid {
							zenithTxSet.SubmittedAuctionBid = bidReq
							err = UpdateZenithTxSet(zenithTxSet, txs, txClientSubmit.TxConfig.TxDecoder(), zenithBid.SimulatedSwap.UserAddress, config.HotWalletAddress)

							if err != nil {
								fmt.Println("Zenith: Tracking info may be unavailable for TX set due to unexpected error " + err.Error())
							} else {
								rmList = append(rmList, key.(string)) //allow it to be removed from the queue later
							}
						}
					} else {
						fmt.Printf("Issue in GetZenithBid(), failed to bid: %s\n", err.Error())
					}
				}

				return false
			})

			// for _, k := range rmList {
			// 	txqueue.Delete(k)
			// }
		}
	}
}

// Tracks TXs that were already submitted on chain.
// Track the TX set using the hash from the first TX in the set as the key
func AddAuthzTxSet(txs [][]byte, simulation *simulator.SimulatedSwapResult, txDecoder sdk.TxDecoder, userAddress string, hotWalletAddress string) (requestId string, err error) {

	if len(txs) == 0 {
		err = errors.New("no TXs in AddTxSet()")
		return
	}

	requestId = randSeq(10)

	txSet := []SubmittedTx{}
	for _, txBytes := range txs {
		hash := fmt.Sprintf("%X", tmtypes.Tx(txBytes).Hash())
		stx := SubmittedTx{
			TxHash: hash,
		}

		_, err = txDecoder(txBytes)
		if err != nil {
			fmt.Printf("Cannot decode TX with hash %s. Err %s\n", hash, err.Error())
			errStr := fmt.Sprintf("Cannot decode TX with hash %s", hash)
			err = errors.New(errStr)
			return
		}

		txSet = append(txSet, stx)
	}

	set := &AuthzArbitrageTxSet{
		SubmittedTxSet: SubmittedTxSet{
			UserProfitShareTx:     UserProfitShareTx{},
			Simulation:            simulation,
			TradeTxs:              txSet,
			UserAddress:           userAddress,
			HotWalletAddress:      hotWalletAddress,
			UserTxFees:            sdk.Coins{},
			HotWalletTxFees:       sdk.Coins{},
			TotalArbitrageRevenue: sdk.Coins{},
		},
	}
	txqueue.Store(requestId, set)
	return
}

// Tracks TXs that were already submitted on chain.
// Track the TX set using the hash from the first TX in the set as the key
func UpdateZenithTxSet(zenithTxSet *ZenithArbitrageTxSet, txs [][]byte, txDecoder sdk.TxDecoder, userAddress string, hotWalletAddress string) error {
	txSet := []SubmittedTx{}
	for _, txBytes := range txs {
		hash := fmt.Sprintf("%X", tmtypes.Tx(txBytes).Hash())
		stx := SubmittedTx{
			TxHash: hash,
		}

		_, err := txDecoder(txBytes)
		if err != nil {
			uE := fmt.Errorf("cannot decode TX with hash %s", hash)
			fmt.Printf("%s. Verbose error: %s\n", uE.Error(), err.Error())
			return uE
		}

		txSet = append(txSet, stx)
	}

	zenithTxSet.TradeTxs = txSet
	zenithTxSet.UserProfitShareTx = UserProfitShareTx{}
	zenithTxSet.UserAddress = userAddress
	zenithTxSet.HotWalletAddress = hotWalletAddress
	zenithTxSet.UserTxFees = sdk.Coins{}
	zenithTxSet.HotWalletTxFees = sdk.Coins{}
	zenithTxSet.TotalArbitrageRevenue = sdk.Coins{}
	return nil
}

func toSubmittedTx(parsedTx osmosis.OsmosisTx, userAddr string, hotWalletAddr string) SubmittedTx {
	sTx := SubmittedTx{
		TxHash:    parsedTx.Hash,
		Committed: true,
		Succeeded: parsedTx.IsSuccessfulTx,
		Swaps:     []Swap{},
	}

	//Zenith fees may or may not be present, and are sent by the hot wallet with a MsgSend to a given address
	if parsedTx.IsSuccessfulTx {
		for _, swap := range parsedTx.Swaps {
			newSwap := Swap{
				TxHash:          parsedTx.Hash,
				IsArbitrageSwap: swap.TokenIn.Denom == swap.TokenOut.Denom,
				IsUserSwap:      swap.Address == userAddr,
				IsHotWalletSwap: swap.Address == hotWalletAddr,
				TokenIn:         swap.TokenIn,
				TokenOut:        swap.TokenOut,
			}
			sTx.Swaps = append(sTx.Swaps, newSwap)
		}
	}

	return sTx
}

func getHashStr(txs []SubmittedTx) string {
	allHash := ""
	for i, tx := range txs {
		allHash = allHash + tx.TxHash
		if i != len(txs)-1 {
			allHash = allHash + ", "
		}
	}

	return allHash
}

func queryOsmosisTxs(txs []SubmittedTx, txClientSearch client.Context) []osmosis.OsmosisTx {
	osmosisTxs := []osmosis.OsmosisTx{}

	//Query the chain and parse the TXs into a common format
	for _, tx := range txs {
		//the block already got committed (which called this handler) so don't wait long
		resp, err := osmosis.AwaitTx(txClientSearch, tx.TxHash, 500*time.Millisecond)
		if err != nil {
			fmt.Printf("Error %s looking up TX with hash %s\n", err.Error(), tx.TxHash)
		} else {
			parsedTx := osmosis.ParseRedpointSwaps(resp, tx.TxHash)
			osmosisTxs = append(osmosisTxs, parsedTx)
		}
	}

	return osmosisTxs
}

func getArbTxHash(osmosisTxs []SubmittedTx) string {
	arbTxHash := ""
	for _, parsedTx := range osmosisTxs {
		for _, swap := range parsedTx.Swaps {
			//Calculate the arbitrage profits
			if swap.IsArbitrageSwap && swap.IsHotWalletSwap {
				arbTxHash = parsedTx.TxHash
			}

		}
	}

	return arbTxHash
}

// This function is called for every new block produced on the chain.
// We check if there are TXs in our submittedtxs Map that completed on chain.
// If so, we will log the expected vs. actual profits our Hot Wallet made.
func AuthzBlockNotificationHandler(chainHeight int64, _ int64) {
	conf := config.Conf
	txClientSearch, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSearchTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
		return
	}

	txqueue.Range(func(_, val any) bool {
		authzTxSet, ok := val.(*AuthzArbitrageTxSet)
		if ok && !authzTxSet.Committed {
			authzTxSet.LastChainHeight = chainHeight
			osmosisTxs := queryOsmosisTxs(authzTxSet.TradeTxs, txClientSearch)
			if len(osmosisTxs) == len(authzTxSet.TradeTxs) {
				authzTxSet.Committed = true
			} else {
				fmt.Printf("Waiting for TXs to finish: %s\n", getHashStr(authzTxSet.TradeTxs))
				return true
			}
			authzTxSet.TradeTxs = []SubmittedTx{}

			//Handle TX fees and fees paid to Zenith (if applicable), record any swaps that happened
			for _, parsedTx := range osmosisTxs {
				submittedTx := toSubmittedTx(parsedTx, authzTxSet.UserAddress, authzTxSet.HotWalletAddress)

				//TX fees are taken whether or not the TX succeeded
				if parsedTx.FeePayer == authzTxSet.UserAddress {
					authzTxSet.UserTxFees = authzTxSet.UserTxFees.Add(parsedTx.Fees...)
				} else if parsedTx.FeePayer == authzTxSet.HotWalletAddress {
					authzTxSet.HotWalletTxFees = authzTxSet.HotWalletTxFees.Add(parsedTx.Fees...)
				}

				for _, swap := range submittedTx.Swaps {
					swap.Succeeded = parsedTx.IsSuccessfulTx
				}

				//Zenith fees may or may not be present, and are sent by the hot wallet with a MsgSend to a given address
				if parsedTx.IsSuccessfulTx {
					//Why is there a MsgSend in an authz TX???
					for _, send := range parsedTx.Sends {
						fmt.Printf("Unrecognized MsgSend (sender:%s,receiver:%s,amount:%s) in TX %s\n", send.Sender, send.Receiver, send.Token, parsedTx.Hash)
					}

					for _, swap := range submittedTx.Swaps {
						//Calculate the arbitrage profits
						if swap.IsArbitrageSwap && swap.IsHotWalletSwap {
							profit := swap.TokenOut.Sub(swap.TokenIn)
							authzTxSet.TotalArbitrageRevenue.Add(profit)
						}
					}
				}

				authzTxSet.TradeTxs = append(authzTxSet.TradeTxs, submittedTx)
			}
		} else if ok && authzTxSet.Committed && !authzTxSet.UserProfitShareTx.Initiated {
			authzTxSet.UserProfitShareTx.Initiated = true
			allHash := getHashStr(authzTxSet.TradeTxs)
			arbTxHash := getArbTxHash(authzTxSet.TradeTxs)

			hotWalletProfit, isNegative := authzTxSet.TotalArbitrageRevenue.SafeSub(authzTxSet.HotWalletTxFees)
			authzTxSet.HotWalletArbitrageProfitActual = hotWalletProfit

			//Print summary of TXs
			fmt.Printf("Begin summary of TXs submitted by Redpoint backend. TX hashes: %s\n", allHash)
			if !authzTxSet.TotalArbitrageRevenue.IsZero() && !isNegative {
				fmt.Printf("Arbitrage revenue (actual): %s for TX '%s'\n", authzTxSet.TotalArbitrageRevenue, arbTxHash)
				if authzTxSet.Simulation.HasArbitrageOpportunity {
					fmt.Printf("Arbitrage revenue (estimated): %s for TX '%s'\n",
						authzTxSet.Simulation.ArbitrageSwap.EstimatedProfitHumanReadable, arbTxHash)
				}
			} else {
				fmt.Printf("TX set had no arbitrage, TX hash: %s\n", authzTxSet.TradeTxs[0].TxHash)
				return true
			}

			if !authzTxSet.HotWalletArbitrageProfitActual.IsZero() {
				fmt.Printf("Hot wallet arbitrage profit (arbitrage-fees): %s (TX: %s)\n", authzTxSet.HotWalletArbitrageProfitActual, arbTxHash)
			}

			fmt.Printf("End summary of TXs submitted by Redpoint backend. TX hashes: %s\n", allHash)

			//Send the user their share
			conf := config.Conf
			userProfitShare := 0.85
			if conf.Api.UserProfitSharePercentage <= .85 {
				userProfitShare = conf.Api.UserProfitSharePercentage
			}
			userProfitShareStr := strconv.FormatFloat(userProfitShare, 'f', 6, 64)
			userProfitShareDec, err := sdk.NewDecFromStr(userProfitShareStr)
			if err != nil {
				fmt.Printf("server misconfiguration (user arbitrage profit share), cannot send any arbitrage profits to user")
			}

			//Amount of arbitrage revenue that will be sent to the user
			msgSends := []sdk.Msg{}
			for _, coin := range authzTxSet.HotWalletArbitrageProfitActual {
				userShare := coin.Amount.ToDec().Mul(userProfitShareDec)
				tokenUserShare := sdk.NewCoin(coin.Denom, userShare.TruncateInt())
				if tokenUserShare.IsLT(coin) {
					msgSendArbToUser := &bank.MsgSend{
						FromAddress: authzTxSet.HotWalletAddress,
						ToAddress:   authzTxSet.UserAddress,
						Amount:      sdk.Coins{tokenUserShare},
					}
					authzTxSet.UserProfitShareTx.ArbitrageProfitsPending.Add(tokenUserShare)
					msgSends = append(msgSends, msgSendArbToUser)
					fmt.Printf("Creating TX to send arb to user. Total arb: %s, user share: %s, user: %s\n", coin.String(), tokenUserShare.String(), authzTxSet.UserAddress)
				} else {
					fmt.Printf("user share cannot be greater than total arb revenue, cannot send any arbitrage profits to user")
				}
			}

			if len(msgSends) > 0 {
				txClientSubmit, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSubmitTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
				if err != nil {
					config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
					return true
				}

				resp, err := osmosis.SignSubmitTx(txClientSubmit, msgSends, 0)
				if err != nil {
					config.Logger.Error("Error sending user TX profit share", zap.Error(err))
					return true
				}

				authzTxSet.UserProfitShareTx.TxHash = resp.TxHash
				config.Logger.Error("Send user profit share", zap.Uint32("TX code", resp.Code), zap.String("tx hash", resp.TxHash))
			}
		} else if ok && authzTxSet.Committed && authzTxSet.UserProfitShareTx.Initiated && !authzTxSet.UserProfitShareTx.Committed {
			//See if the user received their share
			resp, err := osmosis.AwaitTx(txClientSearch, authzTxSet.UserProfitShareTx.TxHash, 500*time.Millisecond)
			coinsReceived := sdk.Coins{}
			if err != nil {
				fmt.Printf("Error %s looking up TX with hash %s\n", err.Error(), authzTxSet.UserProfitShareTx.TxHash)
			} else {
				authzTxSet.UserProfitShareTx.Committed = true
				authzTxSet.UserProfitShareTx.Succeeded = resp.TxResponse.Code == 0
				if authzTxSet.UserProfitShareTx.Succeeded {
					parsedTx := osmosis.ParseRedpointSwaps(resp, authzTxSet.UserProfitShareTx.TxHash)
					for _, msg := range parsedTx.Sends {
						if msg.Receiver == authzTxSet.UserAddress {
							coinsReceived = coinsReceived.Add(msg.Token)
						}
					}

					fmt.Printf("User %s received following tokens as profit sharing: %s. TX: %s\n", authzTxSet.UserAddress, coinsReceived.String(), authzTxSet.UserProfitShareTx.TxHash)
					authzTxSet.UserProfitShareTx.ArbitrageProfitsReceived = coinsReceived
				}
			}
		} else if ok {
			fmt.Printf("Unknown authz TX in submittedtxs map\n")
		}
		return true
	})
}

// This function is called for every new block produced on the chain.
// We check if there are TXs in our submittedtxs Map that completed on chain.
// If so, we will log the expected vs. actual profits our Hot Wallet made.
func ParseZenithCommittedTxs(chainHeight int64, _ int64) {
	conf := config.Conf
	txClientSearch, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSearchTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
		return
	}

	txqueue.Range(func(_, val any) bool {
		zenithTxSet, ok := val.(*ZenithArbitrageTxSet)

		if ok {
			zenithTxSet.LastChainHeight = chainHeight

			if zenithTxSet.Committed && (zenithTxSet.SubmittedAuctionBid != nil &&
				(zenithTxSet.LastChainHeight > zenithTxSet.SubmittedAuctionBid.Height)) {

				if len(zenithTxSet.TradeTxs) == 0 {
					zenithTxSet.Committed = false
				} else {
					osmosisTxs := queryOsmosisTxs(zenithTxSet.TradeTxs, txClientSearch)
					if len(osmosisTxs) == 0 {
						zenithTxSet.Committed = false
					}
				}
			}
		}

		if ok && !zenithTxSet.Committed && !zenithTxSet.IsAwaitingZenithBlock() {
			osmosisTxs := queryOsmosisTxs(zenithTxSet.TradeTxs, txClientSearch)
			if len(zenithTxSet.TradeTxs) != 0 && len(osmosisTxs) > 0 {
				zenithTxSet.Committed = true
			} else {
				fmt.Printf("Waiting for TXs to finish: %s\n", getHashStr(zenithTxSet.TradeTxs))
				return true
			}
			zenithTxSet.TradeTxs = []SubmittedTx{}

			//Handle TX fees and fees paid to Zenith (if applicable), record any swaps that happened
			for _, parsedTx := range osmosisTxs {
				submittedTx := toSubmittedTx(parsedTx, zenithTxSet.UserAddress, zenithTxSet.HotWalletAddress)

				//TX fees are taken whether or not the TX succeeded
				if parsedTx.FeePayer == zenithTxSet.UserAddress {
					zenithTxSet.UserTxFees = zenithTxSet.UserTxFees.Add(parsedTx.Fees...)
				} else if parsedTx.FeePayer == zenithTxSet.HotWalletAddress {
					zenithTxSet.HotWalletTxFees = zenithTxSet.HotWalletTxFees.Add(parsedTx.Fees...)
				}

				for _, swap := range submittedTx.Swaps {
					swap.Succeeded = parsedTx.IsSuccessfulTx
				}

				//Zenith fees may or may not be present, and are sent by the hot wallet with a MsgSend to a given address
				if parsedTx.IsSuccessfulTx {
					for _, send := range parsedTx.Sends {
						if send.Sender == zenithTxSet.HotWalletAddress && send.Receiver != zenithTxSet.UserAddress {
							zenithTxSet.HotWalletZenithFees = zenithTxSet.HotWalletZenithFees.Add(send.Token)
						} else {
							fmt.Printf("Unrecognized MsgSend (sender:%s,receiver:%s,amount:%s) in TX %s\n", send.Sender, send.Receiver, send.Token, parsedTx.Hash)
						}
					}

					for _, swap := range submittedTx.Swaps {
						//Calculate the arbitrage profits
						if swap.IsArbitrageSwap && swap.IsHotWalletSwap {
							profit := swap.TokenOut.Sub(swap.TokenIn)
							zenithTxSet.TotalArbitrageRevenue.Add(profit)
						}
					}
				}

				zenithTxSet.TradeTxs = append(zenithTxSet.TradeTxs, submittedTx)
			}
		} else if ok && zenithTxSet.Committed && !zenithTxSet.UserProfitShareTx.Initiated {
			zenithTxSet.UserProfitShareTx.Initiated = true
			allHash := getHashStr(zenithTxSet.TradeTxs)
			arbTxHash := getArbTxHash(zenithTxSet.TradeTxs)

			hotWalletProfit, _ := zenithTxSet.TotalArbitrageRevenue.SafeSub(zenithTxSet.HotWalletTxFees)
			hotWalletProfit, isNegative := hotWalletProfit.SafeSub(zenithTxSet.HotWalletZenithFees)
			// hotWalletProfit, _ = hotWalletProfit.SafeSub(arbTxSet.UserProfitShareTx.UserArbitrageProfitsSent)
			zenithTxSet.HotWalletArbitrageProfitActual = hotWalletProfit

			//Print summary of TXs
			fmt.Printf("Begin summary of TXs submitted by Redpoint backend. TX hashes: %s\n", allHash)
			if !zenithTxSet.TotalArbitrageRevenue.IsZero() && !isNegative {
				fmt.Printf("Arbitrage revenue (actual): %s for TX '%s'\n", zenithTxSet.TotalArbitrageRevenue, arbTxHash)
				if zenithTxSet.Simulation.HasArbitrageOpportunity {
					fmt.Printf("Arbitrage revenue (estimated): %s for TX '%s'\n",
						zenithTxSet.Simulation.ArbitrageSwap.EstimatedProfitHumanReadable, arbTxHash)
				}
			} else {
				fmt.Printf("TX set had no arbitrage, TX hash: %s\n", zenithTxSet.TradeTxs[0].TxHash)
				return true
			}

			if !zenithTxSet.HotWalletArbitrageProfitActual.IsZero() {
				fmt.Printf("Hot wallet arbitrage profit (arbitrage-fees): %s (TX: %s)\n", zenithTxSet.HotWalletArbitrageProfitActual, arbTxHash)
			}

			fmt.Printf("End summary of TXs submitted by Redpoint backend. TX hashes: %s\n", allHash)

			//Send the user their share
			conf := config.Conf
			userProfitShare := 0.85
			if conf.Api.UserProfitSharePercentage <= .85 {
				userProfitShare = conf.Api.UserProfitSharePercentage
			}
			userProfitShareStr := strconv.FormatFloat(userProfitShare, 'f', 6, 64)
			userProfitShareDec, err := sdk.NewDecFromStr(userProfitShareStr)
			if err != nil {
				fmt.Printf("server misconfiguration (user arbitrage profit share), cannot send any arbitrage profits to user")
			}

			//Amount of arbitrage revenue that will be sent to the user
			msgSends := []sdk.Msg{}
			for _, coin := range zenithTxSet.HotWalletArbitrageProfitActual {
				userShare := coin.Amount.ToDec().Mul(userProfitShareDec)
				tokenUserShare := sdk.NewCoin(coin.Denom, userShare.TruncateInt())
				if tokenUserShare.IsLT(coin) {
					msgSendArbToUser := &bank.MsgSend{
						FromAddress: zenithTxSet.HotWalletAddress,
						ToAddress:   zenithTxSet.UserAddress,
						Amount:      sdk.Coins{tokenUserShare},
					}
					zenithTxSet.UserProfitShareTx.ArbitrageProfitsPending.Add(tokenUserShare)
					msgSends = append(msgSends, msgSendArbToUser)
					fmt.Printf("Creating TX to send arb to user. Total arb: %s, user share: %s, user: %s\n", coin.String(), tokenUserShare.String(), zenithTxSet.UserAddress)
				} else {
					fmt.Printf("user share cannot be greater than total arb revenue, cannot send any arbitrage profits to user")
				}
			}

			if len(msgSends) > 0 {
				txClientSubmit, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSubmitTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
				if err != nil {
					config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
					return true
				}

				resp, err := osmosis.SignSubmitTx(txClientSubmit, msgSends, 0)
				if err != nil {
					config.Logger.Error("Error sending user TX profit share", zap.Error(err))
					return true
				}

				zenithTxSet.UserProfitShareTx.TxHash = resp.TxHash
				config.Logger.Error("Send user profit share", zap.Uint32("TX code", resp.Code), zap.String("tx hash", resp.TxHash))
			}
		} else if ok && zenithTxSet.Committed && zenithTxSet.UserProfitShareTx.Initiated && !zenithTxSet.HotWalletArbitrageProfitActual.IsAnyNegative() && !zenithTxSet.UserProfitShareTx.Committed {
			//See if the user received their share
			resp, err := osmosis.AwaitTx(txClientSearch, zenithTxSet.UserProfitShareTx.TxHash, 500*time.Millisecond)
			coinsReceived := sdk.Coins{}
			if err != nil {
				fmt.Printf("Error %s looking up TX with hash %s\n", err.Error(), zenithTxSet.UserProfitShareTx.TxHash)
			} else {
				zenithTxSet.UserProfitShareTx.Committed = true
				zenithTxSet.UserProfitShareTx.Succeeded = resp.TxResponse.Code == 0
				if zenithTxSet.UserProfitShareTx.Succeeded {
					parsedTx := osmosis.ParseRedpointSwaps(resp, zenithTxSet.UserProfitShareTx.TxHash)
					for _, msg := range parsedTx.Sends {
						if msg.Receiver == zenithTxSet.UserAddress {
							coinsReceived = coinsReceived.Add(msg.Token)
						}
					}

					fmt.Printf("User %s received following tokens as profit sharing: %s. TX: %s\n", zenithTxSet.UserAddress, coinsReceived.String(), zenithTxSet.UserProfitShareTx.TxHash)
					zenithTxSet.UserProfitShareTx.ArbitrageProfitsReceived = coinsReceived
				}
			}
		} else {
			fmt.Printf("Unknown val in submittedtxs map\n")
		}
		return true
	})
}
