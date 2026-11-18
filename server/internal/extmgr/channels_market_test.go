package extmgr

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

// marketChannelsByCatalog is marketChannels as it was: the whole catalog built, then the entries with a channel
// picked from it.
func marketChannelsByCatalog(h *Manager, have []channel.View) []channel.View {
	if h.extensions == nil {
		return nil
	}
	var out []channel.View
	seen := map[string]bool{}
	for _, v := range have {
		seen[v.ID] = true
	}
	for _, it := range h.catalog("", "") {
		if it.Installed != "" || len(it.Contributes["channel"]) == 0 || it.Problem != "" {
			continue
		}
		for _, id := range it.Contributes["channel"] {
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, channel.View{ID: id, Name: it.Name, Kind: channel.KindIM, Source: channel.SourceMarket, Plugin: it.ID, Market: it.Market,
				State: channel.StateNotInstalled, Bind: channel.BindNone, Description: it.Description})
		}
	}
	return out
}

// mixedMarkets is a manager with every kind of market entry the channel list tells apart, in three markets.
func mixedMarkets(t *testing.T, lang i18n.Lang) *Manager {
	t.Helper()
	h := marketManager(t, lang, marketManifest("has-already", "installedchat"))
	with := func(m *extension.Manifest, change func(*extension.MarketEntry)) extension.MarketEntry {
		e := marketEntryOf(m)
		if change != nil {
			change(&e)
		}
		return e
	}
	raw := func(m *extension.Manifest, channels string) *extension.Manifest {
		m.Contributes["channel"] = json.RawMessage(channels)
		return m
	}
	newer := marketManifest("needs-newer-hub", "newerchat")
	newer.Engines.Persona = ">=999912310000"
	h.build.Number = 202611110000
	putMarket(h, "first", 1, extension.SigUnsigned,
		with(marketManifest("plain"), nil),                           // no channel
		with(marketManifest("one-chat", "chatone"), nil),             // listed
		with(marketManifest("has-already", "installedchat"), nil),    // this hub has it
		with(marketManifest("two-chats", "chattwo", "chatone"), nil), // chattwo listed, chatone is one-chat's
		with(marketManifest("other-platform", "elsewherechat"), func(e *extension.MarketEntry) {
			e.Packages[0].Platforms = []string{"plan9-mips"} // no package for this platform
		}),
		with(newer, nil), // needs a newer hub
		with(marketManifest("untranslated", "plainchat"), func(e *extension.MarketEntry) { e.L10n = nil }),
		with(marketManifest("bad-translations", "oddchat"), func(e *extension.MarketEntry) { e.L10n = json.RawMessage(`{"en":7}`) }),
		with(raw(marketManifest("empty-list"), `[]`), nil),
		with(raw(marketManifest("null-list"), `null`), nil),
		with(raw(marketManifest("not-a-list"), `{"id":"x"}`), nil),
		with(raw(marketManifest("invalid-item"), `[{"id":"","name":"No id"}]`), nil),
		with(raw(marketManifest("one-valid-item"), `[{"id":"","name":"No id"},{"id":"validchat","name":"Valid","kind":"im"}]`), nil),
		with(marketManifest("is-feishu", channel.Feishu), nil), // an id the list has already
		with(marketManifest("renamed", "renamedchat"), func(e *extension.MarketEntry) { e.Replaces = "old-name" }),
	)
	// The official market lists its entries most downloaded first, then by name: not the order of its index.
	putMarket(h, "official", 9, extension.SigSigned,
		downloaded(with(marketManifest("few-downloads", "fewchat"), nil), 5),
		downloaded(with(marketManifest("shared-low", "sharedchat"), nil), 1), // loses sharedchat to shared-high, listed before it
		downloaded(with(marketManifest("plain-popular"), nil), 10000),
		with(marketManifest("no-count", "nocountchat"), nil),
		downloaded(with(marketManifest("b-name", "bchat"), nil), 50),
		downloaded(with(marketManifest("a-name", "achat"), nil), 50),
		downloaded(with(marketManifest("shared-high", "sharedchat", "highchat"), nil), 100),
		downloaded(with(marketManifest("most-downloads", "mostchat"), nil), 900),
	)
	makeOfficial(t, h, "official")
	putMarket(h, "second", 2, extension.SigUnsigned,
		with(marketManifest("again", "chatone", "chatsix"), nil), // chatone again, from a later market
		with(marketManifest("plain-too"), nil),
	)
	putMarket(h, "refused", 3, extension.SigInvalid, // its signature fails: nothing of it can be installed
		with(marketManifest("unsigned-chat", "refusedchat"), nil),
	)
	ms := h.marketsState()
	ms.mu.Lock()
	ms.byID["unread"] = &marketState{rec: extension.MarketRecord{ID: "unread", Spec: "https://markets.example/unread"}} // no index yet
	ms.mu.Unlock()
	return h
}

// The market's part of the channel list is what the whole catalog gave, entry for entry and in the same order: in
// every language, with and without the channels the list already has, and with no markets at all.
func TestMarketChannelsAreTheCatalogs(t *testing.T) {
	haves := [][]channel.View{
		nil,
		{{ID: channel.App}, {ID: channel.Feishu}, {ID: "voice"}},
		{{ID: channel.App}, {ID: "chatone"}, {ID: "chatsix"}, {ID: "plainchat"}},
	}
	for _, lang := range append([]i18n.Lang{""}, i18n.Langs...) {
		h := mixedMarkets(t, lang)
		for i, have := range haves {
			got, want := h.marketChannels(have), marketChannelsByCatalog(h, have)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("language %q, have %d:\n got %+v\nwant %+v", lang, i, got, want)
			}
		}
		empty := marketManager(t, lang)
		if got, want := empty.marketChannels(haves[1]), marketChannelsByCatalog(empty, haves[1]); got != nil || want != nil {
			t.Errorf("language %q, no markets: %+v, %+v", lang, got, want)
		}
	}
	// What the table holds, so a change that empties both sides does not pass.
	h := mixedMarkets(t, "en")
	var ids []string
	for _, v := range h.marketChannels(haves[1]) {
		ids = append(ids, v.ID+"/"+v.Plugin+"/"+v.Market)
		if v.Source != channel.SourceMarket || v.State != channel.StateNotInstalled || v.Kind != channel.KindIM || v.Bind != channel.BindNone || v.Description == "" {
			t.Errorf("market channel: %+v", v)
		}
	}
	want := []string{"mostchat/most-downloads/official", "sharedchat/shared-high/official", "highchat/shared-high/official",
		"achat/a-name/official", "bchat/b-name/official", "fewchat/few-downloads/official", "nocountchat/no-count/official",
		"chatone/one-chat/first", "chattwo/two-chats/first", "plainchat/untranslated/first", "oddchat/bad-translations/first",
		"validchat/one-valid-item/first", "renamedchat/renamed/first", "chatsix/again/second"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("market channels:\n got %q\nwant %q", ids, want)
	}
	if got := h.marketChannels(haves[1]); got[7].Name != "Example one-chat (en)" || got[9].Name != "Example untranslated" {
		t.Errorf("names: %q, %q", got[7].Name, got[9].Name)
	}
	if (&Manager{}).marketChannels(nil) != nil {
		t.Error("a manager without extensions has market channels")
	}
}

// The catalog itself is unchanged by being built entry by entry: every market's items are the ones catalogItem
// builds, in the catalog's order (the official market's most downloaded first), and a search and a market filter give
// the items they gave.
func TestCatalogItemsAreTheCatalogs(t *testing.T) {
	h := mixedMarkets(t, "ja")
	all := h.catalog("", "")
	if len(all) != 26 {
		t.Fatalf("%d catalog items, want 26", len(all))
	}
	var built []CatalogItem
	for _, s := range h.marketStates() {
		if s.index == nil {
			continue
		}
		var items []CatalogItem
		for i := range s.index.Extensions {
			e := &s.index.Extensions[i]
			items = append(items, h.catalogItem(h.userLang(), s, e, e.Translations()))
		}
		if extension.IsOfficialMarket(s.rec.Spec) {
			slices.SortFunc(items, officialCatalogOrder)
		}
		built = append(built, items...)
	}
	if !reflect.DeepEqual(built, all) {
		t.Errorf("entry by entry:\n got %+v\nwant %+v", built, all)
	}
	byID := map[string]CatalogItem{}
	var ids []string
	for _, it := range all {
		byID[it.Market+"/"+it.ID] = it
		ids = append(ids, it.ID)
	}
	if want := []string{"plain-popular", "most-downloads", "shared-high", "a-name", "b-name", "few-downloads", "shared-low", "no-count"}; !reflect.DeepEqual(ids[:8], want) {
		t.Errorf("the official market's order: %q, want %q", ids[:8], want)
	}
	if got := h.catalog("two-chats", ""); len(got) != 1 || !reflect.DeepEqual(got[0], byID["first/two-chats"]) {
		t.Errorf("search: %+v", got)
	}
	if got := h.catalog("", "second"); len(got) != 2 || !reflect.DeepEqual(got[0], byID["second/again"]) || !reflect.DeepEqual(got[1], byID["second/plain-too"]) {
		t.Errorf("one market: %+v", got)
	}
	if byID["first/has-already"].Installed == "" || byID["first/other-platform"].Problem == "" || byID["first/needs-newer-hub"].Problem == "" ||
		byID["refused/unsigned-chat"].Problem == "" || byID["first/one-chat"].Problem != "" || byID["official/most-downloads"].Problem != "" ||
		byID["official/most-downloads"].Downloads == nil || *byID["official/most-downloads"].Downloads != 900 {
		t.Errorf("installed, problems and downloads: %+v", all)
	}
}

// Only the entries that contribute a channel are built: 2 of the 83 here, 360 allocations a call measured, where the
// whole catalog was 12,719. The bounds leave room for an item that grows, not for building the entries without one.
func TestMarketChannelsBuildOnlyChannelEntries(t *testing.T) {
	h := officialLikeMarket(t, 83)
	have := []channel.View{{ID: channel.App}, {ID: channel.Feishu}, {ID: "voice"}}
	if got, want := h.marketChannels(have), marketChannelsByCatalog(h, have); len(got) != 2 || !reflect.DeepEqual(got, want) {
		t.Fatalf("market channels:\n got %+v\nwant %+v", got, want)
	}
	one := testing.AllocsPerRun(20, func() { h.marketChannels(have) })
	all := testing.AllocsPerRun(20, func() { marketChannelsByCatalog(h, have) })
	if one > 1000 || all < 10*one {
		t.Errorf("the market's channels allocated %.0f times, the whole catalog %.0f: want at most 1000, and a tenth of the catalog", one, all)
	}
}
