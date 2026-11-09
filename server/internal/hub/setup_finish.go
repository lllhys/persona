package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/mdp/qrterminal/v3"
)

// --- running it ---------------------------------------------------------------------------------------------------

func (e *setupEnv) health(ctx context.Context, cfg *setupConfig) error {
	wait := e.healthWait
	if e.o.noService {
		wait = min(wait, 5*time.Second) // nothing was started here: it runs already, or not
	}
	deadline := time.Now().Add(wait)
	for {
		if e.hubAnswers(ctx, cfg.Config) {
			e.report("health", true, "hub 在运行，本机检查 /api/v1/health 通过", "")
			return nil
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			break
		}
		e.sleep(time.Second)
	}
	next := "运行 journalctl --user -u personad -n 50 看 hub 的日志"
	switch {
	case e.o.noService:
		next = "在另一个终端运行 personad hub run，起来后重跑 personad hub setup --no-service"
	case e.goos == "darwin":
		next = "看 " + filepath.Join(e.root, "hub.log") + " 里 hub 的日志"
	}
	return e.fail("health", fmt.Sprintf("%d 秒内 hub 没有在本机端口 %s 上应答", int(wait.Seconds()), portOf(cfg.Listen)), next)
}

func loopbackClient() (*http.Client, func()) {
	transport := &http.Transport{Proxy: nil}
	return &http.Client{Timeout: 5 * time.Second, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, transport.CloseIdleConnections
}

// hubAnswers: the hub this hub.json describes answers on loopback (with its token: another hub on the port does not).
func (e *setupEnv) hubAnswers(ctx context.Context, cfg *Config) bool {
	client, done := loopbackClient()
	defer done()
	for _, path := range []string{"/api/v1/health", "/api/v1/endpoints"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", portOf(cfg.Listen))+path, nil)
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
	}
	return true
}

// finish prints the way in: the phone's QR code and the admin URL with the setup code (docs/assistant/onboarding.md 4.1).
func (e *setupEnv) finish(ctx context.Context, cfg *setupConfig) error {
	p, err := cfg.requestPairCode(ctx)
	if err != nil {
		return e.fail("health", "hub 在运行，但没拿到配对码："+err.Error(), "运行 personad hub pair 再试")
	}
	code, codeExpires, err := e.setupCode(ctx, cfg.Config)
	if err != nil {
		return e.fail("health", "hub 在运行，但没拿到管理界面的设置码："+err.Error(), "运行 personad hub admin setup-code 再试")
	}
	adminURL, localOnly := e.adminURL(cfg.Config)
	if code != "" {
		adminURL += "#setup=" + url.QueryEscape(code)
	}
	expires := p.ExpiresAt
	if code != "" && codeExpires.Before(expires) {
		expires = codeExpires
	}
	if e.o.json {
		done := map[string]any{"done": true, "pair_url": p.PairURL, "admin_url": adminURL, "admin_local_only": localOnly,
			"setup_code": code, "expires_at": expires.UTC().Format(time.RFC3339)}
		b, _ := marshalNoEscape(done)
		_, _ = e.out.Write(append(b, '\n'))
		return nil
	}
	fmt.Fprintf(e.out, "\nhub 装好了。\n\n1. 用 Persona App 扫这个二维码，配对你的手机（%d 分钟内有效）：\n\n", minutesLeft(p.ExpiresAt))
	qrterminal.GenerateHalfBlock(p.PairURL, qrterminal.L, e.out)
	fmt.Fprintf(e.out, "\n   扫不了时，把这条配对链接发到手机上打开：%s\n\n", p.PairURL)
	browser := "用手机浏览器"
	if localOnly {
		browser = "在这台电脑的浏览器里"
	}
	if code != "" {
		fmt.Fprintf(e.out, "2. %s打开下面的地址，设一个管理员密码（设置码已经带在地址里，%d 分钟内有效）：\n   %s\n",
			browser, minutesLeft(codeExpires), adminURL)
	} else {
		fmt.Fprintf(e.out, "2. 管理员密码已经设过了，管理界面在：\n   %s\n", adminURL)
	}
	if localOnly {
		fmt.Fprintf(e.out, "   hub 现在只在这台电脑上监听（listen 是 %s），这个地址只能在这台电脑上打开；"+
			"要用手机打开，把 %s 里的 \"listen\" 改成 \":%s\"（让 hub 在所有地址上监听），再运行一次 personad hub setup。\n", cfg.Listen, e.hubJSON(), portOf(cfg.Listen))
	}
	fmt.Fprintln(e.out, "\n过期了就再运行一次 personad hub setup（已经装好的不会重做），或者 personad hub pair 只出配对码。")
	return nil
}

func minutesLeft(t time.Time) int { return max(1, int(time.Until(t).Round(time.Minute).Minutes())) }

// setupCode asks the running hub for a first-run setup code ("" when the admin password is set already).
func (e *setupEnv) setupCode(ctx context.Context, cfg *Config) (string, time.Time, error) {
	resp, err := adminLoopbackPost(ctx, cfg, "/api/v1/admin/setup-code") // `hub admin setup-code`'s request
	if err != nil {
		return "", time.Time{}, errors.New("连不上本机 hub")
	}
	defer resp.Body.Close()
	var body struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.Unmarshal(raw, &body); err != nil || body.Code == "" {
			return "", time.Time{}, errors.New("hub 返回的内容无效")
		}
		return body.Code, body.ExpiresAt, nil
	case http.StatusConflict: // already_set
		return "", time.Time{}, nil
	}
	return "", time.Time{}, fmt.Errorf("HTTP %d", resp.StatusCode)
}

// adminURL is where the admin UI opens, on the plaintext port: a hub listening on loopback only answers there
// (localOnly: only a browser on this machine can open it), one bound to a single address answers on that one, and
// one on every address is opened by a phone on the LAN at this machine's LAN address.
func (e *setupEnv) adminURL(cfg *Config) (u string, localOnly bool) {
	port := portOf(cfg.Listen)
	if h, _, err := net.SplitHostPort(cfg.Listen); err == nil {
		switch ip := net.ParseIP(h); {
		case h == "localhost" || (ip != nil && ip.IsLoopback()):
			if ip == nil || ip.To4() != nil {
				h = "127.0.0.1"
			}
			return "http://" + net.JoinHostPort(h, port) + "/admin", true
		case ip != nil && !ip.IsUnspecified():
			return "http://" + net.JoinHostPort(h, port) + "/admin", false
		}
	}
	host := e.lanIPv4()
	if host == "" {
		if u, err := url.Parse(cfg.PublicURL); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
	}
	if host == "" {
		host = "<这台电脑的局域网 IP>"
	}
	return "http://" + net.JoinHostPort(host, port) + "/admin", false
}
