package zenith

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

// POST request to api.mekatek.xyz/v0/bid
type BidRequest struct {
	ChainID string   `json:"chain_id"`
	Height  int64    `json:"height"`
	Kind    string   `json:"kind,omitempty"` //either top or block, leave empty for top
	Txs     []string `json:"txs"`            //base 64 encoded cosmos TXs
}

type BidResponse struct {
	ChainID  string   `json:"chain_id"`
	Height   int64    `json:"height"`
	Kind     string   `json:"kind,omitempty"` //either top or block, leave empty for top
	TxHashes []string `json:"tx_hashes"`      //tx hashes
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
