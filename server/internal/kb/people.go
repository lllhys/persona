package kb

import (
	"fmt"
	"strings"
)

func (e *Engine) notePersonNote(c *Chat, name, note string) {
	name, note = strings.TrimSpace(name), strings.TrimSpace(note)
	if name == "" || note == "" {
		return
	}
	for _, pid := range c.People {
		if p, ok := e.Store.Person(pid); ok && p.Name == name {
			p.Note = oneLine(note)
			_ = e.Store.PutPerson(p)
			e.index(KindPerson, p.ID, p.Name, p.Tagging, p.Note, p.LastSeen)
			return
		}
	}
}

// WritePersonSummary renders people/<id>/summary.md from what is known (no model call): who, where, which projects,
// and the DM's context.
func (e *Engine) WritePersonSummary(pid string) {
	p, ok := e.Store.Person(pid)
	if !ok {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", p.Name)
	if p.Pinned != "" {
		fmt.Fprintf(&b, "## 已确认的事实\n%s\n\n", p.Pinned)
	}
	if p.Note != "" {
		fmt.Fprintf(&b, "%s\n\n", p.Note)
	}
	if p.Importance != "" {
		fmt.Fprintf(&b, "重要级 %s · 与我相关性 %d · %s\n\n", p.Importance, p.Relevance, strings.Join(p.Tags, " "))
	}
	if len(p.Projects) > 0 {
		b.WriteString("## 项目\n")
		for _, slug := range p.Projects {
			if pr, ok := e.Store.Project(slug); ok {
				fmt.Fprintf(&b, "- %s（projects/%s）%s\n", pr.Name, pr.Slug, prefixed(" — ", pr.OneLine))
			}
		}
		b.WriteString("\n")
	}
	if len(p.Chats) > 0 {
		b.WriteString("## 会话\n")
		for _, cid := range p.Chats {
			if c, ok := e.Store.Chat(cid); ok {
				fmt.Fprintf(&b, "- %s（chats/%s，%s）%s\n", c.Name, c.ID, c.Kind, prefixed(" — ", c.OneLine))
			}
		}
		b.WriteString("\n")
	}
	for _, cid := range p.Chats {
		if c, ok := e.Store.Chat(cid); ok && (c.Kind == "dm" || c.Kind == "mail") {
			if sum := strings.TrimSpace(e.Store.Summary(KindChat, cid)); sum != "" {
				fmt.Fprintf(&b, "## 和我的私聊\n%s\n", strings.TrimPrefix(sum, "# "+c.Name+"\n\n"))
			}
		}
	}
	_ = e.Store.PutSummary(KindPerson, pid, b.String())
}

func prefixed(p, s string) string {
	if s == "" {
		return ""
	}
	return p + s
}
