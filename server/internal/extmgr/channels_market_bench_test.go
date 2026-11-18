package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

// marketManager is a manager with a registry and no markets yet, showing its text in lang ("" the default); installed
// are extensions this hub already has.
func marketManager(tb testing.TB, lang i18n.Lang, installed ...*extension.Manifest) *Manager {
	tb.Helper()
	h := &Manager{}
	if lang != "" {
		h.opts.Lang = func() i18n.Lang { return lang }
	}
	reg := extension.NewRegistry(tb.TempDir(), nil, nil, 0)
	for _, m := range installed {
		if err := reg.AddBuiltin(m, "/opt/example", true); err != nil {
			tb.Fatal(err)
		}
	}
	if err := reg.Load(context.Background()); err != nil {
		tb.Fatal(err)
	}
	h.extensions = newExtensions(tb.TempDir(), nil, func(string, ...any) {})
	h.extensions.reg = reg
	h.extensions.markets.loaded = true
	h.extensions.markets.byID = map[string]*marketState{}
	return h
}

// putMarket gives h a market with these entries, added at the n-th minute (the catalog lists markets by when they
// were added).
func putMarket(h *Manager, id string, n int, signature string, entries ...extension.MarketEntry) {
	ms := h.marketsState()
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.byID[id] = &marketState{
		rec: extension.MarketRecord{ID: id, Spec: "https://markets.example/" + id, Name: "Market " + id,
			AddedAt: time.Date(2026, 11, 1, 8, n, 0, 0, time.UTC), Signature: extension.MarketSignature{State: signature}},
		index: &extension.MarketIndex{MarketVersion: 1, Name: "Market " + id, Extensions: entries}}
}

// makeOfficial makes market id the official market for this test or benchmark (TestMain turns the official market
// off for the rest), its index signed with the pinned key: the catalog lists its entries most downloaded first.
func makeOfficial(tb testing.TB, h *Manager, id string) {
	tb.Helper()
	was := extension.Official
	extension.Official = extension.OfficialMarket{Spec: "github:example/extensions", SigningKey: "ed25519:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
	tb.Cleanup(func() { extension.Official = was })
	ms := h.marketsState()
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.byID[id].rec.Spec = extension.Official.Spec
	ms.byID[id].rec.Signature = extension.MarketSignature{State: extension.SigSigned, KeyID: keyIDOf(extension.Official.SigningKey)}
	if !extension.IsOfficialMarket(ms.byID[id].rec.Spec) {
		tb.Fatal("the market is not the official one")
	}
}

// downloaded is e with a download count.
func downloaded(e extension.MarketEntry, n int64) extension.MarketEntry {
	e.Downloads = &n
	return e
}

// marketManifest is an extension's manifest as a market lists it: a connector, the hosts it reaches, and the
// channels named, if any.
func marketManifest(id string, channels ...string) *extension.Manifest {
	connectors, _ := json.Marshal([]extension.ConnectorItem{{ID: strings.ReplaceAll(id, "-", "_"), Name: "Example " + id,
		Capabilities: []string{"read", "search"}, Hosts: []string{"api." + id + ".example"}}})
	m := &extension.Manifest{ManifestVersion: extension.ManifestVersion, ID: id, Name: "Example " + id, Version: "1.2.0",
		Description: "Reads the " + id + " service for the assistant: search, recent items and their details, with the account you sign in with.",
		Publisher:   "Example Publisher", Homepage: "https://extensions.example/" + id,
		Engines:     extension.Engines{ExtensionProtocol: extension.Protocol},
		Runtime:     &extension.Runtime{Command: extension.Command{Path: id}},
		Permissions: extension.Permissions{Network: []string{"api." + id + ".example", "auth." + id + ".example", "*.cdn.example"}},
		Contributes: map[string]json.RawMessage{"connectors": connectors}}
	if len(channels) > 0 {
		items := make([]channel.Item, len(channels))
		for i, c := range channels {
			items[i] = channel.Item{ID: c, Name: "Chat " + c, Kind: channel.KindIM,
				Capabilities: channel.Capabilities{Format: channel.FormatPlain, Asks: channel.AsksText}}
		}
		m.Contributes["channel"], _ = json.Marshal(items)
	}
	return m
}

// marketEntryOf is a market's entry for m: translated into every language, tagged, with a package for any platform.
func marketEntryOf(m *extension.Manifest) extension.MarketEntry {
	l10n := map[string]any{}
	for _, l := range i18n.Langs {
		l10n[string(l)] = map[string]string{"name": m.Name + " (" + string(l) + ")", "description": m.Description + " (" + string(l) + ")"}
	}
	raw, _ := json.Marshal(l10n)
	return extension.MarketEntry{Parsed: m, Tags: []string{"example", "work", "search"}, L10n: raw,
		Packages: []extension.MarketPackage{{URL: "packages/" + m.ID + ".tar.gz", SHA256: strings.Repeat("ab", 32)}}}
}

// officialLikeMarket is a manager whose one market has n entries, as the official one has them (83 when this was
// written, one of them contributing a channel): every 40th entry contributes a channel.
func officialLikeMarket(tb testing.TB, n int) *Manager {
	tb.Helper()
	h := marketManager(tb, "")
	entries := make([]extension.MarketEntry, n)
	for i := range entries {
		id := fmt.Sprintf("example-%02d", i)
		if i%40 == 7 {
			entries[i] = marketEntryOf(marketManifest(id, fmt.Sprintf("chat%02d", i)))
		} else {
			entries[i] = marketEntryOf(marketManifest(id))
		}
		entries[i] = downloaded(entries[i], int64(i*37%500))
	}
	putMarket(h, "official", 0, extension.SigSigned, entries...)
	makeOfficial(tb, h, "official")
	return h
}

// BenchmarkMarketChannels: the market's part of the channel list, with a market of the official one's size.
func BenchmarkMarketChannels(b *testing.B) {
	h := officialLikeMarket(b, 83)
	have := []channel.View{{ID: channel.App}, {ID: channel.Feishu}, {ID: "voice"}}
	if got := h.marketChannels(have); len(got) != 2 || got[0].ID != "chat07" || got[0].Source != channel.SourceMarket {
		b.Fatalf("market channels: %+v", got)
	}
	b.ReportAllocs()
	for b.Loop() {
		h.marketChannels(have)
	}
}
