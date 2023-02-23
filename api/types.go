package api

import (
	"github.com/DefiantLabs/RedpointSwap/simulator"
	"github.com/DefiantLabs/RedpointSwap/zenith"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/golang-jwt/jwt/v4"
)

var Initialized bool

type JWTClaim struct {
	jwt.RegisteredClaims
}

var jwtKey []byte

func SetSecretKey(jwtSecret string) {
	jwtKey = []byte(jwtSecret)
}

func GetSecretKey() []byte {
	return jwtKey
}

func (zenithTxSet *ZenithArbitrageTxSet) IsAwaitingZenithBlock() bool {
	return zenithTxSet.SubmittedAuctionBid == nil ||
		(zenithTxSet.LastChainHeight > zenithTxSet.SubmittedAuctionBid.Height && !zenithTxSet.Committed)
}

func (zenithTxSet *ZenithArbitrageTxSet) SubmittedToAuction() bool {
	return zenithTxSet.SubmittedAuctionBid != nil
}

func (zenithTxSet *ZenithArbitrageTxSet) IncludedInBlock() bool {
	return zenithTxSet.SubmittedAuctionBid != nil && zenithTxSet.Committed
}

type AuthzArbitrageTxSet struct {
	SubmittedTxSet
}

type ZenithArbitrageTxSet struct {
	UserBidRequest      *zenith.UserZenithRequest //The user's request including expiration, user TX, etc
	SubmittedAuctionBid *zenith.ZenithBidRequest  //The last auction we bid on for this TX set
	ErrorPlacingBid     bool                      //true if there was an error attempt
	HotWalletZenithFees sdk.Coins
	SubmittedTxSet
}

type SubmittedTxSet struct {
	LastChainHeight                int64
	Committed                      bool //Once the 'TXs' are committed on-chain
	HotWalletAddress               string
	UserAddress                    string
	UserProfitShareTx              UserProfitShareTx //the TX that sends the user their portion of the arb earnings
	TradeTxs                       []SubmittedTx     //includes user swap, arb swap, zenith payments
	Simulation                     *simulator.SimulatedSwapResult
	HotWalletTxFees                sdk.Coins //Total fees that the hot wallet paid for this TX set (Zenith fees and TX fees)
	UserTxFees                     sdk.Coins //Total TX fees that the user paid for this TX set
	TotalArbitrageRevenue          sdk.Coins //Total arbitrage revenue (does not include fees)
	TotalArbitrageProfits          sdk.Coins //arbitrage revenue-fees paid by the hot wallet
	HotWalletArbitrageProfitActual sdk.Coins //Arbitrage revenue-fees-amount we sent to the user
}
