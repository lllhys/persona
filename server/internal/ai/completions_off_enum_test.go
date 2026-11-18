package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestChatNoEffortFallsBackOnMustBeOneOf(t *testing.T) {
	var requests atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"reasoning_effort must be one of 'minimal', 'low', 'medium', 'high'","param":"reasoning_effort"}}`)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer gateway.Close()

	client := New(Config{API: "chat", BaseURL: gateway.URL, APIKey: "review-key", Model: "review-model", ReasoningEffort: "off"})
	if _, err := client.Chat(context.Background(), Request{}); err != nil {
		t.Fatalf("off request was not degraded after a none-only enum rejection: requests=%d err=%v", requests.Load(), err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests=%d, want one rejection plus one without-field retry", got)
	}
}
