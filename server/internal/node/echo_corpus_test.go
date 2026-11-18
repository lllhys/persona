package node

// Ties every message in a corpus of real transcripts (never committed) to its own line, as a send of its raw text
// would be, and counts the misses:
//
//	go test ./internal/node -run EchoCorpus -echocorpus ~/.claude/projects
//
// Nothing here runs without -echocorpus.

import (
	"bufio"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

var echoCorpus = flag.String("echocorpus", "", "comma-separated directories of real Claude Code transcripts")

func TestEchoCorpus(t *testing.T) {
	if *echoCorpus == "" {
		t.Skip("no -echocorpus")
	}
	var files []string
	for _, d := range strings.Split(*echoCorpus, ",") {
		_ = filepath.Walk(strings.TrimSpace(d), func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() && strings.HasSuffix(p, ".jsonl") && !strings.Contains(p, "/subagents/") {
				files = append(files, p)
			}
			return nil
		})
	}
	var messages, tied, wrong int
	var misses []string
	for _, p := range files {
		fh, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		var meta transcript.Meta
		seq := 0
		for sc.Scan() {
			seq++
			line := sc.Bytes()
			items := transcript.ParseLine(seq, line, &meta)
			sent, images, ok := corpusTyped(line)
			if !ok || len(items) == 0 {
				continue
			}
			if items[0].Role != "user" || items[0].From != "" || items[0].Kind == proto.KindToolResult || items[0].Task != nil {
				continue
			}
			messages++
			var l echoLog
			for i := range items { // an old corpus: the send is now, and so is its line
				items[i].TS = time.Time{}
			}
			l.expect(echoRecord{SessionID: "s", ClientMsgID: "x", Text: sent, Images: images, SentAt: time.Now(), AfterSeq: seq - 1})
			found := l.match("s", echoInTranscript, items)
			switch {
			case len(found) == 1 && found[0].Line == echoLineKey(items[0].ID):
				tied++
			case len(found) == 1:
				wrong++
			default:
				if len(misses) < 20 {
					misses = append(misses, filepath.Base(p)+": "+clipRunes(sent, 60)+" → "+clipRunes(items[0].Text, 60))
				}
			}
		}
		fh.Close()
	}
	t.Logf("%d files, %d messages: %d tied, %d tied to another line, %d missed", len(files), messages, tied, wrong, messages-tied-wrong)
	for _, m := range misses {
		t.Log(m)
	}
}

// corpusTyped is what was typed for a user line or a queued message: its text content ("! cmd" for Claude's bash
// mode) and how many pictures it had.
func corpusTyped(line []byte) (string, int, bool) {
	var e struct {
		Type    string `json:"type"`
		Message *struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		Attachment *struct {
			Type   string          `json:"type"`
			Prompt json.RawMessage `json:"prompt"`
		} `json:"attachment"`
	}
	if json.Unmarshal(line, &e) != nil {
		return "", 0, false
	}
	var raw json.RawMessage
	switch {
	case e.Type == "user" && e.Message != nil:
		raw = e.Message.Content
	case e.Type == "attachment" && e.Attachment != nil && e.Attachment.Type == "queued_command":
		raw = e.Attachment.Prompt
	default:
		return "", 0, false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if name := corpusTag(s, "command-name"); name != "" { // typed as "/name args"
			s = strings.TrimSpace(name + " " + corpusTag(s, "command-args"))
		}
		if strings.HasPrefix(s, "<bash-input>") {
			s = "! " + strings.TrimSuffix(strings.TrimPrefix(s, "<bash-input>"), "</bash-input>")
		}
		return s, 0, true
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return "", 0, false
	}
	var parts []string
	images := 0
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			return "", 0, false
		case "text":
			parts = append(parts, b.Text)
		case "image":
			images++
		}
	}
	return strings.Join(parts, "\n"), images, true
}

func clipRunes(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func corpusTag(s, tag string) string {
	_, after, ok := strings.Cut(s, "<"+tag+">")
	if !ok {
		return ""
	}
	body, _, _ := strings.Cut(after, "</"+tag+">")
	return strings.TrimSpace(body)
}
