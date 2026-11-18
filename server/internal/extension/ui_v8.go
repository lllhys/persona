package extension

import "github.com/nwverse/persona/sdk/go/wire"

// UI version 8 (docs/extensions/extensions.md "UI version 8", #9158): the columns layout of a dashboard page, and a
// section's span, icon, link and columns. downgrade8 draws them with version 7's for an older app.

// SectionLink is a link beside a section's title (UI version 8).
type SectionLink = wire.SectionLink

// downgrade8 draws UI version 8's fields with version 7's, in place: a columns view as a stack (its cards one under
// another, as a narrow screen draws them), and a section's link as the section's last row with the same action on a
// full page, so it stays reachable. A card and a widget get no such row (they open a page of their own), nor does a
// page that already holds ItemsMost components, nor a native stream (native): its events go back to the source by the
// key of the node pressed, and the row is not one of the source's nodes. A section's span, icon, icon color and
// columns are left out.
func downgrade8(v *View, slot UISlot, native bool) {
	if v.Layout == wire.LayoutColumns {
		v.Layout = wire.LayoutStack
	}
	n := 0
	for _, s := range v.Sections {
		for _, e := range s.Items {
			n += max(1, len(e.Tiles))
		}
	}
	for i := range v.Sections {
		s := &v.Sections[i]
		link := s.Link
		s.Span, s.Icon, s.IconColor, s.Link, s.Columns = "", "", "", nil, 0
		if link == nil || native || slot.Size != SizeFull || n >= ItemsMost {
			continue
		}
		s.Items = append(s.Items, UIElem{Type: "row", Title: link.Title, Action: &link.Action})
		n++
	}
}
