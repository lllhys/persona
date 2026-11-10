package egress

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"
)

// hop are the headers of one hop, not passed on.
var hop = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authorization", "Proxy-Authenticate", "Te",
	"Trailer", "Transfer-Encoding", "Upgrade"}

// forwardTransport keeps the approved dial callback while negotiating ordinary TLS HTTP/2.
// A custom DialContext otherwise conservatively disables Go's HTTP/2 support.
func forwardTransport(dial func() (net.Conn, error)) *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           func(context.Context, string, string) (net.Conn, error) { return dial() },
		ForceAttemptHTTP2:     true,
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 2 * time.Minute,
	}
}

// forward sends HTTP through the approved connection and preserves the upstream response.
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, ext string, dial func() (net.Conn, error)) {
	tr := forwardTransport(dial)
	defer tr.CloseIdleConnections()
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range hop {
		out.Header.Del(h)
	}
	if h := p.farHost(out.URL.Host, ""); h != "" {
		out.URL.Host, out.Host = h, h
	}
	resp, err := tr.RoundTrip(out)
	if err != nil {
		http.Error(w, "persona: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if broker, _ := r.Context().Value(runtimeFetchKey{}).(bool); broker {
		resp.Header.Del(RuntimeHeadLengthHeader)
		if r.Method == http.MethodHead {
			if length := resp.Header.Get("Content-Length"); length != "" {
				resp.Header.Set(RuntimeHeadLengthHeader, length)
			}
			resp.Header.Del("Content-Length")
		}
	}
	for _, h := range hop {
		resp.Header.Del(h)
	}
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	body := &readErr{r: resp.Body}
	if _, err := io.Copy(w, body); err != nil {
		// Returning normally would let net/http end a chunked answer cleanly, handing the extension a
		// truncated body as a complete one; aborting drops the connection so its client sees the cut.
		// Only a failed read is an upstream cut-off; a failed write or a canceled request means the
		// extension's own client gave up, which is not worth a log line.
		if body.err != nil && r.Context().Err() == nil {
			p.logf("extension %s: %s: answer cut off: %v", ext, r.URL.Host, body.err)
		}
		panic(http.ErrAbortHandler)
	}
}

// readErr remembers the first read error other than io.EOF, so a copy failure can be told apart
// as a read (upstream) or a write (client) failure.
type readErr struct {
	r   io.Reader
	err error
}

func (e *readErr) Read(b []byte) (int, error) {
	n, err := e.r.Read(b)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}
