package assistant

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/managerconf"
	"github.com/nwverse/persona/server/internal/remoteimage"
)

// Remote images (#9473, docs/protocol/protocol.md "Remote images"): the pictures an Agent's or the assistant's reply
// names by address, which the apps load from here instead of from their host, so the host sees this manager's address
// and never a device's. remoteimage fetches, checks, scales and keeps them; this is the route. Full devices only: an
// extension gets no scope for it (it would be a way out past its own network permissions), and a limited device has
// no replies to draw. A caller's fetches are limited by the device it is (assistantwire.DeviceNameHeader).

func (s *Service) remoteImageRoutes() {
	s.handle(assistantwire.RemoteImageRoute, func(w http.ResponseWriter, r *http.Request) {
		if !fullRole(r) {
			apierr.Write(w, http.StatusForbidden, i18n.Err("assistant-remote_image.forbidden"))
			return
		}
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		caller := r.Header.Get(assistantwire.DeviceNameHeader)
		img, err := s.remoteImages.Get(r.Context(), caller, queryURL(r.URL.RawQuery), size)
		if err != nil {
			var e *remoteimage.Error
			if !errors.As(err, &e) {
				return // the caller went away
			}
			if e.Status >= http.StatusInternalServerError {
				s.logf("remote image: %s", e)
			}
			writeRemoteImageError(w, e)
			return
		}
		writeRemoteImage(w, img)
	})
}

// writeRemoteImage answers the picture as a picture of its own type that nothing may run or sniff as something else,
// kept by the app for a day (the address names the same picture; the route is the device's own).
func writeRemoteImage(w http.ResponseWriter, img *remoteimage.Image) {
	h := w.Header()
	h.Set("Content-Type", img.Type)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("Content-Length", strconv.Itoa(len(img.Data)))
	_, _ = w.Write(img.Data)
}

// writeRemoteImageError answers e's status and code, its sentence in the caller's language, and when to try again
// after the caller's limit.
func writeRemoteImageError(w http.ResponseWriter, e *remoteimage.Error) {
	if e.Code == remoteimage.CodeRateLimited {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(e.RetryAfter.Seconds())))))
	}
	apierr.WriteCode(w, e.Status, e.Code, i18n.Err("assistant-remote_image."+e.Code))
}

// queryURL is the url parameter as written in the raw query: percent escapes undone, a "+" kept as a plus. A picture's
// address keeps its plus signs that way: the apps escape "&", "=", "#" and spaces in it, but iOS's URLComponents
// leaves "+" as it is, which form decoding (Query) would turn into a space.
func queryURL(raw string) string {
	for _, part := range strings.Split(raw, "&") {
		if v, ok := strings.CutPrefix(part, "url="); ok {
			s, err := url.PathUnescape(v)
			if err != nil {
				return ""
			}
			return s
		}
	}
	return ""
}

// remoteImageDoH is manager.json's remote_images.doh: the DNS-over-HTTPS JSON API the route asks for a host's real
// addresses when this machine's resolver answers only a fake-IP proxy's (#9550, remoteimage/doh.go).
// remoteimage.DefaultDoH when it names none; "" asks none, and such pictures are refused. Read when one is needed.
func (s *Service) remoteImageDoH() string {
	var cfg struct {
		RemoteImages struct {
			DoH *string `json:"doh"`
		} `json:"remote_images"`
	}
	if s.repositoryConfig != "" {
		if raw, err := managerconf.ReadFile(s.repositoryConfig); err == nil && raw != nil {
			_ = json.Unmarshal(raw, &cfg)
		}
	}
	if cfg.RemoteImages.DoH == nil {
		return remoteimage.DefaultDoH
	}
	return strings.TrimSpace(*cfg.RemoteImages.DoH)
}
