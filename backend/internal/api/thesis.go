package api

import (
	"net/http"

	"github.com/hyperagent/hyperagent/internal/thesis"
)

// handleThesis is a passthrough: it wraps thesis.FetchContext so a client
// with no direct Hyperliquid access (the standalone TUI) can still get the
// multi-timeframe grounding text for its /g thesis command. tf defaults to
// the coin's configured timeframe when the caller omits ?tf.
func (s *Server) handleThesis(w http.ResponseWriter, r *http.Request) {
	if s.deps.RestClient == nil {
		writeErr(w, http.StatusServiceUnavailable, "hl rest client not configured")
		return
	}
	coin := r.PathValue("coin")
	tf := r.URL.Query().Get("tf")
	if tf == "" {
		tf = s.deps.Cfg.Timeframe.For(coin)
	}
	ctx, err := thesis.FetchContext(r.Context(), s.deps.RestClient, coin, tf)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "%s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"context": ctx})
}
