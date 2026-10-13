package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"
)

const MaxCompletionTokens = 2048
const MaxContextBytes = 64 << 10

var ErrProvider = errors.New("AGENT_PROVIDER_FAILED")
var ErrUsageUnknown = errors.New("AGENT_USAGE_UNKNOWN")
var modelName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]{0,199}$`)

func validModel(s string) bool { return modelName.MatchString(s) }

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}
type Usage struct {
	Prompt     int `json:"prompt_tokens"`
	Completion int `json:"completion_tokens"`
}
type Answer struct {
	Message Message
	Usage   Usage
}
type Provider interface {
	Complete(context.Context, string, []Message, []Tool) (Answer, error)
	Ready() bool
}

// Endpoint is fixed in production. Client injection is only for HTTP boundary tests.
type OpenRouter struct {
	Key    string
	Client *http.Client
}

func (p OpenRouter) Ready() bool { return p.Key != "" }
func (p OpenRouter) Complete(ctx context.Context, model string, messages []Message, tools []Tool) (Answer, error) {
	if !p.Ready() || !validModel(model) {
		return Answer{}, ErrProvider
	}
	functions := []map[string]any{}
	for _, t := range tools {
		functions = append(functions, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.InputSchema}})
	}
	request := map[string]any{"model": model, "messages": messages, "tools": functions, "parallel_tool_calls": false, "max_tokens": MaxCompletionTokens, "provider": map[string]any{"require_parameters": true, "data_collection": "deny", "allow_fallbacks": false}}
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > MaxContextBytes {
		return Answer{}, ErrProvider
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Answer{}, ErrProvider
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.Key)
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(req)
	if err != nil {
		return Answer{}, ErrUsageUnknown
	}
	defer response.Body.Close()
	// Do not expose provider error bodies, which may echo credentials or prompts.
	if response.StatusCode != 200 {
		return Answer{}, ErrUsageUnknown
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxContextBytes+1))
	if err != nil || len(body) > MaxContextBytes {
		return Answer{}, ErrUsageUnknown
	}
	var value struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
		Usage *struct {
			Prompt     *int `json:"prompt_tokens"`
			Completion *int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &value) != nil || value.Usage == nil || value.Usage.Prompt == nil || value.Usage.Completion == nil || *value.Usage.Prompt < 0 || *value.Usage.Completion < 0 {
		return Answer{}, ErrUsageUnknown
	}
	usage := Usage{Prompt: *value.Usage.Prompt, Completion: *value.Usage.Completion}
	if len(value.Choices) != 1 || value.Choices[0].Message.Role != "assistant" || len(value.Choices[0].Message.ToolCalls) > 1 || len(value.Choices[0].Message.Content) > 16000 || usage.Completion > MaxCompletionTokens {
		return Answer{Usage: usage}, ErrProvider
	}
	message := value.Choices[0].Message
	for _, call := range message.ToolCalls {
		if call.ID == "" || len(call.ID) > 200 || call.Type != "function" || !json.Valid([]byte(call.Function.Arguments)) {
			return Answer{Usage: usage}, ErrProvider
		}
	}
	return Answer{Message: message, Usage: usage}, nil
}
