package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstObject(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```":          `{"a":1}`,
		"sure! {\"a\":{\"b\":\"}\"}} done": `{"a":{"b":"}"}}`,
		`{"s":"quote \" brace {"}`:         `{"s":"quote \" brace {"}`,
	}
	for in, want := range cases {
		got, err := FirstObject(in)
		if err != nil || got != want {
			t.Errorf("FirstObject(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := FirstObject("no json"); err == nil {
		t.Error("expected error")
	}
}

func TestJSONCallsMessagesAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "k" {
			t.Errorf("bad request %s %s", r.URL.Path, r.Header.Get("x-api-key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": "```json\n{\"email\":\"a@b.c\"}\n```"}}})
	}))
	defer srv.Close()
	keyFile := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(keyFile, []byte("k\n"), 0o600)
	c := New(Config{BaseURL: srv.URL, APIKeyFile: keyFile, Model: "m"})
	var out struct{ Email string }
	if err := c.JSON(context.Background(), "sys", "user", &out); err != nil || out.Email != "a@b.c" {
		t.Fatalf("out = %+v, %v", out, err)
	}
}

func TestMessagesAPIWithBearer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("authorization") != "Bearer k" || r.Header.Get("x-api-key") != "" {
			t.Errorf("a bearer gateway gets only Authorization: %q, x-api-key %q", r.Header.Get("authorization"), r.Header.Get("x-api-key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}})
	}))
	defer srv.Close()
	text, err := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m", Bearer: true}).Complete(context.Background(), "sys", "user", 16)
	if err != nil || text != "ok" {
		t.Fatalf("text = %q, %v", text, err)
	}
}

func TestJSONCallsResponsesAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		var req struct {
			Stream bool `json:"stream"`
			Input  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"input"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/openai/v1/responses" || r.Header.Get("Authorization") != "Bearer k" || !req.Stream ||
			len(req.Input) != 2 || req.Input[0].Role != "developer" || !strings.HasPrefix(req.Input[0].Content, "sys") ||
			req.Input[1].Content != "user" || req.Reasoning.Effort != "low" {
			t.Errorf("bad request %s %s %+v", r.URL.Path, r.Header.Get("Authorization"), req)
		}
		w.Header().Set("content-type", "text/event-stream")
		for _, ev := range []string{
			`{"type":"response.created"}`,
			`{"type":"response.output_text.delta","delta":"{\"email\":"}`,
			`{"type":"response.output_text.delta","delta":"\"a@b.c\"}"}`,
			`{"type":"response.completed"}`,
		} {
			_, _ = w.Write([]byte("event: x\ndata: " + ev + "\n\n"))
		}
	}))
	defer srv.Close()
	keyFile := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(keyFile, []byte("k\n"), 0o600)
	c := New(Config{API: "responses", BaseURL: srv.URL + "/openai", APIKeyFile: keyFile, Model: "m", ReasoningEffort: "low"})
	var out struct{ Email string }
	if err := c.JSON(context.Background(), "sys", "user", &out); err != nil || out.Email != "a@b.c" {
		t.Fatalf("out = %+v, %v", out, err)
	}
}

func TestResponsesAPIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`data: {"type":"response.failed","response":{"error":{"message":"boom"}}}` + "\n\n"))
	}))
	defer srv.Close()
	keyFile := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(keyFile, []byte("k"), 0o600)
	c := New(Config{API: "responses", BaseURL: srv.URL, APIKeyFile: keyFile, Model: "m"})
	if _, err := c.Complete(context.Background(), "sys", "user", 10); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

// A Messages API that answers the n-th request with replies[n] and records each request's max_tokens.
func truncatingServer(t *testing.T, replies ...map[string]any) (*Client, *[]int) {
	t.Helper()
	var seen []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		var req struct {
			MaxTokens int `json:"max_tokens"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		seen = append(seen, req.MaxTokens)
		reply := replies[min(len(seen), len(replies))-1]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content":     []any{map[string]any{"type": "text", "text": reply["text"]}},
			"stop_reason": reply["stop"],
		})
	}))
	t.Cleanup(srv.Close)
	keyFile := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(keyFile, []byte("k\n"), 0o600)
	return New(Config{BaseURL: srv.URL, APIKeyFile: keyFile, Model: "m"}), &seen
}

func TestJSONMaxRetriesACutOffReplyWithMoreRoom(t *testing.T) {
	c, seen := truncatingServer(t,
		map[string]any{"text": `{"summary":"half wri`, "stop": "max_tokens"},
		map[string]any{"text": `{"summary":"whole"}`, "stop": "end_turn"})
	var out struct{ Summary string }
	if err := c.JSONMax(context.Background(), "sys", "user", 100, &out); err != nil || out.Summary != "whole" {
		t.Fatalf("out = %+v, %v", out, err)
	}
	if want := []int{100, 200}; !equalInts(*seen, want) {
		t.Errorf("max_tokens = %v, want %v", *seen, want)
	}
}

func TestJSONMaxSaysTheReplyWasCutOff(t *testing.T) {
	c, seen := truncatingServer(t, map[string]any{"text": `thinking… {"summary":"half`, "stop": "max_tokens"})
	var out struct{ Summary string }
	err := c.JSONMax(context.Background(), "sys", "user", 100, &out)
	if !errors.Is(err, ErrTruncated) || !strings.Contains(err.Error(), "max_tokens 200") {
		t.Fatalf("err = %v, want ErrTruncated at 200", err)
	}
	if len(*seen) != 2 {
		t.Errorf("requests = %v, want one retry", *seen)
	}
}

func TestJSONMaxTakesAUsableReplyWithoutRetry(t *testing.T) {
	for _, tc := range []struct {
		name, text, stop, want string
	}{
		{"a complete object from a cut-off reply", `{"summary":"done"} and then some more chatter that ran o`, "max_tokens", "done"},
		{"a quote escaped as in JavaScript", `{"summary":"it\'s done"}`, "end_turn", "it's done"},
		// A model writing a summary in paragraphs puts the line breaks into the string as they are, not as \n (#4872).
		{"a line break typed into a string (#4872)", "{\"summary\":\"第一段\n\n第二段\"}", "end_turn", "第一段\n\n第二段"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, seen := truncatingServer(t, map[string]any{"text": tc.text, "stop": tc.stop})
			var out struct{ Summary string }
			if err := c.JSONMax(context.Background(), "sys", "user", 100, &out); err != nil || out.Summary != tc.want {
				t.Fatalf("out = %+v, %v", out, err)
			}
			if len(*seen) != 1 {
				t.Errorf("requests = %v, want no retry", *seen)
			}
		})
	}
}

func TestJSONMaxDoesNotRetryAWholeReplyWithoutJSON(t *testing.T) {
	c, seen := truncatingServer(t, map[string]any{"text": "no json here", "stop": "end_turn"})
	var out struct{}
	err := c.JSONMax(context.Background(), "sys", "user", 100, &out)
	if err == nil || errors.Is(err, ErrTruncated) || !strings.Contains(err.Error(), "no JSON object") {
		t.Fatalf("err = %v", err)
	}
	if len(*seen) != 1 {
		t.Errorf("requests = %v, want one", *seen)
	}
}

// A refusal (stop_reason "refusal", no text) is ErrRefused, asked once: not a reply without JSON, nor one to retry.
func TestJSONMaxSaysTheModelRefused(t *testing.T) {
	c, seen := truncatingServer(t, map[string]any{"text": "", "stop": "refusal"})
	var out struct{}
	err := c.JSONMax(context.Background(), "sys", "user", 100, &out)
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
	if len(*seen) != 1 {
		t.Errorf("requests = %v, want one", *seen)
	}
}

func TestUnmarshalReplyRepairsOnlyWhatJSONLacks(t *testing.T) {
	cases := map[string]string{
		`{"s":"it\'s"}`:                        "it's",
		`{"s":"C:\Users\me"}`:                  `C:\Users\me`,
		`{"s":"tab\tquote\"slash\/\u00e9\\n"}`: "tab\tquote\"slash/é\\n",
		`{"s":"mixed \' and \"real\" and \d"}`: `mixed ' and "real" and \d`,
		// Control characters written into a string as they are: JSON allows none of them there.
		"{\"s\":\"line one\nline two\"}":        "line one\nline two",
		"{\"s\":\"a\tb\r\nc\"}":                 "a\tb\r\nc",
		"{\"s\":\"bell\x07 and \x1f\"}":         "bell\x07 and \x1f",
		"{\n  \"s\": \"two\nlines\"\n}":         "two\nlines", // the line breaks between the fields stay as they are
		"{\"s\":\"it\\'s\nmine\"}":              "it's\nmine",
		"{\"s\":\"a real \\n and a typed \n\"}": "a real \n and a typed \n",
	}
	for in, want := range cases {
		var out struct{ S string }
		if err := unmarshalReply([]byte(in), &out); err != nil || out.S != want {
			t.Errorf("unmarshalReply(%q) = %q, %v; want %q", in, out.S, err, want)
		}
	}
}

func TestUnmarshalReplyKeepsTheFirstErrorWhenTheRepairDoesNotHelp(t *testing.T) {
	var out struct{ S string }
	err := unmarshalReply([]byte(`{"s":"it\'s",}`), &out) // the trailing comma is still wrong after the repair
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) || !strings.Contains(err.Error(), `\'`) {
		t.Fatalf("err = %v, want the syntax error about \\'", err)
	}
	err = unmarshalReply([]byte("{\"s\":\"a\nb\",}"), &out) // the line break is repaired, the trailing comma is not
	if !errors.As(err, &syntax) || !strings.Contains(err.Error(), `'\n'`) {
		t.Fatalf("err = %v, want the syntax error about the line break", err)
	}
	if err := unmarshalReply([]byte(`{"s":1}`), &out); err == nil {
		t.Fatal("a type error is not repaired: want it back")
	} else if errors.As(err, &syntax) {
		t.Fatalf("err = %v, want the type error", err)
	}
}

func TestCompleteReturnsACutOffReplyAsFarAsItGot(t *testing.T) {
	c, _ := truncatingServer(t, map[string]any{"text": "a long answer that st", "stop": "max_tokens"})
	text, err := c.Complete(context.Background(), "sys", "user", 10)
	if err != nil || text != "a long answer that st" {
		t.Fatalf("text = %q, %v", text, err)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
