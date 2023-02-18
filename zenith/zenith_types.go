package zenith

import "github.com/DefiantLabs/RedpointSwap/api"

// GET request for Mekatek's Zenith "api.mekatek.xyz/v0/auction" endpoint
type AuctionRequest struct {
	ChainID string `json:"chain_id"`
	Height  int64  `json:"height"`
}

// Response for Mekatek's Zenith "api.mekatek.xyz/v0/auction" endpoint
type AuctionResponse struct {
	ChainID  string            `json:"chain_id"`
	Height   int64             `json:"height"`
	Payments []PaymentResponse `json:"payments"`
}

type PaymentResponse struct {
	Address    string  `json:"address"`
	Allocation float64 `json:"allocation"`
	Denom      string  `json:"denom"`
}

// POST request to api.mekatek.xyz/v0/bid.
type BidRequest struct {
	ChainID       string                  `json:"chain_id"`
	Height        int64                   `json:"height"`
	Kind          string                  `json:"kind,omitempty"` //either top or block, leave empty for top
	SwapTx        string                  `json:"user_swap"`      //signed base 64 encoded cosmos TX (user's swap only)
	Payments      []PaymentResponse       `json:"payments"`       //Info from the Zenith GET /v0/auction request (obtained from AuctionResponse)
	SimulatedSwap api.SimulatedSwapResult //Info from the simulator. This helps us estimate the proceeds and make an accurate auction bid.
}

type ZenithBidRequest struct {
	ChainID string   `json:"chain_id"`
	Height  int64    `json:"height"`
	Kind    string   `json:"kind,omitempty"` //either top or block, leave empty for top
	Txs     []string `json:"txs"`            //base 64 encoded TXs
}

type BidResponse struct {
	ChainID  string   `json:"chain_id"`
	Height   int64    `json:"height"`
	Kind     string   `json:"kind,omitempty"` //either top or block, leave empty for top
	TxHashes []string `json:"tx_hashes"`      //tx hashes
	Id       string   //ID to look up status later
}

func (ar *AuctionResponse) Validate() bool {
	totalPayment := 0.0
	for _, payment := range ar.Payments {
		if payment.Address == "" {
			return false
		}
		totalPayment += payment.Allocation
	}

	return totalPayment == 1.0
}
