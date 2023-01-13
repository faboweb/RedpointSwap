package zenith

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/DefiantLabs/RedpointSwap/config"
	"go.uber.org/zap"
)

type ZenithResponse int

const (
	NotZenithAuction      ZenithResponse = iota //The block and chain are valid, but this isn't a Zenith block
	PastAuction                                 //Auction is already in the past
	AuctionTooFarInFuture                       //Auction is too far in the future
	ZenithAuction                               //Auction is a Zenith block
	QueryError                                  //We couldn't complete the query for some reason (see error)
)

func (req *AuctionRequest) getAvailableAuction(zenithUrl string) (*AuctionResponse, ZenithResponse, error) {
	zenithReq, err := url.Parse(zenithUrl)
	if err != nil {
		return nil, QueryError, err
	}

	// Query params
	params := url.Values{}
	params.Add("chain_id", req.ChainID)
	params.Add("height", fmt.Sprintf("%d", req.Height))
	zenithReq.RawQuery = params.Encode()

	var auctionResp AuctionResponse
	var zenithCode ZenithResponse = QueryError

	resp, err := http.Get(zenithReq.String())
	if err != nil {
		return nil, QueryError, err
	}

	err = json.NewDecoder(resp.Body).Decode(&auctionResp)
	if err != nil {
		return nil, QueryError, err
	}

	if resp.StatusCode == 200 {
		zenithCode = ZenithAuction
	} else if resp.StatusCode == 410 {
		zenithCode = PastAuction
	} else if resp.StatusCode == 425 {
		zenithCode = AuctionTooFarInFuture
	} else if resp.StatusCode == 417 {
		zenithCode = NotZenithAuction
	} else {
		zenithCode = QueryError
		config.Logger.Error("Zenith error", zap.Int("Unrecognized HTTP status", resp.StatusCode))
	}

	return &auctionResp, zenithCode, err
}
