package rpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestOnMachineHandlesNullDataFromPeer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	hubConn, nodeConn := net.Pipe()
	hub := rpc.New("h", hubConn)
	node := rpc.New("n", nodeConn)
	node.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
		return nil, &rpc.Error{Code: proto.CodeClaudeMissing, Message: "refusal", Data: json.RawMessage(`null`)}
	}
	hubDone := make(chan error, 1)
	nodeDone := make(chan error, 1)
	go func() { hubDone <- hub.Serve(ctx, hubConn) }()
	go func() { nodeDone <- node.Serve(ctx, nodeConn) }()
	defer func() {
		cancel()
		_ = hubConn.Close()
		_ = nodeConn.Close()
		for _, done := range []chan error{hubDone, nodeDone} {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("RPC Serve did not stop after the pipe closed")
			}
		}
	}()

	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	defer callCancel()
	err := hub.Call(callCtx, "assistant.send", map[string]any{}, nil)
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != proto.CodeClaudeMissing || re.Message != "refusal" || string(re.Data) != "null" {
		t.Fatalf("decoded refusal = %#v", err)
	}
	got := apierr.OnMachine(err, "Studio")
	if !errors.As(got, &re) {
		t.Fatalf("OnMachine returned %T", got)
	}
	if re.Code != proto.CodeClaudeMissing || re.Message != "refusal" {
		t.Fatalf("OnMachine changed the refusal: %#v", re)
	}
	var data map[string]any
	if err := json.Unmarshal(re.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data) != 1 || data["machine_name"] != "Studio" {
		t.Fatalf("data = %#v, want only machine_name Studio", data)
	}

	rec := httptest.NewRecorder()
	apierr.Call(rec, got)
	if rec.Code != http.StatusConflict {
		t.Fatalf("HTTP status = %d, want 409", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != proto.CodeClaudeMissing || body["action"] != "install_claude" || body["machine_name"] != "Studio" {
		t.Fatalf("HTTP body = %#v", body)
	}
}
