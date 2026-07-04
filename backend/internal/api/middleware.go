package api

import "net/http"

// authMiddleware enforces bearer-token auth on every request when a token is
// configured. With no token configured (the loopback-only default), the API
// is open to anything that can reach the port — the config-level loopback
// guard is what keeps that safe.
//
// A browser's WebSocket client cannot set an Authorization header on the
// handshake request, so /api/ws also accepts the token as a ?token= query
// parameter; that check lives here (rather than only in the ws handler) so
// every route uniformly accepts either form.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.deps.Cfg.API.Token
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") == "Bearer "+token {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Query().Get("token") == token {
			next.ServeHTTP(w, r)
			return
		}
		writeErr(w, http.StatusUnauthorized, "unauthorized")
	})
}

// corsMiddleware allows the configured origins to call the API from a
// browser. It answers OPTIONS preflight requests directly (204, no body) and
// otherwise just decorates the response with the allow-origin header before
// handing off to the next handler. Browsers never send Authorization on a
// preflight request, so this must run outside authMiddleware, not inside it.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		w.Header().Set("Vary", "Origin")

		allowed := origin != "" && s.originAllowed(origin)
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}

		if r.Method == http.MethodOptions {
			if allowed {
				w.WriteHeader(http.StatusNoContent)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed reports whether origin is in Cfg.API.CORSOrigins.
func (s *Server) originAllowed(origin string) bool {
	for _, o := range s.deps.Cfg.API.CORSOrigins {
		if o == origin {
			return true
		}
	}
	return false
}
