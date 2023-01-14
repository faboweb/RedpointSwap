package osmosis

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func HasTokens(tokenIn sdk.Coin, walletBalances map[string]sdk.Int) bool {
	balance, ok := walletBalances[tokenIn.Denom]
	if !ok {
		return false
	}

	return balance.GTE(tokenIn.Amount)
}

func GetTokenBalance(denom string, walletBalances map[string]sdk.Int) sdk.Int {
	balance, ok := walletBalances[denom]
	if !ok {
		return sdk.ZeroInt()
	}

	return balance
}

func IsValidCosmosAddress(address string) bool {
	_, err := sdk.AccAddressFromBech32(address)
	return err == nil
}
