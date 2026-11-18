package node

import (
	"fmt"
	"strings"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// Table-aware TOML for the few keys the app edits in a file it does not own (Grok's config.toml, grokconfig.go):
// `table.key` is read and written line by line, so every other line, table, array of tables and comment stays as it
// was. A key without a dot is a top-level key and goes through tomlSet, Codex's helper, unchanged.

var (
	tomlHeader      = lazyre.New(`^\s*\[\s*([A-Za-z0-9_-]+(?:\s*\.\s*[A-Za-z0-9_-]+)*)\s*\]\s*(?:#.*)?$`)
	tomlArrayHeader = lazyre.New(`^\s*\[\[`)
	tomlAnyHeader   = lazyre.New(`^\s*\[`) // outside a multi-line value, a line starting with [ is always a table header
	tomlDottedKV    = lazyre.New(`^(\s*)([A-Za-z0-9_-]+(?:\s*\.\s*[A-Za-z0-9_-]+)*)\s*=\s*(.*)$`)
)

// tomlLine is what one line of the file is, as far as these helpers care.
type tomlLine struct {
	header bool   // a [table] or [[array]] line
	table  string // the table the line belongs to ("" top level; "[[" in an array of tables or a quoted-name table, never matched)
	key    string // a `key = value` line's key, without its table
	value  string // the raw value after =
	inner  bool   // a continuation line of a multi-line string or array
}

func normDotted(k string) string {
	parts := strings.Split(k, ".")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ".")
}

// tomlScan labels each line. Multi-line strings and arrays are followed so their contents are never taken for a
// header or a key.
func tomlScan(lines []string) []tomlLine {
	out := make([]tomlLine, len(lines))
	table := ""
	closer := "" // what ends the multi-line value in progress: `"""`, `'''` or "]"
	depth := 0
	for i, l := range lines {
		if closer != "" {
			out[i] = tomlLine{table: table, inner: true}
			if closer == "]" {
				depth += bracketDelta(l)
				if depth <= 0 {
					closer = ""
				}
			} else if strings.Contains(l, closer) {
				closer = ""
			}
			continue
		}
		if tomlArrayHeader.MatchString(l) {
			table = "[["
			out[i] = tomlLine{header: true, table: table}
			continue
		}
		if m := tomlHeader.FindStringSubmatch(l); m != nil {
			table = normDotted(m[1])
			out[i] = tomlLine{header: true, table: table}
			continue
		}
		if tomlAnyHeader.MatchString(l) { // a quoted name: still the end of the previous table
			table = "[["
			out[i] = tomlLine{header: true, table: table}
			continue
		}
		m := tomlDottedKV.FindStringSubmatch(l)
		if m == nil {
			out[i] = tomlLine{table: table}
			continue
		}
		out[i] = tomlLine{table: table, key: normDotted(m[2]), value: m[3]}
		v := strings.TrimSpace(m[3])
		switch {
		case strings.HasPrefix(v, `"""`) && !strings.Contains(v[3:], `"""`):
			closer = `"""`
		case strings.HasPrefix(v, `'''`) && !strings.Contains(v[3:], `'''`):
			closer = `'''`
		case strings.HasPrefix(v, "["):
			if d := bracketDelta(v); d > 0 {
				closer, depth = "]", d
			}
		}
	}
	return out
}

// bracketDelta counts [ minus ] outside strings and comments.
func bracketDelta(s string) int {
	d := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return d
		case c == '[':
			d++
		case c == ']':
			d--
		}
	}
	return d
}

func joinKey(table, key string) string {
	if table == "" {
		return key
	}
	return table + "." + key
}

// tomlValues reads every string or bool value outside arrays of tables, by its full dotted key
// (`[ui]` + `yolo = true` → "ui.yolo"; a top-level `ui.yolo = true` reads the same).
func tomlValues(text string) map[string]any {
	out := map[string]any{}
	lines := strings.Split(text, "\n")
	for _, l := range tomlScan(lines) {
		if l.key == "" || l.inner || l.table == "[[" {
			continue
		}
		if v, ok := tomlScalar(l.value); ok {
			out[joinKey(l.table, l.key)] = v
		}
	}
	return out
}

// tomlSetDotted sets (or, with nil / "", removes) `table.key`. An existing line is replaced in place; a new key goes
// after the table's last line, and a missing table is added at the end of the file. A key with no dot is tomlSet's.
// A table written inline (`ui = { … }`) is refused: adding [ui] next to it would make the file invalid.
func tomlSetDotted(text, dotted string, value any) (string, error) {
	cut := strings.LastIndex(dotted, ".")
	if cut < 0 {
		return tomlSet(text, dotted, value), nil
	}
	table, key := dotted[:cut], dotted[cut+1:]
	remove := value == nil || value == ""
	line := key + " = " + tomlValue(value)
	lines := strings.Split(text, "\n")
	scan := tomlScan(lines)

	header, end := -1, len(lines) // the table's header line, and the first line after its section
	dotted0, parent := -1, ""     // the last line defining the table with dotted keys (`ui.x = 1`), and that line's table
	for i, l := range scan {
		if l.inner {
			continue
		}
		if l.header {
			if header >= 0 && end == len(lines) {
				end = i
			}
			if l.table == table {
				header, end = i, len(lines)
			}
			continue
		}
		if l.key == "" || l.table == "[[" {
			continue
		}
		full := joinKey(l.table, l.key)
		if full == dotted {
			if remove {
				return strings.Join(append(lines[:i:i], lines[i+1:]...), "\n"), nil
			}
			m := tomlDottedKV.FindStringSubmatch(lines[i])
			lines[i] = m[1] + m[2] + " = " + tomlValue(value)
			return strings.Join(lines, "\n"), nil
		}
		if full == table || strings.HasPrefix(table, full+".") {
			return "", fmt.Errorf("%s is written inline in the file; edit it by hand", full)
		}
		if l.table != table && strings.HasPrefix(full, table+".") {
			dotted0, parent = i, l.table
		}
	}
	if remove {
		return text, nil
	}
	if header < 0 && dotted0 >= 0 {
		// The table is defined by dotted keys, so adding a [table] header would define it twice (invalid TOML):
		// write the key the same way, after that line and any continuation lines of its value.
		at := dotted0 + 1
		for at < len(lines) && scan[at].inner {
			at++
		}
		rel := dotted
		if parent != "" {
			rel = strings.TrimPrefix(dotted, parent+".")
		}
		indent := tomlDottedKV.FindStringSubmatch(lines[dotted0])[1]
		out := append(append(append([]string{}, lines[:at]...), indent+rel+" = "+tomlValue(value)), lines[at:]...)
		return strings.Join(out, "\n"), nil
	}
	if header >= 0 {
		at := end // after the section's last non-blank line, so the blank line before the next table stays
		for at > header+1 && strings.TrimSpace(lines[at-1]) == "" {
			at--
		}
		out := append(append(append([]string{}, lines[:at]...), line), lines[at:]...)
		return strings.Join(out, "\n"), nil
	}
	// A new table at the end, after a blank line.
	body := strings.TrimRight(text, "\n")
	if strings.TrimSpace(body) == "" {
		return "[" + table + "]\n" + line + "\n", nil
	}
	return body + "\n\n[" + table + "]\n" + line + "\n", nil
}
