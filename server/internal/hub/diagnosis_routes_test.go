package hub

import (
	"net/http"
	"testing"

	"github.com/nwverse/persona/server/internal/extwire"
)

// A computer's diagnosis and cleanup (#8037) are for full credentials: one lists what runs on the computer, the other
// removes files. The watch's answer-only token never reaches the manager with them, and no extension scope opens them.
func TestDiagnosisRoutesNeedFullCredentials(t *testing.T) {
	watch := principal{Role: roleAnswer}
	for _, rq := range [][2]string{
		{http.MethodGet, "/api/v1/machines/mac/diagnosis"},
		{http.MethodPost, "/api/v1/machines/mac/cleanup"},
	} {
		if allowed(watch, rq[0], rq[1]) {
			t.Errorf("an answer-only device may %s %s", rq[0], rq[1])
		}
		if !allowed(principal{Role: roleFull}, rq[0], rq[1]) {
			t.Errorf("a full device may not %s %s", rq[0], rq[1])
		}
	}
	for _, p := range []string{"GET /api/v1/machines/{mid}/diagnosis", "POST /api/v1/machines/{mid}/cleanup"} {
		if s, ok := extwire.RouteScopes[p]; ok {
			t.Errorf("%s is open to extensions with %s", p, s)
		}
	}
}
