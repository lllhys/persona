package extmgr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

type packageRateWriter struct {
	io.Writer
	ctx context.Context
}

func (w packageRateWriter) Write(p []byte) (int, error) {
	timer := time.NewTimer(time.Duration(len(p)) * time.Second / (70 * 1024))
	defer timer.Stop()
	select {
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	case <-timer.C:
		return w.Writer.Write(p)
	}
}

func testNodePackage(t *testing.T) (proto.PluginNodeProgram, nodePackage, []byte) {
	t.Helper()
	data := bytes.Repeat([]byte("package"), (4<<20)/7+1)
	sum := sha256.Sum256(data)
	prog := proto.PluginNodeProgram{ID: "example-helper", Version: "1.0.0", Digest: hex.EncodeToString(sum[:])}
	pkg := nodePackage{file: filepath.Join(t.TempDir(), "package.tgz"), digest: prog.Digest, size: int64(len(data))}
	if err := os.WriteFile(pkg.file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return prog, pkg, data
}

func TestNodePackageSlowPipeKeepsPingResponsive(t *testing.T) {
	prog, pkg, data := testNodePackage(t)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		h2nR, h2nW := io.Pipe()
		n2hR, n2hW := io.Pipe()
		defer func() {
			cancel()
			h2nR.Close()
			h2nW.Close()
			n2hR.Close()
			n2hW.Close()
		}()
		hub, node := rpc.New("h", packageRateWriter{Writer: h2nW, ctx: ctx}), rpc.New("n", n2hW)
		var received []byte
		committed := false
		node.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
			switch method {
			case "ping":
				return true, nil
			case "plugin.node.have":
				return proto.PluginNodeHave{}, nil
			case "plugin.node.put.begin":
				return map[string]string{"upload_id": "upload"}, nil
			case "plugin.node.put.chunk":
				var chunk proto.PluginNodePutChunk
				if err := json.Unmarshal(params, &chunk); err != nil {
					return nil, err
				}
				if chunk.UploadID != "upload" || chunk.Offset != int64(len(received)) {
					return nil, fmt.Errorf("invalid chunk: offset %d, received %d", chunk.Offset, len(received))
				}
				received = append(received, chunk.Data...)
				return true, nil
			case "plugin.node.put.commit":
				if !bytes.Equal(received, data) {
					return nil, errors.New("package bytes differ")
				}
				committed = true
				return true, nil
			}
			return nil, fmt.Errorf("unexpected method %s", method)
		}
		go hub.Serve(ctx, n2hR)
		go node.Serve(ctx, h2nR)
		m := &Machine{peer: hub}
		done := make(chan error, 1)
		go func() { done <- putNodePackage(ctx, m, prog, pkg) }()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		pings := 0
		for {
			select {
			case err := <-done:
				if err != nil || !committed || pings < 3 {
					t.Fatalf("transfer: err=%v committed=%v pings=%d", err, committed, pings)
				}
				return
			case <-ticker.C:
				pingCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				err := m.call(pingCtx, "ping", nil, nil)
				stop()
				if err != nil {
					t.Fatalf("ping blocked by package transfer: %v", err)
				}
				pings++
			}
		}
	})
}

type packageCaller struct {
	nodeCaller
	call func(context.Context, string, any, any) error
}

func (p packageCaller) Done() <-chan struct{} { return nil }
func (p packageCaller) Call(ctx context.Context, method string, params, out any) error {
	return p.call(ctx, method, params, out)
}

func TestNodePackageLeaseRetries(t *testing.T) {
	h, _ := extensionTestHub(t)
	reg := h.extensions.reg
	ctx := t.Context()
	pv, err := reg.Preview(ctx, "dir:"+helperPackage(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Install(ctx, pv.ID, pv.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetSecret(ctx, "example-helper", "api_key", "example", "test"); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetSecret(ctx, "example-helper", "hub_only", "example", "test"); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetConfig(ctx, "example-helper", json.RawMessage(`{"computers":["box"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		begins, commits := 0, 0
		fail := true
		m := &Machine{cfg: MachineConfig{ID: "box"}, online: true, info: proto.NodeInfo{OS: "linux", Arch: "amd64"}}
		m.peer = packageCaller{call: func(_ context.Context, method string, params, out any) error {
			switch method {
			case "plugin.node.put.begin":
				begins++
				return json.Unmarshal([]byte(`{"upload_id":"upload"}`), out)
			case "plugin.node.put.chunk":
				if fail {
					return errors.New("connection lost")
				}
			case "plugin.node.put.commit":
				commits++
			}
			return nil
		}}
		h.fleetMu.Lock()
		h.machines = append(h.machines, m)
		h.byID[m.cfg.ID] = m
		h.fleetMu.Unlock()
		if err := h.syncExtensionNodes(ctx, m); err != nil {
			t.Fatal(err)
		}
		if begins != 1 || commits != 0 {
			t.Fatalf("expected initial upload failure: begins=%d commits=%d", begins, commits)
		}
		fail = false
		go h.extensionLeaseLoop(ctx)
		synctest.Wait()
		timer := time.NewTimer(extensionLeaseEvery)
		<-timer.C
		synctest.Wait()
		if begins != 2 || commits != 1 {
			t.Fatalf("stable connection never retried: begins=%d commits=%d", begins, commits)
		}
	})
}

func TestNodePackageRetryAfterReconnect(t *testing.T) {
	prog, pkg, _ := testNodePackage(t)
	synctest.Test(t, func(t *testing.T) {
		begins, chunks := 0, 0
		have, fail := false, true
		lost := errors.New("connection lost")
		peer := func() nodeCaller {
			return packageCaller{call: func(_ context.Context, method string, params, out any) error {
				switch method {
				case "plugin.node.have":
					out.(*proto.PluginNodeHave).Have = have
				case "plugin.node.put.begin":
					begins++
					return json.Unmarshal([]byte(`{"upload_id":"upload"}`), out)
				case "plugin.node.put.chunk":
					chunks++
					if fail {
						return lost
					}
				case "plugin.node.put.commit":
					have = true
				default:
					return fmt.Errorf("unexpected method %s", method)
				}
				return nil
			}}
		}
		m := &Machine{peer: peer()}
		if err := putNodePackage(t.Context(), m, prog, pkg); !errors.Is(err, lost) {
			t.Fatalf("first failure: %v", err)
		}
		for range 3 {
			m.peer = peer()
			if err := putNodePackage(t.Context(), m, prog, pkg); !errors.Is(err, lost) {
				t.Fatalf("reconnect failure: %v", err)
			}
		}
		if begins != 1 || chunks != 1 {
			t.Fatalf("reconnect restarted failed upload: begins=%d chunks=%d", begins, chunks)
		}
		next := prog
		next.Version = "1.0.1"
		if err := putNodePackage(t.Context(), m, next, pkg); !errors.Is(err, lost) || begins != 2 {
			t.Fatalf("another version must not inherit cooldown: begins=%d err=%v", begins, err)
		}
		if err := putNodePackage(t.Context(), m, prog, pkg); !errors.Is(err, lost) || begins != 2 {
			t.Fatalf("another failure erased original cooldown: begins=%d err=%v", begins, err)
		}
		have = true
		if err := putNodePackage(t.Context(), m, prog, pkg); err != nil {
			t.Fatalf("already installed package must bypass retry delay: %v", err)
		}
		have, fail = false, false
		if err := putNodePackage(t.Context(), m, prog, pkg); err != nil {
			t.Fatalf("have=true must clear the old failure: %v", err)
		}
		if begins != 3 || !have {
			t.Fatalf("retry did not complete: begins=%d have=%v", begins, have)
		}
	})
}
