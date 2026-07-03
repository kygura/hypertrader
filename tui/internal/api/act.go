package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hyperagent/hyperagent/internal/metrics"
	"github.com/hyperagent/hyperagent/internal/reasoner"
)

// chatRequest is the wire shape for POST /api/chat.
type chatRequest struct {
	Message string     `json:"message"`
	History []chatTurn `json:"history"`
}

// chatTurn mirrors reasoner.ChatTurn for the wire; the JSON field names
// ("role", "text") match the engine's ChatTurn field names lowercased.
type chatTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// handleChat runs one interactive completion via the chat-role provider, using
// the same context-grounding helper (reasoner.BuildChatContext) the TUI chat
// pane uses so the API agent answers from the same normalized read.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.deps.Engine == nil {
		writeErr(w, http.StatusServiceUnavailable, "chat not configured (no provider)")
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body: %s", err.Error())
		return
	}
	if req.Message == "" {
		writeErr(w, http.StatusBadRequest, "message is required")
		return
	}

	history := make([]reasoner.ChatTurn, 0, len(req.History))
	for _, t := range req.History {
		history = append(history, reasoner.ChatTurn{Role: t.Role, Text: t.Text})
	}

	contextText := s.chatContext()

	reply, err := s.deps.Engine.Chat(r.Context(), req.Message, history, contextText)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "chat provider error: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"reply":    reply,
		"provider": s.deps.Engine.ChatProviderName(),
		"model":    s.deps.Engine.ChatModel(),
	})
}

// chatContext builds the grounding text for /api/chat from whatever the store
// currently has for the first tracked coin — the API has no notion of a
// "selected asset" the way the TUI does, so it grounds on the primary tracked
// market. Empty tracked list or no data yet yields an empty context, same as
// the TUI when nothing is selected.
func (s *Server) chatContext() string {
	if len(s.deps.Cfg.Markets.Tracked) == 0 {
		return ""
	}
	coin := s.deps.Cfg.Markets.Tracked[0]
	tf := s.deps.Cfg.Timeframe.For(coin)
	return reasoner.BuildChatContext(s.deps.Store, coin, tf)
}

// handleProposalsList serves the pending propose-mode candidates.
func (s *Server) handleProposalsList(w http.ResponseWriter, r *http.Request) {
	if s.deps.Exec == nil {
		writeErr(w, http.StatusServiceUnavailable, "execution not configured (no signer)")
		return
	}
	writeJSON(w, http.StatusOK, s.deps.Exec.Proposals().List())
}

// handleProposalApprove resolves a pending proposal by id and runs it through
// the risk gates + submit path.
func (s *Server) handleProposalApprove(w http.ResponseWriter, r *http.Request) {
	if s.deps.Exec == nil {
		writeErr(w, http.StatusServiceUnavailable, "execution not configured (no signer)")
		return
	}
	id := r.PathValue("id")
	if err := s.deps.Exec.Approve(r.Context(), id); err != nil {
		if err.Error() == "no such proposal" {
			writeErr(w, http.StatusNotFound, "%s", err.Error())
			return
		}
		writeErr(w, http.StatusUnprocessableEntity, "%s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "submitted"})
}

// handleProposalReject resolves a pending proposal by id without submitting.
func (s *Server) handleProposalReject(w http.ResponseWriter, r *http.Request) {
	if s.deps.Exec == nil {
		writeErr(w, http.StatusServiceUnavailable, "execution not configured (no signer)")
		return
	}
	id := r.PathValue("id")
	if err := s.deps.Exec.Reject(id); err != nil {
		writeErr(w, http.StatusNotFound, "%s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// orderRequest is the wire shape for POST /api/orders — a direct human/agent
// command, same semantics as the MCP place_order tool (mirrors
// tui/src/mcp.go's place_order handler).
type orderRequest struct {
	Coin       string  `json:"coin"`
	Action     string  `json:"action"`
	SizeUSD    float64 `json:"size_usd"`
	OrderType  string  `json:"order_type"`
	Price      float64 `json:"price"`
	Stop       float64 `json:"stop"`
	TakeProfit float64 `json:"take_profit"`
	Thesis     string  `json:"thesis"`
}

// handleOrders places a direct order: the caller (a human, or an agent acting
// on an explicit HTTP call) IS the confirmation, so it runs straight through
// Exec.Execute (risk gates still apply — no caller bypasses them).
func (s *Server) handleOrders(w http.ResponseWriter, r *http.Request) {
	if s.deps.Exec == nil {
		writeErr(w, http.StatusServiceUnavailable, "no HL_AGENT_KEY configured")
		return
	}
	var a orderRequest
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body: %s", err.Error())
		return
	}
	if a.OrderType == "" {
		if a.Price > 0 {
			a.OrderType = "limit"
		} else {
			a.OrderType = "market"
		}
	}
	thesis := a.Thesis
	if thesis == "" {
		thesis = "api direct order"
	}
	v := reasoner.Verdict{
		Asset:      strings.ToUpper(a.Coin),
		Timeframe:  "api",
		Action:     metrics.Action(a.Action),
		SizeUSD:    a.SizeUSD,
		Entry:      metrics.Entry{Type: a.OrderType, Price: a.Price},
		Stop:       a.Stop,
		TakeProfit: a.TakeProfit,
		Thesis:     thesis,
		Confidence: 1,
		At:         time.Now(),
		Provider:   "api",
	}
	if err := s.deps.Exec.Execute(r.Context(), v); err != nil {
		if isGateError(err) {
			writeErr(w, http.StatusUnprocessableEntity, "%s", err.Error())
			return
		}
		writeErr(w, http.StatusBadGateway, "%s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "submitted"})
}

// handleCancelOrder cancels one resting order by coin + order id.
func (s *Server) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	if s.deps.Exec == nil {
		writeErr(w, http.StatusServiceUnavailable, "no HL_AGENT_KEY configured")
		return
	}
	coin := strings.ToUpper(r.PathValue("coin"))
	oid, err := strconv.ParseUint(r.PathValue("oid"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid oid: %s", err.Error())
		return
	}
	if err := s.deps.Exec.Cancel(r.Context(), coin, oid); err != nil {
		writeErr(w, http.StatusBadGateway, "%s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// isGateError distinguishes a risk-gate rejection or malformed-verdict error
// (422 — the request was well-formed HTTP but the code-enforced limits or
// schema said no) from an upstream submit failure (502 — signer/exchange
// problem). Execute wraps risk-gate errors as "risk gate: %w"; verdict.Validate
// errors surface as its own plain "verdict: ..." messages; a non-trade action
// surfaces as "... is not executable". All three are client-caused, not server
// failures, so all three count as gate errors here.
func isGateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "risk gate:") ||
		strings.HasPrefix(msg, "verdict:") ||
		strings.Contains(msg, "is not executable")
}
