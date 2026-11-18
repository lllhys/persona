package sessionsearch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/sessionlist"
)

func TestDocumentIndexesVisibleTextIndependently(t *testing.T) {
	item := sessionlist.Item{Full: json.RawMessage(`{"state":"idle","last_text":"Visible <script>secret","work":{"phase":"done","line":"searchable conclusion"}}`)}
	doc, err := Document(item)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc.AgentText, "secret") || !strings.Contains(doc.AgentText, "Visible") || !strings.Contains(doc.AgentText, "searchable conclusion") {
		t.Fatalf("indexed prose = %q", doc.AgentText)
	}
}

func TestSearchDoesNotReturnHiddenInlineHTML(t *testing.T) {
	raw := json.RawMessage(`{"machine_id":"machine","session_id":"session","title":"Example","state":"idle","last_text":"Visible <script>secretneedle</script> <style>cssneedle</style> **ordinaryneedle**","work":{"phase":"blocked","line":"realblocker <script>blockedsecret</script> <style>blockedstyle</style>"}}`)
	var index sessionlist.Index
	index.Replace("machine", []sessionlist.Item{{ID: "session", Machine: "machine", Full: raw, Summary: raw}})
	ctx := &sessionlist.SearchContext{Document: Document}
	for _, tc := range []struct {
		query string
		want  int
	}{{"secretneedle", 0}, {"cssneedle", 0}, {"blockedsecret", 0}, {"blockedstyle", 0}, {"ordinaryneedle", 1}, {"realblocker", 1}} {
		page, err := index.Query(sessionlist.Options{Search: tc.query, SearchContext: ctx, Limit: 10})
		if err != nil || len(page.Sessions) != tc.want {
			t.Fatalf("search %q: got %d results, err %v; want %d", tc.query, len(page.Sessions), err, tc.want)
		}
	}
}
