package extmgr

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/managerconf"
)

// dev runs `personad extension dev`: build, hang the extension on this machine's hub, print its log, and on each
// change of its files build again and restart it; Ctrl-C takes it away.
func (c *extensionClient) dev(ctx context.Context, dir string, stdout, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	p, err := openProject(dir)
	if err != nil {
		return err
	}
	here := runtime.GOOS + "-" + runtime.GOARCH
	fmt.Fprintln(stdout, i18n.T(c.l(), "extmgr-cli.dev_building", i18n.Args{"id": p.manifest.ID, "platform": here}))
	if err := p.build(ctx, here, stderr); err != nil {
		return err
	}
	if err := c.devHang(ctx, p, stdout); err != nil {
		return err
	}
	id := p.manifest.ID
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.do(rctx, "DELETE", "/api/v1/extensions/"+url.PathEscape(id)+"/-/dev", nil, nil)
		fmt.Fprintln(stdout, "\n"+i18n.T(c.l(), "extmgr-cli.dev_removed", i18n.Args{"id": id}))
	}()
	fmt.Fprintln(stdout, i18n.T(c.l(), "extmgr-cli.dev_attached", i18n.Args{"id": id}))
	go c.devLogs(ctx, id, stdout)

	last := fingerprint(p.dir)
	manifest := fingerprint(p.dir, extensionManifestNames...)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		now := fingerprint(p.dir)
		if now == last {
			continue
		}
		last = now
		fmt.Fprintln(stdout, "— "+i18n.T(c.l(), "extmgr-cli.dev_changed"))
		if np, err := openProject(p.dir); err != nil {
			fmt.Fprintln(stdout, "✗ "+err.Error())
			continue
		} else {
			p = np
		}
		if err := p.build(ctx, here, stdout); err != nil {
			fmt.Fprintln(stdout, "✗ "+i18n.T(c.l(), "extmgr-cli.dev_build_failed", i18n.Args{"error": err.Error()}))
			continue
		}
		if m := fingerprint(p.dir, extensionManifestNames...); m != manifest || p.manifest.ID != id {
			manifest = m
			if err := c.devHang(ctx, p, stdout); err != nil { // the manifest changed: register it again
				fmt.Fprintln(stdout, "✗ "+err.Error())
			}
			continue
		}
		if err := c.do(ctx, "POST", "/api/v1/extensions/"+url.PathEscape(id)+"/-/dev/restart", nil, nil); err != nil {
			fmt.Fprintln(stdout, "✗ "+i18n.T(c.l(), "extmgr-cli.dev_restart_failed", i18n.Args{"error": err.Error()}))
		}
	}
}

var extensionManifestNames = []string{"persona-extension.json", "persona-plugin.json", "extension.dev.json"}

// devHang registers (again) the extension in its directory, with extension.dev.json's settings and secrets.
func (c *extensionClient) devHang(ctx context.Context, p *devProject, stdout io.Writer) error {
	dc, err := readDevConfig(p.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("extension.dev.json: %w", err)
	}
	var d ExtensionDetail
	if err := c.do(ctx, "POST", "/api/v1/extensions/-/dev", map[string]any{"dir": p.dir, "config": dc.Config, "secrets": dc.Secrets}, &d); err != nil {
		return err
	}
	if len(d.NeedsSetup) > 0 {
		fmt.Fprintln(stdout, i18n.T(c.l(), "extmgr-cli.dev_needs_setup", i18n.Args{"list": cliList(c.l(), d.NeedsSetup)}))
	}
	return nil
}

// devLogs prints the extension's log stream until ctx ends, connecting again when the hub restarts.
func (c *extensionClient) devLogs(ctx context.Context, id string, stdout io.Writer) {
	for ctx.Err() == nil {
		if err := c.stream(ctx, "/api/v1/extensions/"+url.PathEscape(id)+"/-/dev/logs", stdout); err != nil && ctx.Err() == nil {
			fmt.Fprintln(stdout, i18n.T(c.l(), "extmgr-cli.dev_logs_lost", i18n.Args{"error": err.Error()}))
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *extensionClient) stream(ctx context.Context, path string, w io.Writer) error {
	_, port, err := net.SplitHostPort(c.cfg.Listen)
	if _, perr := strconv.Atoi(port); err != nil || perr != nil {
		return errors.New("hub.json listen has no valid port")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", managerconf.CLIHubURL(c.cfg.Listen)+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	res, err := (&http.Client{Transport: &http.Transport{Proxy: nil, DialContext: managerconf.CLIDial}}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		fmt.Fprintln(w, sc.Text())
	}
	return sc.Err()
}

// fingerprint sums the names, sizes and times of the files under dir an edit would change (only these names when
// given), leaving out what building writes (bin, dist, node_modules) and git's.
func fingerprint(dir string, only ...string) string {
	h := sha256.New()
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != dir && (name == "bin" || name == "dist" || name == "node_modules" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(only) > 0 && !slices.Contains(only, name) {
			return nil
		}
		if info, err := d.Info(); err == nil {
			fmt.Fprintf(h, "%s %d %d\n", path, info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}
