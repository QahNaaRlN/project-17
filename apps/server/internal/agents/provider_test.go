package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }
func providerWith(fn transport) OpenRouter {
	return OpenRouter{Key: "server-only-test-secret", Client: &http.Client{Transport: fn}}
}
func TestOpenRouterBoundary(t *testing.T) { // AGT-040/043/050…052.
	p := providerWith(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://openrouter.ai/api/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer server-only-test-secret" {
			t.Fatal("provider boundary")
		}
		var request map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&request)
		if string(request["parallel_tool_calls"]) != "false" || string(request["max_tokens"]) != "2048" || string(request["model"]) != `"test/model"` {
			t.Fatal(string(request["model"]))
		}
		var routing struct {
			Parameters bool   `json:"require_parameters"`
			Collection string `json:"data_collection"`
			Fallbacks  bool   `json:"allow_fallbacks"`
		}
		json.Unmarshal(request["provider"], &routing)
		if !routing.Parameters || routing.Collection != "deny" || routing.Fallbacks {
			t.Fatal(routing)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"one","type":"function","function":{"name":"get_changeset","arguments":"{}"}}]}}],"usage":{"prompt_tokens":123,"completion_tokens":9}}`))}, nil
	})
	answer, err := p.Complete(context.Background(), "test/model", []Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: "untrusted prompt"}}, Tools())
	if err != nil || answer.Usage.Prompt != 123 || len(answer.Message.ToolCalls) != 1 {
		t.Fatal(answer, err)
	}
	if _, err = p.Complete(context.Background(), "bad\nmodel", nil, nil); !errors.Is(err, ErrProvider) {
		t.Fatal(err)
	}
	if _, err = p.Complete(context.Background(), "test/model", []Message{{Content: strings.Repeat("x", MaxContextBytes)}}, Tools()); !errors.Is(err, ErrProvider) {
		t.Fatal(err)
	}
	if _, err = (OpenRouter{}).Complete(context.Background(), "test/model", nil, nil); !errors.Is(err, ErrProvider) {
		t.Fatal(err)
	}
}
func TestOpenRouterUsageNeverDefaultsToZero(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var requests int
	http.DefaultTransport = transport(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://elsewhere.test/"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
	})
	if _, err := (OpenRouter{Key: "server-secret"}).Complete(context.Background(), "test/model", nil, nil); err != ErrUsageUnknown || requests != 1 {
		t.Fatal("followed provider redirect", requests, err)
	}
	for _, body := range []string{`{}`, `bad`, strings.Repeat("x", MaxContextBytes+1), `{"usage":{"prompt_tokens":1}}`, `{"usage":{"prompt_tokens":-1,"completion_tokens":2}}`} {
		p := providerWith(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		if _, err := p.Complete(context.Background(), "test/model", nil, nil); !errors.Is(err, ErrUsageUnknown) {
			t.Fatal(body, err)
		}
	}
	for _, p := range []OpenRouter{providerWith(func(*http.Request) (*http.Response, error) { return nil, errors.New("transport with secret") }), providerWith(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("secret"))}, nil
	})} {
		if _, err := p.Complete(context.Background(), "test/model", nil, nil); err != ErrUsageUnknown {
			t.Fatal(err)
		}
	}
	for _, message := range []string{`{}`, `{"role":"user"}`, `{"role":"assistant","tool_calls":[{"id":"x","type":"function","function":{"name":"x","arguments":"bad"}}]}`} {
		p := providerWith(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":` + message + `}],"usage":{"prompt_tokens":23,"completion_tokens":3}}`))}, nil
		})
		answer, err := p.Complete(context.Background(), "test/model", nil, nil)
		if err != ErrProvider || answer.Usage.Prompt != 23 {
			t.Fatal("known usage lost", answer, err)
		}
	}
}
