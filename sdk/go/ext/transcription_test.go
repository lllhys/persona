package ext

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
)

// scriptedHub is a Hub whose calls answer answer(method, params): a result, or an error.
func scriptedHub(t *testing.T, answer func(method string, params json.RawMessage) (any, *Error)) *Hub {
	t.Helper()
	toHubR, toHubW := io.Pipe()
	fromHubR, fromHubW := io.Pipe()
	p := newPeer(toHubW)
	go func() { _ = p.serve(context.Background(), fromHubR) }()
	go func() {
		sc := bufio.NewScanner(toHubR)
		sc.Buffer(make([]byte, 64<<10), 16<<20)
		for sc.Scan() {
			var f frame
			if json.Unmarshal(sc.Bytes(), &f) != nil || f.Method == "" {
				continue
			}
			out := frame{ID: f.ID}
			res, e := answer(f.Method, f.Params)
			if e != nil {
				out.Error = e
			} else {
				out.Result, _ = json.Marshal(res)
			}
			line, _ := json.Marshal(out)
			if _, err := fromHubW.Write(append(line, '\n')); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { toHubW.Close(); fromHubW.Close() })
	return &Hub{p: p}
}

// Transcribe sends the audio in pieces of at most TranscriptionChunkMost, in order, then finishes (#8485).
func TestTranscribe(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	var got []byte
	h := scriptedHub(t, func(method string, params json.RawMessage) (any, *Error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, method)
		switch method {
		case "transcription.start":
			var p wire.TranscriptionStartParams
			_ = json.Unmarshal(params, &p)
			if p.Language != "zh-CN" {
				return nil, &Error{Code: "invalid_params", Message: "language"}
			}
			return wire.TranscriptionStartResult{Job: "j1"}, nil
		case "transcription.audio":
			var p wire.TranscriptionAudioParams
			_ = json.Unmarshal(params, &p)
			b, _ := base64.StdEncoding.DecodeString(p.Data)
			if p.Job != "j1" || len(b) > TranscriptionChunkMost {
				return nil, &Error{Code: "invalid_params", Message: "audio"}
			}
			got = append(got, b...)
			return wire.Empty{}, nil
		case "transcription.finish":
			return wire.TranscriptionFinishResult{Segments: []wire.TranscriptionSegment{{From: 0.5, To: 1, Text: "Hi."}},
				Gaps: []wire.TranscriptionGap{{From: 5, To: 9}}, Seconds: 20}, nil
		}
		return nil, &Error{Code: "unknown_method", Message: method}
	})
	pcm := make([]byte, 20*16000*2)
	for i := range pcm {
		pcm[i] = byte(i)
	}
	out, err := Transcribe(context.Background(), h, pcm, wire.TranscriptionStartParams{Language: "zh-CN"})
	if err != nil || len(out.Segments) != 1 || len(out.Gaps) != 1 || out.Seconds != 20 {
		t.Fatalf("%+v %v", out, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := "transcription.start transcription.audio transcription.audio transcription.audio transcription.finish"; strings.Join(calls, " ") != want {
		t.Fatalf("calls %v", calls)
	}
	if string(got) != string(pcm) {
		t.Fatal("the audio did not arrive whole and in order")
	}
}

// A piece the hub refuses cancels the job; odd PCM is refused before any call.
func TestTranscribeCancelsAFailedJob(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	h := scriptedHub(t, func(method string, _ json.RawMessage) (any, *Error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, method)
		switch method {
		case "transcription.start":
			return wire.TranscriptionStartResult{Job: "j1"}, nil
		case "transcription.audio":
			return nil, &Error{Code: wire.TranscriptionErrorCodeTooLong, Message: "too long"}
		}
		return wire.Empty{}, nil
	})
	_, err := Transcribe(context.Background(), h, make([]byte, 4), wire.TranscriptionStartParams{})
	var e *Error
	if !errors.As(err, &e) || e.Code != wire.TranscriptionErrorCodeTooLong {
		t.Fatalf("%v", err)
	}
	mu.Lock()
	if strings.Join(calls, " ") != "transcription.start transcription.audio transcription.cancel" {
		t.Fatalf("calls %v", calls)
	}
	mu.Unlock()
	if _, err := Transcribe(context.Background(), h, make([]byte, 3), wire.TranscriptionStartParams{}); err == nil {
		t.Fatal("odd PCM")
	}
}
