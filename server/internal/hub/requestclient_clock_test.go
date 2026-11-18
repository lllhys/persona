//go:build go1.25

package hub

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestRequestClientSlowLog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub(t)
		var logs bytes.Buffer
		h.logger = log.New(&logs, "", 0)
		handler := logRequests(h, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(3 * time.Second); w.WriteHeader(200) }))
		r := httptest.NewRequest("GET", "/api/v1/machines", nil)
		r.Header.Set("X-Persona-Client", "ios/1.2")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		if !strings.Contains(logs.String(), `client="ios/1.2"`) {
			t.Fatalf("slow log: %q", logs.String())
		}
	})
}

// A meeting's audio socket, relayed to the manager, lasts as long as the meeting records: it is never logged as a
// slow request. An upload to the same path still is.
func TestSlowLogLeavesTheAudioSocket(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub(t)
		var logs bytes.Buffer
		h.logger = log.New(&logs, "", 0)
		handler := logRequests(h, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-time.After(time.Hour) }))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/recordings/r1/audio", nil))
		if logs.Len() != 0 {
			t.Fatalf("the audio socket was logged: %q", logs.String())
		}
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PUT", "/api/v1/recordings/r1/audio", nil))
		if !strings.Contains(logs.String(), "PUT /api/v1/recordings/r1/audio -> 200 (1h0m0s)") {
			t.Fatalf("a slow upload: %q", logs.String())
		}
	})
}
