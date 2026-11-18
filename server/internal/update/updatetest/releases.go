package updatetest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Releases is a publisher's releases as an update source serves them, where a newer one can be published at any time:
// for tests of what a hub or manager downloads when a release comes out between its check and the download (#9263).
// Two kinds of source serve them. GitHub is a repository's releases/latest/download, which redirects every name to
// releases/download/<tag>/<name> of the latest release, as GitHub does; Client reaches github.com at this server. DL
// is a site's dl/ that is not GitHub's: the latest release's files under one directory, which moves with it.
type Releases struct {
	srv    *httptest.Server
	mu     sync.Mutex
	tags   map[string]map[string][]byte // tag -> file name -> what is served
	latest string
	asked  []string
}

// releasesRepo is the GitHub repository Releases serves.
const releasesRepo = "/example/persona"

// NewReleases is a source with no release yet.
func NewReleases(t testing.TB) *Releases {
	r := &Releases{tags: map[string]map[string][]byte{}}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *Releases) serve(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.asked = append(r.asked, req.URL.Path)
	latest := r.latest
	var body []byte
	var ok bool
	switch path := req.URL.Path; {
	case strings.HasPrefix(path, releasesRepo+"/releases/latest/download/"):
		if latest != "" {
			http.Redirect(w, req, releasesRepo+"/releases/download/"+latest+"/"+strings.TrimPrefix(path, releasesRepo+"/releases/latest/download/"), http.StatusFound)
			r.mu.Unlock()
			return
		}
	case strings.HasPrefix(path, releasesRepo+"/releases/download/"):
		tag, name, _ := strings.Cut(strings.TrimPrefix(path, releasesRepo+"/releases/download/"), "/")
		body, ok = r.tags[tag][name]
	case strings.HasPrefix(path, "/dl/"):
		body, ok = r.tags[latest][strings.TrimPrefix(path, "/dl/")]
	}
	r.mu.Unlock()
	if !ok {
		http.NotFound(w, req)
		return
	}
	_, _ = w.Write(body)
}

// Publish puts up release tag with files (name -> what is served under it, latest.json among them); latest makes it
// the release both sources serve.
func (r *Releases) Publish(tag string, files map[string][]byte, latest bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tags[tag] = files
	if latest {
		r.latest = tag
	}
}

// GitHub is the repository's releases/latest/download, as an update source names it.
func (r *Releases) GitHub() string {
	return "https://github.com" + releasesRepo + "/releases/latest/download"
}

// DL is a site's dl/ that serves the latest release's files.
func (r *Releases) DL() string { return r.srv.URL + "/dl" }

// Client is a client of both sources: its requests to github.com reach this server.
func (r *Releases) Client() *http.Client {
	return &http.Client{Transport: releasesTransport{host: r.srv.Listener.Addr().String(), next: r.srv.Client().Transport}}
}

// Asked is the paths asked for so far, in order.
func (r *Releases) Asked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.asked...)
}

// Fetched is the paths asked for so far that end in name.
func (r *Releases) Fetched(name string) []string {
	var out []string
	for _, p := range r.Asked() {
		if strings.HasSuffix(p, "/"+name) {
			out = append(out, p)
		}
	}
	return out
}

// TagPath is the path release tag serves name at on GitHub.
func TagPath(tag, name string) string { return releasesRepo + "/releases/download/" + tag + "/" + name }

// LatestPath is the path GitHub's moving base serves name at.
func LatestPath(name string) string { return releasesRepo + "/releases/latest/download/" + name }

type releasesTransport struct {
	host string
	next http.RoundTripper
}

func (t releasesTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "github.com" {
		req = req.Clone(req.Context())
		req.URL.Scheme, req.URL.Host = "http", t.host
	}
	return t.next.RoundTrip(req)
}
