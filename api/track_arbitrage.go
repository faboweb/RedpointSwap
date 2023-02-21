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

// Tracks arbitrage TX sets that have been submitted to the chain
var submittedtxs sync.Map

// Tracks Zenith requests that are queued waiting for a Zenith block. Once submitted, will be moved to the submittedtxs queue
var zenithQueue sync.Map

type ArbitrageTxSet struct {
	Processed                      bool              //Once the 'TXs' are finished on-chain and we query for initial stats
	TradeTxs                       []SubmittedTx     //includes user swap, arb swap, zenith payments
	UserProfitShareTx              UserProfitShareTx //the TX that sends the user their portion of the arb earnings
	Protocol                       string            //Either 'Zenith' or 'Authz' (at present)
	UserAddress                    string
	HotWalletAddress               string
	Simulation                     *simulator.SimulatedSwapResult
	HotWalletZenithFees            sdk.Coins
	HotWalletTxFees                sdk.Coins //Total fees that the hot wallet paid for this TX set (Zenith fees and TX fees)
	UserTxFees                     sdk.Coins //Total TX fees that the user paid for this TX set
	TotalArbitrageRevenue          sdk.Coins //Total arbitrage revenue (does not include fees)
	TotalArbitrageProfits          sdk.Coins //arbitrage revenue-fees paid by the hot wallet
	HotWalletArbitrageProfitActual sdk.Coins //Arbitrage revenue-fees-amount we sent to the user
}

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

func IsZenithQueued(id string) bool {
	_, ok := zenithQueue.Load(id)
	return ok
}

func GetStatusForSubmittedTxs(id string) (*ArbitrageTxSet, error) {
	val, ok := submittedtxs.Load(id)
	if ok {
		ats, ok := val.(*ArbitrageTxSet)
		if ok {
			return ats, nil
		}
	}

	return nil, fmt.Errorf("no TXs found for ID %s", id)
}

func QueueZenithRequest(zenithBid zenith.QueuedBidRequest) {
	zenithQueue.Store(zenithBid.SimulatedSwap.UserAddress, zenithBid)
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
			var addrKey string
			var zenithBid *zenith.QueuedBidRequest

			zenithQueue.Range(func(key any, val any) bool {
				ok := false
				zenithBid, ok = val.(*zenith.QueuedBidRequest)
				if ok {
					b64ZenithTxs, txs, err := zenith.GetZenithBid(zBlock, *zenithBid, txClientSubmit)
					if err == nil {
						bidReq := &zenith.ZenithBidRequest{
							ChainID: zBlock.Auction.ChainID,
							Height:  zBlock.Height,
							Txs:     b64ZenithTxs,
						}

						err = zenith.PlaceBid(bidReq)
						if err == nil {
							_, err := AddTxSet(txs, &zenithBid.SimulatedSwap, txClientSubmit.TxConfig.TxDecoder(),
								"Zenith", zenithBid.SimulatedSwap.UserAddress, config.HotWalletAddress)
							if err != nil {
								fmt.Println("Zenith: Tracking info may be unavailable for TX set due to unexpected error " + err.Error())
							} else {
								addrKey = key.(string) //allow it to be removed from the queue later
							}
						}
					} else {
						fmt.Printf("Issue in GetZenithBid(), failed to bid: %s\n", err.Error())
					}
				}

				return false
			})

			if addrKey != "" {
				zenithQueue.Delete(addrKey)
			}
		}
	}
}

// Tracks TXs that were already submitted on chain.
// Track the TX set using the hash from the first TX in the set as the key
func AddTxSet(txs [][]byte, simulation *simulator.SimulatedSwapResult, txDecoder sdk.TxDecoder, protocol string, userAddress string, hotWalletAddress string) (id string, err error) {

	if len(txs) == 0 {
		err = errors.New("no TXs in AddTxSet()")
		return
	} else if protocol != "Zenith" && protocol != "Authz" {
		err = errors.New("invalid protocol")
		return
	}

	id = string(tmtypes.Tx(txs[0]).Hash())

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

	set := &ArbitrageTxSet{
		UserProfitShareTx:     UserProfitShareTx{},
		Simulation:            simulation,
		TradeTxs:              txSet,
		Protocol:              protocol,
		UserAddress:           userAddress,
		HotWalletAddress:      hotWalletAddress,
		UserTxFees:            sdk.Coins{},
		HotWalletTxFees:       sdk.Coins{},
		HotWalletZenithFees:   sdk.Coins{},
		TotalArbitrageRevenue: sdk.Coins{},
	}
	submittedtxs.Store(id, set)
	return
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
func ArbitrageBlockNotificationHandler(_ int64, _ int64) {
	conf := config.Conf
	txClientSearch, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSearchTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
		return
	}

	submittedtxs.Range(func(_, val any) bool {
		arbTxSet, ok := val.(*ArbitrageTxSet)
		if ok && !arbTxSet.Processed {
			osmosisTxs := queryOsmosisTxs(arbTxSet.TradeTxs, txClientSearch)
			if len(osmosisTxs) == len(arbTxSet.TradeTxs) {
				arbTxSet.Processed = true
			} else {
				fmt.Printf("Waiting for TXs to finish: %s\n", getHashStr(arbTxSet.TradeTxs))
				return true
			}
			arbTxSet.TradeTxs = []SubmittedTx{}

			//Handle TX fees and fees paid to Zenith (if applicable), record any swaps that happened
			for _, parsedTx := range osmosisTxs {
				submittedTx := toSubmittedTx(parsedTx, arbTxSet.UserAddress, arbTxSet.HotWalletAddress)

				//TX fees are taken whether or not the TX succeeded
				if parsedTx.FeePayer == arbTxSet.UserAddress {
					arbTxSet.UserTxFees = arbTxSet.UserTxFees.Add(parsedTx.Fees...)
				} else if parsedTx.FeePayer == arbTxSet.HotWalletAddress {
					arbTxSet.HotWalletTxFees = arbTxSet.HotWalletTxFees.Add(parsedTx.Fees...)
				}

				for _, swap := range submittedTx.Swaps {
					swap.Succeeded = parsedTx.IsSuccessfulTx
				}

				//Zenith fees may or may not be present, and are sent by the hot wallet with a MsgSend to a given address
				if parsedTx.IsSuccessfulTx {
					for _, send := range parsedTx.Sends {
						if send.Sender == arbTxSet.HotWalletAddress && send.Receiver != arbTxSet.UserAddress {
							arbTxSet.HotWalletZenithFees = arbTxSet.HotWalletZenithFees.Add(send.Token)
						} else {
							fmt.Printf("Unrecognized MsgSend (sender:%s,receiver:%s,amount:%s) in TX %s\n", send.Sender, send.Receiver, send.Token, parsedTx.Hash)
						}
					}

					for _, swap := range submittedTx.Swaps {
						//Calculate the arbitrage profits
						if swap.IsArbitrageSwap && swap.IsHotWalletSwap {
							profit := swap.TokenOut.Sub(swap.TokenIn)
							arbTxSet.TotalArbitrageRevenue.Add(profit)
						}
					}
				}

				arbTxSet.TradeTxs = append(arbTxSet.TradeTxs, submittedTx)
			}
		} else if ok && arbTxSet.Processed && !arbTxSet.UserProfitShareTx.Initiated {
			arbTxSet.UserProfitShareTx.Initiated = true
			allHash := getHashStr(arbTxSet.TradeTxs)
			arbTxHash := getArbTxHash(arbTxSet.TradeTxs)

			hotWalletProfit, _ := arbTxSet.TotalArbitrageRevenue.SafeSub(arbTxSet.HotWalletTxFees)
			hotWalletProfit, isNegative := hotWalletProfit.SafeSub(arbTxSet.HotWalletZenithFees)
			// hotWalletProfit, _ = hotWalletProfit.SafeSub(arbTxSet.UserProfitShareTx.UserArbitrageProfitsSent)
			arbTxSet.HotWalletArbitrageProfitActual = hotWalletProfit

			//Print summary of TXs
			fmt.Printf("Begin summary of TXs submitted by Redpoint backend. TX hashes: %s\n", allHash)
			if !arbTxSet.TotalArbitrageRevenue.IsZero() && !isNegative {
				fmt.Printf("Arbitrage revenue (actual): %s for TX '%s'\n", arbTxSet.TotalArbitrageRevenue, arbTxHash)
				if arbTxSet.Simulation.HasArbitrageOpportunity {
					fmt.Printf("Arbitrage revenue (estimated): %s for TX '%s'\n",
						arbTxSet.Simulation.ArbitrageSwap.EstimatedProfitHumanReadable, arbTxHash)
				}
			} else {
				fmt.Printf("TX set had no arbitrage, TX hash: %s\n", arbTxSet.TradeTxs[0].TxHash)
				return true
			}

			if !arbTxSet.HotWalletArbitrageProfitActual.IsZero() {
				fmt.Printf("Hot wallet arbitrage profit (arbitrage-fees): %s (TX: %s)\n", arbTxSet.HotWalletArbitrageProfitActual, arbTxHash)
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
			for _, coin := range arbTxSet.HotWalletArbitrageProfitActual {
				userShare := coin.Amount.ToDec().Mul(userProfitShareDec)
				tokenUserShare := sdk.NewCoin(coin.Denom, userShare.TruncateInt())
				if tokenUserShare.IsLT(coin) {
					msgSendArbToUser := &bank.MsgSend{
						FromAddress: arbTxSet.HotWalletAddress,
						ToAddress:   arbTxSet.UserAddress,
						Amount:      sdk.Coins{tokenUserShare},
					}
					arbTxSet.UserProfitShareTx.ArbitrageProfitsPending.Add(tokenUserShare)
					msgSends = append(msgSends, msgSendArbToUser)
					fmt.Printf("Creating TX to send arb to user. Total arb: %s, user share: %s, user: %s\n", coin.String(), tokenUserShare.String(), arbTxSet.UserAddress)
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

				arbTxSet.UserProfitShareTx.TxHash = resp.TxHash
				config.Logger.Error("Send user profit share", zap.Uint32("TX code", resp.Code), zap.String("tx hash", resp.TxHash))
			}
		} else if ok && arbTxSet.Processed && arbTxSet.UserProfitShareTx.Initiated && !arbTxSet.UserProfitShareTx.Committed {
			//See if the user received their share
			resp, err := osmosis.AwaitTx(txClientSearch, arbTxSet.UserProfitShareTx.TxHash, 500*time.Millisecond)
			coinsReceived := sdk.Coins{}
			if err != nil {
				fmt.Printf("Error %s looking up TX with hash %s\n", err.Error(), arbTxSet.UserProfitShareTx.TxHash)
			} else {
				arbTxSet.UserProfitShareTx.Committed = true
				arbTxSet.UserProfitShareTx.Succeeded = resp.TxResponse.Code == 0
				if arbTxSet.UserProfitShareTx.Succeeded {
					parsedTx := osmosis.ParseRedpointSwaps(resp, arbTxSet.UserProfitShareTx.TxHash)
					for _, msg := range parsedTx.Sends {
						if msg.Receiver == arbTxSet.UserAddress {
							coinsReceived = coinsReceived.Add(msg.Token)
						}
					}

					fmt.Printf("User %s received following tokens as profit sharing: %s. TX: %s\n", arbTxSet.UserAddress, coinsReceived.String(), arbTxSet.UserProfitShareTx.TxHash)
					arbTxSet.UserProfitShareTx.ArbitrageProfitsReceived = coinsReceived
				}
			}
		} else {
			fmt.Printf("Unknown val in submittedtxs map\n")
		}
		return true
	})
}
