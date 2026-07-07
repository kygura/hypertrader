package main

import (
	"math/rand"
	"time"
)

// market is one row of the live ingest picture.
type market struct {
	sym     string
	last    float64
	chg24h  float64 // percent
	funding float64 // percent per 8h
	oiDelta float64 // percent, 1h
	cvd     float64 // millions USD, 1h
}

// position is one row of the execution book.
type position struct {
	sym   string
	side  string
	size  float64
	entry float64
	lev   float64
}

// entry is one line of the append-only decision journal.
type entry struct {
	at   time.Time
	tag  string // INGEST | REASON | EXECUTE | FILL | RISK | OPERATOR
	text string
}

// mandate mirrors the pitch example: goal, horizon, risk envelope.
type mandate struct {
	quote       string
	allocPct    float64 // current ETH allocation, percent
	allocTarget float64
	drawdownPct float64
	drawdownMax float64
	leverage    float64
	leverageCap float64
	day         int
	horizonDays int
}

type gate struct {
	name  string
	extra string
}

var gates = []gate{
	{"max position", ""},
	{"max exposure", ""},
	{"max concurrency", ""},
	{"price sanity vs mark", ""},
	{"post-stop cooldown", ""},
	{"daily-loss kill-switch", "armed"},
}

func initialMarkets() []market {
	return []market{
		{"ETH", 3412.40, 1.82, 0.0042, 0.9, 4.2},
		{"BTC", 67231.00, 0.64, 0.0011, 0.3, 11.8},
		{"SOL", 148.92, -2.10, -0.0064, -1.4, -2.6},
		{"HYPE", 38.75, 3.95, 0.0125, 2.8, 6.1},
		{"ARB", 0.8412, -0.88, 0.0008, 0.2, -0.4},
		{"AVAX", 29.34, 1.12, 0.0031, 0.6, 0.9},
	}
}

func initialPositions() []position {
	return []position{
		{"ETH-PERP", "LONG", 2.44, 3402.10, 1.4},
	}
}

func initialMandate() mandate {
	return mandate{
		quote:       "Reach a 60/40 ETH–stablecoin split over 90 days. Keep drawdown under 8%. Leverage capped at 2×.",
		allocPct:    48.2,
		allocTarget: 60.0,
		drawdownPct: 2.1,
		drawdownMax: 8.0,
		leverage:    1.4,
		leverageCap: 2.0,
		day:         23,
		horizonDays: 90,
	}
}

// script is the loop the demo journal cycles through: ingest → reason →
// execute → fill, with risk gates visible at every step.
var script = []entry{
	{tag: "INGEST", text: "ETH digest: funding +0.0042%/8h drifting positive, CVD +$4.2M, OI +0.9% — bid absorption below 3,400"},
	{tag: "REASON", text: "Funding regime favors patient bids; spread too wide to cross. Staging tranche 3 of 6 toward 60% target"},
	{tag: "EXECUTE", text: "limit buy 0.85 ETH @ 3,391.50 post-only — gates 6/6 pass, exposure 1.4× of 2.0× cap"},
	{tag: "FILL", text: "0.85 ETH filled @ 3,391.50 — allocation 48.2% → 49.6%, thesis recorded, journal #4821"},
	{tag: "INGEST", text: "BTC lead-lag: basis flat, cross-asset corr 0.87 — no contagion signal against ETH thesis"},
	{tag: "REASON", text: "Drawdown 2.1% of 8% envelope; no action required. Next tranche gated on funding < +0.008%/8h"},
	{tag: "RISK", text: "hourly envelope check: max position ok, concurrency 1/3, daily-loss kill-switch armed"},
	{tag: "INGEST", text: "liquidation map: nearest cluster 3,308 (-2.5%) — stop placement clears cluster by 40 bps"},
	{tag: "REASON", text: "OI building into resistance 3,460; replacing resting bid rather than chasing — thesis unchanged"},
	{tag: "EXECUTE", text: "amend limit buy → 3,396.00, size 0.85 ETH — price sanity vs mark +0.12%, pass"},
	{tag: "INGEST", text: "funding print +0.0038%/8h, trajectory cooling — tranche condition satisfied on next window"},
	{tag: "FILL", text: "0.85 ETH filled @ 3,396.00 — allocation 49.6% → 51.1%, 90-day pace +0.4d ahead, journal #4822"},
}

const (
	tickEvery    = 700 * time.Millisecond
	journalEvery = 2400 * time.Millisecond
	maxJournal   = 200
)

// jitter applies a small random walk to the market picture so the demo
// reads as live ingest rather than a static screenshot.
func jitter(ms []market) {
	for i := range ms {
		m := &ms[i]
		m.last *= 1 + rand.NormFloat64()*0.0004
		m.chg24h += rand.NormFloat64() * 0.02
		m.funding += rand.NormFloat64() * 0.0001
		m.oiDelta += rand.NormFloat64() * 0.03
		m.cvd += rand.NormFloat64() * 0.15
	}
}
