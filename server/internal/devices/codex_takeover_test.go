package devices

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestCodexTakeoverIsNotAnAssistantNodeTool(t *testing.T) {
	d := &Devices{}
	for _, method := range []string{"session.codex.takeover.prepare", "session.codex.takeover.commit"} {
		_, err := d.Call(context.Background(), devicewire.NodeCall{MachineID: "test", Method: method})
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != "user_confirmation_required" {
			t.Errorf("tool route %s accepted: %v", method, err)
		}
	}
}

func TestCodexTakeoverRequiresSignedInFullDevice(t *testing.T) {
	d := &Devices{}
	for _, role := range []string{"", "answer", "extension", "system"} {
		for _, method := range []string{"GET", "POST"} {
			req := httptest.NewRequest(method, "/", strings.NewReader(`{"token":"test","confirm_stop":true}`))
			req.Header.Set(assistantwire.RoleHeader, role)
			w := httptest.NewRecorder()
			if method == "GET" {
				d.prepareCodexTakeover(w, req, nil)
			} else {
				d.commitCodexTakeover(w, req, nil)
			}
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "user_confirmation_required") {
				t.Errorf("%s role=%q: %d %s", method, role, w.Code, w.Body.String())
			}
		}
	}
}

func TestCodexTakeoverRejectsMissingAndMalformedConfirmation(t *testing.T) {
	d := &Devices{}
	for _, body := range []string{`{}`, `{"confirm_stop":true}`, `{`, `{"token":42}`} {
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.Header.Set(assistantwire.RoleHeader, "full")
		w := httptest.NewRecorder()
		d.commitCodexTakeover(w, req, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("bad confirmation %q accepted: %d", body, w.Code)
		}
	}
}
