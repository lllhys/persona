package node

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/transcript"
)

// A read of a session this machine has no transcript for (an unknown id: a stale link, a client that sent the wrong
// field) answers not_found with the words it always had, which the hub serves as 404, not 502 internal (#9551). The
// other callers of file keep the plain error: the manager takes a not_found send as "the session is gone" and
// withdraws its card, which a missing file alone must not do.
func TestReadsOfAnUnknownSessionAreNotFound(t *testing.T) {
	home := t.TempDir()
	n := &Node{home: home, paths: map[string]string{}, files: map[string]*transcript.File{}, procs: map[string]*assistantProc{},
		agents: map[string]host.Agent{}, state: &stateFile{Sessions: map[string]sessionState{}}}
	const unknown = "99999999-2222-3333-4444-555555555555"
	const words = "transcript for session " + unknown + " not found"
	for _, method := range []string{"session.items", "session.item", "session.image"} {
		_, err := n.handle(context.Background(), method, json.RawMessage(`{"session_id":"`+unknown+`","item_id":"x","turns":2}`))
		if rpcCode(err) != codeNotFound || err.Error() != words {
			t.Errorf("%s: want not_found %q, got code %q: %v", method, words, rpcCode(err), err)
		}
	}
	if _, err := n.handle(context.Background(), "session.items", json.RawMessage(`{"session_id":"`+unknown+`","user_inputs":true}`)); rpcCode(err) != codeNotFound {
		t.Errorf("user_inputs: want not_found, got code %q: %v", rpcCode(err), err)
	}
	// A subagent of a session with no transcript is not there either.
	if _, err := n.handle(context.Background(), "session.items", json.RawMessage(`{"session_id":"`+unknown+subagentSep+`a1"}`)); rpcCode(err) != codeNotFound {
		t.Errorf("subagent: want not_found, got code %q: %v", rpcCode(err), err)
	}
	if _, err := n.file(unknown); err == nil || rpcCode(err) != "" || err.Error() != words {
		t.Errorf("file: want the plain error %q, got code %q: %v", words, rpcCode(err), err)
	}
}
