package transcript

import (
	"fmt"
	"testing"
)

// A gateway's zeros on a turn's replies and its sum of the whole turn on the last one (#3300) are not the context:
// the last real number stands.
func TestContextTokensSkipImpossibleUsage(t *testing.T) {
	var meta Meta
	for i, input := range []int{84_000, 0, 0, 1_706_464} {
		line := fmt.Sprintf(`{"type":"assistant","uuid":"a%d","message":{"id":"m%d","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":%d,"output_tokens":1}}}`, i, i, input)
		ParseLine(i+1, []byte(line), &meta)
	}
	if meta.ContextTokens != 84_000 {
		t.Errorf("ContextTokens = %d", meta.ContextTokens)
	}
}

// A compaction makes the context unknown until the next reply (#9207): the size before it no longer stands, and the
// boundary's postTokens leaves out the system prompt and tools. A subagent's compaction is not the session's.
func TestContextTokensAfterCompaction(t *testing.T) {
	reply := func(id string, cacheRead int) string {
		return fmt.Sprintf(`{"type":"assistant","uuid":"%s","message":{"id":"%s","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":2,"cache_read_input_tokens":%d,"output_tokens":1}}}`, id, id, cacheRead)
	}
	boundary := `{"type":"system","subtype":"compact_boundary","uuid":"c%d","compactMetadata":{"trigger":"auto","preTokens":970002,"postTokens":15000}}`
	var meta Meta
	ParseLine(1, []byte(reply("a1", 970_000)), &meta)
	ParseLine(2, []byte(fmt.Sprintf(boundary, 1)), &meta)
	if meta.ContextTokens != 0 || !meta.ContextCompacted || meta.Compactions != 1 {
		t.Fatalf("after the compaction: %d tokens, compacted %v, %d compactions", meta.ContextTokens, meta.ContextCompacted, meta.Compactions)
	}
	ParseLine(3, []byte(reply("a2", 61_000)), &meta)
	if meta.ContextTokens != 61_002 || meta.ContextCompacted {
		t.Fatalf("after the next reply: %d tokens, compacted %v", meta.ContextTokens, meta.ContextCompacted)
	}
	ParseLine(4, []byte(`{"type":"system","subtype":"compact_boundary","uuid":"s1","isSidechain":true}`), &meta)
	if meta.ContextTokens != 61_002 || meta.ContextCompacted {
		t.Fatalf("after a subagent's compaction: %d tokens, compacted %v", meta.ContextTokens, meta.ContextCompacted)
	}
}

func TestPlausibleContext(t *testing.T) {
	for _, c := range []struct {
		tokens int
		size   int64
		want   bool
	}{
		{999_999, 0, true}, {1_000_001, 0, false}, {1_706_464, 1_240_000, false},
		{150_000, 1_000, true}, {250_000, 1_000, false}, {900_000, 2_000_000, true},
	} {
		if got := PlausibleContext(c.tokens, c.size); got != c.want {
			t.Errorf("PlausibleContext(%d, %d) = %v", c.tokens, c.size, got)
		}
	}
}
