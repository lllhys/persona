package prompts

// KBGetPeople is the line kb_get writes under a project's meta.json when it shows only some of its people
// (assistant/kb_get_people): Shown of Total, and Meta, the meta.json with all of them.
type KBGetPeople struct {
	Shown, Total int
	Meta         string
}

func RenderKBGetPeople(p KBGetPeople) string { return render("assistant/kb_get_people", p) }

// KBGetCut is the line a kb_get result cut at its size ceiling ends with (assistant/kb_get_cut): how much was left
// out, in characters and estimated tokens, and Path, the entry's file or directory.
type KBGetCut struct {
	Chars, Tokens int
	Path          string
}

func RenderKBGetCut(c KBGetCut) string { return render("assistant/kb_get_cut", c) }
