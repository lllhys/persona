package node

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// Delete can remove the only readable entry and leave just deleted_<local uuid>. The association observed while the
// entry existed still identifies its current and earlier transcripts; an unrelated local-only marker identifies none.
func TestDesktopDeletedOnlyEntryHidesObservedTranscripts(t *testing.T) {
	const (
		current = "86730000-0000-4000-8000-000000000001"
		prior   = "86730000-0000-4000-8000-000000000002"
		fresh   = "86730000-0000-4000-8000-000000000003"
		other   = "86730000-0000-4000-8000-000000000004"
		local   = "86730000-0000-4000-8000-000000000101"
	)
	n, home := testNode(t)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	dir := filepath.Join(home, ".claude", "projects", "-src-app")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeEntrypointTranscript(t, dir, current, "claude-desktop-3p", 59*time.Minute)
	for _, id := range []string{prior, other} {
		writeEntrypointTranscript(t, dir, id, "claude-desktop-3p", time.Hour)
	}
	writeEntrypointTranscript(t, dir, fresh, "claude-desktop-3p", 30*time.Second)
	_, third := useDesktopDirs(t)
	entry := writeDesktopEntry(t, third, local, current, false, []string{prior}, "")
	if got := readClaudeDesktopIndex(home).entries; got != 1 {
		t.Fatalf("before Delete: entries=%d, want 1", got)
	}
	before := desktopMappingScanIDs(n.scanOffline())
	if !before[current] || before[prior] {
		t.Fatalf("before Delete: current listed=%t, prior listed=%t; want current only", before[current], before[prior])
	}

	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	indexDir := filepath.Dir(entry)
	marker := filepath.Join(indexDir, "deleted_"+local)
	if err := os.WriteFile(marker, []byte("1794736695195"), 0o600); err != nil {
		t.Fatal(err)
	}
	x := readClaudeDesktopIndex(home)
	if x.entries != 0 || !x.deleted[local] || x.deleted[current] || x.deleted[prior] {
		t.Fatalf("invalid fixture: entries=%d, localMarker=%t, currentMarker=%t, priorMarker=%t",
			x.entries, x.deleted[local], x.deleted[current], x.deleted[prior])
	}
	for pass := 0; pass < 3; pass++ {
		got := desktopMappingScanIDs(n.scanOffline())
		if got[current] || got[prior] {
			t.Fatalf("pass %d: deleted current listed=%t, prior listed=%t; want both hidden", pass+1, got[current], got[prior])
		}
		if !got[other] || !got[fresh] {
			t.Fatalf("pass %d: unrelated old listed=%t, fresh listed=%t; want both listed", pass+1, got[other], got[fresh])
		}
	}

	// Hiding the offline copy must not hide or duplicate the same session while it is live.
	n.agents[current] = host.Agent{Agent: "claude", AgentStatus: proto.StatusIdle, Cwd: "/src/app"}
	forgetOfflineScan()
	var liveRows []proto.Session
	for _, s := range n.sessionList(context.Background(), false) {
		if s.SessionID == current {
			liveRows = append(liveRows, s)
		}
	}
	if len(liveRows) != 1 || liveRows[0].Status != proto.StatusIdle {
		t.Fatalf("live rows=%+v, want exactly one idle row", liveRows)
	}

	// A current readable entry wins over a retained deletion association.
	override := writeDesktopEntry(t, third, "override", current, false, nil, "")
	if got := desktopMappingScanIDs(n.scanOffline()); !got[current] || got[prior] {
		t.Fatalf("readable override: current listed=%t, prior listed=%t; want current only", got[current], got[prior])
	}
	if err := os.Remove(override); err != nil {
		t.Fatal(err)
	}
	if got := desktopMappingScanIDs(n.scanOffline()); got[current] || got[prior] {
		t.Fatalf("after override removal: current listed=%t, prior listed=%t; want both hidden", got[current], got[prior])
	}

	// An unreadable entry protects every id it mentions, just as it does for direct deletion markers.
	unreadable := writeDesktopEntry(t, third, "unreadable", other, false, nil, "")
	if err := os.WriteFile(unreadable, []byte(`{"cliSessionId":"`+current+`","priorCliSessionIds":["`+prior+`"],"isArchived":tru`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := desktopMappingScanIDs(n.scanOffline()); !got[current] || !got[prior] {
		t.Fatalf("unreadable mention: current listed=%t, prior listed=%t; want both protected", got[current], got[prior])
	}
	if err := os.Remove(unreadable); err != nil {
		t.Fatal(err)
	}
	if got := desktopMappingScanIDs(n.scanOffline()); got[current] || got[prior] {
		t.Fatalf("after unreadable removal: current listed=%t, prior listed=%t; want both hidden", got[current], got[prior])
	}

	// Removing the tombstone retires its retained association instead of hiding the transcripts forever.
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if got := desktopMappingScanIDs(n.scanOffline()); !got[current] || !got[prior] {
		t.Fatalf("after marker removal: current listed=%t, prior listed=%t; want both listed", got[current], got[prior])
	}
	claudeDesktopFiles.Lock()
	_, retained := claudeDesktopFiles.associations[marker]
	claudeDesktopFiles.Unlock()
	if retained {
		t.Fatal("the removed marker's retained association was not cleaned up")
	}
}

func TestDesktopLocalMarkerWithoutObservedEntryDoesNotHide(t *testing.T) {
	const (
		neverIndexed = "86730000-0000-4000-8000-000000000011"
		fresh        = "86730000-0000-4000-8000-000000000012"
		readable     = "86730000-0000-4000-8000-000000000013"
		local        = "86730000-0000-4000-8000-000000000111"
	)
	n, home := testNode(t)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	dir := filepath.Join(home, ".claude", "projects", "-src-app")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeEntrypointTranscript(t, dir, neverIndexed, "claude-desktop-3p", time.Hour)
	writeEntrypointTranscript(t, dir, fresh, "claude-desktop-3p", 30*time.Second)
	writeEntrypointTranscript(t, dir, readable, "claude-desktop-3p", time.Hour)
	_, third := useDesktopDirs(t)
	indexDir := filepath.Join(third, "claude-code-sessions", "account", "org")
	if err := os.MkdirAll(indexDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "deleted_"+local), []byte("1794736695195"), 0o600); err != nil {
		t.Fatal(err)
	}
	x := readClaudeDesktopIndex(home)
	if x.entries != 0 || len(x.deleted) != 1 || x.named[neverIndexed] {
		t.Fatalf("invalid control: entries=%d deleted=%d named=%t", x.entries, len(x.deleted), x.named[neverIndexed])
	}
	if got := desktopMappingScanIDs(n.scanOffline()); !got[neverIndexed] || !got[fresh] {
		t.Fatalf("zero entries: never-indexed listed=%t, fresh listed=%t; want both listed", got[neverIndexed], got[fresh])
	}

	// With the age fallback active, a new transcript keeps its grace; the old unnamed transcript follows the old rule.
	writeDesktopEntry(t, third, "readable", readable, false, nil, "")
	if got := desktopMappingScanIDs(n.scanOffline()); got[neverIndexed] || !got[fresh] || !got[readable] {
		t.Fatalf("with a readable entry: never-indexed listed=%t, fresh listed=%t, readable listed=%t; want false, true, true",
			got[neverIndexed], got[fresh], got[readable])
	}
}

func TestDesktopAssociationBounds(t *testing.T) {
	t.Run("transcripts per entry", func(t *testing.T) {
		e := claudeDesktopEntry{transcript: "a6730000-0000-4000-8000-000000000001"}
		for i := 0; i < maxDesktopAssociationTranscripts+10; i++ {
			e.prior = append(e.prior, fmt.Sprintf("a6730000-0000-4000-8000-%012d", i+2))
		}
		e.prior = append(e.prior, "not-a-transcript", e.transcript)
		got := desktopAssociationTranscripts(e)
		if len(got) != maxDesktopAssociationTranscripts || got[0] != e.transcript {
			t.Fatalf("retained transcripts=%d, first=%q; want %d with current first", len(got), got[0], maxDesktopAssociationTranscripts)
		}
		seen := map[string]bool{}
		for _, id := range got {
			if seen[id] || !validID(id) {
				t.Fatalf("invalid or duplicate retained id %q", id)
			}
			seen[id] = true
		}
	})

	t.Run("entries", func(t *testing.T) {
		claudeDesktopFiles.Lock()
		oldAssociations, oldClock := claudeDesktopFiles.associations, claudeDesktopFiles.associationClock
		claudeDesktopFiles.associations, claudeDesktopFiles.associationClock = map[string]*claudeDesktopAssociation{}, 0
		claudeDesktopFiles.Unlock()
		t.Cleanup(func() {
			claudeDesktopFiles.Lock()
			claudeDesktopFiles.associations, claudeDesktopFiles.associationClock = oldAssociations, oldClock
			claudeDesktopFiles.Unlock()
		})
		_, third := useDesktopDirs(t)
		localID := func(i int) string { return fmt.Sprintf("96730000-0000-4000-8000-%012d", i) }
		for i := 0; i <= maxDesktopAssociations; i++ {
			transcript := fmt.Sprintf("96731000-0000-4000-8000-%012d", i)
			writeDesktopEntry(t, third, localID(i), transcript, false, nil, "")
		}
		readClaudeDesktopIndex("")
		claudeDesktopFiles.Lock()
		defer claudeDesktopFiles.Unlock()
		if got := len(claudeDesktopFiles.associations); got != maxDesktopAssociations {
			t.Fatalf("retained entries=%d, want bound %d", got, maxDesktopAssociations)
		}
		indexDir := filepath.Join(third, "claude-code-sessions", "account", "org")
		if _, ok := claudeDesktopFiles.associations[filepath.Join(indexDir, "deleted_"+localID(0))]; ok {
			t.Fatal("the oldest association was not evicted")
		}
		if _, ok := claudeDesktopFiles.associations[filepath.Join(indexDir, "deleted_"+localID(maxDesktopAssociations))]; !ok {
			t.Fatal("the newest association was evicted")
		}
	})
}

func desktopMappingScanIDs(rows []proto.Session) map[string]bool {
	out := make(map[string]bool, len(rows))
	for _, s := range rows {
		out[s.SessionID] = true
	}
	return out
}
