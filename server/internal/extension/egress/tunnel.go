package egress

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
)

// closeWrite shuts the write side of c, when c can (a TCP or Unix connection).
func closeWrite(c net.Conn) error {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return errors.ErrUnsupported
}

// tunnel is CONNECT (and an upgrade on plain HTTP: a WebSocket): the bytes both ways once the far end is reached.
func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request, dial func() (net.Conn, error)) {
	far, err := dial()
	if err != nil {
		http.Error(w, "persona: "+err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		far.Close()
		http.Error(w, "persona: cannot tunnel", http.StatusInternalServerError)
		return
	}
	near, buf, err := hj.Hijack()
	if err != nil {
		far.Close()
		return
	}
	client := near
	stopNear := context.AfterFunc(r.Context(), func() { _ = client.Close() })
	defer stopNear()
	if r.Method == http.MethodConnect {
		_, err = near.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
	} else {
		out := r.Clone(context.Background())
		out.RequestURI = ""
		out.URL = &url.URL{Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery}
		out.Host = p.farHost(r.URL.Host, out.Host)
		out.Header.Del("Proxy-Authorization")
		out.Header.Del("Proxy-Connection")
		err = out.Write(far)
	}
	if err != nil {
		near.Close()
		far.Close()
		return
	}
	if plan, ok := far.(*connectPlan); ok {
		// Detach from net/http's connReader before probing with deadlines: a
		// timeout through that reader cancels the request, even after Hijack.
		pending := make([]byte, buf.Reader.Buffered())
		_, _ = io.ReadFull(buf.Reader, pending)
		near = prependConnect(near, pending)
		buf = nil
		near, far, err = p.prepareConnect(r.Context(), near, plan)
		if err != nil {
			p.logf("egress CONNECT %s: %v", r.Host, err)
			client.Close()
			return
		}
	}
	server := far
	stopFar := context.AfterFunc(r.Context(), func() { _ = server.Close() })
	defer stopFar()
	// The extension ending its side (EOF) only half-closes the far end: the reply still on its way comes back, as
	// the hub's other relays keep a half-close. Both close once the far end is done, or on an error either way.
	var once sync.Once
	done := func() { near.Close(); far.Close() }
	go func() {
		if buf != nil && buf.Reader.Buffered() > 0 {
			b, _ := buf.Reader.Peek(buf.Reader.Buffered())
			if _, err := far.Write(b); err != nil {
				once.Do(done)
				return
			}
		}
		if _, err := io.Copy(far, near); err != nil || closeWrite(far) != nil {
			once.Do(done)
		}
	}()
	_, _ = io.Copy(near, far)
	once.Do(done)
}
