package extension

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

// The text tried for a language: its own translation, then English unless it is Simplified Chinese, then the
// manifest's; a blank translation is no translation.
func TestTranslationsPickByLanguage(t *testing.T) {
	ts := Translations{
		i18n.En: {Name: "Notes", Description: "Notes into the knowledge base"},
		i18n.Ja: {Name: "ノート", Description: "  "},
	}
	for _, c := range []struct {
		l          i18n.Lang
		name, desc string
	}{
		{i18n.Ja, "ノート", "Notes into the knowledge base"},
		{i18n.En, "Notes", "Notes into the knowledge base"},
		{i18n.ZhHans, "示例笔记", "把笔记放进知识库"},
	} {
		if got := ts.Name(c.l, "示例笔记"); got != c.name {
			t.Errorf("%s name: %q", c.l, got)
		}
		if got := ts.Description(c.l, "把笔记放进知识库"); got != c.desc {
			t.Errorf("%s description: %q", c.l, got)
		}
	}
	var none Translations
	if none.Name(i18n.Ja, "own") != "own" || none.Schema(i18n.Ja, nil) != nil {
		t.Fatal("no translations: the manifest's text")
	}
	if got := ts.Secret(i18n.Ja, SecretSpec{Name: "api_key", Title: "API Key"}); got.Title != "API Key" {
		t.Fatal("a secret no translation names keeps its title:", got)
	}
}

// The form's copy has the translated titles, descriptions, labels and confirmations, nested ones too; a confirmation
// is never added, and the shared schema is not touched.
func TestTranslationsSchema(t *testing.T) {
	s, err := ParseSchema(json.RawMessage(`{"type": "object", "properties": {
	  "region": {"type": "string", "title": "地区", "oneOf": [{"const": "cn", "title": "中国"}, {"const": "intl"}]},
	  "level": {"type": "integer", "enum": [1, 2]},
	  "tags": {"type": "array", "title": "标签", "items": {"type": "string", "enum": ["a", "b"]}},
	  "on": {"type": "boolean", "title": "开启", "x-confirm": "确定？"},
	  "quiet": {"type": "boolean", "title": "安静"},
	  "proxy": {"type": "object", "title": "代理", "properties": {"host": {"type": "string", "title": "主机"}}}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	en := &Translation{Configuration: map[string]*FieldText{
		"region": {Title: "Region", Description: "Where the account is", Choices: map[string]string{"cn": "China", "intl": "International"}},
		"level":  {Choices: map[string]string{"2": "High"}},
		"tags":   {Choices: map[string]string{"a": "Alpha"}},
		"on":     {Title: "On", Confirm: "Sure?"},
		"quiet":  {Confirm: "Never asked"},
		"proxy":  {Properties: map[string]*FieldText{"host": {Title: "Host"}}},
	}}
	ja := &Translation{Configuration: map[string]*FieldText{"region": {Title: "地域"}}}
	got := Translations{i18n.En: en, i18n.Ja: ja}.Schema(i18n.Ja, s)
	field := func(s *Schema, name string) Field {
		for _, f := range s.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("no field %s", name)
		return Field{}
	}
	region := field(got, "region")
	if region.Title != "地域" || region.Description != "Where the account is" || region.Enum[0].Title != "China" || region.Enum[1].Title != "International" {
		t.Fatalf("Japanese, then English: %+v", region)
	}
	if level := field(got, "level"); level.Enum[0].Title != "" || level.Enum[1].Title != "High" {
		t.Fatalf("a number's choice by its JSON: %+v", level.Enum)
	}
	if tags := field(got, "tags"); tags.Title != "标签" || tags.Items.Enum[0].Title != "Alpha" || tags.Items.Enum[1].Title != "" {
		t.Fatalf("an array's choices label its items: %+v", tags.Items.Enum)
	}
	if on, quiet := field(got, "on"), field(got, "quiet"); on.Title != "On" || on.Confirm != "Sure?" || quiet.Confirm != "" {
		t.Fatalf("confirmations translated, never added: %+v %+v", on, quiet)
	}
	if host := field(got, "proxy").Fields[0]; host.Title != "Host" {
		t.Fatalf("an object's own properties: %+v", host)
	}
	if field(s, "region").Title != "地区" || field(s, "region").Enum[0].Title != "中国" || field(s, "tags").Items.Enum[0].Title != "" ||
		field(s, "on").Confirm != "确定？" || field(s, "proxy").Fields[0].Title != "主机" {
		t.Fatal("the shared schema changed")
	}
	if Translations(nil).Schema(i18n.Ja, s) != s || (Translations{i18n.En: en}).Schema(i18n.ZhHans, s) != s {
		t.Fatal("nothing to translate: the same schema")
	}
}

func writeL10n(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, L10nDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, L10nDir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A package's files: each language this hub has; one that does not check out is left out with a warning, in the
// status text the clients translate; another language's file and a link are not read.
func TestReadTranslations(t *testing.T) {
	dir := t.TempDir()
	if ts, w := ReadTranslations(dir); ts != nil || w != nil {
		t.Fatal("no l10n directory: nothing", ts, w)
	}
	writeL10n(t, dir, "en.json", `{"name": "Notes", "secrets": {"api_key": {"title": "API key"}}, "later": {"a": 1}}`)
	writeL10n(t, dir, "ja.json", `{"name": "`+strings.Repeat("長", 41)+`"}`)
	writeL10n(t, dir, "fr.json", `{"name": "Notes (fr)"}`)
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	_ = os.WriteFile(outside, []byte(`{"name": "Read through a link"}`), 0o644)
	if err := os.Symlink(outside, filepath.Join(dir, L10nDir, "zh-Hans.json")); err != nil {
		t.Fatal(err)
	}
	ts, warnings := ReadTranslations(dir)
	if len(ts) != 1 || ts[i18n.En].Name != "Notes" || ts[i18n.En].Secrets["api_key"].Title != "API key" {
		t.Fatalf("%+v", ts)
	}
	if len(warnings) != 2 || !strings.Contains(warnings[0], "l10n/zh-Hans.json") || !strings.Contains(warnings[1], "l10n/ja.json is left out: name: 1 to 40 characters") {
		t.Fatal(warnings)
	}
	if zh := i18n.ExtensionStatusText(i18n.ZhHans, warnings[1]); !strings.HasPrefix(zh, "l10n/ja.json 已略过") {
		t.Fatal("clients get the warning in their language:", zh)
	}
	writeL10n(t, dir, "en.json", `{"name": 3}`)
	writeL10n(t, dir, "ja.json", strings.Repeat(" ", l10nFileMost+1))
	if ts, warnings := ReadTranslations(dir); ts != nil || len(warnings) != 3 {
		t.Fatal("a wrong type and a file too large are left out too:", ts, warnings)
	}
}

// An index entry's l10n: languages this hub has, all of them checking out, or none; a broken one leaves the entry in
// the index with a warning.
func TestEntryTranslations(t *testing.T) {
	ts, err := ParseEntryTranslations(json.RawMessage(`{"en": {"name": "Notes"}, "ja": {"name": "ノート"}, "ko": {"name": 1}}`))
	if err != nil || len(ts) != 2 || ts.Name(i18n.Ja, "") != "ノート" {
		t.Fatal(ts, err)
	}
	if _, err := ParseEntryTranslations(json.RawMessage(`{"en": {"description": ["x"]}}`)); err == nil {
		t.Fatal("a translation of the wrong shape")
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(exampleManifest), &m)
	entry := func(l10n any) map[string]any {
		e := map[string]any{"manifest": m, "packages": []any{map[string]any{"url": "notes.tar.gz", "sha256": strings.Repeat("a", 64)}}, "l10n": l10n}
		return e
	}
	for _, c := range []struct {
		l10n     any
		name     string
		warnings int
	}{
		{map[string]any{"ja": map[string]any{"name": "ノート"}}, "ノート", 0},
		{map[string]any{"ja": map[string]any{"name": ""}, "en": map[string]any{"name": strings.Repeat("x", 41)}}, "Example Notes", 1},
		{"not an object", "Example Notes", 1},
	} {
		b, _ := json.Marshal(map[string]any{"market_version": 1, "name": "Example", "extensions": []any{entry(c.l10n)}})
		ix, warnings, err := ParseIndex(b)
		if err != nil || len(ix.Extensions) != 1 || len(warnings) != c.warnings {
			t.Fatalf("%v: %v %v", c.l10n, warnings, err)
		}
		if got := ix.Extensions[0].Translations().Name(i18n.Ja, ix.Extensions[0].Parsed.Name); got != c.name {
			t.Fatalf("%v: %q", c.l10n, got)
		}
		if c.warnings > 0 && (ix.Extensions[0].L10n != nil || !strings.Contains(warnings[0], "example-notes: the index entry's l10n is left out")) {
			t.Fatal(warnings)
		}
	}
}

// A package's translations show in its preview and once installed: the name and description, the secret a setup
// misses; what is approved and installed is the manifest as it is.
func TestPackageTranslationsPreviewAndInstalled(t *testing.T) {
	ctx := context.Background()
	r, _, _ := newTestRegistry(t)
	pkg := t.TempDir()
	writePackage(t, pkg, nil)
	writeL10n(t, pkg, "ja.json", `{"name": "サンプルノート", "description": "ノートをナレッジベースへ", "secrets": {"api_key": {"title": "API キー"}}}`)
	writeL10n(t, pkg, "en.json", `{"name": "Example Notes (en)"}`)
	pv, err := r.Preview(ctx, "dir:"+pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	shown := pv.In(i18n.Ja)
	if shown.Manifest.Name != "サンプルノート" || shown.Manifest.Description != "ノートをナレッジベースへ" || shown.Digest != pv.Digest {
		t.Fatalf("%+v", shown.Manifest)
	}
	if pv.Manifest.Name != "Example Notes" || pv.In(i18n.ZhHans).Manifest.Name != "Example Notes" {
		t.Fatal("the approved manifest is untouched; Simplified Chinese has no translation here:", pv.Manifest.Name)
	}
	p, err := r.Install(ctx, pv.ID, pv.Digest, "cli")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.Dir, L10nDir, "ja.json")); err != nil {
		t.Fatal("the translations are installed with the package:", err)
	}
	if p.L10n.Name(i18n.Ja, p.Full.Name) != "サンプルノート" || p.L10n.Name(i18n.En, p.Full.Name) != "Example Notes (en)" || len(p.Warnings) != 0 {
		t.Fatalf("%+v %v", p.L10n, p.Warnings)
	}
	if m := r.Missing(i18n.With(ctx, i18n.Ja), p); len(m) != 1 || !strings.Contains(m[0], "API キー") {
		t.Fatal("the missing secret by its translated title:", m)
	}
	// read again from the installed package when the hub starts
	r2 := NewRegistry(r.dir, r.store, rot13{}, 202611100000)
	if err := r2.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if got := r2.Get("example-notes"); got == nil || got.L10n.Name(i18n.Ja, "") != "サンプルノート" {
		t.Fatalf("%+v", got)
	}
}

// A sidebar group's title (#9071) is translated by its id under groups, like a page's under ui; a title too long leaves
// the file out with a warning, as any other.
func TestTranslationsGroupTitle(t *testing.T) {
	dir := t.TempDir()
	writeL10n(t, dir, "en.json", `{"groups": {"workbench": "Workbench"}}`)
	writeL10n(t, dir, "ja.json", `{"groups": {"workbench": "`+strings.Repeat("長", 41)+`"}}`)
	ts, warnings := ReadTranslations(dir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "groups.workbench: at most 40 characters") {
		t.Fatal(warnings)
	}
	if got := ts.GroupTitle(i18n.En, "workbench", "工作台"); got != "Workbench" {
		t.Fatalf("en: %q", got)
	}
	if got := ts.GroupTitle(i18n.ZhHans, "workbench", "工作台"); got != "工作台" {
		t.Fatalf("zh-Hans keeps the manifest's own: %q", got)
	}
	if got := ts.GroupTitle(i18n.En, "assets", "资产"); got != "资产" {
		t.Fatalf("a group no translation names keeps its title: %q", got)
	}
}
