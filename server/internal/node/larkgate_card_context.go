package node

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// larkCardContext is the human context of one gated Lark message write. OK is false when its metadata could not
// be read; larkSummaryWithContext then keeps the old method/path/body summary instead of delaying the card.
type larkCardContext struct {
	OK          bool
	Chat        string
	ReplySender string
	ReplyText   string
	InTopic     bool
}

// larkContextFromCLI reads only the message's own chat and, for a reply, the message it answers. It is a variable
// so tests can substitute a fake binary or reader; no gate decision depends on it.
var larkContextFromCLI = func(ctx context.Context, n *Node, cfg proto.AssistantLark, c larkCall) larkCardContext {
	kind, chatID, messageID := larkMessageTarget(c)
	if kind == "" {
		return larkCardContext{}
	}
	bin := "lark-cli"
	if cfg.CLI != nil && cfg.CLI.Bin != "" {
		bin = cfg.CLI.Bin
	}
	if !filepath.IsAbs(bin) {
		p, err := exec.LookPath(bin)
		if err != nil {
			return larkCardContext{}
		}
		bin = p
	}
	read := func(path string, out any) bool {
		cli := cfg.CLI
		if cli == nil {
			cli = &proto.LarkCLI{}
		}
		args := append(larkCLIProfile(cli), "api", "GET", path)
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = larkCLIEnv(bin)
		data, err := cmd.Output()
		if err != nil || json.Unmarshal(data, out) != nil {
			return false
		}
		return true
	}
	chat := chatID
	if kind == "reply" {
		var answer struct {
			Message struct {
				ChatID   string `json:"chat_id"`
				ThreadID string `json:"thread_id"`
				Sender   struct {
					Name string `json:"name"`
					ID   string `json:"id"`
				} `json:"sender"`
				Body struct {
					Content string `json:"content"`
				} `json:"body"`
			} `json:"message"`
		}
		if !read("/open-apis/im/v1/messages/"+messageID, &answer) {
			return larkCardContext{}
		}
		text := larkNestedText(answer.Message.Body.Content)
		if text == "" {
			return larkCardContext{}
		}
		if answer.Message.ChatID != "" {
			chat = answer.Message.ChatID
		}
		sender := answer.Message.Sender.Name
		if sender == "" {
			sender = answer.Message.Sender.ID
		}
		out := larkCardContext{Chat: chat, ReplySender: sender, ReplyText: larkExcerpt(text, 160), InTopic: answer.Message.ThreadID != ""}
		var info struct {
			Chat struct {
				Name string `json:"name"`
			} `json:"chat"`
		}
		if chat != "" && read("/open-apis/im/v1/chats/"+chat, &info) && info.Chat.Name != "" {
			out.Chat = info.Chat.Name
		}
		out.OK = out.Chat != ""
		return out
	}
	var info struct {
		Chat struct {
			Name string `json:"name"`
		} `json:"chat"`
	}
	if chat == "" || !read("/open-apis/im/v1/chats/"+chat, &info) {
		return larkCardContext{}
	}
	if info.Chat.Name != "" {
		chat = info.Chat.Name
	}
	return larkCardContext{OK: true, Chat: chat}
}

// larkCardContexts enriches each message write independently, with a short total budget per call. Any other gated
// call, or a failed read, is left to the old summary; nothing here grants or denies a call.
func (n *Node) larkCardContexts(ctx context.Context, cfg proto.AssistantLark, calls []larkCall) []larkCardContext {
	out := make([]larkCardContext, len(calls))
	for i, c := range calls {
		if c.CLI {
			continue
		}
		if kind, _, _ := larkMessageTarget(c); kind == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		out[i] = larkContextFromCLI(ctx, n, cfg, c)
		cancel()
	}
	return out
}

// larkMessageTarget recognizes only the message-send and message-reply routes whose body is user-visible text.
func larkMessageTarget(c larkCall) (kind, chatID, messageID string) {
	if c.Method != "POST" {
		return "", "", ""
	}
	parts := strings.Split(strings.Trim(c.Path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "conversations" && parts[2] == "messages" {
		if len(parts) == 3 {
			return "send", parts[1], ""
		}
		if len(parts) == 5 && parts[4] == "replies" {
			return "reply", parts[1], parts[3]
		}
	}
	if len(parts) == 3 && parts[0] == "messages" && parts[2] == "replies" {
		return "reply", "", parts[1]
	}
	for i, part := range parts {
		if part == "chats" && i+2 < len(parts) && parts[i+2] == "messages" && i+2 == len(parts)-1 {
			return "send", parts[i+1], ""
		}
		if part == "messages" && i+2 < len(parts) && parts[i+2] == "replies" && i+2 == len(parts)-1 {
			return "reply", "", parts[i+1]
		}
	}
	return "", "", ""
}

func larkBodyText(body string) string {
	var v map[string]any
	if json.Unmarshal([]byte(body), &v) != nil {
		return ""
	}
	for _, key := range []string{"text", "content"} {
		if s, ok := v[key].(string); ok {
			if key == "content" {
				return larkNestedText(s)
			}
			return strings.TrimSpace(s)
		}
		if obj, ok := v[key].(map[string]any); ok {
			if s, ok := obj["text"].(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func larkNestedText(content string) string {
	var v struct {
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(content), &v) == nil && v.Text != "" {
		return strings.TrimSpace(v.Text)
	}
	return strings.TrimSpace(content)
}

func larkExcerpt(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	r := []rune(text)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return text
}

// larkMessageSummary renders a send or reply without its HTTP route or internal JSON fields. ok is false when
// metadata was unavailable, in which case the caller uses the old summary.
func larkMessageSummary(c larkCall, meta larkCardContext, lang i18n.Lang) (string, bool) {
	kind, _, _ := larkMessageTarget(c)
	if kind == "" || !meta.OK {
		return "", false
	}
	text := larkBodyText(c.Body)
	if text == "" {
		return "", false
	}
	var b strings.Builder
	if kind == "send" {
		b.WriteString(i18n.T(lang, "node-larkgate.send_to", i18n.Args{"chat": meta.Chat}))
	} else if meta.ReplySender != "" {
		b.WriteString(i18n.T(lang, "node-larkgate.reply_to", i18n.Args{"chat": meta.Chat, "sender": meta.ReplySender}))
	} else {
		b.WriteString(i18n.T(lang, "node-larkgate.reply_in_chat", i18n.Args{"chat": meta.Chat}))
	}
	if meta.InTopic {
		b.WriteString(i18n.T(lang, "node-larkgate.topic_suffix"))
	}
	if kind == "reply" && meta.ReplyText != "" {
		b.WriteString("\n" + i18n.T(lang, "node-larkgate.original", i18n.Args{"text": meta.ReplyText}))
	}
	b.WriteString("\n" + i18n.T(lang, "node-larkgate.outgoing", i18n.Args{"text": larkExcerpt(text, 2000)}))
	return b.String(), true
}
