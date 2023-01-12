package zenith

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type ZenithBlockResponse int

const (
	NotZenithAuction      ZenithBlockResponse = iota //The block and chain are valid, but this isn't a Zenith block
	PastAuction                                      //Auction is already in the past
	AuctionTooFarInFuture                            //Auction is too far in the future
	ZenithAuction                                    //Auction is a Zenith block
	QueryError                                       //We couldn't complete the query for some reason (see error)
)

func (req *AuctionRequest) getAvailableAuction(zenithUrl string) (*AuctionResponse, ZenithBlockResponse, error) {
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
	resp, err := http.Get(zenithReq.String())
	if err != nil {
		return &auctionResp, QueryError, err
	}

	err = json.NewDecoder(resp.Body).Decode(&auctionResp)
	var zenithCode ZenithBlockResponse = NotZenithAuction

	//Any http code 200-299
	if resp.StatusCode/200 == 1 {
		zenithCode = ZenithAuction
	} else if resp.StatusCode == 410 {
		zenithCode = PastAuction
	} else if resp.StatusCode == 425 {
		zenithCode = AuctionTooFarInFuture
	} else if resp.StatusCode == 417 {
		zenithCode = NotZenithAuction
	}

	return &auctionResp, zenithCode, err
}
