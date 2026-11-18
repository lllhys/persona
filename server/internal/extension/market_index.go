package extension

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// MarketIndex is the canonical extension index.
type MarketIndex struct {
	MarketVersion int    `json:"market_version"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Homepage      string `json:"homepage,omitempty"`
	// SigningKey is legacy catalog metadata, never a trust grant.
	SigningKey         string `json:"signing_key,omitempty"`
	DownloadsUpdatedAt string `json:"downloads_updated_at,omitempty"`
	// Extensions are "extensions" in an index; the old key "plugins" is read too, and still the one written out (the
	// hub keeps the index it read, which a hub rolled back to a build before #1844 reads again).
	Extensions []MarketEntry `json:"plugins"`
}

func (ix *MarketIndex) UnmarshalJSON(data []byte) error {
	type plain MarketIndex
	var v struct {
		plain
		Current []MarketEntry `json:"extensions"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*ix = MarketIndex(v.plain)
	if v.Current != nil {
		ix.Extensions = v.Current
	}
	return nil
}

// MarketEntry is one plugin of a market: its manifest (so the permissions are known before downloading) and its
// packages.
type MarketEntry struct {
	Replaces  string          `json:"replaces,omitempty"`
	Icon      string          `json:"icon,omitempty"`
	IconURL   string          `json:"icon_url,omitempty"`
	Downloads *int64          `json:"downloads,omitempty"`
	Manifest  json.RawMessage `json:"manifest"`
	Tags      []string        `json:"tags,omitempty"`
	Packages  []MarketPackage `json:"packages"`
	// L10n is the package's translations again ({"<language>": <l10n/<language>.json>}, l10n.go), so the catalog can
	// show them before anything is downloaded. Kept as written (read with Translations); hubs before it ignore the key.
	L10n json.RawMessage `json:"l10n,omitempty"`
	// Parsed is Manifest, read.
	Parsed *Manifest `json:"-"`
	// Only ParseCatalog can create this proof; public fields remain checked
	// against the snapshot before the registry accepts official provenance.
	verifiedKey   string
	verifiedEntry [sha256.Size]byte
}

// MarketPackage is a package of an entry.
type MarketPackage struct {
	// Platforms are the GOOS-GOARCH it runs on; none: any (a package for every platform, or one that runs no code).
	Platforms []string `json:"platforms,omitempty"`
	// URL: relative to the index, an https URL, or (a GitHub market) release:<tag>/<asset>.
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

// ParseIndex reads and checks an index. Entries that do not check out are dropped with a warning each (one broken
// plugin does not hide a market); an index that is not one is an error.
func ParseIndex(data []byte) (*MarketIndex, []string, error) {
	if len(data) > IndexMost {
		return nil, nil, i18n.Err("extension-market_read.too_large", i18n.Args{"bytes": IndexMost})
	}
	var ix MarketIndex
	if err := json.Unmarshal(data, &ix); err != nil {
		return nil, nil, notCatalog(data)
	}
	switch {
	case ix.MarketVersion > 1:
		return nil, nil, i18n.Err("extension-market_read.newer_format", i18n.Args{"version": ix.MarketVersion})
	case ix.MarketVersion != 1:
		return nil, nil, notCatalog(data)
	}
	ix.Name = strings.TrimSpace(ix.Name)
	if ix.Name == "" || len([]rune(ix.Name)) > 60 {
		return nil, nil, i18n.Err("extension-market_read.no_name")
	}
	if len([]rune(ix.Description)) > StringMost {
		return nil, nil, i18n.Err("extension-market_read.description_too_long")
	}
	if ix.Homepage != "" && !isHTTPS(ix.Homepage) {
		ix.Homepage = ""
	}
	if ix.DownloadsUpdatedAt != "" {
		if _, err := time.Parse(time.RFC3339, ix.DownloadsUpdatedAt); err != nil {
			ix.DownloadsUpdatedAt = ""
		}
	}
	if len(ix.Extensions) > IndexExtensionsMost {
		return nil, nil, i18n.Err("extension-market_read.catalog_over_limit", i18n.Args{"most": IndexExtensionsMost})
	}
	// Identity declarations are never silently dropped or resolved first-entry-wins.
	declaredTargets := map[string]bool{}
	for _, e := range ix.Extensions {
		if e.Replaces == "" {
			continue
		}
		m, err := Parse(e.Manifest)
		if err != nil || declaredTargets[m.ID] || !ValidID(e.Replaces) || e.Replaces == m.ID {
			return nil, nil, ErrIdentity
		}
		declaredTargets[m.ID] = true
	}
	// Warnings are kept with the market in the default language and shown in the reader's (i18n.MarketText).
	var warnings []string
	seen := map[string]bool{}
	kept := ix.Extensions[:0]
	for i, e := range ix.Extensions {
		m, err := e.check()
		switch {
		case err != nil:
			warnings = append(warnings, i18n.T(i18n.Default, "extension-market_read.entry_skipped", i18n.Args{"n": i + 1, "err": err.Error()}))
		case seen[m.ID]:
			warnings = append(warnings, i18n.T(i18n.Default, "extension-market_read.entry_duplicate", i18n.Args{"id": m.ID}))
		default:
			seen[m.ID] = true
			e.Parsed = m
			if e.Icon != "" && !ValidIconPath(e.Icon) {
				e.Icon, e.IconURL = "", ""
			}
			if !ValidIconURL(e.IconURL) {
				e.IconURL = ""
			}
			if e.Downloads != nil && *e.Downloads < 0 {
				e.Downloads = nil
			}
			if _, err := ParseEntryTranslations(e.L10n); err != nil { // the entry stays, in its manifest's words
				warnings = append(warnings, i18n.T(i18n.En, "extension-status.entry_translation_skipped", i18n.Args{"id": m.ID, "err": err.Error()}))
				e.L10n = nil
			}
			kept = append(kept, e)
		}
	}
	ix.Extensions = kept
	if err := ValidateReplacements(ix.Extensions); err != nil {
		return nil, warnings, err
	}
	return &ix, warnings, nil
}

// notCatalog says what a document that is not a market's catalog is, for whoever entered the address: a web page,
// or JSON that is something else (a GitHub API answer, another file, or the answer of a proxy or sign-in page on the
// way, quoted when it carries a message). Without market_version it is never taken for a catalog of format 0.
func notCatalog(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '<' {
		return i18n.Err("extension-market_read.web_page")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) == nil {
		// GitHub answers a file's page with the page's own data when asked for JSON: {"meta": …, "payload": {…}}.
		if p := bytes.TrimSpace(fields["payload"]); len(p) > 0 && p[0] == '{' && fields["meta"] != nil {
			return i18n.Err("extension-market_read.web_page")
		}
		for _, key := range []string{"message", "error", "msg"} {
			var said string
			if json.Unmarshal(fields[key], &said) == nil {
				if said = clipSaid(said); said != "" {
					return i18n.Err("extension-market_read.not_catalog_said", i18n.Args{"message": said})
				}
			}
		}
	}
	return i18n.Err("extension-market_read.not_catalog")
}

// clipSaid is what a non-catalog answer said, on one line and at most 160 characters.
func clipSaid(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	return s
}

// Translations are the entry's l10n, read (ParseIndex has left out one that does not check out).
func (e *MarketEntry) Translations() Translations {
	ts, _ := ParseEntryTranslations(e.L10n)
	return ts
}

func (e *MarketEntry) check() (*Manifest, error) {
	if len(e.Manifest) == 0 {
		return nil, i18n.Err("extension-market_read.entry_no_manifest")
	}
	m, err := Parse(e.Manifest)
	if err != nil {
		return nil, err
	}
	if e.Replaces != "" && (!ValidID(e.Replaces) || e.Replaces == m.ID) {
		return nil, ErrIdentity
	}
	// An entry without tags of its own shows the manifest's (an index written from the source manifest as it is).
	if len(e.Tags) == 0 {
		e.Tags = m.Tags
	}
	if len(e.Tags) > marketTagMost {
		return nil, i18n.Err("extension-market_read.entry_too_many_tags")
	}
	for _, t := range e.Tags {
		if t == "" || len([]rune(t)) > 24 {
			return nil, i18n.Err("extension-market_read.entry_bad_tag")
		}
	}
	if len(e.Packages) == 0 || len(e.Packages) > marketPackageMost {
		return nil, i18n.Err("extension-market_read.entry_package_count", i18n.Args{"id": m.ID, "most": marketPackageMost})
	}
	for _, p := range e.Packages {
		if b, err := hex.DecodeString(p.SHA256); err != nil || len(b) != sha256.Size {
			return nil, i18n.Err("extension-market_read.entry_package_sha256", i18n.Args{"id": m.ID})
		}
		if strings.HasPrefix(p.URL, "oci:") {
			if _, err := parseOCI(p.URL); err != nil || p.Size <= 0 || p.Size > PackageMost {
				return nil, i18n.Err("extension-market_security.package")
			}
			continue
		}
		if p.Size < 0 || p.Size > PackageMost {
			return nil, i18n.Err("extension-market_security.package")
		}
		if p.URL == "" || strings.HasPrefix(p.URL, "http://") || strings.Contains(p.URL, "://") && !strings.HasPrefix(p.URL, "https://") {
			return nil, i18n.Err("extension-market_read.entry_package_url", i18n.Args{"id": m.ID})
		}
	}
	return m, nil
}

func ValidIconURL(s string) bool {
	if len(s) > 2048 {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}

func isHTTPS(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

// Platform is this hub's GOOS-GOARCH.
func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// PackageFor is the entry's package for platform, or nil.
func (e *MarketEntry) PackageFor(platform string) *MarketPackage {
	for i, p := range e.Packages {
		if len(p.Platforms) == 0 || slices.Contains(p.Platforms, platform) {
			return &e.Packages[i]
		}
	}
	return nil
}
