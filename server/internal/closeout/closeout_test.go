package closeout

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestParse(t *testing.T) {
	reply := "Fixed the retry.\n\n<closeout status=\"done\" merged=\"no\" deployed=\"n/a\">\n" +
		"did: The retry now backs off instead of looping.\n- did：Added a test for the cap.\n" +
		"artifact: pr https://example.com/acme/app/pull/12\nartifact: commit abc1234\nartifact: tweet nope\n" +
		"verified: go test ./retry, 14 passed\nmissing: none\n</closeout>\n"
	c, arts, ok := Parse(reply)
	if !ok {
		t.Fatal("no closeout read")
	}
	want := proto.Closeout{Status: proto.CloseoutDone, Merged: proto.CloseoutNo,
		Did:      []string{"The retry now backs off instead of looping.", "Added a test for the cap."},
		Verified: "go test ./retry, 14 passed"}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("closeout = %+v, want %+v", c, want)
	}
	wantArts := []proto.WorkArtifact{{Kind: "pr", Ref: "https://example.com/acme/app/pull/12"}, {Kind: "commit", Ref: "abc1234"}}
	if !reflect.DeepEqual(arts, wantArts) {
		t.Fatalf("artifacts = %+v, want %+v", arts, wantArts)
	}
}

func TestParseBlockedAndLast(t *testing.T) {
	reply := "<closeout status=\"done\"></closeout> earlier draft\n" +
		"<closeout status=\"blocked\" merged=\"no\" deployed=\"no\">\nwaiting: the release review\nmissing: approval to merge\n</closeout>"
	c, _, ok := Parse(reply)
	if !ok || c.Status != proto.CloseoutBlocked || c.Waiting != "the release review" || c.Missing != "approval to merge" || c.Deployed != proto.CloseoutNo {
		t.Fatalf("closeout = %+v ok=%v: want the last block, blocked", c, ok)
	}
}

func TestParseRefuses(t *testing.T) {
	for name, reply := range map[string]string{
		"none":          "All done, merged and deployed.",
		"no status":     "<closeout merged=\"yes\">\ndid: x\n</closeout>",
		"made-up state": "<closeout status=\"almost\">\ndid: x\n</closeout>",
		"unclosed":      "<closeout status=\"done\">\ndid: x",
	} {
		if Has(reply) {
			t.Errorf("%s: read as a closeout", name)
		}
	}
}

func TestParseBounds(t *testing.T) {
	long := strings.Repeat("字", 500)
	reply := "<closeout status=\"not-done\">\n" + strings.Repeat("did: "+long+"\n", 5) + strings.Repeat("artifact: url https://example.com/x\n", 9) + "</closeout>"
	c, arts, ok := Parse(reply)
	if !ok || c.Status != proto.CloseoutNotDone {
		t.Fatalf("closeout = %+v ok=%v", c, ok)
	}
	if len(c.Did) != 3 || len([]rune(c.Did[0])) != 200 || len(arts) != 5 {
		t.Fatalf("did %d lines of %d runes, %d artifacts: want 3 of 200, 5", len(c.Did), len([]rune(c.Did[0])), len(arts))
	}
}

func TestStrip(t *testing.T) {
	if got := Strip("Fixed.\n\n<closeout status=\"done\">\ndid: x\n</closeout>\n"); got != "Fixed." {
		t.Fatalf("Strip = %q", got)
	}
}
