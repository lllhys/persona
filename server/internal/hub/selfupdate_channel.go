package hub

import (
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/update"
)

// Stable and beta are two GitHub feeds, not different names for web-dev.
// The latter is an operator-configured private source and remains unchanged.
func (u *updater) betaLocked() bool { return u.settings.Source == update.ChannelBeta }

func (u *updater) selectedBeta() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.betaLocked()
}

func (u *updater) selectedChannel() string {
	if u.selectedBeta() {
		return update.ChannelBeta
	}
	return update.ChannelStable
}

// releaseSource chooses the feed for one release operation. For beta it pins
// the immutable tag that its pointer manifest identifies, so later prereleases
// cannot change files after a check.
func (u *updater) releaseSource() (updateSource, error) {
	s := u.h.updateSource()
	if !u.betaLocked() || s.url == "" {
		return s, nil
	}
	feed, err := update.BetaFeed(s.url)
	if err != nil {
		return updateSource{}, err
	}
	s.url = feed
	return s, nil
}

// installSource keeps both the hub and the optional node packages on the same
// immutable beta tag after a candidate was selected.
func (u *updater) installSource(m *update.Manifest) (updateSource, error) {
	s, err := u.releaseSource()
	if err != nil || !u.betaLocked() || s.url == "" {
		return s, err
	}
	pinned := update.BetaReleaseBase(s.url, m)
	if pinned == "" {
		return updateSource{}, errInvalidBetaVersion
	}
	s.url = pinned
	return s, nil
}

var errInvalidBetaVersion = i18n.Err("hub-selfupdate.err_beta_channel")

// androidSource is the beta pointer when opted in, otherwise the existing
// stable/custom source. A beta without android.json leaves the previous APK
// rather than silently falling back to a stable one.
func (u *updater) androidSource() (string, error) {
	s, err := u.releaseSource()
	return s.url, err
}

// Only beta needs a new cache directory. Stable keeps the pre-channel layout.
func (u *updater) androidCacheDir() string {
	if u.selectedBeta() {
		return "beta"
	}
	return ""
}
