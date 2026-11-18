package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

// A document that is not a market's catalog says what it is, never "market_version is 0" (#5597): a GitHub API answer
// or another JSON file, a proxy's or sign-in page's answer (quoted), a web page; a newer format asks for a newer hub.
func TestNotACatalogSaysWhatItIs(t *testing.T) {
	for _, c := range []struct {
		name, doc string
		key       string
		en        string
	}{
		{"github contents api", `{"name":"package.json","path":"web/package.json","sha":"abc","size":12,"download_url":"https://example.com/x"}`,
			"extension-market_read.not_catalog", "For a GitHub repository, enter github:owner/repo"},
		{"another json file", `{"name":"example","version":"1.0.0","dependencies":{}}`, "extension-market_read.not_catalog", "didn't return an extension marketplace"},
		{"proxy answer", `{"code":403,"message":"Forbidden by\n proxy"}`, "extension-market_read.not_catalog_said", "it answered “Forbidden by proxy”"},
		{"sign-in page json", `{"error":"login_required","redirect":"/login"}`, "extension-market_read.not_catalog_said", "“login_required”"},
		{"array", `[{"name":"x"}]`, "extension-market_read.not_catalog", "didn't return an extension marketplace"},
		{"empty", ``, "extension-market_read.not_catalog", "didn't return an extension marketplace"},
		{"web page", "\n<!doctype html><html><body>Sign in</body></html>", "extension-market_read.web_page", "returned a web page"},
		// A GitHub file page asked for JSON: its page data, with a "payload" that is no signed envelope's.
		{"github file page", `{"meta":{"title":"x"},"payload":{"blob":{"rawLines":["{}"]}}}`, "extension-market_read.web_page", "returned a web page"},
		{"payload without signatures", `{"payload":{"x":1}}`, "extension-market_read.not_catalog", "didn't return an extension marketplace"},
		{"zero", `{"market_version":0,"name":"Old","extensions":[]}`, "extension-market_read.not_catalog", "didn't return an extension marketplace"},
		{"newer format", `{"market_version":2,"name":"Future","extensions":[]}`, "extension-market_read.newer_format", "uses format 2, which needs a newer hub"},
	} {
		_, err := ParseCatalog([]byte(c.doc), "")
		var m *i18n.Message
		if err == nil || !asMessage(err, &m) || m.Key != c.key {
			t.Errorf("%s: %v, want %s", c.name, err, c.key)
			continue
		}
		if got := i18n.Localize(err, i18n.En); !strings.Contains(got, c.en) || strings.Contains(got, "market_version") {
			t.Errorf("%s: %q, want %q", c.name, got, c.en)
		}
		if strings.Contains(err.Error(), "market_version") {
			t.Errorf("%s: the default text names the field: %q", c.name, err.Error())
		}
	}
	// A long answer is quoted on one line and clipped.
	long, _ := json.Marshal(map[string]string{"message": strings.Repeat("a", 500)})
	if _, err := ParseCatalog(long, ""); err == nil || len([]rune(i18n.Localize(err, i18n.En))) > 450 {
		t.Fatalf("long answer: %v", err)
	}
}

func asMessage(err error, m **i18n.Message) bool {
	for err != nil {
		if v, ok := err.(*i18n.Message); ok {
			*m = v
			return true
		}
		if u, ok := err.(interface{ Unwrap() []error }); ok {
			for _, e := range u.Unwrap() {
				if asMessage(e, m) {
					return true
				}
			}
			return false
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// Read from an address whose server answers 200 with something else, the market says so in the reader's language.
func TestMarketReadOfANonCatalog(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded for 203.0.113.1."}`))
	}))
	defer srv.Close()
	mk := &Market{Spec: MarketSpec{Kind: "https", URL: srv.URL + "/persona-extensions.json"}, Client: srv.Client()}
	_, _, err := mk.Fetch(context.Background())
	if err == nil {
		t.Fatal("read")
	}
	for l, want := range map[i18n.Lang]string{
		i18n.En:     "it answered “API rate limit exceeded for 203.0.113.1.”",
		i18n.ZhHans: "它回的是「API rate limit exceeded for 203.0.113.1.」",
		i18n.Ja:     "「API rate limit exceeded for 203.0.113.1.」と応答しました",
	} {
		if got := i18n.Localize(err, l); !strings.Contains(got, want) {
			t.Errorf("%s: %q, want %q", l, got, want)
		}
	}
}

// Every user-facing error and warning of a market read has its text in each language: refusals of the HTTP
// answer, a rate limit with when to retry, the address forms, the entries a catalog drops.
func TestMarketReadErrorsAreTranslated(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1200")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	mk := &Market{Spec: MarketSpec{Kind: "github", Owner: "example", Repo: "market"}, Client: srv.Client(), GitHubAPI: srv.URL}
	_, _, err := mk.Fetch(context.Background())
	if got := i18n.Localize(err, i18n.En); got != "GitHub is limiting requests from this hub (HTTP 429). Try again in about 20 minutes. A GitHub token raises the limit, for public repositories too." {
		t.Errorf("rate limit: %q", got)
	}
	if got := i18n.Localize(err, i18n.Ja); !strings.Contains(got, "約20分後にもう一度お試しください。") {
		t.Errorf("rate limit ja: %q", got)
	}
	for spec, want := range map[string]string{
		"github:example":            "Write a GitHub marketplace as github:owner/repo",
		"http://example.com/x.json": "Marketplaces must use https.",
		"https://u:p@example.com/":  "Don't put an account or password in the address",
		"example":                   "A marketplace's address is https://… or github:owner/repo.",
	} {
		_, err := ParseMarket(spec)
		if got := i18n.Localize(err, i18n.En); !strings.Contains(got, want) {
			t.Errorf("%s: %q, want %q", spec, got, want)
		}
	}
	// A catalog's dropped entries are warnings in the default language, written again for the reader.
	var m map[string]any
	_ = json.Unmarshal([]byte(exampleManifest), &m)
	ok := map[string]any{"manifest": m, "packages": []any{map[string]any{"url": "notes.tar.gz", "sha256": strings.Repeat("a", 64)}}}
	b, _ := json.Marshal(map[string]any{"market_version": 1, "name": "Example", "extensions": []any{
		map[string]any{"packages": []any{}}, ok, ok,
		map[string]any{"manifest": m, "packages": []any{map[string]any{"url": "http://example.com/x.tar.gz", "sha256": strings.Repeat("a", 64)}}},
	}})
	_, warnings, err := ParseIndex(b)
	if err != nil || len(warnings) != 3 {
		t.Fatalf("%v %q", err, warnings)
	}
	for i, want := range []string{
		"Extension 1 isn't listed: It has no manifest.",
		"Extension example-notes is listed more than once; only the first is used.",
		"Extension 4 isn't listed: example-notes: a package's address must be a relative path or https.",
	} {
		if got := i18n.MarketText(i18n.En, warnings[i]); got != want {
			t.Errorf("warning %d: %q, want %q", i, got, want)
		}
	}
}

func TestOversizedCatalogErrorIsTranslated(t *testing.T) {
	entries := make([]any, IndexExtensionsMost+1)
	for i := range entries {
		entries[i] = map[string]any{}
	}
	raw, err := json.Marshal(map[string]any{
		"market_version": 1,
		"name":           "Oversized",
		"extensions":     entries,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ParseIndex(raw)
	if err == nil {
		t.Fatal("ParseIndex accepted 501 extensions")
	}
	if got, want := i18n.MarketText(i18n.En, err.Error()), "The catalog lists more than 500 extensions."; got != want {
		t.Fatalf("MarketText() = %q, want %q (error %q)", got, want, err)
	}
}
