package assistant

import (
	"context"
	"path"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

// What a remembered kind of task (tasks.trust, trustKey: machine|directory|read or edit) covers (#8095): the directory
// and every directory under it, on that machine, with that access. Directories are compared as the machine names them
// once written the same way (trustDir): "~" is its home, extra and trailing slashes and "." / ".." are cleaned, and the
// home as its links resolve (Device.HomeReal, from the node) is the home as the node names it. A directory too broad
// to cover a tree (trustTreeDir: "/", "/home", the home itself) covers only itself, as every kind did before.
//
// The keys stay as they were written: a key remembered before keeps matching what it matched, and covers its tree too
// when its directory may.

// trustHome is a machine's home as its node names it and as its links resolve ("" when the same or not reported).
type trustHome struct{ home, real string }

// trustHomes reads the machines' homes once for one decision.
func (s *Service) trustHomes(ctx context.Context) map[string]trustHome {
	out := map[string]trustHome{}
	for _, d := range s.devices(ctx) {
		out[d.ID] = trustHome{home: d.Home, real: d.HomeReal}
	}
	return out
}

// trustDir is dir as remembered kinds compare it on a machine with home h; "" when it is no absolute POSIX directory
// (a Windows path, a relative one), which then matches only as written.
func trustDir(dir string, h trustHome) string {
	dir = proto.ExpandHome(strings.TrimSpace(dir), h.home)
	if !path.IsAbs(dir) || strings.Contains(dir, "\\") {
		return ""
	}
	dir = path.Clean(dir)
	for _, home := range []string{h.home, h.real} {
		if home == "" || !path.IsAbs(home) {
			return dir
		}
	}
	real, home := path.Clean(h.real), path.Clean(h.home)
	if real == home {
		return dir
	}
	if dir == real {
		return home
	}
	if rest, ok := strings.CutPrefix(dir, real+"/"); ok {
		return path.Join(home, rest)
	}
	return dir
}

// trustTreeDir: a remembered kind in the directory dir (as trustDir writes it) may cover the directories under it. Not
// "/", a directory right under it ("/home", "/Users"), or the machine's home: those would let every project start without
// asking. The same rule as a standing authorization's directory (validStandingDirectory), plus the home.
func trustTreeDir(dir string, h trustHome) bool {
	if !validStandingDirectory(dir) {
		return false
	}
	for _, home := range []string{h.home, h.real} {
		if home != "" && path.IsAbs(home) && trustDir(home, h) == dir {
			return false
		}
	}
	return true
}

// splitTrustKey is a key's machine, directory and access ("read" or "edit"); false for a key the separator does not
// split in three (a machine id or a directory with '|' in it, which then matches only as written).
func splitTrustKey(key string) (machine, dir, mode string, ok bool) {
	parts := strings.Split(key, "|")
	if len(parts) != 3 || (parts[2] != "read" && parts[2] != "edit") {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// trustCovers is whether a remembered kind (have's keys) covers the kind key: the same key, or one on the same machine
// with the same access whose directory is key's or holds it (trustTreeDir).
func trustCovers(have map[string]bool, homes map[string]trustHome, key string) bool {
	if have[key] {
		return true
	}
	machine, dir, mode, ok := splitTrustKey(key)
	if !ok {
		return false
	}
	h := homes[machine]
	want := trustDir(dir, h)
	if want == "" {
		return false
	}
	for k := range have {
		m, d, md, ok := splitTrustKey(k)
		if !ok || m != machine || md != mode {
			continue
		}
		got := trustDir(d, h)
		if got == "" {
			continue
		}
		if got == want || trustTreeDir(got, h) && strings.HasPrefix(want, got+"/") {
			return true
		}
	}
	return false
}

// trustCover is trustCovers for one decision over the kinds and homes read now: covered(key) says whether key starts
// without asking. A failed read covers nothing but the keys read (none), so a plan asks.
func (s *Service) trustCover(ctx context.Context) func(key string) bool {
	have := s.trustedKinds(ctx)
	if len(have) == 0 {
		return func(string) bool { return false }
	}
	homes := s.trustHomes(ctx)
	return func(key string) bool { return trustCovers(have, homes, key) }
}
