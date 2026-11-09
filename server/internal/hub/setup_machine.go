package hub

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nwverse/persona/server/internal/pgdocker"
)

// --- the machine --------------------------------------------------------------------------------------------------

func runCommand(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute) // docker run may pull the image
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.WaitDelay = time.Second
	return cmd.CombinedOutput()
}

func runOnTerminal(ctx context.Context, name string, args ...string) error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer tty.Close()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	return cmd.Run()
}

// askTerminal asks a yes / no question on the terminal (install.sh runs as `curl | sh`: stdin is the script).
func askTerminal(question string) (bool, bool) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, false
	}
	defer tty.Close()
	fmt.Fprintf(tty, "%s [Y/n] ", question)
	var answer string
	_, _ = fmt.Fscanln(tty, &answer)
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
		return true, true
	}
	return false, true
}

func tcpPortFree(port int) bool {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func pingPostgres(ctx context.Context, dbURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	return conn.Ping(ctx)
}

// defaultLANIPv4 is this machine's private IPv4 address on the interface of the default route (several interfaces:
// the one traffic goes out of), else the first private one on an interface that is up and not virtual.
func defaultLANIPv4() string {
	// A UDP "connect" only picks the route: nothing is sent (192.0.2.1 is TEST-NET-1).
	if c, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		ip := c.LocalAddr().(*net.UDPAddr).IP
		c.Close()
		if ip.IsPrivate() {
			return ip.String()
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || virtualInterfaceName(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
				return n.IP.String()
			}
		}
	}
	return ""
}

func virtualInterfaceName(name string) bool {
	name = strings.ToLower(name)
	for _, p := range []string{"docker", "br-", "veth", "virbr", "vmnet", "vboxnet", "bridge", "cni", "flannel", "podman"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return vpnInterface(name)
}

func setupFirstLine(s string) string { return pgdocker.FirstLine(s) }
