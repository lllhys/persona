package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/netaddr"
)

func TestDoctorReadOnly(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	d := &doctorReport{out: &out}
	missing := filepath.Join(dir, "missing")
	if d.config(missing) != nil {
		t.Fatal("missing configuration was accepted")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("doctor created a data directory")
	}
	cert, pin, err := loadOrCreateTLS(filepath.Join(dir, "tls"), []string{"example.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	token := randomHex(24)
	var paths []string
	redirect := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		paths = append(paths, r.URL.Path)
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("doctor request must be authenticated and read-only")
		}
		if redirect {
			http.Redirect(w, r, "/must-not-follow", http.StatusFound)
			return
		}
		if r.URL.Path == "/api/v1/health" {
			_, _ = w.Write([]byte(`{"ok":true}`))
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"endpoints": []Endpoint{{URL: "https://[2001:db8::2]:19443", Kind: "ipv6", Port: 19443, Scheme: "https"}}})
		}
	}))
	defer srv.Close()
	cfg := Config{Listen: srv.Listener.Addr().String(), Token: token, Public: PublicConfig{TLSListen: ":19443"}}
	cfg.Security.AllowPublicPlaintext = true
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	files := []string{"hub.json", "tls/hub.crt", "tls/hub.key"}
	before := map[string][]byte{}
	for _, file := range files {
		before[file], err = os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
	}
	loaded := d.config(dir)
	if loaded == nil {
		t.Fatal(out.String())
	}
	if endpoints := d.running(context.Background(), loaded); len(endpoints) != 1 {
		t.Fatalf("missing published endpoints: %s", out.String())
	}
	if !reflect.DeepEqual(paths, []string{"/api/v1/health", "/api/v1/endpoints", "/api/v1/devices/push"}) {
		t.Fatalf("unexpected requests: %v", paths)
	}
	for _, want := range []string{pin, cert.Leaf.NotAfter.UTC().Format(time.RFC3339), "当前已开启公网明文", "2001:db8::2", "回环 health 检查通过"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing output %q", want)
		}
	}
	if strings.Contains(out.String(), token) {
		t.Fatal("doctor disclosed the master token")
	}
	for _, file := range files {
		after, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil || !bytes.Equal(before[file], after) {
			t.Fatalf("doctor changed %s", file)
		}
	}
	redirect, paths = true, nil
	d.running(context.Background(), loaded)
	if !reflect.DeepEqual(paths, []string{"/api/v1/health", "/api/v1/endpoints"}) || !strings.Contains(out.String(), "HTTP 302") {
		t.Fatal("doctor followed a redirect or failed to report it")
	}
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded = d.config(dir)
	d.running(context.Background(), loaded)
	b, _ = os.ReadFile(filepath.Join(dir, "hub.json"))
	if string(b) != `{}` || loaded.Token != "" || !strings.Contains(out.String(), "配置缺少主 token") {
		t.Fatal("doctor must not generate or persist missing credentials")
	}
}

// The credential check reports credential files and whole-secret folders that are readable by others, leaves every
// mode untouched (doctor is read-only), and stays quiet about ordinary files and credentials that are not there.
func TestDoctorCredentialPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not apply on Windows")
	}
	dir := t.TempDir()
	write := func(rel string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("credential-or-not"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil { // past the umask
			t.Fatal(err)
		}
	}
	chmod := func(rel string, mode os.FileMode) {
		t.Helper()
		if err := os.Chmod(filepath.Join(dir, rel), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("hub.json", 0o600)
	write("secret.key", 0o600)
	write("relay-tunnel.token", 0o644)
	write("tls/hub.key", 0o600)
	chmod("tls", 0o700)
	write("docker/db-password", 0o644)
	chmod("docker", 0o755)
	write("mcp.json", 0o644) // an ordinary file: its mode is none of the check's business

	var out bytes.Buffer
	d := &doctorReport{out: &out}
	d.credentials(dir)
	for _, want := range []string{"relay-tunnel.token", "docker", "db-password", "chmod 600", "chmod 700"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing finding %q in:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "mcp.json") || len(d.problems) != 3 {
		t.Fatalf("want exactly the three open credentials, got %v", d.problems)
	}
	// Read-only: the modes the run found are still the ones it must report.
	if st, _ := os.Stat(filepath.Join(dir, "relay-tunnel.token")); st.Mode().Perm() != 0o644 {
		t.Fatalf("doctor changed a mode: %v", st.Mode())
	}

	chmod("relay-tunnel.token", 0o600)
	chmod("docker/db-password", 0o600)
	chmod("docker", 0o700)
	out.Reset()
	d = &doctorReport{out: &out}
	d.credentials(dir)
	if len(d.problems) != 0 || !strings.Contains(out.String(), "正常｜凭据文件权限") {
		t.Fatalf("want a clean report, got %v:\n%s", d.problems, out.String())
	}

	// A deployment without the container's password file and a data directory with no credentials at all are clean.
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "mcp.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	d = &doctorReport{out: &out}
	d.credentials(plain)
	if len(d.problems) != 0 {
		t.Fatalf("an absent credential is nothing to check: %v", d.problems)
	}
}

func TestDoctorRouteAndAddressDiagnostics(t *testing.T) {
	v4 := "Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\n" +
		"wg0 00000000 00000000 0001 0 0 0 00000000 0 0 0\n" +
		"eth0 000002C0 00000000 0001 0 0 0 00FFFFFF 0 0 0\n"
	v6 := "00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 00000000 00000000 00000000 00200200 lo\n" +
		"20010db8000000000000000000000000 20 00000000000000000000000000000000 00 00000000000000000000000000000000 00000000 00000000 00000000 00000001 tun0\n"
	if got := doctorRouteInterfaces(v4, false); !reflect.DeepEqual(got, []string{"wg0"}) {
		t.Fatalf("IPv4 routes: %v", got)
	}
	if got := doctorRouteInterfaces(v6, true); !reflect.DeepEqual(got, []string{"tun0"}) {
		t.Fatalf("IPv6 routes: %v", got)
	}
	var out bytes.Buffer
	d := &doctorReport{out: &out}
	d.route("wg0", "测试路由")
	if !strings.Contains(out.String(), "TUN 代理可能吞掉入站回包") || len(d.next) == 0 {
		t.Fatal("missing actionable TUN warning")
	}
	a := netaddr.Addr{IP: netip.MustParseAddr("2001:db8::2"), InterfaceFlags: net.FlagUp,
		FlagsKnown: true, ScopeKnown: true, Virtual: true,
		Flags:          netaddr.FlagTemporary | netaddr.FlagDeprecated | netaddr.FlagTentative | netaddr.FlagDADFailed,
		LifetimesKnown: true}
	reason := doctorAddressReason(a)
	for _, want := range []string{"非全局 IPv6", "虚拟或回环网卡", "临时隐私地址", "已废弃地址", "地址检测尚未完成", "重复地址检测失败", "地址有效期已结束"} {
		if !strings.Contains(reason, want) {
			t.Errorf("missing exclusion reason %q", want)
		}
	}
}
