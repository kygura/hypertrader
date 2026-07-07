// Package apiclient is the tui module's only dependency on backend/: a thin
// HTTP+WS client. No import of backend/internal/* — every wire type here is
// hand-derived from the JSON the daemon's API actually produces.
package apiclient

import "time"

// Bar mirrors backend/internal/metrics.Bar (untagged Go struct — wire fields
// are the exported Go names verbatim; Open/CloseTime marshal as RFC3339).
type Bar struct {
	Coin                   string
	Timeframe              string
	OpenTime               time.Time
	CloseTime              time.Time
	Open, High, Low, Close float64
	Volume                 float64

	// Flow.
	BuyVolume  float64
	SellVolume float64
	TradeCount int
	Final      bool
	LargePrint bool
	CVD        float64
	TradeImbal float64

	// Perp regime (snapshot at bar close).
	Funding      float64
	FundingDelta float64
	OpenInterest float64
	OIDelta      float64
	Basis        float64
	MarkPrice    float64

	// Derived structure.
	Return      float64
	RealizedVol float64
	RangePos    float64
	LiqProx     float64

	// Cross-asset.
	BTCCorr     float64
	RelStrength float64
}

// IsBullish reports whether the bar closed up.
func (b Bar) IsBullish() bool { return b.Close >= b.Open }

// AssetCtx mirrors backend/internal/metrics.AssetCtx.
type AssetCtx struct {
	Coin         string
	MarkPrice    float64
	OraclePrice  float64
	Funding      float64
	OpenInterest float64
	Premium      float64
	DayVolume    float64
	Time         time.Time
}

// Position mirrors backend/internal/metrics.Position.
type Position struct {
	Coin       string
	Size       float64
	EntryPrice float64
	MarkPrice  float64
	UnrealPnl  float64
	OpenedAt   time.Time
}

// IsLong reports the position direction.
func (p Position) IsLong() bool  { return p.Size > 0 }
func (p Position) IsShort() bool { return p.Size < 0 }
func (p Position) IsFlat() bool  { return p.Size == 0 }

// Thesis mirrors the thesis JSON of GET /api/theses and the WS "thesis"
// topic (design: docs/superpowers/specs/2026-07-07-patient-agent-design.md,
// "API contract"). A "neutral" direction is a real thesis ("stay out"),
// distinct from no thesis at all (never reviewed, or invalidated).
type Thesis struct {
	Coin         string    `json:"coin"`
	Direction    string    `json:"direction"` // "long" | "short" | "neutral"
	Summary      string    `json:"summary"`
	Invalidation float64   `json:"invalidation"`
	Targets      []float64 `json:"targets"`
	Horizon      string    `json:"horizon"`    // "days" | "weeks"
	Confidence   float64   `json:"confidence"` // 0..1
	CreatedAt    time.Time `json:"created_at"`
	ReviewedAt   time.Time `json:"reviewed_at"`
	Version      int       `json:"version"`
}

// MarketEntry mirrors the marketEntry JSON shape of GET /api/markets
// (backend/internal/api/read.go).
type MarketEntry struct {
	Coin     string   `json:"coin"`
	Bar      Bar      `json:"bar"`
	Mid      float64  `json:"mid"`
	AssetCtx AssetCtx `json:"asset_ctx"`
	Position Position `json:"position"`
}

// Action is the discrete decision the reasoner can emit per asset. It mirrors
// backend/internal/metrics.Action.
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

// Verdict mirrors backend/internal/metrics.Verdict.
type Verdict struct {
	Asset                string  `json:"asset"`
	Timeframe            string  `json:"timeframe"`
	Action               Action  `json:"action"`
	SizeUSD              float64 `json:"size_usd"`
	Entry                Entry   `json:"entry"`
	Stop                 float64 `json:"stop"`
	TakeProfit           float64 `json:"take_profit"`
	Thesis               string  `json:"thesis"`
	Reading              string  `json:"reading"`
	Confidence           float64 `json:"confidence"`
	RequiresConfirmation bool    `json:"requires_confirmation"`

	At       time.Time `json:"-"`
	Provider string    `json:"-"`
}

// Entry mirrors backend/internal/metrics.Entry.
type Entry struct {
	Type  string  `json:"type"`
	Price float64 `json:"price,omitempty"`
}

// ChatTurn mirrors backend/internal/reasoner.ChatTurn (json tags role/text).
type ChatTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// SettingsResponse mirrors GET /api/settings's body.
type SettingsResponse struct {
	Mode           string              `json:"mode"`
	Batch          RoleSettings        `json:"batch"`
	Chat           RoleSettings        `json:"chat"`
	ProviderNames  []string            `json:"provider_names"`
	ProviderModels map[string][]string `json:"provider_models"`
	KeyHints       map[string]string   `json:"key_hints"`
	Visualized     []string            `json:"visualized"`
	Tracked        []string            `json:"tracked"`
	Timeframes     map[string]string   `json:"timeframes"`
	Risk           RiskSettings        `json:"risk"`
}

type RoleSettings struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// RiskSettings mirrors backend/internal/api/settings.go's riskSettings.
type RiskSettings struct {
	MaxPositionUSD      float64 `json:"max_position_usd"`
	MaxTotalExposureUSD float64 `json:"max_total_exposure_usd"`
	MaxConcurrent       int     `json:"max_concurrent"`
	DailyLossKillUSD    float64 `json:"daily_loss_kill_usd"`
}
