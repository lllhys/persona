package node

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// Codex's options the app may change ("Settings › Codex Settings"), in ~/.codex/config.toml. Only these top-level keys are read
// or written, line by line: the rest of the file (comments, [model_providers.*], [projects.*]…) stays byte for byte.
// The same shape as Claude's (claudeconfig.go), so the app draws both with one page.
var codexSettings = []ClaudeSetting{
	{Key: "model", Title: "模型", Detail: "没指定模型时 Codex 用哪个，比如 gpt-5.5", Type: "string", Default: ""},
	{Key: "model_reasoning_effort", Title: "思考强度", Detail: "越高越慢、越仔细", Type: "enum", Options: codexEffortLevels, Default: "medium"},
	{Key: "service_tier", Title: "Speed", Detail: "Fast increases usage. Applies to new Codex sessions on supported models.", Type: "enum", Options: []string{"default", "fast"}, Default: "default"},
	{Key: "model_reasoning_summary", Title: "思考摘要", Detail: "把模型的思考整理成摘要显示，也可以不显示", Type: "enum", Options: []string{"auto", "concise", "detailed", "none"}, Default: "auto"},
	{Key: "model_verbosity", Title: "回答详细程度", Detail: "回答写多长", Type: "enum", Options: []string{"low", "medium", "high"}, Default: "medium"},
	{Key: "approval_policy", Title: "什么时候问你", Detail: "执行命令前要不要先问你，也可以一律不问", Type: "enum", Options: []string{"untrusted", "on-failure", "on-request", "never"}, Default: "on-request"},
	{Key: "sandbox_mode", Title: "沙箱", Detail: "命令能读写哪些地方，也可以完全不设限", Type: "enum", Options: []string{"read-only", "workspace-write", "danger-full-access"}, Default: "read-only"},
	{Key: "hide_agent_reasoning", Title: "隐藏思考过程", Detail: "不显示模型的思考事件", Type: "bool", Default: false},
	{Key: "show_raw_agent_reasoning", Title: "显示原始思考", Detail: "模型给出原始思考内容时照原样显示", Type: "bool", Default: false},
}

func (n *Node) codexConfigPath() string {
	p := filepath.Join(n.codexHome(), "config.toml")
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real // a symlink into a dotfiles repo: edit the target
	}
	return p
}

func (n *Node) getCodexSettings() ([]ClaudeSetting, error) {
	b, err := os.ReadFile(n.codexConfigPath())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	vals := tomlTopLevel(string(b))
	out := make([]ClaudeSetting, len(codexSettings))
	for i, s := range codexSettings {
		if v, ok := vals[s.Key]; ok {
			// Both spellings occur in native configurations.
			if s.Key == "service_tier" && v == "priority" {
				v = "fast"
			}
			s.Value = v
		}
		out[i] = s
	}
	return out, nil
}

func (n *Node) setCodexSetting(key string, value any) error {
	var def *ClaudeSetting
	for i := range codexSettings {
		if codexSettings[i].Key == key {
			def = &codexSettings[i]
		}
	}
	if def == nil {
		return fmt.Errorf("setting %q is not editable from the app", key)
	}
	if err := checkSettingValue(*def, value); err != nil {
		return err
	}
	path := n.codexConfigPath()
	agentConfigMu.Lock()
	defer agentConfigMu.Unlock()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := tomlSet(string(b), key, value)
	return writeConfigFile(path, []byte(text), 0o600)
}

// checkSettingValue: a value of the option's type (nil = back to the default).
func checkSettingValue(def ClaudeSetting, value any) error {
	if value == nil {
		return nil
	}
	switch def.Type {
	case "bool":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be true or false", def.Key)
		}
	case "enum":
		s, _ := value.(string)
		for _, o := range def.Options {
			if o == s {
				return nil
			}
		}
		return fmt.Errorf("%s must be one of %v", def.Key, def.Options)
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", def.Key)
		}
	}
	return nil
}

// --- the few TOML lines we touch ---

var (
	tomlTable = lazyre.New(`^\s*\[`)
	tomlKV    = lazyre.New(`^\s*([A-Za-z0-9_-]+)\s*=\s*(.*)$`)
)

// tomlTopLevel reads the top-level `key = value` lines (before the first table) whose value is a string or a bool.
func tomlTopLevel(text string) map[string]any {
	out := map[string]any{}
	for _, line := range strings.Split(text, "\n") {
		if tomlTable.MatchString(line) {
			break
		}
		m := tomlKV.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if v, ok := tomlScalar(m[2]); ok {
			out[m[1]] = v
		}
	}
	return out
}

// tomlScalar parses a basic or literal string or a bool, with an optional trailing comment.
func tomlScalar(raw string) (any, bool) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, `"`):
		var b strings.Builder
		for i := 1; i < len(raw); i++ {
			c := raw[i]
			if c == '\\' && i+1 < len(raw) {
				i++
				switch raw[i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(raw[i])
				}
				continue
			}
			if c == '"' {
				return b.String(), true
			}
			b.WriteByte(c)
		}
		return nil, false
	case strings.HasPrefix(raw, `'`):
		if j := strings.Index(raw[1:], `'`); j >= 0 {
			return raw[1 : 1+j], true
		}
		return nil, false
	}
	word := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
	if b, err := strconv.ParseBool(word); err == nil && (word == "true" || word == "false") {
		return b, true
	}
	return nil, false
}

func tomlValue(v any) string {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x)
	case string:
		r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
		return `"` + r.Replace(x) + `"`
	}
	return fmt.Sprint(v)
}

// tomlSet sets (or, with nil / "", removes) one top-level key: its line is replaced in place, or a new line goes
// after the last top-level line, before the first table.
func tomlSet(text, key string, value any) string {
	lines := strings.Split(text, "\n")
	end := len(lines) // first table header
	for i, l := range lines {
		if tomlTable.MatchString(l) {
			end = i
			break
		}
	}
	remove := value == nil || value == ""
	for i := 0; i < end; i++ {
		if m := tomlKV.FindStringSubmatch(lines[i]); m != nil && m[1] == key {
			if remove {
				return strings.Join(append(lines[:i:i], lines[i+1:]...), "\n")
			}
			lines[i] = key + " = " + tomlValue(value)
			return strings.Join(lines, "\n")
		}
	}
	if remove {
		return text
	}
	at := end // after the last non-blank top-level line, so a blank line before the first table stays
	for at > 0 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	line := key + " = " + tomlValue(value)
	if text == "" {
		return line + "\n"
	}
	out := append(append(append([]string{}, lines[:at]...), line), lines[at:]...)
	return strings.Join(out, "\n")
}
