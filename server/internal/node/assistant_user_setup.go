package node

import (
	"path/filepath"
	"slices"

	"github.com/nwverse/persona/server/internal/identity"
)

// What of the user's own Claude Code setup reaches an assistant conversation in a private profile (#7318): one of the
// config directories Persona creates for its assistant (<data dir>/profiles/<launcher>, assistantProfileSelection).
// Every other directory, ~/.claude, the node's own default ($CLAUDE_CONFIG_DIR), the machine's claude_start_dir, a
// wrapper's CLAUDE_CONFIG_DIR or an explicit config_dir elsewhere, is one the user set up and may run plain claude in,
// so it is left as it is. Claude Code reads a private profile's settings and instructions instead of the user's,
// except in two ways:
//   - its directory below the home still has Claude Code walk up to the home and read ~/.claude as the home's project
//     .claude (docs/lessons/agents.md, #5786), so ~/.claude/CLAUDE.md and every ~/.claude/rules file, written for
//     coding Agents, came in as project instructions. They are excluded; what the assistant needs of them belongs in
//     About me, which its own instructions carry.
//   - the skills the user turned off in their own settings.json came back, as that file is not read. Only those
//     switches are carried over: the user's env, hooks, permissions and plugins stay in their profile.
// A guest's settings set their own excludes and keep no skill switches (guestSettingsFile), so none of this reaches a
// guest.

// assistantProfilesRoot is the directory Persona creates assistant profiles in (assistantProfileSelection).
func (n *Node) assistantProfilesRoot() string {
	root := n.dir
	if root == "" {
		root = identity.DataDir(n.home)
	}
	return filepath.Join(root, "profiles")
}

// userClaudeDir is the profile the user's own plain claude runs in on this machine: the machine's claude_start_dir when
// set (launcherCommandQ), else the node's own default (defaultClaudeDir).
func (n *Node) userClaudeDir() string {
	if d := n.claudeStartDir.Load(); d != nil {
		if dir, ok := absConfigDir(*d, n.home); ok {
			return dir
		}
	}
	return n.defaultClaudeDir()
}

// privateProfile: profile (a conversation's Claude config directory, "" for none) is one Persona created, below
// assistantProfilesRoot, and none of the user's own (~/.claude, the node's default, claude_start_dir).
func (n *Node) privateProfile(profile string) bool {
	if profile == "" || n.home == "" {
		return false
	}
	root := n.assistantProfilesRoot()
	if !sessionScopeWithin(root, profile) || sameScopePath(root, profile) {
		return false
	}
	for _, own := range []string{filepath.Join(n.home, ".claude"), n.defaultClaudeDir(), n.userClaudeDir()} {
		if sameScopePath(profile, own) {
			return false
		}
	}
	return true
}

// assistantUserSetup adds to an assistant start's merged settings s, for a conversation in cwd and a private profile,
// the excludes of ~/.claude's instructions (both spellings of a symlinked home, merged with any already there) and the
// user's skill switches that are off (userSkillsOff; a name s sets keeps its value).
func (n *Node) assistantUserSetup(s map[string]any, cwd, profile string) {
	if !n.privateProfile(profile) {
		return
	}
	excludes, _ := s["claudeMdExcludes"].([]any)
	homes := []string{n.home}
	if real, err := filepath.EvalSymlinks(n.home); err == nil && real != filepath.Clean(n.home) {
		homes = append(homes, real)
	}
	for _, home := range homes {
		for _, f := range []string{filepath.Join(home, ".claude", "CLAUDE.md"), filepath.Join(home, ".claude", "rules", "**")} {
			if !slices.Contains(excludes, any(f)) {
				excludes = append(excludes, f)
			}
		}
	}
	s["claudeMdExcludes"] = excludes
	if off := n.userSkillsOff(profile, cwd); len(off) > 0 {
		overrides, _ := s["skillOverrides"].(map[string]any)
		if overrides == nil {
			overrides = map[string]any{}
		}
		for name, value := range off {
			if _, set := overrides[name]; !set {
				overrides[name] = value
			}
		}
		s["skillOverrides"] = overrides
	}
}

// userSkillsOff is the skillOverrides a conversation in a private profile takes from the user's own settings.json (in
// userClaudeDir): its entries whose value is "off", less the names the conversation's own settings files set (its
// profile's settings.json, cwd's .claude/settings.json and settings.local.json), which keep their values. nil in any
// other profile. The skill catalog (visibleClaudeSkills) reads the same, so it lists what the conversation can use.
func (n *Node) userSkillsOff(profile, cwd string) map[string]any {
	if !n.privateProfile(profile) {
		return nil
	}
	user, err := readSettingsFile(filepath.Join(n.userClaudeDir(), "settings.json"))
	if err != nil {
		return nil
	}
	values, _ := user["skillOverrides"].(map[string]any)
	own := []string{filepath.Join(profile, "settings.json")}
	if cwd != "" {
		own = append(own, filepath.Join(cwd, ".claude", "settings.json"), filepath.Join(cwd, ".claude", "settings.local.json"))
	}
	set := map[string]bool{}
	for _, file := range own {
		if s, err := readSettingsFile(file); err == nil {
			ours, _ := s["skillOverrides"].(map[string]any)
			for name := range ours {
				set[name] = true
			}
		}
	}
	var off map[string]any
	for name, value := range values {
		if value == "off" && !set[name] {
			if off == nil {
				off = map[string]any{}
			}
			off[name] = "off"
		}
	}
	return off
}
