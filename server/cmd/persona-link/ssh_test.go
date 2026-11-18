package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/link"
)

const cmdStandInSSHSource = `package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	mode := strings.TrimSpace(os.Getenv("PERSONA_CMD_TEST_SSH_MODE"))
	switch mode {
	case "auth_refused":
		fmt.Fprintln(os.Stderr, "Permission denied (publickey).")
		os.Exit(255)
	case "host_key":
		fmt.Fprintln(os.Stderr, "Host key verification failed.")
		os.Exit(255)
	}
	var forward string
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "-L" && i+1 < len(os.Args) {
			forward = os.Args[i+1]
			break
		}
	}
	rest, ok := strings.CutPrefix(forward, "127.0.0.1:")
	if !ok {
		os.Exit(255)
	}
	localPort, target, ok := strings.Cut(rest, ":")
	if !ok {
		os.Exit(255)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+localPort)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(255)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		switch mode {
		case "forward_refused":
			fmt.Fprintln(os.Stderr, "channel 2: open failed: administratively prohibited: open failed")
			_ = c.Close()
		case "target_unreachable":
			fmt.Fprintln(os.Stderr, "channel 2: open failed: connect failed: Connection refused")
			_ = c.Close()
		default:
			go func(local net.Conn) {
				remote, err := net.DialTimeout("tcp", target, 3*time.Second)
				if err != nil {
					fmt.Fprintf(os.Stderr, "channel 2: open failed: connect failed: %v\n", err)
					_ = local.Close()
					return
				}
				var wg sync.WaitGroup
				wg.Add(2)
				go func() { defer wg.Done(); _, _ = io.Copy(remote, local); _ = remote.Close() }()
				go func() { defer wg.Done(); _, _ = io.Copy(local, remote); _ = local.Close() }()
				wg.Wait()
			}(c)
		}
	}
}
`

var (
	standInSSHOnce sync.Once
	standInSSHBin  string
	standInSSHErr  error
)

func buildStandInSSH(t *testing.T) string {
	t.Helper()
	standInSSHOnce.Do(func() {
		dir, err := os.MkdirTemp("", "persona-link-ssh-test-*")
		if err != nil {
			standInSSHErr = err
			return
		}
		src := filepath.Join(dir, "main.go")
		if err := os.WriteFile(src, []byte(cmdStandInSSHSource), 0o600); err != nil {
			standInSSHErr = err
			return
		}
		bin := filepath.Join(dir, "ssh")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", bin, src)
		if out, err := cmd.CombinedOutput(); err != nil {
			standInSSHErr = err
			_ = out
			return
		}
		standInSSHBin = bin
	})
	if standInSSHErr != nil {
		t.Fatalf("build stand-in ssh: %v", standInSSHErr)
	}
	return standInSSHBin
}

func newPairAndHealthHub(t *testing.T) (srv *httptest.Server, port, pin string) {
	t.Helper()
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/v1/auth/tokens":
			if r.Header.Get("Authorization") != "Bearer jp_ok" {
				http.Error(w, `{"error":"bad token"}`, http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"jd_cli","role":"link"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	port = strconv.Itoa(srv.Listener.Addr().(*net.TCPAddr).Port)
	sum := sha256.Sum256(srv.Certificate().RawSubjectPublicKeyInfo)
	return srv, port, base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestPairAndSSHCommands(t *testing.T) {
	if got := subcommandHelp("ssh"); !strings.Contains(got, "persona-link ssh") || !strings.Contains(got, "--off") {
		t.Fatalf("ssh help:\n%s", got)
	}
	if got := subcommandHelp("pair"); !strings.Contains(got, "--ssh") || !strings.Contains(got, "--ssh-target") {
		t.Fatalf("pair help:\n%s", got)
	}

	sshBin := buildStandInSSH(t)
	t.Setenv("PERSONA_CMD_TEST_SSH_MODE", "")
	_, port, pin := newPairAndHealthHub(t)

	f := newFakeEnv(t)
	f.sshBin = sshBin

	// Passive mode rejects --ssh.
	if err := pairCmd(f.env, []string{"--listen", "--ssh", "user@example.com"}); err == nil {
		t.Fatal("pair --listen accepted --ssh")
	}

	// Loopback pairing link with --ssh infers Target=127.0.0.1:<TLS port> and Always=true.
	loopLink := "persona://link?" + url.Values{
		"url":     {"http://127.0.0.1:1"},
		"tls":     {port},
		"fp":      {pin},
		"hub":     {"h1"},
		"token":   {"jp_ok"},
		"machine": {"laptop"},
	}.Encode()
	f.out.Reset()
	if err := pairCmd(f.env, []string{"--ssh", "user@example.com", loopLink}); err != nil {
		t.Fatalf("pairCmd --ssh: %v", err)
	}
	cfg, err := link.Load(f.configPath())
	if err != nil || cfg.Token != "jd_cli" || cfg.SSH == nil || cfg.SSH.Server != "user@example.com" ||
		cfg.SSH.Target != "127.0.0.1:"+port || !cfg.SSH.Always {
		t.Fatalf("paired config = %+v, err = %v", cfg, err)
	}

	// Non-loopback pairing link defaults Always to false when --ssh is given without --ssh-always.
	lanLink := "persona://link?" + url.Values{
		"url":     {"http://192.0.2.10:1"},
		"tls":     {port},
		"fp":      {pin},
		"hub":     {"h1"},
		"token":   {"jp_ok"},
		"machine": {"laptop"},
	}.Encode()
	f.out.Reset()
	if err := pairCmd(f.env, []string{"--ssh", "user@example.com", "--ssh-always", lanLink}); err != nil {
		t.Fatalf("pairCmd --ssh --ssh-always: %v", err)
	}
	cfg, _ = link.Load(f.configPath())
	if cfg.SSH == nil || !cfg.SSH.Always {
		t.Fatalf("expected Always=true, got %+v", cfg.SSH)
	}

	// `persona-link ssh --no-always` updates link.json without re-pairing.
	f.out.Reset()
	if err := sshCmd(f.env, []string{"user@example.com:2222", "--target", "127.0.0.1:" + port, "--no-always"}); err != nil {
		t.Fatalf("sshCmd update: %v", err)
	}
	cfg, _ = link.Load(f.configPath())
	if cfg.Token != "jd_cli" || cfg.SSH == nil || cfg.SSH.Server != "user@example.com:2222" || cfg.SSH.Always {
		t.Fatalf("updated ssh config = %+v", cfg)
	}

	// `persona-link ssh --test --json` verifies end to end over the stand-in ssh client.
	f.out.Reset()
	if err := sshCmd(f.env, []string{"--test", "--json"}); err != nil {
		t.Fatalf("sshCmd --test --json: %v (out=%s)", err, f.out.String())
	}
	var testRep sshTestJSON
	if err := json.Unmarshal(f.out.Bytes(), &testRep); err != nil || !testRep.OK || testRep.Target != "127.0.0.1:"+port {
		t.Fatalf("ssh test json = %+v (%v)", testRep, err)
	}
	if want := "经 SSH 连上了 127.0.0.1:" + port + " 上的 hub。"; testRep.Message != want {
		t.Fatalf("canonical SSH message = %q, want %q", testRep.Message, want)
	}
	successJSON := append([]byte(nil), f.out.Bytes()...)
	for _, language := range []string{"en", "ja", "fr"} {
		t.Setenv("PERSONA_LANG", language)
		f.out.Reset()
		if err := sshCmd(f.env, []string{"--test", "--json"}); err != nil {
			t.Fatalf("sshCmd --test --json (%s): %v", language, err)
		}
		if !bytes.Equal(f.out.Bytes(), successJSON) {
			t.Fatalf("%s changed machine JSON:\n%s\nwant:\n%s", language, f.out, successJSON)
		}
	}
	t.Setenv("PERSONA_LANG", "zh-Hans")

	// Failing `persona-link ssh --test --json` reports the failure code and exits 1.
	t.Setenv("PERSONA_CMD_TEST_SSH_MODE", "host_key")
	f.out.Reset()
	err = sshCmd(f.env, []string{"--test", "--json"})
	if codeOf(err) != 1 {
		t.Fatalf("sshCmd --test host_key err = %v", err)
	}
	var failRep sshTestJSON
	if jerr := json.Unmarshal(f.out.Bytes(), &failRep); jerr != nil || failRep.OK || failRep.Code != link.SSHHostKey ||
		!strings.Contains(failRep.Message, "ssh -p 2222 user@example.com") {
		t.Fatalf("host_key json = %+v (%v)", failRep, jerr)
	}

	// `persona-link ssh --off` removes the tunnel while keeping the pairing credential.
	f.out.Reset()
	if err := sshCmd(f.env, []string{"--off"}); err != nil {
		t.Fatalf("sshCmd --off: %v", err)
	}
	cfg, _ = link.Load(f.configPath())
	if cfg.Token != "jd_cli" || cfg.SSH != nil {
		t.Fatalf("after --off = %+v", cfg)
	}
}

func TestStatusReportsSSHRouteAndErrors(t *testing.T) {
	f := newFakeEnv(t)
	f.pairHere(t)
	if _, err := link.SetSSH(f.configPath(), &link.SSH{Server: "user@example.com", Target: "127.0.0.1:18662", Always: true}); err != nil {
		t.Fatal(err)
	}
	release, err := link.LockRun(f.configPath())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	since := f.now()
	f.saveStatus(t, link.Status{
		State:   "connected",
		Mode:    link.ModeActive,
		Machine: "pi",
		HubName: "home",
		Address: "user@example.com",
		Kind:    link.KindSSH,
		Since:   &since,
		Node:    "online",
	}, 4242)

	f.out.Reset()
	if err := status(f.env, nil); err != nil {
		t.Fatalf("status text: %v", err)
	}
	txt := f.out.String()
	for _, want := range []string{
		"链路：已连接 · SSH user@example.com",
		"  · SSH user@example.com → 127.0.0.1:18662（在用，总是走 SSH）",
	} {
		if !strings.Contains(txt, want) {
			t.Fatalf("status output missing %q:\n%s", want, txt)
		}
	}

	f.out.Reset()
	if err := status(f.env, []string{"--json"}); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var rep report
	if err := json.Unmarshal(f.out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.SSH == nil || rep.SSH.Server != "user@example.com" || rep.Link == nil || rep.Link.Kind != link.KindSSH {
		t.Fatalf("status json = %+v", rep)
	}
}
