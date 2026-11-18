package devices

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const (
	fakeSSHProxyParentEnv = "PERSONA_FAKE_SSH_PROXY_PARENT"
	fakeSSHProxyChildEnv  = "PERSONA_FAKE_SSH_PROXY_CHILD"
	fakeSSHParentExitEnv  = "PERSONA_FAKE_SSH_PARENT_EXIT"
)

// TestFakeSSHProxyParent is not a test: as a subprocess it stands in for ssh, starts a ProxyCommand-like child, and
// stays connected without answering the node handshake. The child deliberately does not inherit the parent's pipes.
func TestFakeSSHProxyParent(t *testing.T) {
	pidFile := os.Getenv(fakeSSHProxyParentEnv)
	if pidFile == "" {
		t.Skip("helper process")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestFakeSSHProxyChild$")
	cmd.Env = append(os.Environ(), fakeSSHProxyChildEnv+"=1")
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		panic(err)
	}
	if os.Getenv(fakeSSHParentExitEnv) == "1" {
		os.Exit(0)
	}
	select {}
}

// TestFakeSSHProxyChild is not a test: it is the ProxyCommand child left behind when only its ssh parent is killed.
func TestFakeSSHProxyChild(t *testing.T) {
	if os.Getenv(fakeSSHProxyChildEnv) != "1" {
		t.Skip("helper process")
	}
	select {}
}

// Abandoning an ssh handshake must take its ProxyCommand down too, whether ssh is still running or already exited.
// Killing only ssh leaves that child adopted by the service manager; for a relay-backed ProxyCommand it keeps
// competing with the next connection for relay replies.
func TestAbandonedSSHHandshakeKillsProxyCommand(t *testing.T) {
	for _, parentExits := range []bool{false, true} {
		name := "parent_running"
		if parentExits {
			name = "parent_exited"
		}
		t.Run(name, func(t *testing.T) {
			m := newMachine(newTestDevices(t), MachineConfig{ID: "box", Kind: "ssh"})
			pidFile := filepath.Join(t.TempDir(), "proxy.pid")
			m.nodeCmd = func(ctx context.Context) *exec.Cmd {
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFakeSSHProxyParent$")
				cmd.Env = append(os.Environ(), fakeSSHProxyParentEnv+"="+pidFile)
				if parentExits {
					cmd.Env = append(cmd.Env, fakeSSHParentExitEnv+"=1")
				}
				return cmd
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			finished := false
			t.Cleanup(func() {
				cancel()
				if !finished {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Errorf("ssh close did not return during cleanup")
					}
				}
				if data, err := os.ReadFile(pidFile); err == nil {
					if pid, convErr := strconv.Atoi(string(data)); convErr == nil {
						killProxyProcess(pid)
					}
				}
			})
			go func() { done <- m.connect(ctx) }()

			proxyPID := waitForProxyPID(t, pidFile)
			t.Cleanup(func() { killProxyProcess(proxyPID) })
			if parentExits {
				// EOF makes connect retry the handshake. Check the first process tree before stopping those retries.
				requireProxyProcessGone(t, proxyPID)
				cancel()
			} else {
				cancel()
			}
			select {
			case err := <-done:
				finished = true
				if err == nil {
					t.Fatal("closing the handshake returned no error")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("closing the handshake did not close ssh")
			}
			if !parentExits {
				requireProxyProcessGone(t, proxyPID)
			}
		})
	}
}

func waitForProxyPID(t *testing.T, name string) int {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-ticker.C:
			data, err := os.ReadFile(name)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			pid, convErr := strconv.Atoi(string(data))
			if convErr != nil || pid <= 0 {
				t.Fatalf("invalid ProxyCommand pid %q: %v", data, convErr)
			}
			return pid
		case <-timeout.C:
			t.Fatal("fake ssh did not start its ProxyCommand")
		}
	}
}

func requireProxyProcessGone(t *testing.T, pid int) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		if !proxyProcessAlive(pid) {
			return
		}
		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatalf("ProxyCommand process %d outlived the abandoned ssh connection", pid)
		}
	}
}
