// Package telegram mirrors journal entries to a Telegram log channel via the Bot
// API (stdlib net/http, no SDK) and supports propose-then-confirm with inline
// approve/reject buttons. The journal mirror is an external record independent
// of the machine — if the daemon dies, the Telegram log survives.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
)

// Client is a minimal Telegram Bot API client.
type Client struct {
	token  string
	chatID string
	http   *http.Client

	// approveFn, if set, is invoked when a user taps "approve" on a proposed
	// candidate. The string is the candidate id encoded in the callback data.
	approveFn func(id string)
	rejectFn  func(id string)
}

// New builds a client. token and chatID come from config.
func New(token, chatID string) *Client {
	return &Client{
		token:  token,
		chatID: chatID,
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

// OnApprove / OnReject register inline-button handlers.
func (c *Client) OnApprove(fn func(id string)) { c.approveFn = fn }
func (c *Client) OnReject(fn func(id string))  { c.rejectFn = fn }

func (c *Client) api(method string) string {
	return fmt.Sprintf("https://api.telegram.org/bot%s/%s", c.token, method)
}

// Mirror subscribes to the journal topic and forwards every entry to Telegram.
// Candidate proposals get inline approve/reject buttons. Blocks until ctx done.
func (c *Client) Mirror(ctx context.Context, b *bus.Bus) {
	events := b.SubscribeJournal(256)
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			c.handle(ctx, ev)
		}
	}
}

func (c *Client) handle(ctx context.Context, ev bus.JournalEvent) {
	text := fmt.Sprintf("*%s* `%s`\n%s", escapeMD(ev.Coin), ev.Kind, escapeMD(ev.Summary))
	// Proposed candidates awaiting confirmation get inline buttons, keyed by
	// the id the executor's shared proposal registry assigned — embedded in
	// Summary as "id=<id>" so Telegram and the API resolve the same proposal.
	if ev.Kind == "alert" && ev.Verdict != nil {
		if id, ok := proposalID(ev.Summary); ok {
			c.sendWithButtons(ctx, text, id)
			return
		}
	}
	c.send(ctx, text)
}

// proposalID extracts the "id=<id>" token the executor embeds in a proposed
// candidate's journal summary.
func proposalID(summary string) (string, bool) {
	const marker = "id="
	idx := strings.Index(summary, marker)
	if idx < 0 {
		return "", false
	}
	rest := summary[idx+len(marker):]
	if end := strings.IndexByte(rest, ' '); end >= 0 {
		rest = rest[:end]
	}
	if rest == "" {
		return "", false
	}
	return rest, true
}

func (c *Client) send(ctx context.Context, text string) {
	form := url.Values{}
	form.Set("chat_id", c.chatID)
	form.Set("text", text)
	form.Set("parse_mode", "MarkdownV2")
	c.post(ctx, "sendMessage", form)
}

func (c *Client) sendWithButtons(ctx context.Context, text, id string) {
	kb := map[string]any{
		"inline_keyboard": [][]map[string]string{{
			{"text": "✅ Approve", "callback_data": "approve:" + id},
			{"text": "❌ Reject", "callback_data": "reject:" + id},
		}},
	}
	kbJSON, _ := json.Marshal(kb)
	form := url.Values{}
	form.Set("chat_id", c.chatID)
	form.Set("text", text)
	form.Set("parse_mode", "MarkdownV2")
	form.Set("reply_markup", string(kbJSON))
	c.post(ctx, "sendMessage", form)
}

func (c *Client) post(ctx context.Context, method string, form url.Values) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.api(method), bytes.NewBufferString(form.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// PollCallbacks long-polls getUpdates for inline-button taps and dispatches them
// to the registered handlers. Runs until ctx is cancelled.
func (c *Client) PollCallbacks(ctx context.Context) {
	var offset int64
	for {
		if ctx.Err() != nil {
			return
		}
		updates, next := c.getUpdates(ctx, offset)
		offset = next
		for _, u := range updates {
			if u.CallbackQuery == nil {
				continue
			}
			data := u.CallbackQuery.Data
			switch {
			case len(data) > 8 && data[:8] == "approve:":
				if c.approveFn != nil {
					c.approveFn(data[8:])
				}
			case len(data) > 7 && data[:7] == "reject:":
				if c.rejectFn != nil {
					c.rejectFn(data[7:])
				}
			}
			c.answerCallback(ctx, u.CallbackQuery.ID)
		}
	}
}

type update struct {
	UpdateID      int64 `json:"update_id"`
	CallbackQuery *struct {
		ID   string `json:"id"`
		Data string `json:"data"`
	} `json:"callback_query"`
}

func (c *Client) getUpdates(ctx context.Context, offset int64) ([]update, int64) {
	form := url.Values{}
	form.Set("timeout", "25")
	if offset > 0 {
		form.Set("offset", fmt.Sprint(offset))
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, c.api("getUpdates")+"?"+form.Encode(), nil)
	if err != nil {
		return nil, offset
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, offset
	}
	defer resp.Body.Close()
	var out struct {
		OK     bool     `json:"ok"`
		Result []update `json:"result"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return nil, offset
	}
	next := offset
	for _, u := range out.Result {
		if u.UpdateID >= next {
			next = u.UpdateID + 1
		}
	}
	return out.Result, next
}

func (c *Client) answerCallback(ctx context.Context, id string) {
	form := url.Values{}
	form.Set("callback_query_id", id)
	c.post(ctx, "answerCallbackQuery", form)
}

// escapeMD escapes MarkdownV2 special characters.
func escapeMD(s string) string {
	const special = "_*[]()~`>#+-=|{}.!"
	out := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		for j := 0; j < len(special); j++ {
			if ch == special[j] {
				out = append(out, '\\')
				break
			}
		}
		out = append(out, ch)
	}
	return string(out)
}
