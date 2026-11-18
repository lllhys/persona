package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/modelhttp"
)

func TestUserAgentDirectRequests(t *testing.T) {
	custom, empty := "ExampleAgent/1.0", ""
	for _, api := range []string{"messages", "responses", "chat"} {
		for _, tc := range []struct {
			name    string
			agent   *string
			want    string
			present bool
		}{
			{name: "default", want: modelhttp.UserAgent(), present: true},
			{name: "custom", agent: &custom, want: custom, present: true},
			{name: "empty", agent: &empty},
		} {
			t.Run(api+"/"+tc.name, func(t *testing.T) {
				calls := 0
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					values, present := r.Header["User-Agent"]
					if present != tc.present || r.Header.Get("User-Agent") != tc.want {
						t.Errorf("User-Agent = %q, present = %v; want %q, %v", values, present, tc.want, tc.present)
					}
					w.WriteHeader(http.StatusBadRequest)
				}))
				defer s.Close()
				_, err := New(Config{API: api, BaseURL: s.URL, APIKey: "example-key", Model: "model", UserAgent: tc.agent}).Complete(context.Background(), "system", "hello", 8)
				if err == nil || calls != 1 {
					t.Fatalf("expected one rejected request, calls=%d err=%v", calls, err)
				}
			})
		}
	}
}

func TestChatCompletionsStreaming(t *testing.T) {
	for _, base := range []string{"/prefix", "/prefix/v1/"} {
		t.Run(base, func(t *testing.T) {
			var request struct {
				Model     string    `json:"model"`
				Messages  []Message `json:"messages"`
				Stream    bool      `json:"stream"`
				MaxTokens int       `json:"max_tokens"`
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/prefix/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer example-key" || r.Header.Get("Accept") != "text/event-stream" {
					t.Errorf("unexpected request: %s, headers %v", r.URL.Path, r.Header)
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				fmt.Fprint(w, ": keepalive\r\n\r\n")
				fmt.Fprint(w, "data: {\"model\":\"model-v2\",\r\ndata: \"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"secret\",\"content\":\"Hello\"}}]}\r\n\r\n")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":1,\"delta\":{\"content\":\"other\"}},{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":\"length\"}]}\n\n")
				fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3,\"prompt_tokens_details\":{\"cached_tokens\":4}}}\n\ndata: [DONE]\n\n")
			}))
			defer s.Close()
			turns := []Message{{RoleUser, "first"}, {RoleAssistant, "second"}, {RoleUser, "third"}}
			reply, err := New(Config{API: "chat", BaseURL: s.URL + base, APIKey: "example-key", Model: "model"}).Chat(context.Background(), Request{System: "system", Messages: turns, MaxTokens: 100})
			if err != nil {
				t.Fatal(err)
			}
			wantTurns := append([]Message{{Role: "system", Content: "system"}}, turns...)
			if request.Model != "model" || !request.Stream || request.MaxTokens != 100 || !reflect.DeepEqual(request.Messages, wantTurns) {
				t.Fatalf("request: %+v", request)
			}
			want := Reply{Text: "Hello world", Model: "model-v2", Truncated: true, Usage: Usage{InputTokens: 8, OutputTokens: 3, CacheReadInputTokens: 4}}
			if reply != want {
				t.Fatalf("reply = %+v, want %+v", reply, want)
			}
		})
	}
}

func TestChatCompletionsReasoningEffort(t *testing.T) {
	for _, tc := range []struct {
		name, effort, want string
	}{
		{name: "unset preserves the old body"},
		{name: "explicit", effort: "high", want: "high"},
		{name: "off", effort: "off", want: "none"},
		{name: "none", effort: "none", want: "none"},
		{name: "disabled", effort: "disabled", want: "none"},
		{name: "max passes through like Responses", effort: "max", want: "max"},
		{name: "unknown level is left to upstream like Responses", effort: "ultra", want: "ultra"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var raw []byte
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var err error
				raw, err = io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer s.Close()
			_, err := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model", ReasoningEffort: tc.effort}).Chat(context.Background(),
				Request{Messages: []Message{{RoleUser, "hi"}}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.effort == "" {
				want := `{"messages":[{"role":"user","content":"hi"}],"model":"model","stream":true,"stream_options":{"include_usage":true}}`
				if string(raw) != want {
					t.Fatalf("body changed without effort:\n got %s\nwant %s", raw, want)
				}
				return
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			if body["reasoning_effort"] != tc.want {
				t.Fatalf("reasoning_effort = %v, want %q; body %v", body["reasoning_effort"], tc.want, body)
			}
		})
	}
}

func TestChatRejectsReasoningEffort(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"unsupported parameter", `{"error":{"message":"Unsupported parameter: 'reasoning_effort' is not supported with this model."}}`, true},
		{"numeric code preserves field rejection", `{"error":{"code":40010,"message":"Unsupported parameter: 'reasoning_effort'"}}`, true},
		{"malformed code preserves field rejection", `{"error":{"code":{"bad":true},"message":"Unsupported parameter: 'reasoning_effort'"}}`, true},
		{"top-level unknown field code allows fallback", `{"code":"unknown_parameter","error":{"message":"Unsupported parameter: 'reasoning_effort'"}}`, true},
		{"top-level ceiling code vetoes fallback", `{"code":"reasoning_effort_above_ceiling","error":{"message":"Unsupported parameter","param":"reasoning_effort"}}`, false},
		{"nested field code beats top-level ceiling", `{"code":"reasoning_effort_above_ceiling","error":{"code":"unsupported_parameter","message":"Unsupported parameter","param":"reasoning_effort"}}`, true},
		{"top-level unsupported value code vetoes fallback", `{"code":"unsupported_value","error":{"message":"Unsupported parameter","param":"reasoning_effort"}}`, false},
		{"top-level invalid value code vetoes fallback", `{"code":"invalid_value","error":{"message":"Unsupported parameter","param":"reasoning_effort"}}`, false},
		{"parameter named by param", `{"error":{"message":"Unsupported parameter","param":"reasoning_effort"}}`, true},
		{"unknown argument", `{"error":{"message":"Unrecognized request argument supplied: reasoning_effort"}}`, true},
		{"extra field", `{"detail":[{"loc":["body","reasoning_effort"],"msg":"Extra inputs are not permitted"}]}`, true},
		{"field not supported", `{"error":{"message":"reasoning_effort is not supported"}}`, true},
		{"unsupported value", `{"error":{"message":"Unsupported value: 'max'. Supported values are: 'low', 'high'.","param":"reasoning_effort"}}`, false},
		{"unsupported named level", `{"error":{"message":"unsupported reasoning_effort \"ultra\""}}`, false},
		{"operator ceiling", `{"error":{"code":"reasoning_effort_above_ceiling","message":"reasoning_effort high exceeds the ceiling"}}`, false},
		{"ceiling code wins over generic message", `{"error":{"code":"reasoning_effort_above_ceiling","message":"Unsupported parameter","param":"reasoning_effort"}}`, false},
		{"invalid value code wins over generic message", `{"error":{"code":"invalid_value","message":"Unsupported parameter","param":"reasoning_effort"}}`, false},
		{"invalid value", `{"error":{"message":"reasoning_effort must be a non-empty string"}}`, false},
		{"other field", `{"error":{"message":"Unsupported parameter: stream_options"}}`, false},
		{"echoed effort does not associate another field's error", `{"error":{"message":"Unsupported parameter: max_tokens","param":"max_tokens","details":{"reasoning_effort":"high"}}}`, false},
		{"unassociated details do not associate a generic error", `{"error":{"message":"Unsupported parameter","details":{"reasoning_effort":"high"}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChatRejectsReasoningEffort([]byte(tc.body)); got != tc.want {
				t.Fatalf("ChatRejectsReasoningEffort() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChatRejectsNoEffortValue(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"none unsupported with allowed levels", `{"error":{"message":"Unsupported value: 'none' is not supported with this model. Supported values are: 'minimal', 'low', 'medium', 'high'.","param":"reasoning_effort"}}`, true},
		{"none named as unsupported", `{"error":{"message":"unsupported reasoning_effort \"none\""}}`, true},
		{"pydantic enum rejection", `{"detail":[{"loc":["body","reasoning_effort"],"msg":"Input should be 'minimal', 'low', 'medium' or 'high'","type":"enum"}]}`, true},
		{"generic invalid value code for the field", `{"error":{"code":"invalid_value","message":"Unsupported parameter","param":"reasoning_effort"}}`, true},
		{"numeric code preserves off fallback", `{"error":{"code":40011,"message":"unsupported reasoning_effort \"none\""}}`, true},
		{"malformed code preserves off fallback", `{"error":{"code":{"bad":true},"message":"unsupported reasoning_effort \"none\""}}`, true},
		{"policy code vetoes off fallback", `{"error":{"code":"reasoning_effort_above_ceiling","message":"unsupported reasoning_effort \"none\""}}`, false},
		{"top-level policy code vetoes off fallback", `{"code":"reasoning_effort_above_ceiling","error":{"message":"unsupported reasoning_effort \"none\""}}`, false},
		{"nested off-value code beats top-level policy", `{"code":"reasoning_effort_above_ceiling","error":{"code":"unsupported_value","message":"unsupported reasoning_effort \"none\""}}`, true},
		{"different named level without field association", `{"error":{"message":"unsupported reasoning_effort \"high\""}}`, false},
		{"ceiling still wins", `{"error":{"code":"reasoning_effort_above_ceiling","message":"Unsupported parameter","param":"reasoning_effort"}}`, false},
		{"ceiling message wins over a value code", `{"error":{"code":"unsupported_value","message":"reasoning_effort_above_ceiling","param":"reasoning_effort"}}`, false},
		{"other field", `{"error":{"message":"Unsupported value: 'none'","param":"max_tokens"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChatRejectsNoEffortValue([]byte(tc.body)); got != tc.want {
				t.Fatalf("ChatRejectsNoEffortValue() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChatCompletionsOffPydanticEnumRetry(t *testing.T) {
	var requests []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, body)
		if body["reasoning_effort"] == "none" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"detail":[{"loc":["body","reasoning_effort"],"msg":"Input should be 'minimal', 'low', 'medium' or 'high'","type":"enum"}]}`)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer s.Close()
	client := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model", ReasoningEffort: "off"})
	for i := 0; i < 2; i++ {
		if _, err := client.Chat(context.Background(), Request{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(requests) != 3 || requests[0]["reasoning_effort"] != "none" {
		t.Fatalf("requests = %v", requests)
	}
	for _, i := range []int{1, 2} {
		if _, present := requests[i]["reasoning_effort"]; present {
			t.Fatalf("request %d should omit none: %v", i+1, requests[i])
		}
	}
}

func TestChatCompletionsReasoningEffortRetry(t *testing.T) {
	oldLog := logf
	var logs []string
	logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	t.Cleanup(func() { logf = oldLog })
	for _, tc := range []struct {
		name, rejection      string
		rejections, requests int
		ok                   bool
		wantLogs             int
	}{
		{"field rejected", `{"error":{"message":"Unsupported parameter: 'reasoning_effort'"}}`, 1, 2, true, 1},
		{"field rejected twice", `{"error":{"message":"Unsupported parameter: 'reasoning_effort'"}}`, 2, 2, false, 1},
		{"value rejected", `{"error":{"message":"unsupported reasoning_effort \"high\""}}`, 1, 1, false, 0},
		{"ceiling rejected", `{"error":{"code":"reasoning_effort_above_ceiling","message":"reasoning_effort high exceeds the ceiling"}}`, 1, 1, false, 0},
		{"ceiling code with generic message", `{"error":{"code":"reasoning_effort_above_ceiling","message":"Unsupported parameter","param":"reasoning_effort"}}`, 1, 1, false, 0},
		{"top-level ceiling code with generic message", `{"code":"reasoning_effort_above_ceiling","error":{"message":"Unsupported parameter","param":"reasoning_effort"}}`, 1, 1, false, 0},
		{"nested field code overrides top-level ceiling", `{"code":"reasoning_effort_above_ceiling","error":{"code":"unsupported_parameter","message":"Unsupported parameter","param":"reasoning_effort"}}`, 1, 2, true, 1},
		{"another parameter rejected", `{"error":{"message":"Unsupported parameter: max_tokens","param":"max_tokens","details":{"reasoning_effort":"high"}}}`, 1, 1, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs = nil
			var requests []map[string]any
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				requests = append(requests, body)
				if len(requests) <= tc.rejections {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, tc.rejection)
					return
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer s.Close()
			reply, err := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model", ReasoningEffort: "high"}).Chat(context.Background(), Request{})
			if len(requests) != tc.requests || (err == nil) != tc.ok || tc.ok && reply.Text != "ok" {
				t.Fatalf("reply=%+v err=%v requests=%v", reply, err, requests)
			}
			if requests[0]["reasoning_effort"] != "high" {
				t.Fatalf("first request: %v", requests[0])
			}
			if tc.requests == 2 {
				if _, present := requests[1]["reasoning_effort"]; present {
					t.Fatalf("retry kept the rejected field: %v", requests[1])
				}
			}
			if len(logs) != tc.wantLogs {
				t.Fatalf("retry logs = %q, want %d", logs, tc.wantLogs)
			}
			if tc.wantLogs == 1 && (!strings.Contains(logs[0], "reasoning_effort") || !strings.Contains(logs[0], "without")) {
				t.Fatalf("degradation log = %q", logs)
			}
		})
	}
}

func TestChatCompletionsEffortRejectionCache(t *testing.T) {
	oldNow, oldLog := now, logf
	current := time.Now()
	now = func() time.Time { return current }
	var logs []string
	logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	t.Cleanup(func() { now, logf = oldNow, oldLog })
	var requests []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, body)
		if body["reasoning_effort"] != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"Unsupported parameter: 'reasoning_effort'"}}`)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer s.Close()
	client := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model", ReasoningEffort: "high"})
	for i := 0; i < 2; i++ {
		if _, err := client.Chat(context.Background(), Request{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(requests) != 3 || len(logs) != 1 {
		t.Fatalf("after two calls: requests=%d logs=%q", len(requests), logs)
	}
	if _, present := requests[2]["reasoning_effort"]; present {
		t.Fatalf("second call retried the rejected field before sending: %v", requests[2])
	}
	current = current.Add(2 * time.Hour)
	if _, err := client.Chat(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 5 || len(logs) != 2 || requests[3]["reasoning_effort"] != "high" {
		t.Fatalf("expired cache was not retried: requests=%v logs=%q", requests, logs)
	}
	if _, present := requests[4]["reasoning_effort"]; present {
		t.Fatalf("post-expiry retry kept the field: %v", requests[4])
	}
}

func TestChatPolicyCodeDoesNotCacheEffortRejection(t *testing.T) {
	var requests []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, body)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"code":"reasoning_effort_above_ceiling","error":{"message":"Unsupported parameter","param":"reasoning_effort"}}`)
	}))
	defer s.Close()
	client := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model", ReasoningEffort: "high"})
	for range 2 {
		if _, err := client.Chat(context.Background(), Request{}); err == nil {
			t.Fatal("policy rejection unexpectedly succeeded")
		}
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %v, want one unchanged request per call and no learned downgrade", requests)
	}
	for _, body := range requests {
		if body["reasoning_effort"] != "high" {
			t.Fatalf("policy code changed or cached the requested effort: %v", body)
		}
	}
}

func TestChatEffortCacheKey(t *testing.T) {
	base := chatEffortCacheKey("https://gw.example.com", "first-key", "model-a", "node-a")
	if base != chatEffortCacheKey("https://gw.example.com/", "first-key", "model-a", "node-a") {
		t.Fatal("trailing slashes split one connection")
	}
	for _, other := range []string{
		chatEffortCacheKey("https://other.example.com", "first-key", "model-a", "node-a"),
		chatEffortCacheKey("https://gw.example.com", "second-key", "model-a", "node-a"),
		chatEffortCacheKey("https://gw.example.com", "first-key", "model-b", "node-a"),
		chatEffortCacheKey("https://gw.example.com", "first-key", "model-a", "node-b"),
	} {
		if other == base {
			t.Fatal("different gateway, credential, model or route shares a cache entry")
		}
	}
}

func TestChatCompletionsEffortCacheSeparatesRoutes(t *testing.T) {
	var requests []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, body)
		if len(requests) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"Unsupported parameter: 'reasoning_effort'"}}`)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer s.Close()
	for _, via := range []string{"node-a", "node-b", "node-a"} {
		client := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model", ViaMachine: via, ReasoningEffort: "high"})
		if _, err := client.Chat(context.Background(), Request{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(requests) != 4 || requests[2]["reasoning_effort"] != "high" {
		t.Fatalf("second route inherited the first route's rejection: %v", requests)
	}
	if _, present := requests[3]["reasoning_effort"]; present {
		t.Fatalf("first route did not use its own cached rejection: %v", requests[3])
	}
}

func TestChatEffortCompatibilityCacheBoundAndScope(t *testing.T) {
	oldNow := now
	current := time.Now()
	now = func() time.Time { return current }
	t.Cleanup(func() { now = oldNow })
	cache := &chatEffortCache{}
	for i := 0; i < chatEffortCompatLimit+12; i++ {
		current = current.Add(time.Second)
		cache.rememberChatEffortRejection(fmt.Sprintf("model-%d", i), false)
	}
	if len(cache.entries) != chatEffortCompatLimit {
		t.Fatalf("cache size = %d, want %d", len(cache.entries), chatEffortCompatLimit)
	}
	if cache.omitChatEffort("model-0", "high") {
		t.Fatal("oldest model was not evicted")
	}
	if !cache.omitChatEffort(fmt.Sprintf("model-%d", chatEffortCompatLimit+11), "high") {
		t.Fatal("newest model is missing")
	}
	cache.rememberChatEffortRejection("off-only", true)
	if !cache.omitChatEffort("off-only", "off") || cache.omitChatEffort("off-only", "high") {
		t.Fatal("none-only cache changed an explicit non-off level")
	}
	current = current.Add(2 * time.Hour)
	if cache.omitChatEffort("off-only", "off") {
		t.Fatal("expired none-only rejection was applied")
	}
}

func TestChatNoRetryAllowsEffortFallbackAndCachesIt(t *testing.T) {
	var requests []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, body)
		if len(requests) == 1 {
			w.Header().Set("X-Should-Retry", "false")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"Unsupported parameter: 'reasoning_effort'"}}`)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer s.Close()
	client := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model", ReasoningEffort: "high"})
	if _, err := client.Chat(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Chat(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 || requests[0]["reasoning_effort"] != "high" {
		t.Fatalf("the rejected request did not keep its level: %v", requests)
	}
	for i, body := range requests[1:] {
		if _, present := body["reasoning_effort"]; present {
			t.Fatalf("modified request %d still has reasoning_effort: %v", i+1, body)
		}
	}
}

func TestChatCompletionsTokenParameterRetry(t *testing.T) {
	for _, test := range []struct {
		name, errorMessage string
		retry              bool
	}{
		{"unsupported", "Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead.", true},
		{"unrelated", "max_tokens is invalid", false},
		{"no unsupported", "max_tokens and max_completion_tokens cannot both be set", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests []map[string]any
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				requests = append(requests, body)
				if len(requests) == 1 {
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": test.errorMessage}})
					return
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer s.Close()
			r, err := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model"}).Chat(context.Background(), Request{MaxTokens: 42})
			if requests[0]["max_tokens"] != float64(42) {
				t.Fatalf("first request: %v", requests[0])
			}
			if test.retry {
				if err != nil || r.Text != "ok" || len(requests) != 2 || requests[1]["max_completion_tokens"] != float64(42) {
					t.Fatalf("reply=%+v err=%v requests=%v", r, err, requests)
				}
				if _, exists := requests[1]["max_tokens"]; exists {
					t.Fatalf("both fields sent: %v", requests[1])
				}
			} else if err == nil || len(requests) != 1 {
				t.Fatalf("err=%v requests=%v", err, requests)
			}
		})
	}
}

func TestChatCompletionsRequestsStreamedUsage(t *testing.T) {
	var request struct {
		StreamOptions *struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		// OpenAI-contract servers send the usage chunk only when the request asks for it.
		if request.StreamOptions != nil && request.StreamOptions.IncludeUsage {
			fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":2}}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer s.Close()
	reply, err := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model"}).Chat(context.Background(), Request{Messages: []Message{{RoleUser, "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if request.StreamOptions == nil || !request.StreamOptions.IncludeUsage {
		t.Fatalf("request did not ask for streamed usage: %+v", request)
	}
	if want := (Usage{InputTokens: 9, OutputTokens: 2}); reply.Usage != want {
		t.Fatalf("usage = %+v, want %+v", reply.Usage, want)
	}
}

func TestChatCompletionsStreamOptionsRetry(t *testing.T) {
	const (
		streamOptions = `{"error":{"message":"Unrecognized request argument supplied: stream_options"}}`
		includeUsage  = `{"detail":[{"loc":["body","include_usage"],"msg":"Extra inputs are not permitted"}]}`
		tokens        = `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."}}`
	)
	type rejection struct {
		status int
		body   string
	}
	for _, test := range []struct {
		name       string
		rejections []rejection // answered in order; later requests get a stream
		requests   int
		ok         bool
		// What the last request carried.
		lastStreamOptions, lastCompletionTokens bool
	}{
		{"400 names stream_options", []rejection{{http.StatusBadRequest, streamOptions}}, 2, true, false, false},
		{"422 names include_usage", []rejection{{http.StatusUnprocessableEntity, includeUsage}}, 2, true, false, false},
		{"stream_options then max_tokens", []rejection{{http.StatusBadRequest, streamOptions}, {http.StatusBadRequest, tokens}}, 3, true, false, true},
		{"max_tokens then stream_options", []rejection{{http.StatusBadRequest, tokens}, {http.StatusBadRequest, streamOptions}}, 3, true, false, true},
		{"unrelated 400", []rejection{{http.StatusBadRequest, `{"error":{"message":"model not found"}}`}}, 1, false, true, false},
		{"server error naming stream_options", []rejection{{http.StatusInternalServerError, streamOptions}}, 1, false, true, false},
		{"stream_options rejected twice", []rejection{{http.StatusBadRequest, streamOptions}, {http.StatusBadRequest, streamOptions}}, 2, false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests []map[string]any
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				requests = append(requests, body)
				if n := len(requests); n <= len(test.rejections) {
					w.WriteHeader(test.rejections[n-1].status)
					fmt.Fprint(w, test.rejections[n-1].body)
					return
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer s.Close()
			reply, err := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model"}).Chat(context.Background(), Request{MaxTokens: 42})
			if len(requests) != test.requests {
				t.Fatalf("requests = %d, want %d: %v", len(requests), test.requests, requests)
			}
			if first, _ := requests[0]["stream_options"].(map[string]any); first["include_usage"] != true {
				t.Fatalf("first request: %v", requests[0])
			}
			if test.ok != (err == nil) || test.ok && reply.Text != "ok" {
				t.Fatalf("reply=%+v err=%v", reply, err)
			}
			last := requests[len(requests)-1]
			_, hasStreamOptions := last["stream_options"]
			_, hasMaxTokens := last["max_tokens"]
			_, hasCompletionTokens := last["max_completion_tokens"]
			if hasStreamOptions != test.lastStreamOptions || hasCompletionTokens != test.lastCompletionTokens || hasMaxTokens == hasCompletionTokens || last["stream"] != true {
				t.Fatalf("last request: %v", last)
			}
		})
	}
}

func TestChatCompletionsErrors(t *testing.T) {
	for _, tt := range []struct {
		name, stream string
		want         error
		contains     string
	}{
		{"refusal", `data: {"choices":[{"index":0,"delta":{"refusal":"no"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n", ErrRefused, ""},
		{"filter", `data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}` + "\n\n", ErrRefused, ""},
		{"error", `data: {"error":{"message":"provider failed"}}` + "\n\n", nil, "provider failed"},
		{"malformed", "data: {bad json}\n\n", nil, "decode stream"},
		{"interrupted", `data: {"choices":[{"delta":{"content":"partial"}}]}` + "\n\n", nil, "without a completed reply"},
		{"empty done", "data: [DONE]\n\n", nil, "without a reply"},
		{"empty choice", `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n", nil, "without a reply"},
		{"reasoning only", `data: {"choices":[{"delta":{"reasoning_content":"private","content":"  "},"finish_reason":"stop"}]}` + "\n\n", nil, "without a reply"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readCompletions(strings.NewReader(tt.stream))
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error=%v, want %v", err, tt.want)
			}
			if tt.contains != "" && (err == nil || !strings.Contains(err.Error(), tt.contains)) {
				t.Fatalf("error=%v, want %q", err, tt.contains)
			}
		})
	}
}

func TestChatCompletionsRateLimitAndReadError(t *testing.T) {
	t.Run("rate limit", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"quota exhausted"}}`)
		}))
		defer s.Close()
		_, err := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model"}).Chat(context.Background(), Request{MaxTokens: 20})
		if !IsPaused(err) {
			t.Fatalf("expected paused quota error, got %v", err)
		}
	})
	t.Run("read error", func(t *testing.T) {
		_, err := readCompletions(io.MultiReader(strings.NewReader("data: {\"choices\":[]}"), failingReader{}))
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestChatCompletionsJSONRetriesTruncation(t *testing.T) {
	var limits []int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		limits = append(limits, req.MaxTokens)
		if len(limits) == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n")
		} else {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"answer\\\":42}\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}
	}))
	defer s.Close()
	var out struct{ Answer int }
	err := New(Config{API: "chat", BaseURL: s.URL, APIKey: "example-key", Model: "model"}).JSONMax(context.Background(), "system", "user", 16, &out)
	if err != nil || out.Answer != 42 || !reflect.DeepEqual(limits, []int{16, 32}) {
		t.Fatalf("output=%+v limits=%v error=%v", out, limits, err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
