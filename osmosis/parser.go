package osmosis

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	gammTypes "github.com/osmosis-labs/osmosis/v13/x/gamm/types"
)

func convertSdkResp(currTx *txTypes.Tx, currTxResp *sdk.TxResponse) (*MergedTx, error) {
	// Indexer types only used by the indexer app (similar to the cosmos types)
	indexerMergedTx := &MergedTx{}
	var indexerTx IndexerTx
	var txBody Body
	var currMessages []sdk.Msg
	var currLogMsgs []LogMessage

	// Get the Messages and Message Logs
	for msgIdx := range currTx.Body.Messages {
		currMsg := currTx.Body.Messages[msgIdx].GetCachedValue()
		if currMsg != nil {
			msg := currMsg.(sdk.Msg)
			currMessages = append(currMessages, msg)
			if len(currTxResp.Logs) >= msgIdx+1 {
				msgEvents := currTxResp.Logs[msgIdx].Events
				currTxLog := LogMessage{
					MessageIndex: msgIdx,
					Events:       toEvents(msgEvents),
				}
				currLogMsgs = append(currLogMsgs, currTxLog)
			}
		} else {
			return nil, fmt.Errorf("tx message could not be processed. CachedValue is not present. TX Hash: %s, Msg type: %s, Msg index: %d, Code: %d",
				currTxResp.TxHash,
				currTx.Body.Messages[msgIdx].TypeUrl,
				msgIdx,
				currTxResp.Code,
			)
		}
	}

	txBody.Messages = currMessages
	indexerTx.Body = txBody

	indexerTxResp := Response{
		TxHash:    currTxResp.TxHash,
		Height:    fmt.Sprintf("%d", currTxResp.Height),
		TimeStamp: currTxResp.Timestamp,
		RawLog:    currTxResp.RawLog,
		Log:       currLogMsgs,
		Code:      currTxResp.Code,
	}

	indexerTx.AuthInfo = *currTx.AuthInfo
	indexerTx.Signers = currTx.GetSigners()
	indexerMergedTx.TxResponse = indexerTxResp
	indexerMergedTx.Tx = indexerTx
	indexerMergedTx.Tx.AuthInfo = *currTx.AuthInfo
	return indexerMergedTx, nil
}

func toEvents(msgEvents sdk.StringEvents) (list []LogMessageEvent) {
	for _, evt := range msgEvents {
		lme := LogMessageEvent{Type: evt.Type, Attributes: toAttributes(evt.Attributes)}
		list = append(list, lme)
	}

	return list
}

func toAttributes(attrs []sdk.Attribute) []Attribute {
	list := []Attribute{}
	for _, attr := range attrs {
		lma := Attribute{Key: attr.Key, Value: attr.Value}
		list = append(list, lma)
	}

	return list
}

type Swap struct {
	TokenIn  sdk.Coin
	TokenOut sdk.Coin
	Address  string
}

type Send struct {
	Token    sdk.Coin
	Sender   string
	Receiver string
}

type OsmosisTx struct {
	IsSuccessfulTx bool
	FeePayer       string
	Fees           sdk.Coins
	Swaps          []Swap
	Sends          []Send
	Hash           string
}

// Parser adapted from Defiant Labs' Sycamore tax app, app.sycamore.tax, github.com/DefiantLabs/cosmos-tax-cli
// Parses and returns token in, token out, fees, and addresses for 'MsgSwapExactAmountIn' and other types.
func ParseRedpointSwaps(txResponse *txTypes.GetTxResponse, txHash string) OsmosisTx {
	swapTx := OsmosisTx{
		Swaps:    []Swap{},
		Sends:    []Send{},
		FeePayer: txResponse.Tx.FeePayer().String(),
		Hash:     txHash,
	}

	if txResponse.TxResponse.Code != 0 {
		return swapTx
	}

	swapTx.Fees = txResponse.Tx.GetFee()

	mergedTx, err := convertSdkResp(txResponse.Tx, txResponse.TxResponse)
	if err != nil {
		fmt.Printf("Error parsing SDK response for TX %s. Error: %s\n", txHash, err.Error())
		return swapTx
	}

	for messageIndex, msg := range mergedTx.Tx.Body.Messages {
		messageLog := GetMessageLogForIndex(mergedTx.TxResponse.Log, messageIndex)
		msgType := txResponse.Tx.Body.Messages[messageIndex].TypeUrl

		switch v := msg.(type) {
		case *gammTypes.MsgSwapExactAmountIn:
			msgSwap := msg.(*gammTypes.MsgSwapExactAmountIn)
			swap, err := ParseMsgSwapExactAmountIn(msgSwap, messageLog, msgType)
			if err != nil {
				fmt.Printf("Error for TX with hash %s during ParseMsgSwapExactAmountIn: %s\n", txHash, err.Error())
				return swapTx
			}
			swapTx.Swaps = append(swapTx.Swaps, swap)
		case *bank.MsgSend:
			//In the Redpoint backend, there are two reasons one of the TXs would contain a MsgSend.
			// 1) The app makes revenue through arbitrage; it then sends a large portion of this revenue to the user
			// 2) The app can get guaranteed block placement through Mekatek's Zenith service, which requires paying small fees
			// This parses the amount, sender, and receiver. It doesn't care which situation (1) or (2) happened.
			msgSend := msg.(*bank.MsgSend)
			if len(msgSend.Amount) != 1 {
				fmt.Printf("Error for TX with hash %s; unexpected MsgSend, %d tokens sent\n", txHash, len(msgSend.Amount))
				return swapTx
			}
			send := Send{
				Sender:   msgSend.FromAddress,
				Receiver: msgSend.ToAddress,
				Token:    msgSend.Amount[0],
			}
			swapTx.Sends = append(swapTx.Sends, send)
		case *authz.MsgExec: //TODO: verify the log messages produced when you do a MsgExec w/ an inner swap
			msgExec := msg.(*authz.MsgExec)
			fmt.Printf("placeholder %+v", msgExec)
			msgs, err := msgExec.GetMessages()
			if err != nil {
				fmt.Printf("Error for TX with hash %s (message index: %d) during msgExec.GetMessages(): %s\n", txHash, messageIndex, err.Error())
				return swapTx
			}

			for _, msg := range msgs {
				msgSwap := msg.(*gammTypes.MsgSwapExactAmountIn)
				swap, err := ParseMsgSwapExactAmountIn(msgSwap, messageLog, msgType)
				if err != nil {
					fmt.Printf("Error for TX with hash %s during ParseMsgSwapExactAmountIn: %s\n", txHash, err.Error())
					return swapTx
				}
				swapTx.Swaps = append(swapTx.Swaps, swap)
				fmt.Printf("Parsed MsgExec (grantee swap). Swap address: %s, token in: %s, token out: %s\n", swap.Address, swap.TokenIn, swap.TokenOut)
			}
		default:
			fmt.Printf("Unknown type '%s', msg String(): %s\n", v, msg.String())
		}
	}

	swapTx.IsSuccessfulTx = true
	return swapTx
}

// Parse an Osmosis gamm MsgSwapExactAmountIn, using the message logs to determine amount received
func ParseMsgSwapExactAmountIn(msg *gammTypes.MsgSwapExactAmountIn, messageLog *LogMessage, msgType string) (Swap, error) {
	var swap Swap
	// Confirm that the action listed in the message log matches the Message type
	validLog := IsMessageActionEquals(msgType, messageLog)
	if !validLog {
		return swap, fmt.Errorf("error 'IsMessageActionEquals'. Message type: %s, message log: %+v", msgType, messageLog)
	}

	// The attribute in the log message that shows you the tokens swapped
	tokensSwappedEvt := GetEventWithType(gammTypes.TypeEvtTokenSwapped, messageLog)
	if tokensSwappedEvt == nil {
		return swap, fmt.Errorf("error getting event type '%s', message log: %+v", gammTypes.TypeEvtTokenSwapped, messageLog)
	}

	// Address of whoever initiated the swap. Will be both sender/receiver.
	senderReceiver := GetValueForAttribute("sender", tokensSwappedEvt)
	if senderReceiver == "" {
		return swap, fmt.Errorf("error getting sender from token swapped event: %+v", tokensSwappedEvt)
	}

	// This gets the first token swapped in (if there are multiple pools we do not care about intermediates)
	tokenInStr := GetValueForAttribute(gammTypes.AttributeKeyTokensIn, tokensSwappedEvt)
	tokenIn, err := sdk.ParseCoinNormalized(tokenInStr)
	if err != nil {
		return swap, fmt.Errorf("error parsing coins in. Event: %+v, Err %s: ", tokensSwappedEvt, err.Error())
	}

	// This gets the last token swapped out (if there are multiple pools we do not care about intermediates)
	tokenOutStr := GetLastValueForAttribute(gammTypes.AttributeKeyTokensOut, tokensSwappedEvt)
	tokenOut, err := sdk.ParseCoinNormalized(tokenOutStr)
	if err != nil {
		return swap, fmt.Errorf("error parsing coins out. err %s: ", err.Error())
	}

	swap.TokenIn = tokenIn
	swap.TokenOut = tokenOut
	swap.Address = msg.Sender
	return swap, nil
}

func GetLastValueForAttribute(key string, evt *LogMessageEvent) string {
	if evt == nil || evt.Attributes == nil {
		return ""
	}

	for i := len(evt.Attributes) - 1; i >= 0; i-- {
		attr := evt.Attributes[i]
		if attr.Key == key {
			return attr.Value
		}
	}

	return ""
}

// If order is reversed, the last attribute containing the given key will be returned
// otherwise the first attribute will be returned
func GetValueForAttribute(key string, evt *LogMessageEvent) string {
	if evt == nil || evt.Attributes == nil {
		return ""
	}

	for _, attr := range evt.Attributes {
		if attr.Key == key {
			return attr.Value
		}
	}

	return ""
}

func IsMessageActionEquals(msgType string, msg *LogMessage) bool {
	logEvent := GetEventWithType("message", msg)
	if logEvent == nil {
		return false
	}

	for _, attr := range logEvent.Attributes {
		if attr.Key == "action" {
			if attr.Value == msgType {
				return true
			}
		}
	}

	return false
}

func GetEventWithType(eventType string, msg *LogMessage) *LogMessageEvent {
	if msg == nil || msg.Events == nil {
		return nil
	}

	for _, logEvent := range msg.Events {
		if logEvent.Type == eventType {
			return &logEvent
		}
	}

	return nil
}

func GetMessageLogForIndex(logs []LogMessage, index int) *LogMessage {
	for _, log := range logs {
		if log.MessageIndex == index {
			return &log
		}
	}

	return nil
}

// In the json, TX data is split into 2 arrays, used to merge the full dataset
type MergedTx struct {
	Tx         IndexerTx
	TxResponse Response
}

type IndexerTx struct {
	Body     Body `json:"body"`
	AuthInfo txTypes.AuthInfo
	Signers  []sdk.AccAddress
}

type Body struct {
	Messages []sdk.Msg `json:"messages"`
}

type Response struct {
	TxHash    string       `json:"txhash"`
	Height    string       `json:"height"`
	TimeStamp string       `json:"timestamp"`
	Code      uint32       `json:"code"`
	RawLog    string       `json:"raw_log"`
	Log       []LogMessage `json:"logs"`
}

// TxLogMessage:
// Cosmos blockchains return Transactions with an array of "logs" e.g.
//
// "logs": [
//
//	{
//		"msg_index": 0,
//		"events": [
//		  {
//			"type": "coin_received",
//			"attributes": [
//			  {
//				"key": "receiver",
//				"value": "juno128taw6wkhfq29u83lmh5qyfv8nff6h0w577vsy"
//			  }, ...
//			]
//		  } ...
//
// The individual log always has a msg_index corresponding to the Message from the Transaction.
// But the events are specific to each Message type, for example MsgSend might be different from
// any other message type.
//
// This struct just parses the KNOWN fields and leaves the other fields as raw JSON.
// More specific type parsers for each message type can parse those fields if they choose to.
type LogMessage struct {
	MessageIndex int               `json:"msg_index"`
	Events       []LogMessageEvent `json:"events"`
}

type LogMessageEvent struct {
	Type       string      `json:"type"`
	Attributes []Attribute `json:"attributes"`
}

type Attribute struct {
	Key   string
	Value string
}
