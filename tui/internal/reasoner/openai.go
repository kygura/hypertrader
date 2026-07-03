// OpenAI-compatible adapter. One implementation covers OpenAI proper and any
// base-URL-swappable endpoint (Deepseek, local vLLM, OpenRouter, ...). They
// differ only in base_url + model + key — exactly the plan's design.
package reasoner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompatible speaks the chat-completions wire format.
type OpenAICompatible struct {
	name    string
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewOpenAICompatible builds an adapter. name is used for status/journaling
// (e.g. "openai" or "deepseek"); baseURL must include the version path, e.g.
// "https://api.deepseek.com/v1".
func NewOpenAICompatible(name, apiKey, model, baseURL string) *OpenAICompatible {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &OpenAICompatible{
		name:    name,
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 90 * time.Second},
	}
}

func (o *OpenAICompatible) Name() string { return o.name }

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Temperature float64      `json:"temperature"`
}

type oaiResponse struct {
	Choices []struct {
		Message oaiMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete runs one chat completion, then parses verdicts (batch) or returns the
// reply text (chat).
func (o *OpenAICompatible) Complete(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = o.model
	}
	msgs := buildMessages(req)
	body := oaiRequest{Model: model, Messages: msgs, Temperature: 0.4}
	buf, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", o.name, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, fmt.Errorf("%s: read: %w", o.name, err)
	}

	var out oaiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		// Non-JSON bodies happen on auth/proxy errors; surface status + snippet.
		return Response{}, fmt.Errorf("%s: http %d: %s", o.name, resp.StatusCode, bodySnippet(raw))
	}
	if out.Error != nil {
		return Response{}, fmt.Errorf("%s: api error (http %d): %s", o.name, resp.StatusCode, out.Error.Message)
	}
	if resp.StatusCode/100 != 2 {
		return Response{}, fmt.Errorf("%s: http %d: %s", o.name, resp.StatusCode, bodySnippet(raw))
	}
	if len(out.Choices) == 0 {
		return Response{}, fmt.Errorf("%s: empty response", o.name)
	}
	text := out.Choices[0].Message.Content
	return finishResponse(req, text, o.name, model)
}

// bodySnippet trims an error body to a single short line for log/error text.
func bodySnippet(raw []byte) string {
	s := strings.Join(strings.Fields(string(raw)), " ")
	if s == "" {
		return "(empty body)"
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// buildMessages turns a Request into chat messages for OpenAI-style APIs.
func buildMessages(req Request) []oaiMessage {
	system := req.System
	if system == "" {
		if req.Role == RoleChat {
			system = ChatSystemPrompt
		} else {
			system = SystemPrompt
		}
	}
	msgs := []oaiMessage{{Role: "system", Content: system}}
	if req.Role == RoleChat {
		for _, t := range req.ChatHistory {
			msgs = append(msgs, oaiMessage{Role: t.Role, Content: t.Text})
		}
		user := req.UserMessage
		if req.Context != "" {
			user = req.Context + "\n\n" + user
		}
		msgs = append(msgs, oaiMessage{Role: "user", Content: user})
	} else {
		msgs = append(msgs, oaiMessage{Role: "user", Content: BuildBatchPrompt(req.Digests, req.Context)})
	}
	return msgs
}

// finishResponse turns model text into the appropriate Response for the role.
func finishResponse(req Request, text, provider, model string) (Response, error) {
	if req.Role == RoleChat {
		return Response{Reply: text, Model: model}, nil
	}
	verdicts, err := ParseVerdicts(text, provider)
	if err != nil {
		return Response{Model: model}, err
	}
	return Response{Verdicts: verdicts, Model: model}, nil
}
