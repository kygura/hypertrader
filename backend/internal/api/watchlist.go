package api

import (
	"encoding/json"
	"net/http"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

type subscribeRequest struct {
	Coins []string `json:"coins"`
}

// handleWatchlistSubscribe opens live feeds for new visualized coins.
func (s *Server) handleWatchlistSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.deps.Ingestor == nil {
		writeErr(w, http.StatusServiceUnavailable, "ingestor not configured")
		return
	}
	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	s.deps.Ingestor.Subscribe(req.Coins...)
	w.WriteHeader(http.StatusNoContent)
}

type trackRequest struct {
	Coin      string `json:"coin"`
	Timeframe string `json:"timeframe"`
}

// handleWatchlistTrack adds a coin to the batcher's reasoned-over set.
// RequiresConfirmation reads Exec.Mode() live (nil Exec => always confirm)
// rather than a snapshot taken once at startup, so a coin tracked after a
// live mode switch never carries a stale confirm flag.
func (s *Server) handleWatchlistTrack(w http.ResponseWriter, r *http.Request) {
	if s.deps.Batcher == nil {
		writeErr(w, http.StatusServiceUnavailable, "batcher not configured")
		return
	}
	var req trackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Coin == "" || req.Timeframe == "" {
		writeErr(w, http.StatusBadRequest, "coin and timeframe are required")
		return
	}
	confirm := true
	if s.deps.Exec != nil {
		confirm = s.deps.Exec.Mode() != "autonomous"
	}
	s.deps.Batcher.Track(metrics.AssetStrategy{
		Coin:                 req.Coin,
		Timeframe:            req.Timeframe,
		RequiresConfirmation: confirm,
		MaxPositionUSD:       s.deps.Cfg.Execution.MaxPositionUSD,
		MaxPositionPct:       s.deps.Cfg.Execution.MaxPositionPct,
	})
	w.WriteHeader(http.StatusNoContent)
}

type untrackRequest struct {
	Coin string `json:"coin"`
}

func (s *Server) handleWatchlistUntrack(w http.ResponseWriter, r *http.Request) {
	if s.deps.Batcher == nil {
		writeErr(w, http.StatusServiceUnavailable, "batcher not configured")
		return
	}
	var req untrackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Coin == "" {
		writeErr(w, http.StatusBadRequest, "coin is required")
		return
	}
	s.deps.Batcher.Untrack(req.Coin)
	w.WriteHeader(http.StatusNoContent)
}

type scanRequest struct {
	Coins []string `json:"coins"`
}

func (s *Server) handleWatchlistScan(w http.ResponseWriter, r *http.Request) {
	if s.deps.Batcher == nil {
		writeErr(w, http.StatusServiceUnavailable, "batcher not configured")
		return
	}
	var req scanRequest
	// Empty/missing body is valid: it means "scan everything tracked."
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.deps.Batcher.Scan(req.Coins...)
	w.WriteHeader(http.StatusNoContent)
}
