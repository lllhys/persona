package repository

import (
	"strings"
	"testing"
)

func TestTaskLinkURLAndIdentityUseTheSameNormalization(t *testing.T) {
	for _, raw := range []string{
		"http://gitlab.example.com/Team/Api/-/issues/7",
		"http://gitlab.example.com:8080/Team/Api/-/merge_requests/7",
		"https://gitlab.example.com:8443/Team/Api/-/merge_requests/7",
		"http://git.example.com:8080/Example/Web/pull/7",
		"https://git.example.com:8443/Example/Web/issues/7",
		"http://GitHub.com/Example/Web/pull/7",
		"http://www.github.com:80/Example/Web/issues/7",
		"http://gitlab.com/Team/Api/-/merge_requests/7",
		"https://GitHub.com:443/Example/Web/pull/7",
	} {
		t.Run(raw, func(t *testing.T) {
			identity, parsed, err := ParseTaskLink(raw)
			if err != nil {
				t.Fatal(err)
			}
			direct, err := NormalizeLink(identity, LinkInput{URL: raw})
			if err != nil || direct != parsed {
				t.Fatalf("direct and reported links disagree: %+v %+v %v", direct, parsed, err)
			}
			canonical, err := NormalizeLink(identity, LinkInput{URL: parsed.CanonicalURL})
			if err != nil || canonical != parsed {
				t.Fatal("canonical URL does not round-trip", canonical, err)
			}
			if strings.Contains(raw, ".example.com:") && !strings.HasPrefix(parsed.CanonicalURL, identity.InstanceURL+"/") {
				t.Fatal("self-hosted authority changed", identity, parsed)
			}
		})
	}
}

func TestTaskLinkNormalizationKeepsAddressBoundaries(t *testing.T) {
	identity, _ := Parse("http://gitlab.example.com:8080/team/api", "gitlab")
	for _, raw := range []string{
		"https://gitlab.example.com:8080/team/api/-/issues/7",
		"http://gitlab.example.com:8081/team/api/-/issues/7",
		"http://gitlab.example.com/team/api/-/issues/7",
		"http://other.example.com:8080/team/api/-/issues/7",
		"http://gitlab.example.com:8080/team/other/-/issues/7",
		"http://user:secret@gitlab.example.com:8080/team/api/-/issues/7",
		"http://gitlab.example.com:8080/team/api/-/issues/7?view=all",
		"http://gitlab.example.com:8080/team/api/-/issues/7?",
		"http://gitlab.example.com:8080/team/api/-/issues/7#note",
		"http://gitlab.example.com:8080/team/api/-/issues/%37",
		"http://gitlab.example.com:8080/team/api/../api/-/issues/7",
		"http://gitlab.example.com:8080/team/api/-/issues/7/notes",
	} {
		if _, err := NormalizeLink(identity, LinkInput{URL: raw}); err == nil {
			t.Fatal("accepted a different or unsafe link", raw)
		}
	}
}

func TestTaskLinkTitleLimitUsesUTF8Bytes(t *testing.T) {
	identity, _ := Parse("https://github.com/example/web", "github")
	for _, unit := range []string{"a", "é", "界", "🦊"} {
		title := strings.Repeat(unit, 1024/len(unit))
		if _, err := NormalizeLink(identity, LinkInput{Kind: "issue", Number: "7", Title: title}); err != nil {
			t.Fatal("rejected a title within the UTF-8 byte limit", len(title), err)
		}
		if _, err := NormalizeLink(identity, LinkInput{Kind: "issue", Number: "7", Title: title + unit}); err == nil {
			t.Fatal("accepted a title beyond the UTF-8 byte limit", len(title+unit))
		}
	}
}
