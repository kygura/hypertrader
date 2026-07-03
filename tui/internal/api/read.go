package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/hyperagent/hyperagent/internal/journal"
	"github.com/hyperagent/hyperagent/internal/metrics"
)

// marketEntry is one tracked coin's snapshot for GET /api/markets: the same
// three sources the TUI's market panel reads (latest bar, mid, perp context).
type marketEntry struct {
	Coin     string           `json:"coin"`
	Bar      metrics.Bar      `json:"bar"`
	Mid      float64          `json:"mid"`
	AssetCtx metrics.AssetCtx `json:"asset_ctx"`
}

// handleMarkets returns one entry per tracked coin that has a bar yet — a
// coin still warming up (no finalized bar for its timeframe) is omitted
// rather than sent with zero values, so the client doesn't render a false
// "$0" price. Only when every tracked coin is still empty do we 404: the
// dashboard should show a "warming up" state, not an empty success.
func (s *Server) handleMarkets(w http.ResponseWriter, r *http.Request) {
	var entries []marketEntry
	for _, coin := range s.deps.Cfg.Markets.Tracked {
		tf := s.deps.Cfg.Timeframe.For(coin)
		bar, ok := s.deps.Store.LatestBar(coin, tf)
		if !ok {
			continue
		}
		ctx, _ := s.deps.Store.AssetCtx(coin)
		entries = append(entries, marketEntry{
			Coin:     coin,
			Bar:      bar,
			Mid:      s.deps.Store.Mid(coin),
			AssetCtx: ctx,
		})
	}
	if len(entries) == 0 {
		writeErr(w, http.StatusNotFound, "store warming up")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleBars serves the OHLCV+metrics series for one coin: ?tf defaults to
// the coin's configured timeframe, ?n defaults to 100 and is capped at 1000
// so a client can't force an unbounded scan of the ring.
func (s *Server) handleBars(w http.ResponseWriter, r *http.Request) {
	coin := r.PathValue("coin")
	tf := r.URL.Query().Get("tf")
	if tf == "" {
		tf = s.deps.Cfg.Timeframe.For(coin)
	}
	n := 100
	if raw := r.URL.Query().Get("n"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			n = v
		}
	}
	if n > 1000 {
		n = 1000
	}
	bars := s.deps.Store.History(coin, tf, n)
	if len(bars) == 0 {
		writeErr(w, http.StatusNotFound, "no bars for %s %s", coin, tf)
		return
	}
	writeJSON(w, http.StatusOK, bars)
}

// handleDigest serves the latest frozen digest for one coin from the
// bus-fed cache (runCaches) — the batcher's output, not the store.
func (s *Server) handleDigest(w http.ResponseWriter, r *http.Request) {
	coin := r.PathValue("coin")
	s.state.mu.RLock()
	d, ok := s.state.digests[coin]
	s.state.mu.RUnlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "no digest for %s", coin)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleVerdicts serves the latest verdict per asset, newest-first, from the
// bus-fed cache. An empty watchlist-so-far is a normal 200 with [], not 404 —
// unlike markets/digests, "nothing has reasoned yet" isn't a warm-up error.
func (s *Server) handleVerdicts(w http.ResponseWriter, r *http.Request) {
	s.state.mu.RLock()
	verdicts := append([]metrics.Verdict{}, s.state.verdicts...)
	s.state.mu.RUnlock()
	writeJSON(w, http.StatusOK, verdicts)
}

// handleJournal serves one day's journal entries. date defaults to today
// (UTC) when omitted; a malformed date is a 400, not a silent empty result.
func (s *Server) handleJournal(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	entries, err := journal.ReadDay(s.deps.Cfg.Storage.Dir, date)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "%s", err.Error())
		return
	}
	if entries == nil {
		entries = []journal.Entry{}
	}
	writeJSON(w, http.StatusOK, entries)
}
