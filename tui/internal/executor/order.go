// Order construction for the Hyperliquid exchange endpoint. The action objects
// are built as signing.OrderedMap so their msgpack encoding (and thus the action
// hash) is byte-exact with Hyperliquid's reference. This is part of the owned,
// sealed signing surface — no SDK.
package executor

import (
	"strconv"

	"github.com/hyperagent/hyperagent/internal/signing"
)

// OrderRequest is a single perp order in HL's expected field shape.
type OrderRequest struct {
	AssetID    int     // HL asset index (perp universe order)
	IsBuy      bool    // true = long/buy
	Price      float64 // limit price; for market, an aggressive price
	Size       float64 // base-asset size
	ReduceOnly bool
	OrderType  string // "limit" or "market" (we encode market as IOC limit)
}

// fmtFloat renders a float the way HL expects: trimmed, no trailing zeros, no
// exponent. HL is strict about wire numeric formatting in the action hash.
func fmtFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// buildOrderAction constructs the ordered action map for one or more orders.
// Shape: {type:"order", orders:[{a,b,p,s,r,t}], grouping:"na"}.
func buildOrderAction(orders []OrderRequest) *signing.OrderedMap {
	arr := make([]any, 0, len(orders))
	for _, o := range orders {
		// t is the order-type object: {limit:{tif:"Gtc"|"Ioc"}}.
		tif := "Gtc"
		if o.OrderType == "market" {
			tif = "Ioc"
		}
		limit := signing.NewOrderedMap().Set("tif", tif)
		typ := signing.NewOrderedMap().Set("limit", limit)

		om := signing.NewOrderedMap().
			Set("a", o.AssetID).
			Set("b", o.IsBuy).
			Set("p", fmtFloat(o.Price)).
			Set("s", fmtFloat(o.Size)).
			Set("r", o.ReduceOnly).
			Set("t", typ)
		arr = append(arr, om)
	}
	return signing.NewOrderedMap().
		Set("type", "order").
		Set("orders", arr).
		Set("grouping", "na")
}

// buildCancelAction constructs the cancel action for one resting order.
// Shape: {type:"cancel", cancels:[{a: assetID, o: oid}]}.
func buildCancelAction(assetID int, oid uint64) *signing.OrderedMap {
	c := signing.NewOrderedMap().Set("a", assetID).Set("o", oid)
	return signing.NewOrderedMap().
		Set("type", "cancel").
		Set("cancels", []any{c})
}
