package cockpit

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

func testModel() *Model {
	cache := apiclient.NewCache()
	return New(Config{
		Cache: cache,
		Settings: apiclient.SettingsResponse{
			Mode:       "propose",
			Visualized: []string{"ETH", "BTC"},
			Timeframes: map[string]string{"ETH": "1h"},
			Risk: apiclient.RiskSettings{
				MaxPositionUSD: 5000, MaxTotalExposureUSD: 10000,
				MaxConcurrent: 3, DailyLossKillUSD: 500,
			},
		},
	})
}

func TestJournalMsgAppendsTaggedEntry(t *testing.T) {
	m := testModel()
	m.Update(journalMsg{Coin: "ETH", Kind: "fill", Summary: "0.85 ETH filled"})
	if len(m.journal) != 1 {
		t.Fatalf("journal len = %d, want 1", len(m.journal))
	}
	if m.journal[0].tag != "FILL" {
		t.Errorf("tag = %q, want FILL", m.journal[0].tag)
	}
	if m.phase != "FILL" {
		t.Errorf("phase = %q, want FILL", m.phase)
	}
}

func TestStatusConnUpdatesConnected(t *testing.T) {
	m := testModel()
	m.Update(statusMsg{Kind: statusConn, Connected: true})
	if !m.connected {
		t.Error("connected not set")
	}
	m.Update(statusMsg{Kind: statusConn, Connected: false})
	if m.connected {
		t.Error("connected not cleared")
	}
}

func TestStatusNoticeUpdatesModeNotConn(t *testing.T) {
	m := testModel()
	m.connected = true
	m.Update(statusMsg{Kind: statusNotice, Mode: "autonomous", Detail: "mode → autonomous"})
	if m.mode != "autonomous" {
		t.Errorf("mode = %q, want autonomous", m.mode)
	}
	if !m.connected {
		t.Error("notice must not touch connection state")
	}
}

func TestVerdictMsgAppendsReason(t *testing.T) {
	m := testModel()
	m.Update(verdictMsg{Asset: "ETH", Action: apiclient.ActionOpenLong, Confidence: 0.7, Thesis: "funding favors bids"})
	if len(m.journal) != 1 || m.journal[0].tag != "REASON" {
		t.Fatalf("verdict not journaled as REASON: %+v", m.journal)
	}
}

func TestQuitKey(t *testing.T) {
	m := testModel()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q should produce a quit command")
	}
}

func TestSlashOpensChat(t *testing.T) {
	m := testModel()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.chatOpen {
		t.Error("/ should open chat")
	}
}
