// Anthropic adapter (Messages API). Used for the stronger chat/escalation role
// per the plan's split. Anthropic separates the system prompt from the message
// list and returns content as a list of blocks, which is the main wire-shape
// difference from the OpenAI adapter.
package reasoner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const anthropicVersion = "2023-06-01"

// Anthropic speaks the Messages API.
type Anthropic struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewAnthropic builds the adapter. baseURL defaults to the public API root.
func NewAnthropic(apiKey, model, baseURL string) *Anthropic {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &Anthropic{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (a *Anthropic) Name() string { return "anthropic" }

type antMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type antRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []antMessage `json:"messages"`
}

type antResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete runs one Messages call.
func (a *Anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	system := req.System
	if system == "" {
		if req.Role == RoleChat {
			system = ChatSystemPrompt
		} else {
			system = SystemPrompt
		}
	}

	model := req.Model
	if model == "" {
		model = a.model
	}

	var msgs []antMessage
	if req.Role == RoleChat {
		for _, t := range req.ChatHistory {
			msgs = append(msgs, antMessage{Role: t.Role, Content: t.Text})
		}
		user := req.UserMessage
		if req.Context != "" {
			user = req.Context + "\n\n" + user
		}
		msgs = append(msgs, antMessage{Role: "user", Content: user})
	} else {
		msgs = append(msgs, antMessage{Role: "user", Content: BuildBatchPrompt(req.Digests, req.Context)})
	}

	body := antRequest{Model: model, MaxTokens: 4096, System: system, Messages: msgs}
	buf, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: read: %w", err)
	}

	var out antResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		// Non-JSON bodies happen on auth/proxy errors; surface status + snippet.
		return Response{}, fmt.Errorf("anthropic: http %d: %s", resp.StatusCode, bodySnippet(raw))
	}
	if out.Error != nil {
		return Response{}, fmt.Errorf("anthropic: api error (http %d): %s", resp.StatusCode, out.Error.Message)
	}
	if resp.StatusCode/100 != 2 {
		return Response{}, fmt.Errorf("anthropic: http %d: %s", resp.StatusCode, bodySnippet(raw))
	}
	var text string
	for _, c := range out.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	if text == "" {
		return Response{}, fmt.Errorf("anthropic: empty response")
	}
	return finishResponse(req, text, "anthropic", model)
}
