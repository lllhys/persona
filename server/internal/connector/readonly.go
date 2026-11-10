package connector

import (
	"strings"
	"unicode"
)

// writeVerbs are words in a tool's name that say it changes something. A subscription calls its tool again and again
// with nobody watching, so a declared read-only tool is still refused when its name says otherwise (deny only: no
// word ever admits a tool). Words that are as often nouns of reads ("post", "schedule", "label", "tag", "run", "call",
// "request", "book", "export") are left out; the rest are refused even when a read happens to use them.
var writeVerbs = map[string]bool{
	"accept": true, "add": true, "append": true, "apply": true, "approve": true, "archive": true, "assign": true,
	"attach": true, "block": true, "buy": true, "cancel": true, "charge": true, "clear": true, "close": true,
	"comment": true, "confirm": true, "create": true, "decline": true, "delete": true, "deploy": true, "destroy": true,
	"detach": true, "disable": true, "dismiss": true, "drop": true, "edit": true, "empty": true, "enable": true,
	"erase": true, "execute": true, "flag": true, "follow": true, "forward": true, "grant": true, "insert": true,
	"invite": true, "invoke": true, "kill": true, "like": true, "lock": true, "mark": true, "merge": true,
	"modify": true, "move": true, "mute": true, "order": true, "patch": true, "pay": true, "pin": true, "place": true,
	"publish": true, "purchase": true, "purge": true, "put": true, "react": true, "refund": true, "reject": true,
	"remove": true, "rename": true, "reopen": true, "reply": true, "reserve": true, "reset": true, "restart": true,
	"restore": true, "revoke": true, "save": true, "sell": true, "send": true, "set": true, "share": true,
	"sign": true, "snooze": true, "star": true, "start": true, "stop": true, "submit": true, "subscribe": true,
	"trade": true, "transfer": true, "trash": true, "trigger": true, "unarchive": true, "unblock": true,
	"unfollow": true, "unlock": true, "unmute": true, "unpin": true, "unstar": true, "unsubscribe": true,
	"update": true, "upload": true, "upsert": true, "withdraw": true, "write": true,
}

// WriteVerb is the first word of a tool's name that says it changes something, "" when none does. The name is split
// at underscores, hyphens, dots, spaces and lower-to-upper case changes ("sendMessage", "messages_send").
func WriteVerb(name string) string {
	for _, w := range nameWords(name) {
		if writeVerbs[w] {
			return w
		}
	}
	return ""
}

func nameWords(name string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	rs := []rune(name)
	for i, r := range rs {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r) && len(cur) > 0 && (unicode.IsLower(cur[len(cur)-1]) ||
			i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(cur[len(cur)-1])):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}
