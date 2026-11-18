package node

import (
	"strings"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// Claude Code's send-now hint beside a queued message (#6088) names the keys of its chat:sendNow binding as
// `<keys> to send now`: lower case, "+" between a key and its modifiers, a space between the strokes of a chord, built
// from the binding rather than kept as text. 2.1.311 to 2.1.321 bind ctrl+x ctrl+s and ctrl+enter both and name
// ctrl+enter where the terminal reports extended keys outside tmux and screen (a herdr pane), ctrl+x ctrl+s elsewhere
// (#2467), and "click to send now" when no key is bound. The hint is its own line under the message (2.1.311 on), or
// follows the message on its line after a gap (2.1.319); 2.1.326 labels its own line "⎿  Queued · <keys> to send now"
// where that fits the width, the bare hint as before where it doesn't (#9628). The node reads it by that shape, not as fixed
// strings (#4537), so another binding, label, separator or letter case still gives its keys. Keys a terminal can't
// press (cmd, fn, a click) are quoted and nothing is pressed.

// sendNowLineRE is a line ending in the send-now words: what comes before them (the keys, perhaps after the queued
// message) and from the separator on. A gap or " · " may follow, for more hints on the line.
var sendNowLineRE = lazyre.New(`(?i)^(.*?)(\s*(?:\bto|·|•|:|-|–|—|\||→)?\s*send\s+now)\s*\)?(?:\s*$|\s+·\s|\s{2,})`)

// hintStartRE is where the hint's keys start after other words on its line: the gap after a queued message the hint
// follows, or the " · " after a label ("Queued · ctrl+enter to send now").
var hintStartRE = lazyre.New(`\s+[·•]\s+|\s{2,}|\t`)

// sendNowOffer is the send-now hint the screen shows beside a queued message (the lowest one, under any older text
// that quotes one) and the keys it names, as Persona's key names: no keys when a terminal can't press them, "" for no
// hint.
func sendNowOffer(screen string) (hint string, keys []string) {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		m := sendNowLineRE.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		before := m[1]
		if strings.TrimSpace(before) == "" && i > 0 {
			if before = lines[i-1]; !allStrokes(before) { // the hint wrapped: its keys are the line above, alone
				continue
			}
		}
		chord := hintChord(before)
		if chord == nil {
			continue
		}
		hint = strings.Join(strings.Fields(strings.Join(chord, " ")+m[2]), " ")
		return hint, strokeKeys(chord)
	}
	return "", nil
}

// hintChord is the strokes the hint names at the end of before: all of it when it is keys alone (the hint's own
// line), else what follows the last gap or " · " (after the queued message or a label), so a message ending in a key
// name ("…with ctrl+c") lends none; nil for neither.
func hintChord(before string) []string {
	if !allStrokes(before) {
		starts := hintStartRE.FindAllStringIndex(strings.TrimRight(before, " \t"), -1)
		if len(starts) == 0 {
			return nil
		}
		if before = before[starts[len(starts)-1][1]:]; !allStrokes(before) {
			return nil
		}
	}
	return strings.Fields(strings.TrimPrefix(strings.TrimSpace(before), "("))
}

// allStrokes: s (after an opening parenthesis) is only key strokes.
func allStrokes(s string) bool {
	f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(s), "("))
	if len(f) == 0 {
		return false
	}
	for _, t := range f {
		if _, _, ok := parseStroke(t); !ok {
			return false
		}
	}
	return true
}

// strokeKeys is the chord's strokes as Persona's key names, nil when one of them can't be pressed from a terminal.
func strokeKeys(chord []string) []string {
	var keys []string
	for _, t := range chord {
		k, pressable, ok := parseStroke(t)
		if !ok || !pressable {
			return nil
		}
		keys = append(keys, k)
	}
	return keys
}

// strokeMods are the modifier words and glyphs Claude Code's key hints use, as Persona's modifiers; "" for one a
// terminal can't send (cmd / super, fn).
var strokeMods = map[string]string{
	"ctrl": "ctrl", "control": "ctrl", "⌃": "ctrl", "shift": "shift", "⇧": "shift",
	"alt": "alt", "opt": "alt", "option": "alt", "meta": "alt", "⌥": "alt",
	"cmd": "", "command": "", "super": "", "win": "", "⌘": "", "fn": "",
}

// strokeNames are the key names and glyphs Claude Code's key hints use, as Persona's key names.
var strokeNames = map[string]string{
	"enter": "enter", "return": "enter", "⏎": "enter", "↵": "enter", "tab": "tab", "⇥": "tab",
	"esc": "esc", "escape": "esc", "⎋": "esc", "space": "space", "␣": "space",
	"backspace": "backspace", "⌫": "backspace", "delete": "delete", "del": "delete", "⌦": "delete",
	"insert": "insert", "home": "home", "end": "end",
	"pageup": "pageup", "pgup": "pageup", "pagedown": "pagedown", "pgdn": "pagedown",
	"up": "up", "↑": "up", "down": "down", "↓": "down", "left": "left", "←": "left", "right": "right", "→": "right",
}

// parseStroke reads one stroke of a key hint ("ctrl+enter", "Ctrl+X", "opt+s", "⌃⏎", "^x", "click") as a Persona key
// name. ok is false for a word that isn't a stroke: a lone letter or a word with no modifier, which the hint never
// shows alone. pressable is false for a stroke a terminal can't send (cmd, fn, a click).
func parseStroke(t string) (key string, pressable bool, ok bool) {
	t = strings.ToLower(t)
	if t == "click" || t == "tap" {
		return t, false, true
	}
	var mods []string
	var key0 string
	switch r := []rune(t); {
	case len(r) == 2 && r[0] == '^' && r[1] > ' ':
		mods, key0 = []string{"⌃"}, string(r[1]) // ^X: ctrl+x
	case strings.Contains(t, "+") && len(t) > 1:
		parts := strings.Split(t, "+")
		if strings.HasSuffix(t, "++") { // the + key itself
			parts = append(parts[:len(parts)-2], "+")
		}
		mods, key0 = parts[:len(parts)-1], parts[len(parts)-1]
	default:
		for len(r) > 1 {
			if _, isMod := strokeMods[string(r[0])]; !isMod {
				break
			}
			mods, r = append(mods, string(r[0])), r[1:]
		}
		key0 = string(r)
	}
	pressable = true
	var ctrl, shift, alt bool
	for _, m := range mods {
		p, known := strokeMods[m]
		if !known {
			return "", false, false
		}
		switch p {
		case "ctrl":
			ctrl = true
		case "shift":
			shift = true
		case "alt":
			alt = true
		default:
			pressable = false
		}
	}
	name, named := strokeNames[key0]
	switch {
	case named:
	case len(mods) > 0 && len([]rune(key0)) == 1 && key0 > " ":
		name = key0
	case len(key0) >= 2 && len(key0) <= 3 && key0[0] == 'f' && strings.Trim(key0[1:], "0123456789") == "":
		name = key0
	default:
		return "", false, false
	}
	prefix := ""
	if ctrl {
		prefix += "ctrl+"
	}
	if shift {
		prefix += "shift+"
	}
	if alt {
		prefix += "alt+"
	}
	return prefix + name, pressable, true
}

// scrolledUpRE is Claude Code's pill while its fullscreen transcript is scrolled away from the end (2.1.311 to
// 2.1.321): "Jump to bottom" or "N new messages", the key that scrolls back where it names one ("(ctrl+End)"; on macOS
// "(click)" or ": fn+↓ to scroll"), then ↓.
var scrolledUpRE = lazyre.New(`(?i)(?:\bjump to bottom|\b\d+ new messages?)(?: \(([^)\n]*)\))?[^\n]*?↓`)

// scrollToBottom is Claude Code's own scroll:bottom, ctrl+End, bound on every platform, pressed when the pill names
// no key.
var scrollToBottom = []string{"ctrl+end"}

// scrolledUp: the screen is a fullscreen Claude Code scrolled up, showing older lines rather than the end where a
// queued message and its hint are (#4537), and the keys that bring the end back.
func scrolledUp(screen string) ([]string, bool) {
	m := scrolledUpRE.FindStringSubmatch(screen)
	if m == nil {
		return nil, false
	}
	if keys := strokeKeys(strings.Fields(m[1])); keys != nil {
		return keys, true
	}
	return scrollToBottom, true
}
