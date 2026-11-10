// Package closeout reads the structured record an agent ends a turn with (docs/assistant/agent-work.md "Closeout"):
// whether the work is done, what changed, whether it is merged and deployed, what was verified and what is missing.
// The node's Stop hook asks for one when a turn that did work ends without it; the manager's triage reads it before
// it asks a model.
package closeout

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/proto"
)

var (
	blockRe = regexp.MustCompile(`(?s)<closeout\b([^>]*)>(.*?)</closeout>`)
	attrRe  = regexp.MustCompile(`([a-z_]+)\s*=\s*"([^"]*)"`)
)

// Example is the form an agent is asked to write, as the prompts show it.
const Example = `<closeout status="done|not_done|blocked" merged="yes|no|n/a" deployed="yes|no|n/a">
did: <what changed, one sentence; up to three such lines>
artifact: <pr|commit|branch|doc|build|url> <reference>
verified: <what you ran or measured and its reading, or "untested">
missing: <what you still need from someone, if anything>
waiting: <for status="blocked": who or what you wait for>
</closeout>`

// Parse reads the last closeout block in an agent's reply. ok is false when there is none or its status is not one
// of done, not_done and blocked: a block that does not say where the work stands is no closeout.
func Parse(reply string) (c proto.Closeout, artifacts []proto.WorkArtifact, ok bool) {
	all := blockRe.FindAllStringSubmatch(reply, -1)
	if len(all) == 0 {
		return c, nil, false
	}
	m := all[len(all)-1]
	for _, a := range attrRe.FindAllStringSubmatch(m[1], -1) {
		v := strings.ToLower(strings.TrimSpace(a[2]))
		switch a[1] {
		case "status":
			c.Status = strings.ReplaceAll(v, "-", "_")
		case "merged":
			c.Merged = yesNo(v)
		case "deployed":
			c.Deployed = yesNo(v)
		}
	}
	switch c.Status {
	case proto.CloseoutDone, proto.CloseoutNotDone, proto.CloseoutBlocked:
	default:
		return proto.Closeout{}, nil, false
	}
	for _, line := range strings.Split(m[2], "\n") {
		key, val, found := cutKey(line)
		if !found || val == "" {
			continue
		}
		switch key {
		case "did":
			if len(c.Did) < 3 {
				c.Did = append(c.Did, clip(val, 200))
			}
		case "artifact":
			kind, ref, _ := strings.Cut(val, " ")
			kind, ref = strings.ToLower(strings.TrimSuffix(kind, ":")), strings.TrimSpace(ref)
			if len(artifacts) < 5 && ref != "" && isKind(kind) {
				artifacts = append(artifacts, proto.WorkArtifact{Kind: kind, Ref: clip(ref, 300)})
			}
		case "verified":
			c.Verified = clip(val, 300)
		case "missing":
			if !nothing(val) {
				c.Missing = clip(val, 300)
			}
		case "waiting":
			if !nothing(val) {
				c.Waiting = clip(val, 120)
			}
		}
	}
	return c, artifacts, true
}

// Strip returns reply without its closeout blocks, for a reader that is given the closeout separately.
func Strip(reply string) string {
	return strings.TrimSpace(blockRe.ReplaceAllString(reply, ""))
}

// Has reports whether reply carries a closeout Parse accepts.
func Has(reply string) bool {
	_, _, ok := Parse(reply)
	return ok
}

// cutKey splits "key: value", taking a full-width colon as well and a leading list marker.
func cutKey(line string) (key, val string, ok bool) {
	line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-*"))
	i := strings.IndexAny(line, ":：")
	if i <= 0 {
		return "", "", false
	}
	_, size := utf8.DecodeRuneInString(line[i:])
	return strings.ToLower(strings.TrimSpace(line[:i])), strings.TrimSpace(line[i+size:]), true
}

func yesNo(v string) string {
	switch v {
	case "yes", "true":
		return proto.CloseoutYes
	case "no", "false":
		return proto.CloseoutNo
	}
	return "" // n/a, or anything else: not claimed
}

func isKind(k string) bool {
	switch k {
	case "pr", "commit", "branch", "doc", "build", "url":
		return true
	}
	return false
}

// nothing: the agent filled an optional line with a word for "none" instead of leaving it out.
func nothing(v string) bool {
	switch strings.ToLower(strings.Trim(v, " .。-")) {
	case "", "none", "nothing", "n/a", "no", "无", "没有", "なし":
		return true
	}
	return false
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
