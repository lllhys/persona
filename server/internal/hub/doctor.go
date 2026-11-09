package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/netaddr"
)

// Doctor inspects local state without creating configuration, renewing certificates or changing network rules.
func Doctor(ctx context.Context, out io.Writer) error {
	d := &doctorReport{out: out}
	fmt.Fprintln(out, "hub 自检（只读）")
	cfg := d.config(dataDir())
	var endpoints []Endpoint
	if cfg != nil {
		endpoints = d.running(ctx, cfg)
	}
	d.credentials(dataDir())
	addrs := d.ipv6()
	d.system(ctx)
	port := "<TLS 端口>"
	if cfg != nil {
		if p, err := doctorPort(cfg.Public.TLSListen); err == nil {
			port = p
		}
	}
	d.next = append(d.next, "在主机、路由器和光猫上，对公网只放行 hub 的 TCP TLS 端口 "+port+"；明文端口仅限内网或 VPN，并检查 Docker 端口映射")
	var probes []string
	for _, e := range endpoints {
		u, err := url.Parse(e.URL)
		if err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && e.Kind != "lan" {
			u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/health"
			u.RawPath, u.RawQuery, u.Fragment = "", "", ""
			probes = append(probes, u.String())
		}
	}
	if len(probes) == 0 {
		for _, a := range netaddr.Candidates(addrs) {
			probes = append(probes, "https://"+net.JoinHostPort(a.IP.String(), port)+"/api/v1/health")
		}
	}
	if len(probes) == 0 {
		probes = []string{"https://[<全局 IPv6 地址>]:" + port + "/api/v1/health"}
	}
	fmt.Fprintln(out, "外部可达性：未验证。本机连自己的地址走环回，不能证明公网可达。")
	for _, probe := range probes {
		d.next = append(d.next, "用手机关掉 Wi-Fi，打开 "+probe+" 测一次（不要携带 token）")
	}
	d.next = append(d.next, "浏览器出现自签证书警告说明已到达 TLS 服务，但尚未验证 hub 身份；用 App 核对钉扎值。若连接超时，检查 IPv6 入站防火墙和 TUN 回包路由")
	fmt.Fprintln(out, "\n下一步：")
	for _, step := range d.next {
		fmt.Fprintln(out, "- "+step)
	}
	return ctx.Err()
}

type doctorReport struct {
	out  io.Writer
	next []string
	// problems are the "Problem" lines, as "item: action" (`personad hub setup` prints only these).
	problems []string
}

// ipv6 checks this machine's global IPv6 addresses (for reaching the hub from outside) and returns every address.
func (d *doctorReport) ipv6() []netaddr.Addr {
	addrs, err := netaddr.List()
	if err != nil {
		d.problem("本机 IPv6", "无法读取地址；请检查本机网卡状态")
	} else {
		stable := 0
		for _, a := range addrs {
			if !a.IP.Is6() {
				continue
			}
			if netaddr.Stable(a) {
				stable++
				d.ok("本机 IPv6", fmt.Sprintf("%s（%s，Stable）", a.IP, a.Interface))
			} else {
				fmt.Fprintf(d.out, "  已排除 %s（%s）：%s\n", a.IP, a.Interface, doctorAddressReason(a))
			}
		}
		if stable == 0 {
			d.problem("本机 IPv6", "没有可确认稳定的全局 IPv6；请检查运营商 IPv6 和网卡地址，非 Linux 系统无法确认地址标志")
		}
	}
	return addrs
}

// setupDoctor is the check at the end of `personad hub setup` (docs/assistant/onboarding.md 4.1): only what is wrong, none of
// the excluded addresses or the routes and firewall, which are about reaching the hub from outside (outside: asked
// for with --outside, then those too).
func setupDoctor(ctx context.Context, dir string, outside bool) []string {
	d := &doctorReport{out: io.Discard}
	if cfg := d.config(dir); cfg != nil {
		d.running(ctx, cfg)
	}
	if outside {
		d.ipv6()
		d.system(ctx)
	}
	return d.problems
}

func (d *doctorReport) ok(item, detail string) {
	fmt.Fprintf(d.out, "正常｜%s：%s\n", item, detail)
}

func (d *doctorReport) problem(item, action string) {
	fmt.Fprintf(d.out, "有问题｜%s：%s\n", item, action)
	d.next = append(d.next, action)
	d.problems = append(d.problems, item+"："+action)
}

func (d *doctorReport) config(dir string) *Config {
	b, err := os.ReadFile(filepath.Join(dir, "hub.json"))
	if err != nil {
		d.problem("配置", "无法读取 ~/.persona/hub.json；请确认配置存在且当前用户有读取权限")
		return nil
	}
	// Match the runtime's omitted-listen default, without generating a token or host name.
	cfg := &Config{Listen: ":18660", dir: dir}
	if json.Unmarshal(b, cfg) != nil {
		d.problem("配置", "hub.json 格式有误；请修正 JSON 后重新运行 personad hub doctor")
		return nil
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(b, &raw)
	if _, err := doctorPort(cfg.Listen); err != nil {
		d.problem("明文 listen", "未设置有效监听端口；请在 hub.json 中配置 listen")
	} else {
		detail := cfg.Listen
		if _, set := raw["listen"]; !set {
			detail += "（未显式设置，使用默认值）"
		}
		d.ok("明文 listen", detail+"；仅供内网或 VPN 使用")
	}
	if _, err := doctorPort(cfg.Public.TLSListen); err != nil {
		d.problem("public.tls_listen", "未设置有效 TLS 监听端口；请配置 public.tls_listen 后重启 hub")
	} else {
		d.ok("public.tls_listen", cfg.Public.TLSListen+"（配置值，尚未验证 TLS 监听）")
	}
	if cfg.Security.AllowPublicPlaintext {
		d.problem("security.allow_public_plaintext", "当前已开启公网明文；请设为 false 并重启 hub，公网连接改用 HTTPS")
	} else {
		d.ok("security.allow_public_plaintext", "已关闭")
	}
	if pp := cfg.Security.ProxyProtocol; pp != nil {
		switch p, err := pp.policy(); {
		case err != nil:
			d.problem("security.proxy_protocol", "配置有误，hub 启动不了："+err.Error())
		case p.tls && !p.http && cfg.Public.TLSListen == "":
			d.problem("security.proxy_protocol", "只作用于 TLS 端口，但没开 public.tls_listen：现在不起作用")
		default:
			d.ok("security.proxy_protocol", "读 "+strings.Join(pp.From, "、")+" 发来的 PROXY 头；配好后 hub 日志里公网来源不应再是回环")
		}
	}
	certFile, keyFile := cfg.Public.CertFile, cfg.Public.KeyFile
	if certFile == "" && keyFile == "" {
		certFile, keyFile = filepath.Join(dir, "tls", "hub.crt"), filepath.Join(dir, "tls", "hub.key")
	}
	cert, fp, err := loadTLSFiles(certFile, keyFile)
	if err != nil {
		d.problem("TLS 证书", "无法读取证书或密钥不匹配；请检查 ~/.persona/tls/ 或 public.cert_file、public.key_file。托管证书由启用 TLS 的 hub 启动时生成，自检不会创建")
	} else {
		detail := fmt.Sprintf("SPKI SHA-256 钉扎值 %s；到期时间 %s", fp, cert.Leaf.NotAfter.UTC().Format(time.RFC3339))
		now := time.Now()
		if now.Before(cert.Leaf.NotBefore) || !now.Before(cert.Leaf.NotAfter) {
			d.problem("TLS 证书", detail+"；证书尚未生效或已过期，请检查系统时间并续签证书，保留原密钥")
		} else if !cert.Leaf.NotAfter.After(now.Add(30 * 24 * time.Hour)) {
			d.problem("TLS 证书", detail+"；即将到期，请检查 hub 自动续签状态；自定义证书需自行续签，保留原密钥")
		} else {
			d.ok("TLS 证书", detail)
		}
	}
	return cfg
}

// doctorCredentialFiles are credentials below the top level, in a folder the path guard and the agent sandbox close
// whole: the Postgres superuser password the container entrypoint writes (docs/operations/docker.md). Everything else
// this check classifies is directly in the data directory.
var doctorCredentialFiles = []string{filepath.Join("docker", "db-password")}

// credentials checks that the data directory's credential files are readable by their owner only (0600) and the
// folders closed whole are not open to others (0700). It reports only: doctor never changes a file; the load path
// tightens hub.json itself (readConfigFile), and every credential Persona writes is made 0600 as it is written, so a
// finding means a file was copied in or chmoded by hand. POSIX permission bits do not exist on Windows, where the
// check is skipped.
func (d *doctorReport) credentials(dir string) {
	if runtime.GOOS == "windows" {
		fmt.Fprintln(d.out, "凭据文件权限：已跳过 Windows 专用检查（POSIX 权限位不适用）")
		return
	}
	bad := 0
	check := func(rel string, want os.FileMode) {
		path := filepath.Join(dir, rel)
		st, err := os.Stat(path) // follow a link: the target's mode is what keeps the secret
		if os.IsNotExist(err) {
			return // a deployment without this credential has nothing to check
		}
		if err != nil {
			bad++
			d.problem("凭据文件权限", "无法读取 "+filepath.ToSlash(rel)+" 的状态："+err.Error())
			return
		}
		if st.Mode().Perm() != want {
			bad++
			d.problem("凭据文件权限", fmt.Sprintf("%s 的权限是 %04o，应为 %04o；请运行 chmod %o %s",
				filepath.ToSlash(rel), st.Mode().Perm(), want, want, path))
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		d.problem("凭据文件权限", "无法读取数据目录 "+dir+"："+err.Error())
		return
	}
	for _, e := range entries {
		switch {
		case e.IsDir():
			if slices.Contains(identity.DataSecretDirs, e.Name()) {
				check(e.Name(), 0o700)
			}
		case identity.DataSecretName(e.Name()):
			check(e.Name(), 0o600)
		}
	}
	for _, rel := range doctorCredentialFiles {
		check(rel, 0o600)
	}
	if bad == 0 {
		d.ok("凭据文件权限", "数据目录中的凭据文件均为 0600，整目录关闭的凭据文件夹均为 0700")
	}
}

func doctorPort(listen string) (string, error) {
	_, port, err := net.SplitHostPort(listen)
	n, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("无效监听端口")
	}
	return port, nil
}

func (d *doctorReport) running(ctx context.Context, cfg *Config) []Endpoint {
	port, err := doctorPort(cfg.Listen)
	if err != nil {
		return nil
	}
	if strings.TrimSpace(cfg.Token) == "" {
		d.problem("运行中的 hub", "配置缺少主 token；请检查 hub.json 的 token，自检不会生成或打印 token")
		return nil
	}
	get := doctorLoopback(cfg, port)
	for _, path := range []string{"/api/v1/health", "/api/v1/endpoints"} {
		resp, err := get(ctx, path)
		if err != nil {
			d.problem("运行中的 hub", "无法连接本机回环，hub 可能没运行或未监听回环地址；请启动 personad hub run 并检查 listen")
			return nil
		}
		var body struct {
			OK        bool       `json:"ok"`
			Endpoints []Endpoint `json:"endpoints"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			d.problem(path, fmt.Sprintf("本机 hub 返回 HTTP %d；请确认运行版本、配置和主 token 一致", resp.StatusCode))
			continue
		}
		if decodeErr != nil || (path == "/api/v1/health" && !body.OK) || (path == "/api/v1/endpoints" && body.Endpoints == nil) {
			d.problem(path, "本机 hub 返回内容无效；请检查 hub 日志和运行版本")
			continue
		}
		if path == "/api/v1/health" {
			d.ok("运行中的 hub", "回环 health 检查通过")
			continue
		}
		if len(body.Endpoints) == 0 {
			d.problem("候选地址", "hub 未发布候选地址；请检查 public.advertise 和网卡地址")
		} else {
			d.ok("候选地址", "已读取运行中的 hub 当前发布列表（不代表外部可达）")
			for _, e := range body.Endpoints {
				fmt.Fprintf(d.out, "  %s（%s，端口 %d，发现时间 %s）\n", e.URL, e.Kind, e.Port, e.SeenAt.Format(time.RFC3339))
			}
		}
		d.pushRoutes(ctx, get)
		return body.Endpoints
	}
	return nil
}

// doctorLoopback GETs a path from the running hub with the master token. Never resolves a configured host or sends
// the token outside loopback.
func doctorLoopback(cfg *Config, port string) func(ctx context.Context, path string) (*http.Response, error) {
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	host, _, _ := net.SplitHostPort(cfg.Listen)
	loopbacks := []netip.Addr{netip.AddrFrom4([4]byte{127, 0, 0, 1}), netip.IPv6Loopback()}
	if ip, err := netip.ParseAddr(host); err == nil && ip.Is6() {
		loopbacks[0], loopbacks[1] = loopbacks[1], loopbacks[0]
	}
	return func(ctx context.Context, path string) (resp *http.Response, err error) {
		for _, ip := range loopbacks {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(ip.String(), port)+path, nil)
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			if resp, err = client.Do(req); err == nil {
				return resp, nil
			}
		}
		return nil, err
	}
}

// pushRoutes reports which way the running hub pushes to each device (#5998): straight to APNs, through the relay the
// phone registered with, or not at all and why.
func (d *doctorReport) pushRoutes(ctx context.Context, get func(context.Context, string) (*http.Response, error)) {
	resp, err := get(ctx, "/api/v1/devices/push")
	if err != nil {
		return // the checks before said whether the hub answers at all
	}
	var body struct {
		Direct  bool         `json:"direct"`
		Devices []pushDevice `json:"devices"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body)
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		d.problem("推送", "运行中的 hub 版本太旧，不会报每台设备的推送路径；升级后再查")
		return
	case resp.StatusCode != http.StatusOK || decodeErr != nil:
		d.problem("推送", fmt.Sprintf("本机 hub 的 /api/v1/devices/push 返回 HTTP %d；请检查 hub 日志", resp.StatusCode))
		return
	}
	way := "hub 没有 apns.json，经各台设备登记的推送中继"
	if body.Direct {
		way = "hub 有自己的 APNs key（apns.json），直连 APNs"
	}
	if len(body.Devices) == 0 {
		d.ok("推送", way+"；还没有设备登记推送（在手机上打开 App、允许通知后会出现在这里）")
		return
	}
	bad := 0
	for _, dev := range body.Devices {
		if dev.Push.Route == pushNone || dev.Push.Reason == pushFailed {
			bad++
		}
	}
	if bad == 0 {
		d.ok("推送", way)
	} else {
		d.problem("推送", fmt.Sprintf("%s；%d 台设备收不到或上一条没推出去，见下面", way, bad))
	}
	for _, dev := range body.Devices {
		name := dev.Name
		if name == "" {
			name = dev.DeviceID
		}
		if dev.Kind != "" {
			name += "（" + dev.Kind + "）"
		}
		fmt.Fprintf(d.out, "  %s：%s\n", name, pushRouteLine(dev.Push))
	}
}

func doctorAddressReason(a netaddr.Addr) string {
	var reasons []string
	if class := netaddr.Classify(a.IP); class != netaddr.GlobalIPv6 {
		reasons = append(reasons, "非全局 IPv6（"+string(class)+"）")
	}
	if !a.FlagsKnown || !a.ScopeKnown {
		reasons = append(reasons, "系统未提供地址标志或作用域，无法确认稳定性")
	}
	if a.ScopeKnown && a.Scope != 0 {
		reasons = append(reasons, "作用域不是全局")
	}
	if a.Virtual || a.InterfaceFlags&net.FlagLoopback != 0 {
		reasons = append(reasons, "虚拟或回环网卡")
	}
	if a.InterfaceFlags&net.FlagUp == 0 {
		reasons = append(reasons, "网卡未启用")
	}
	for _, flag := range []struct {
		bit   netaddr.Flags
		label string
	}{{netaddr.FlagTemporary, "临时隐私地址"}, {netaddr.FlagDeprecated, "已废弃地址"}, {netaddr.FlagTentative, "地址检测尚未完成"}, {netaddr.FlagDADFailed, "重复地址检测失败"}} {
		if a.Flags&flag.bit != 0 {
			reasons = append(reasons, flag.label)
		}
	}
	if a.LifetimesKnown && (a.PreferredLifetime == 0 || a.ValidLifetime == 0) {
		reasons = append(reasons, "地址有效期已结束")
	}
	return strings.Join(reasons, "、")
}

func doctorCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

func (d *doctorReport) system(ctx context.Context) {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(d.out, "路由、防火墙：已跳过 Linux 专用检查；请在系统网络设置中核对")
		return
	}
	for _, item := range []struct {
		path string
		ipv6 bool
	}{{"/proc/net/route", false}, {"/proc/net/ipv6_route", true}} {
		b, err := os.ReadFile(item.path)
		if err != nil {
			fmt.Fprintf(d.out, "路由：无法读取 %s，已跳过\n", item.path)
			continue
		}
		ifaces := doctorRouteInterfaces(string(b), item.ipv6)
		if len(ifaces) == 0 {
			fmt.Fprintf(d.out, "路由：%s 未发现可检查的路由，已跳过\n", item.path)
		}
		for _, iface := range ifaces {
			d.route(iface, item.path)
		}
	}
	if b, err := doctorCommand(ctx, "ip", "-6", "route", "get", "2001:db8::1"); err == nil {
		fields := strings.Fields(string(b))
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "dev" {
				d.route(fields[i+1], "IPv6 路由查询")
				break
			}
		}
	} else {
		fmt.Fprintln(d.out, "IPv6 路由查询：命令不可用或查询失败，已跳过")
	}
	for _, command := range [][]string{{"ufw", "status"}, {"nft", "list", "ruleset"}} {
		b, err := doctorCommand(ctx, command[0], command[1:]...)
		if err != nil {
			fmt.Fprintf(d.out, "防火墙：%s 不可用或无读取权限，已跳过（不使用 sudo）\n", command[0])
			continue
		}
		if strings.Contains(string(b), "Status: inactive") || strings.TrimSpace(string(b)) == "" {
			d.problem("防火墙", command[0]+" 未启用或没有规则；请核对主机入站策略，对公网只放行 hub 的 TLS 端口")
		} else {
			d.ok("防火墙读取", strings.Join(command, " ")+" 已读取；是否放行 TLS 仍需核对以下规则")
			fmt.Fprintln(d.out, string(b))
		}
	}
}

func (d *doctorReport) route(iface, source string) {
	name := strings.ToLower(iface)
	suspect := false
	for _, prefix := range []string{"tun", "utun", "tap", "wg", "tailscale", "vpn", "ipsec"} {
		suspect = suspect || strings.HasPrefix(name, prefix)
	}
	if target, err := filepath.EvalSymlinks(filepath.Join("/sys/class/net", iface)); err == nil {
		suspect = suspect || strings.HasPrefix(target, "/sys/devices/virtual/") && iface != "lo"
	}
	if suspect {
		d.problem("路由", source+" 经过 "+iface+"，疑似 TUN/VPN 或虚拟网卡；TUN 代理可能吞掉入站回包（未实测）。请关闭 TUN 自动路由或配置入站连接回包直连，再用手机蜂窝网络验证")
	} else {
		d.ok("路由", source+" 经过 "+iface+"，未发现 TUN 特征；仍需用手机验证回包路径")
	}
}

func doctorRouteInterfaces(raw string, ipv6 bool) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		var iface, flags string
		if ipv6 {
			if len(f) < 10 {
				continue
			}
			iface, flags = f[9], f[8]
		} else {
			if len(f) < 11 || f[1] != "00000000" || f[7] != "00000000" {
				continue
			}
			iface, flags = f[0], f[3]
		}
		bits, err := strconv.ParseUint(flags, 16, 32)
		if err != nil || bits&1 == 0 || bits&0x200 != 0 || iface == "lo" || seen[iface] {
			continue
		}
		seen[iface] = true
		out = append(out, iface)
	}
	return out
}

// pushRouteLine is a device's way in `hub doctor`'s words.
func pushRouteLine(r pushRoute) string {
	var line string
	switch r.Route {
	case pushDirect:
		line = "直连 APNs"
	case pushRelay:
		line = "经推送中继"
	default:
		line = "收不到后台推送"
	}
	switch r.Reason {
	case pushNoToken:
		line += "：没有 APNs token（这台设备没允许通知，或向 APNs 登记失败）"
	case pushRelayFailed:
		line += "：向推送中继登记失败（" + r.Detail + "）"
	case pushNoRelay:
		line += "：hub 没有 apns.json，这台设备也没报中继（构建没配中继，或 App 太旧）"
	case pushFailed:
		at := ""
		if r.FailedAt != nil {
			at = r.FailedAt.UTC().Format("01-02 15:04 UTC") + " "
		}
		line += fmt.Sprintf("，但 %s上一条没推出去（%s）", at, r.Detail)
	}
	return line
}
