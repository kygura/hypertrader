// Package api is the daemon's HTTP+WS surface: the unified backend core any
// frontend attaches to. It is a bus consumer like the TUI — subscribe, cache,
// serve — and never touches component internals.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/hyperagent/hyperagent/internal/batcher"
	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/config"
	"github.com/hyperagent/hyperagent/internal/executor"
	"github.com/hyperagent/hyperagent/internal/hlclient"
	"github.com/hyperagent/hyperagent/internal/ingestor"
	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/reasoner"
	"github.com/hyperagent/hyperagent/internal/store"
	"github.com/hyperagent/hyperagent/internal/thesis"
)

// Deps are the components the API surfaces — the same ones the TUI model
// holds. Engine and Exec may be nil (no chat provider configured, no signer
// wired up); handlers that need them degrade to 503 rather than panicking.
type Deps struct {
	Bus      *bus.Bus
	Store    *store.Store
	Engine   *reasoner.Engine   // nil → chat endpoint returns 503
	Exec     *executor.Executor // nil → execution endpoints return 503
	Ingestor *ingestor.Ingestor // nil → watchlist subscribe endpoint returns 503
	Batcher  *batcher.Batcher   // nil → watchlist track/untrack/scan endpoints return 503
	Cfg      config.Config
	Version  string

	RestClient *hlclient.Client // nil → thesis passthrough endpoint returns 503

	// Theses is the live thesis store backing GET /api/theses; nil serves an
	// empty snapshot (a daemon wired without the thesis pipeline, or tests).
	Theses *thesis.Store

	// SaveConfig persists a mutation to config.toml under the caller's own
	// guard (mutex + atomic write); nil disables persistence (settings still
	// apply live, just don't survive a restart). Settings/mode/key handlers
	// call it best-effort after applying the change in memory.
	SaveConfig func(apply func(*config.Config)) error

	// CfgSnapshot returns the current, live config value, synchronized with
	// whatever guards SaveConfig's writes. Cfg (above) is frozen at
	// construction time and is fine for the fields no handler ever mutates
	// (Markets, Timeframe, Storage, MarketData, API); reads that need to
	// observe a same-process SaveConfig mutation — currently just the
	// provider-key lookups in settings.go — must go through CfgSnapshot
	// instead so they don't serve a stale pre-mutation copy. May be nil in
	// tests that never touch those paths.
	//
	// A plain `return cfg` under the lock only copies the struct header:
	// reference-type fields (e.g. Providers.Custom, a map) would still alias
	// the live cfg that SaveConfig mutates, letting a reader touch that map
	// after the lock is released while a concurrent SaveConfig writes it —
	// an unsynchronized concurrent map access, which Go can detect at
	// runtime as a fatal error, not just a data race. main.go's
	// implementation closes this by deep-copying Providers.Custom while
	// still holding the lock, so the returned snapshot is safe to read
	// without holding any lock. This is a property of that specific
	// implementation, not a guarantee the config.Config type itself
	// enforces — a future reference-type field added to Config (or a new
	// call site reading one through CfgSnapshot) needs the same treatment.
	CfgSnapshot func() config.Config
}

// serverState is the single cache the background goroutine (runCaches) owns:
// connection/mode status, latest digest per coin, latest verdict per asset,
// and the registry of live WS clients. One goroutine writes the cache fields,
// request/WS handlers read and register — one RWMutex, no scattered locks
// across the read handlers.
type serverState struct {
	mu sync.RWMutex

	connected bool
	mode      string

	digests  map[string]metrics.Digest // coin -> latest digest
	verdicts []metrics.Verdict         // newest-first, latest per asset

	wsClients map[*wsClient]struct{}
}

// Server is the HTTP+WS surface. Construct with NewServer, obtain a wrapped
// http.Handler via Handler (for httptest or a real listener), or run it
// directly with ListenAndServe.
type Server struct {
	deps  Deps
	mux   *http.ServeMux
	state *serverState
}

// NewServer builds the route table and starts the cache goroutine that keeps
// serverState current from the bus. The returned Server is ready to serve;
// callers choose Handler() (for tests/embedding) or ListenAndServe (to bind).
func NewServer(d Deps) *Server {
	s := &Server{
		deps: d,
		mux:  http.NewServeMux(),
		state: &serverState{
			// Seeded from config rather than left to the bus: the daemon's one
			// Mode-carrying status event fires at startup, before any consumer
			// (this server included) can guarantee its subscription is live —
			// a bus miss would otherwise leave mode empty for the process
			// lifetime. Later StatusConn events still update it if it changes.
			mode:      d.Cfg.Execution.Mode,
			digests:   make(map[string]metrics.Digest),
			wsClients: make(map[*wsClient]struct{}),
		},
	}
	s.routes()
	go s.runCaches()
	return s
}

// routes registers every handler on the mux. Kept as one method so the route
// table reads as a single table of truth as endpoints are added task by task.
func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/markets", s.handleMarkets)
	s.mux.HandleFunc("GET /api/bars/{coin}", s.handleBars)
	s.mux.HandleFunc("GET /api/digests/{coin}", s.handleDigest)
	s.mux.HandleFunc("GET /api/verdicts", s.handleVerdicts)
	s.mux.HandleFunc("GET /api/journal", s.handleJournal)
	s.mux.HandleFunc("POST /api/chat", s.handleChat)
	s.mux.HandleFunc("GET /api/proposals", s.handleProposalsList)
	s.mux.HandleFunc("POST /api/proposals/{id}/approve", s.handleProposalApprove)
	s.mux.HandleFunc("POST /api/proposals/{id}/reject", s.handleProposalReject)
	s.mux.HandleFunc("POST /api/orders", s.handleOrders)
	s.mux.HandleFunc("DELETE /api/orders/{coin}/{oid}", s.handleCancelOrder)
	s.mux.HandleFunc("GET /api/ws", s.handleWS)
	s.mux.HandleFunc("POST /api/watchlist/subscribe", s.handleWatchlistSubscribe)
	s.mux.HandleFunc("POST /api/watchlist/track", s.handleWatchlistTrack)
	s.mux.HandleFunc("POST /api/watchlist/untrack", s.handleWatchlistUntrack)
	s.mux.HandleFunc("POST /api/watchlist/scan", s.handleWatchlistScan)
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	s.mux.HandleFunc("PUT /api/execution/mode", s.handlePutMode)
	s.mux.HandleFunc("PUT /api/providers/{name}/key", s.handlePutProviderKey)
	s.mux.HandleFunc("GET /api/thesis/{coin}", s.handleThesis)
	s.mux.HandleFunc("GET /api/theses", s.handleTheses)
}

// runCaches is the single owner of serverState: it subscribes once per topic
// and never blocks a producer (the bus already guarantees non-blocking
// publish; buffered channels here just give the goroutine slack to drain).
// It also fans every event it consumes out to registered WS clients (Task 6)
// — one subscription set, two consumers (the request-handler caches and the
// WS broadcast), rather than a second parallel subscription goroutine.
func (s *Server) runCaches() {
	statusCh := s.deps.Bus.SubscribeStatus(8)
	digestsCh := s.deps.Bus.SubscribeDigests(16)
	verdictsCh := s.deps.Bus.SubscribeVerdicts(16)
	barsCh := s.deps.Bus.SubscribeBars(32)
	journalCh := s.deps.Bus.SubscribeJournal(32)
	midsCh := s.deps.Bus.SubscribeMids(32)
	thesesCh := s.deps.Bus.SubscribeTheses(16)
	for {
		select {
		case ev, ok := <-statusCh:
			if !ok {
				return
			}
			s.state.mu.Lock()
			if ev.Kind == bus.StatusConn {
				s.state.connected = ev.Connected
			}
			if ev.Mode != "" {
				s.state.mode = ev.Mode
			}
			s.state.mu.Unlock()
			s.broadcast("status", ev)
		case d, ok := <-digestsCh:
			if !ok {
				return
			}
			s.state.mu.Lock()
			s.state.digests[d.Coin] = d
			s.state.mu.Unlock()
		case v, ok := <-verdictsCh:
			if !ok {
				return
			}
			s.state.mu.Lock()
			kept := s.state.verdicts[:0:0]
			for _, existing := range s.state.verdicts {
				if existing.Asset != v.Asset {
					kept = append(kept, existing)
				}
			}
			s.state.verdicts = append([]metrics.Verdict{v}, kept...)
			s.state.mu.Unlock()
			s.broadcast("verdict", v)
		case bar, ok := <-barsCh:
			if !ok {
				return
			}
			s.broadcast("bar", bar)
		case je, ok := <-journalCh:
			if !ok {
				return
			}
			s.broadcast("journal", je)
		case mids, ok := <-midsCh:
			if !ok {
				return
			}
			s.broadcast("mids", mids)
		case t, ok := <-thesesCh:
			if !ok {
				return
			}
			// An empty Direction (Version-0 tombstone) tells clients the
			// coin's thesis was invalidated; the snapshot endpoint stays the
			// authority on reconnect.
			s.broadcast("thesis", t)
		}
	}
}

// handleHealth reports connection state, execution mode, active providers,
// and the daemon version — the one endpoint a frontend polls to know whether
// the core is alive at all.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.state.mu.RLock()
	connected := s.state.connected
	mode := s.state.mode
	s.state.mu.RUnlock()

	var batchProvider, chatProvider string
	if s.deps.Engine != nil {
		reg := s.deps.Engine.Registry()
		batchProvider, _ = reg.Active(reasoner.RoleBatch)
		chatProvider, _ = reg.Active(reasoner.RoleChat)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"connected": connected,
		"mode":      mode,
		"providers": map[string]string{
			"batch": batchProvider,
			"chat":  chatProvider,
		},
		"version": s.deps.Version,
	})
}

// Handler returns the auth+CORS-wrapped mux — what httptest binds to, and
// what ListenAndServe hands to the underlying http.Server.
func (s *Server) Handler() http.Handler {
	return s.corsMiddleware(s.authMiddleware(s.mux))
}

// ListenAndServe binds Cfg.API.Addr and serves until ctx is cancelled, then
// shuts down gracefully. It honors ctx the way the daemon's run() expects:
// callers can `go srv.ListenAndServe(ctx)` and rely on cancellation to unwind.
func (s *Server) ListenAndServe(ctx context.Context) error {
	httpSrv := &http.Server{
		Addr:    s.deps.Cfg.API.Addr,
		Handler: s.Handler(),
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpSrv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// writeJSON encodes v as the response body with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr emits the standard {"error":"..."} envelope every handler in this
// package uses for non-2xx responses.
func writeErr(w http.ResponseWriter, code int, format string, args ...any) {
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf(format, args...)})
}
