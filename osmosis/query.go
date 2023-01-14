package osmosis

import (
	"context"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bankTypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func GetAccountBalances(queryClient client.Context, address string) (map[string]sdk.Int, error) {
	var balances = map[string]sdk.Int{} //k = denom, v = amount
	req := &bankTypes.QueryAllBalancesRequest{Address: address}
	querier := bankTypes.NewQueryClient(queryClient)
	b, err := querier.AllBalances(context.Background(), req)
	if err != nil {
		return balances, err
	}

	for _, balance := range b.Balances {
		balances[balance.Denom] = balance.Amount
	}

	return balances, nil
}
