package assistant

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/proto"
)

type knowledgeFolderTestHub struct {
	*fakeHub
	remoteCalls int
	offline     bool
}

func (h *knowledgeFolderTestHub) DeviceCall(ctx context.Context, call DeviceCall) (json.RawMessage, error) {
	switch call.Method {
	case devicewire.KnowledgeSearch, devicewire.KnowledgeRead:
		h.remoteCalls++
	default:
		return h.fakeHub.DeviceCall(ctx, call)
	}
	if call.Device != "notes-node" {
		panic("knowledge call reached the wrong machine")
	}
	if h.offline {
		return nil, apierr.ErrOffline
	}
	if call.Method == devicewire.KnowledgeSearch {
		var request devicewire.KnowledgeSearchRequest
		if err := json.Unmarshal(call.Params, &request); err != nil {
			return nil, err
		}
		if request.Path != "~/notes" || request.Query != "synthesis" || !slices.Equal(request.ExcludeDirs, []string{"private"}) {
			panic("knowledge call did not use the configured policy")
		}
		return json.RawMessage(`{"paths":["reference.md"]}`), nil
	}
	return json.RawMessage(`{"text":"synthetic notes"}`), nil
}

func TestRemoteKnowledgeTools(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub()
	configFile := filepath.Join(t.TempDir(), "manager.json")
	if err := os.WriteFile(configFile, []byte(`{"kb":{"remote_folders":[{"name":"notes","machine":"notes-node","path":"~/notes","exclude_dirs":["private"]}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := readOwnConfig(configFile)
	if err != nil {
		t.Fatal(err)
	}
	f.hello = cfg.over(f.hello)
	h := &knowledgeFolderTestHub{fakeHub: f}
	st := &memStore{}
	s := withSettings(t, f, New(Options{Hub: h, Devices: newFleetDevs(), Store: func() Store { return st }, Logf: testLogf(t), KB: &KBOptions{Dir: t.TempDir()}}))
	t.Cleanup(s.kbEnd)
	for _, assistant := range []bool{true, false} {
		tools := s.mcpTools(ctx, assistant)
		for _, name := range []string{knowledgeSearchTool, knowledgeReadTool} {
			if !slices.ContainsFunc(tools, func(tool proto.Tool) bool { return tool.Name == name }) {
				t.Fatalf("assistant=%v: missing %s", assistant, name)
			}
		}
	}
	call := func(session, tool string, assistant bool, args json.RawMessage) proto.ToolResult {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"name": tool, "args": args, "session_id": session, "assistant": assistant})
		if err != nil {
			t.Fatal(err)
		}
		out, handled, err := s.NodeRequest(ctx, "mac", "mcp.call", raw)
		if err != nil || !handled {
			t.Fatalf("mcp.call: handled=%v, err=%v", handled, err)
		}
		return out.(proto.ToolResult)
	}
	search := json.RawMessage(`{"source":"notes","query":"synthesis","path":"~/outside"}`)
	finishStart := s.beginAssistantStart("mac")
	if got := call("", knowledgeSearchTool, true, search); got.IsError || !strings.Contains(got.Content[0].Text, "reference.md") {
		t.Fatalf("assistant search: %+v", got)
	}
	finishStart()
	read := json.RawMessage(`{"source":"notes","path":"reference.md"}`)
	if got := call("", knowledgeReadTool, false, read); got.IsError || !strings.Contains(got.Content[0].Text, "synthetic notes") {
		t.Fatalf("Agent read: %+v", got)
	}
	if err := s.guestSet(ctx, "mac", "guest", []string{knowledgeReadTool}); err != nil {
		t.Fatal(err)
	}
	if got := call("guest", knowledgeReadTool, false, read); !got.IsError {
		t.Fatal("guest could read the folder")
	}
	if _, err := s.runTool(withOutwardReader(ctx), "", "", knowledgeReadTool, read); err == nil {
		t.Fatal("outward conversation could read the folder")
	}
	if h.remoteCalls != 2 {
		t.Fatalf("expected two node calls, got %d", h.remoteCalls)
	}
	h.offline = true
	if got := call("", knowledgeSearchTool, false, search); !got.IsError || !strings.Contains(got.Content[0].Text, "unreachable") {
		t.Fatalf("offline search: %+v", got)
	}
}
