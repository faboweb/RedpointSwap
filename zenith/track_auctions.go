package zenith

import (
	"sync"
	"time"

	"github.com/DefiantLabs/RedpointSwap/config"
	"go.uber.org/zap"
)

// Tracks upcoming blocks. Any blocks past the current chain height are removed.
// Each future block will be represented in the Map with Key: int64 block height and Value: *FutureBlock.
var zenithBlocks sync.Map

type FutureBlock struct {
	IsZenithBlock          bool      //whether this block will be submitted by Zenith
	Height                 int64     //the block height of the future block
	ProjectedBlocktime     time.Time //the time we think this block will happen on chain
	MillisecondsUntilBlock int64     //how many milliseconds until we think this block will happen
	Auction                *AuctionResponse
}

func GetZenithBlocks() []*FutureBlock {
	zBlocks := []*FutureBlock{}
	zenithBlocks.Range(func(key, val any) bool {
		zBlock := val.(*FutureBlock)
		if zBlock.IsZenithBlock {
			zBlocks = append(zBlocks, zBlock)
		}
		return true
	})

	return zBlocks
}

// TODO: notify the admin if the Zenith endpoint stops working
//
// This function is called for every new block produced on the chain.
// We query the Zenith auction endpoint (see https://meka.tech/zenith#get-_v0_auction)
// for the next 10 blocks (which is the max supported by Zenith).
//
// Overall, we are tracking available Zenith blocks using Mekatek's service endpoints.
// This function only tracks what blocks Zenith will produce -- it does not bid on auctions.
func ZenithBlockNotificationHandler(lastChainHeight int64, millisecondsBetweenBlocks int64) {
	conf := config.Conf

	// Remove any blocks that already happened
	zenithBlocks.Range(func(key, _ any) bool {
		if key.(int64) <= lastChainHeight {
			zenithBlocks.Delete(key)
		}
		return true
	})

	for height := lastChainHeight; height < lastChainHeight+10; height++ {
		//We have never queried Zenith for the given block
		if _, ok := zenithBlocks.Load(height); !ok {

			req := &AuctionRequest{
				ChainID: conf.Api.ChainID,
				Height:  height,
			}

			auctionResp, zenithCode, err := req.getAvailableAuction(conf.Zenith.ZenithAuctionUrl)

			//If there was an error or if the auction is too far in the future, we'll need to requery.
			//Otherwise, the query succeeded and we need to record whether or not this is a Zenith block.
			if err != nil && zenithCode != AuctionTooFarInFuture {
				msUntilBlock := (height - lastChainHeight) * millisecondsBetweenBlocks

				zBlock := &FutureBlock{
					IsZenithBlock:          zenithCode == ZenithAuction,
					Height:                 height,
					ProjectedBlocktime:     time.Now().Add(time.Millisecond * time.Duration(msUntilBlock)),
					MillisecondsUntilBlock: msUntilBlock,
					Auction:                auctionResp,
				}

				if zBlock.IsZenithBlock {
					config.Logger.Debug("Zenith block", zap.Int64("Found zenith block at height", zBlock.Height))
				}

				zenithBlocks.Store(height, zBlock)
			}
		}
	}
}
