package update

import "testing"

// A source is behind when its newest release is older than the hub and than the release the hub's own code contains
// (#8108): the publish site's dl/ stayed on a 2026-11-07 build while hubs deployed from master moved on. A hub deployed
// from master after the newest release is newer than it too, but starts with its tag: that source is current.
func TestBehind(t *testing.T) {
	const hubBuild = 202611141806
	hub := Make("v0.5.3-150-gc0908bfa8-202611141806", Release, "202611141806", "https://example.test/dl")
	for _, c := range []struct {
		name    string
		hub     Build
		version string
		build   int64
		want    bool
	}{
		{"frozen site", hub, "v0.1.17-177-g563f7fde5b-202611071005", 202611071005, true},
		{"frozen release", hub, "0.4.0", 202611080000, true},
		{"the release this hub's code starts from", hub, "0.5.3", 202611140822, false},
		{"same build", hub, "v0.5.3-150-gc0908bfa8-202611141806", hubBuild, false},
		{"newer release", hub, "0.5.4", 202611150900, false},
		// A newer build from older code is Hold's case, not this one: the app shows it held.
		{"newer build from older code", hub, "v0.4.0-3-gabc-202611150900", 202611150900, false},
		{"release candidate before the release", Make("v0.6.0-12-gabc-202611201200", Release, "202611201200", ""), "0.6.0-rc1", 202611180000, true},
		{"release candidate the hub starts from", Make("v0.6.0-rc1-2-gabc-202611201200", Release, "202611201200", ""), "v0.6.0-rc1", 202611180000, false},
		{"dirty build", Make("v0.5.3-150-gc0908bfa8-dirty-202611141806", Release, "202611141806", ""), "0.4.0", 202611080000, true},
		{"official release hub", Make("0.5.3", Release, "202611140822", ""), "0.4.0", 202611080000, true},
		// Nothing to tell a stopped source from a quiet one by.
		{"hub version names no release", Make("master-c0908bfa8c09-202611141806", Release, "202611141806", ""), "0.4.0", 202611080000, false},
		{"source version names no release", hub, "dev-202611071005", 202611071005, false},
		{"no build number", Make("v0.5.3-150-gc0908bfa8", Release, "", ""), "0.4.0", 202611080000, false},
		{"release without a build", hub, "0.4.0", 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.hub.Behind(&Manifest{Version: c.version, Build: c.build}); got != c.want {
				t.Fatalf("%s reading %s (build %d): Behind = %v, want %v", c.hub.Version, c.version, c.build, got, c.want)
			}
		})
	}
	if hub.Behind(nil) {
		t.Fatal("Behind(nil) = true")
	}
}
