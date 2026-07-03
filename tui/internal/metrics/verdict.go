package metrics

import (
	"errors"
	"time"
)

// Action is the discrete decision the reasoner can emit per asset. It lives in
// metrics (the dependency-free domain layer) so both the event bus and the
// reasoner can reference it without an import cycle.
type Action string

const (
	ActionOpenShort Action = "open_short"
	ActionOpenLong  Action = "open_long"
	ActionClose     Action = "close"
	ActionScale     Action = "scale"
	ActionHold      Action = "hold"
	ActionAlertOnly Action = "alert_only"
)

// Valid reports whether a is a recognized action.
func (a Action) Valid() bool {
	switch a {
	case ActionOpenShort, ActionOpenLong, ActionClose, ActionScale, ActionHold, ActionAlertOnly:
		return true
	}
	return false
}

// IsTrade reports whether the action would change a position.
func (a Action) IsTrade() bool {
	switch a {
	case ActionOpenShort, ActionOpenLong, ActionClose, ActionScale:
		return true
	}
	return false
}

// Entry describes the intended fill.
type Entry struct {
	Type  string  `json:"type"` // "limit" | "market"
	Price float64 `json:"price,omitempty"`
}

// Verdict is the structured output the reasoner produces per asset, mirroring
// the plan's JSON schema. It is validated before anything acts on it.
type Verdict struct {
	Asset                string  `json:"asset"`
	Timeframe            string  `json:"timeframe"`
	Action               Action  `json:"action"`
	SizeUSD              float64 `json:"size_usd"`
	Entry                Entry   `json:"entry"`
	Stop                 float64 `json:"stop"`
	TakeProfit           float64 `json:"take_profit"`
	Thesis               string  `json:"thesis"`
	Reading              string  `json:"reading"` // short OI+funding regime summary from batch model
	Confidence           float64 `json:"confidence"`
	RequiresConfirmation bool    `json:"requires_confirmation"`

	At       time.Time `json:"-"`
	Provider string    `json:"-"`
	RawText  string    `json:"-"`
}

// Validate enforces the schema invariants. A verdict that fails is never executed.
func (v Verdict) Validate() error {
	if v.Asset == "" {
		return errors.New("verdict: empty asset")
	}
	if !v.Action.Valid() {
		return errors.New("verdict: invalid action " + string(v.Action))
	}
	if v.Confidence < 0 || v.Confidence > 1 {
		return errors.New("verdict: confidence out of range")
	}
	if v.Action.IsTrade() && v.Action != ActionClose {
		if v.SizeUSD <= 0 {
			return errors.New("verdict: trade action requires positive size_usd")
		}
	}
	return nil
}
