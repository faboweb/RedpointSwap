package zenith

import (
	"fmt"
	"testing"
)

func TestAuction(t *testing.T) {
	height := int64(7764360)
	done := false

	for {
		req := &AuctionRequest{
			ChainID: "osmosis-1",
			Height:  height,
		}

		auctionResp, zenithCode, err := req.getAvailableAuction("http://api.mekatek.xyz/v0/auction")
		if err != nil {
			t.Fail()
		}

		zenithStatus := ""
		if zenithCode == ZenithAuction {
			zenithStatus = fmt.Sprintf("Auction %d is a zenith auction!", height)
		} else if zenithCode == PastAuction {
			zenithStatus = fmt.Sprintf("Auction %d is past", height)
		} else if zenithCode == AuctionTooFarInFuture {
			zenithStatus = fmt.Sprintf("Auction %d is too far in the future", height)
			done = true
		} else if zenithCode == NotZenithAuction {
			zenithStatus = fmt.Sprintf("Auction %d is not a zenith auction", height)
		}

		fmt.Printf("Resp: %+v, zenithStatus: %s, err: %s\n", auctionResp, zenithStatus, err)
		height += 1
		if done {
			break
		}
	}

}
