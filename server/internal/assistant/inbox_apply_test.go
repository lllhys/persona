package assistant

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// blocksDiffer answers as comparing the blocks' JSON did, for each field of each block changed alone (each a few
// ways, invalid UTF-8, a time on another zone and one JSON cannot write among them), nil and empty lists, a block
// of another kind and extras that do not decode. A field added to a block and left out of sameDoc & co. fails here.
func TestBlocksDifferAsJSON(t *testing.T) {
	enc := func(it *InboxItem) []byte {
		b, _ := json.Marshal([]any{it.Lark, it.Doc, it.Moment})
		return b
	}
	jsonDiffer := func(it *InboxItem, x *connector.ThingExtra) bool { // blocksDiffer as it was
		c := *it
		setBlocks(&c, x)
		return !bytes.Equal(enc(it), enc(&c))
	}
	extraOf := func(it *InboxItem) *connector.ThingExtra {
		x := &connector.ThingExtra{}
		for _, b := range []struct {
			to  *json.RawMessage
			set bool
			v   any
		}{{&x.Lark, it.Lark != nil, it.Lark}, {&x.Doc, it.Doc != nil, it.Doc}, {&x.Moment, it.Moment != nil, it.Moment}} {
			if b.set {
				*b.to, _ = json.Marshal(b.v)
			}
		}
		return x
	}
	var items, bases []*InboxItem
	for _, field := range []string{"Lark", "Doc", "Moment"} {
		typ := reflect.TypeFor[InboxItem]()
		f, _ := typ.FieldByName(field)
		for at := -1; ; at++ { // -1: the base, every field set
			fill := blockFill{at: at}
			for fill.edit = range 4 {
				it := &InboxItem{}
				fill.n = 0
				fill.value(reflect.ValueOf(it).Elem().FieldByIndex(f.Index))
				items = append(items, it)
				if at < 0 {
					bases = append(bases, it)
					break
				}
			}
			if at >= fill.n {
				break
			}
		}
	}
	extras := []*connector.ThingExtra{{}, {Lark: json.RawMessage("null")}, {Lark: json.RawMessage(`{"feed_id":1}`)},
		{Doc: json.RawMessage(`{"rows":[],"read_at":null}`)}, {Moment: json.RawMessage(`{"notices":null}`)},
		{Moment: json.RawMessage(`{"notices":[]}`)}}
	for _, it := range bases {
		extras = append(extras, extraOf(it))
	}
	for _, it := range items {
		for _, x := range append(extras, extraOf(it)) {
			if got, want := blocksDiffer(it, x), jsonDiffer(it, x); got != want {
				t.Fatalf("blocksDiffer = %v, JSON says %v\nitem  %s\nthing %s %s %s", got, want, enc(it), x.Lark, x.Doc, x.Moment)
			}
		}
		for _, base := range bases {
			if got, want := blocksDiffer(base, extraOf(it)), jsonDiffer(base, extraOf(it)); got != want {
				t.Fatalf("blocksDiffer = %v, JSON says %v\nitem  %s\nthing %s", got, want, enc(base), enc(it))
			}
		}
	}
	// The same values encode alike, whatever both sides hold (not only what a thing's JSON decodes to).
	for _, a := range items {
		for _, b := range items {
			if sameLark(a.Lark, b.Lark) && sameDoc(a.Doc, b.Doc) && sameMoment(a.Moment, b.Moment) && !bytes.Equal(enc(a), enc(b)) {
				t.Fatalf("the same blocks, other JSON:\n%s\n%s", enc(a), enc(b))
			}
		}
	}
}

// migratedThread and migratedKey are built without fmt; the threads are the ones the plugin keys its things by.
func TestMigratedThreadKeys(t *testing.T) {
	for _, c := range []struct {
		it   InboxItem
		want string
	}{
		{InboxItem{Source: "lark", Lark: &InboxLark{FeedID: "f"}}, "chat:f"},
		{InboxItem{Source: "lark", Lark: &InboxLark{AccountID: 1, FeedID: "f"}}, "chat:f"},
		{InboxItem{Source: "lark", Lark: &InboxLark{AccountID: 12, FeedID: "f"}}, "chat:12:f"},
		{InboxItem{Source: "lark", Lark: &InboxLark{AccountID: 2}, Doc: &InboxDoc{NoticeID: "n"}}, "chat:2:"},
		{InboxItem{Source: "feishu", Doc: &InboxDoc{NoticeID: "n"}}, "doc:n"},
		{InboxItem{Source: "lark", Moment: &InboxMoment{PostID: "p"}}, "moment:p"},
		{InboxItem{Source: "mail"}, ""},
	} {
		if got := migratedThread(&c.it); got != c.want {
			t.Errorf("migratedThread = %q, want %q", got, c.want)
		}
		if got, want := migratedKey(&c.it), c.it.Source+"\x00"+c.want; got != want {
			t.Errorf("migratedKey = %q, want %q", got, want)
		}
	}
}

// A Feishu collection that changes nothing (BenchmarkInboxApply/feishu) allocated 17,167 times when blocksDiffer
// encoded both sides' blocks and each item's thread key took fmt and several strings; now 5,615.
func TestInboxApplyAllocations(t *testing.T) {
	if raceEnabled || !isolated(t) {
		return
	}
	now := time.Now()
	ib := benchInbox(2000, now)
	chats, _ := benchCollection(now)
	if changed := ib.applyMigrated("lark", chats, now); len(changed) != 0 {
		t.Fatalf("the collection changed %d items, want none", len(changed))
	}
	if n := testing.AllocsPerRun(20, func() { ib.applyMigrated("lark", chats, now) }); n > 5785 {
		t.Errorf("a Feishu collection of 300 chats allocated %v times, want at most 5785", n)
	}
}

// blockFill sets every field of a block (by reflection) to a value, but changes the leaf numbered at, the edit-th of
// four ways. Lists hold one element; a list or a pointer is a leaf too (nil, empty, longer).
type blockFill struct{ n, at, edit int }

func (f *blockFill) leaf() bool {
	f.n++
	return f.n-1 == f.at
}

func (f *blockFill) value(v reflect.Value) {
	if v.Type() == reflect.TypeFor[time.Time]() {
		t := time.Date(2026, 11, 10, 8, 0, 0, 5, time.UTC)
		if f.leaf() {
			t = [...]time.Time{t.Add(time.Nanosecond), t.In(time.FixedZone("", 3600)), t.In(time.FixedZone("Z", 0)),
				t.AddDate(9000, 0, 0)}[f.edit]
		}
		v.Set(reflect.ValueOf(t))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("v<&")
		if f.leaf() {
			v.SetString([...]string{"w", "", "\xff", "\xfe"}[f.edit])
		}
	case reflect.Int:
		v.SetInt(2)
		if f.leaf() {
			v.SetInt([...]int64{3, 0, 1, -1}[f.edit])
		}
	case reflect.Bool:
		v.SetBool(!f.leaf())
	case reflect.Pointer:
		if f.leaf() && f.edit == 0 {
			return
		}
		v.Set(reflect.New(v.Type().Elem()))
		f.value(v.Elem())
	case reflect.Slice:
		n := 1
		if f.leaf() {
			n = [...]int{-1, 0, 2, 2}[f.edit]
		}
		if n >= 0 {
			v.Set(reflect.MakeSlice(v.Type(), n, n))
		}
		for i := range max(n, 0) {
			f.value(v.Index(i))
		}
	case reflect.Struct:
		for i := range v.NumField() {
			f.value(v.Field(i))
		}
	default:
		panic("blockFill: a block field of kind " + v.Kind().String())
	}
}
