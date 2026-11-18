package node

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/runtimepkg"
)

// The bridge is written out once per version, and older versions go.
func TestPaseoBridgeIsWrittenOut(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "persona-paseo-runtime")
	files := []runtimepkg.File{{Path: "paseo/bridge.mjs", Data: []byte("// v1")}, {Path: "runtime-sdk/index.js", Data: []byte("export {}")}}
	pkg := &runtimepkg.Package{Name: paseoPackage, Files: files, Hash: runtimepkg.Hash(files)}
	got, err := writePackage(dir, pkg)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(got, "paseo", "bridge.mjs")); string(b) != "// v1" || filepath.Base(got) != pkg.Hash {
		t.Fatalf("wrote %s: %q", got, b)
	}
	again, err := writePackage(dir, pkg)
	if err != nil || again != got {
		t.Fatalf("again %s %v", again, err)
	}
	files2 := []runtimepkg.File{{Path: "paseo/bridge.mjs", Data: []byte("// v2")}}
	pkg2 := &runtimepkg.Package{Name: paseoPackage, Files: files2, Hash: runtimepkg.Hash(files2)}
	if _, err := writePackage(dir, pkg2); err != nil {
		t.Fatal(err)
	}
	if dirExists(got) {
		t.Fatal("the older version stayed")
	}
	bad := &runtimepkg.Package{Name: paseoPackage, Files: []runtimepkg.File{{Path: "../escape.js"}}, Hash: "x"}
	if _, err := writePackage(dir, bad); err == nil {
		t.Fatal("a path out of the package was written")
	}
}

// What runs the bridge: PERSONA_NODEJS first; an app binary (Paseo's desktop app) runs as node with
// ELECTRON_RUN_AS_NODE, and a node without it, also when the node inherited it; one too old is skipped.
func TestPaseoBridgeJSRuntime(t *testing.T) {
	t.Setenv("ELECTRON_RUN_AS_NODE", "1") // as under an Electron host, such as an editor's extension host
	dir := t.TempDir()
	fake := func(name, version string) string {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, []byte(fmt.Sprintf("#!/bin/sh\nif [ \"$ELECTRON_RUN_AS_NODE\" != \"${WANT_ELECTRON:-}\" ]; then exit 3; fi\nprintf %s\n", version)), 0o755)
		return p
	}
	old := fake("node-old", "16.20.0")
	t.Setenv("PERSONA_NODEJS", old)
	t.Setenv("PATH", dir) // nothing else to find
	if js, err := findPaseoJSRuntime(context.Background(), t.TempDir()); err == nil && js.Path == old {
		t.Fatal("a Node.js older than the bridge needs was taken")
	}
	app := fake("Paseo", "22.20.0")
	t.Setenv("PERSONA_NODEJS", app)
	t.Setenv("WANT_ELECTRON", "1")
	js, err := findPaseoJSRuntime(context.Background(), t.TempDir())
	if err != nil || js.Path != app || !js.Electron {
		t.Fatalf("the app binary: %+v %v", js, err)
	}
	t.Setenv("WANT_ELECTRON", "")
	node := fake("node", "22.16.0")
	t.Setenv("PERSONA_NODEJS", node)
	if js, err := findPaseoJSRuntime(context.Background(), t.TempDir()); err != nil || js.Path != node || js.Electron {
		t.Fatalf("node: %+v %v", js, err)
	}
}

// The Paseo daemon's runtime is its process's executable only while that daemon answers at paseo.pid's address, and
// only a Node.js or an Electron app: a paseo.pid left behind by a Paseo that crashed names a process that may now be
// any program of this user, which finding the bridge's Node.js ran with -e … and ELECTRON_RUN_AS_NODE=1.
func TestPaseoDaemonRuntimeOnlyWhilePaseoAnswers(t *testing.T) {
	dir := t.TempDir()
	ran := filepath.Join(dir, "ran")
	program := func(rel string) string { // says Node.js 22 and notes how it was run
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(fmt.Sprintf("#!/bin/sh\necho \"$0 ELECTRON_RUN_AS_NODE=$ELECTRON_RUN_AS_NODE\" >> %q\nprintf 22.20.0\n", ran)), 0o755)
		return p
	}
	other := program("bin/someapp")
	node := program("nodejs/bin/node")
	app := program("Paseo/paseo") // Paseo's app on Linux: its resources beside its binary
	_ = os.MkdirAll(filepath.Join(dir, "Paseo", "resources"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "Paseo", "resources", "app.asar"), nil, 0o600)
	exe := other
	old := processExecutable
	processExecutable = func(int) string { return exe }
	t.Cleanup(func() { processExecutable = old })
	t.Setenv("PERSONA_NODEJS", "")
	t.Setenv("ELECTRON_RUN_AS_NODE", "")

	home := t.TempDir()
	pidAt := func(listen string) {
		b, _ := json.Marshal(map[string]any{"pid": 4242, "listen": listen})
		_ = os.WriteFile(filepath.Join(home, "paseo.pid"), b, 0o600)
	}
	ctx := context.Background()
	gone, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pidAt(gone.Addr().String())
	_ = gone.Close() // nothing answers there: the Paseo that wrote it is gone
	if js, why := paseoDaemonRuntime(ctx, home); js.Path != "" || why != "" {
		t.Fatalf("a Paseo that does not answer: %+v %q", js, why)
	}
	_, _ = findPaseoJSRuntime(ctx, home)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	pidAt(ln.Addr().String())
	if js, why := paseoDaemonRuntime(ctx, home); js.Path != "" || !strings.Contains(why, other) {
		t.Fatalf("Paseo answers, its pid is another program: %+v %q", js, why)
	}
	_, _ = findPaseoJSRuntime(ctx, home)
	if b, _ := os.ReadFile(ran); strings.Contains(string(b), other) {
		t.Fatalf("another program was run: %s", b)
	}

	exe = node
	if js, _ := paseoDaemonRuntime(ctx, home); js.Path != node || js.Electron || js.From != "the Paseo daemon's" {
		t.Fatalf("Paseo on a node: %+v", js)
	}
	exe = app
	if js, _ := paseoDaemonRuntime(ctx, home); js.Path != app || !js.Electron {
		t.Fatalf("Paseo on its app: %+v", js)
	}
	js, err := findPaseoJSRuntime(ctx, home)
	if err != nil || js.Path != app || !js.Electron || js.Version != "22.20.0" {
		t.Fatalf("the daemon's runtime first: %+v %v", js, err)
	}
	if b, _ := os.ReadFile(ran); !strings.Contains(string(b), app+" ELECTRON_RUN_AS_NODE=1") {
		t.Fatalf("the app was not run as Node.js: %s", b)
	}
}

// An Electron app's binary is told by what Electron ships: on a Mac its framework in the app bundle (for the app's
// binary and a helper inside it), elsewhere resources/app.asar or resources/app beside the binary. A program without
// them, or a Mac app on something else, is no JavaScript runtime.
func TestElectronAppLayouts(t *testing.T) {
	root := t.TempDir()
	put := func(rel string) string { // a folder when rel ends in /, else a file
		p := filepath.Join(root, filepath.FromSlash(rel))
		if strings.HasSuffix(rel, "/") {
			_ = os.MkdirAll(p, 0o755)
			return p
		}
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, nil, 0o755)
		return p
	}
	put("Paseo.app/Contents/Frameworks/Electron Framework.framework/")
	put("opt/Paseo/resources/app.asar")
	put("win/Paseo/resources/app/")
	put("Other.app/Contents/Frameworks/Other Framework.framework/")
	for path, want := range map[string]bool{
		put("Paseo.app/Contents/MacOS/Paseo"):                                             true,
		put("Paseo.app/Contents/Frameworks/Paseo Helper.app/Contents/MacOS/Paseo Helper"): true,
		put("opt/Paseo/paseo"):                                                            true,
		put("win/Paseo/Paseo.exe"):                                                        true,
		put("bin/someapp"):                                                                false,
		put("Other.app/Contents/MacOS/Other"):                                             false,
	} {
		if isElectronApp(path) != want {
			t.Errorf("%s: want %v", strings.TrimPrefix(path, root), want)
		}
	}
}

// On a Mac, Paseo's desktop app is a candidate of the bridge's (the system's Applications folder, then the user's),
// run as Node.js; elsewhere there is none.
func TestPaseoAppPaths(t *testing.T) {
	got := paseoAppPaths("darwin", "/Users/me")
	want := []string{"/Applications/Paseo.app/Contents/MacOS/Paseo", "/Users/me/Applications/Paseo.app/Contents/MacOS/Paseo"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
	if got := paseoAppPaths("linux", "/home/me"); got != nil {
		t.Fatalf("on Linux: %q", got)
	}
}

// A Mac process's executable is KERN_PROCARGS2's executable path, never its name: Paseo's supervisor renames itself
// ("Paseo Supervisor"), which rewrites its arguments (and what ps -o comm= says), not that path.
func TestDarwinExecPathIsWhatTheKernelRan(t *testing.T) {
	exe := "/Applications/Paseo.app/Contents/MacOS/Paseo"
	data := make([]byte, 4)
	binary.NativeEndian.PutUint32(data, 2)
	data = append(data, []byte(exe+"\x00\x00\x00\x00")...)
	data = append(data, []byte("Paseo Supervisor\x00\x00\x00\x00\x00\x00PASEO_HOME=/x\x00")...) // the title over its arguments
	if got, err := darwinExecPath(data); err != nil || got != exe {
		t.Fatalf("got %q %v", got, err)
	}
	for _, bad := range [][]byte{nil, data[:3], data[:4], append(slices.Clone(data[:4]), 0, 'x')} {
		if got, err := darwinExecPath(bad); err == nil {
			t.Fatalf("%q read from %q", got, bad)
		}
	}
}

// The runtime's environment sets ELECTRON_RUN_AS_NODE itself: once for an app binary, never for a node (dropping an
// inherited one), and the caller's slice is left as it was.
func TestJSRuntimeEnv(t *testing.T) {
	base := []string{"A=1", "ELECTRON_RUN_AS_NODE=1", "B=2"}
	if got := (jsRuntime{Path: "node"}).env(base); !slices.Equal(got, []string{"A=1", "B=2"}) {
		t.Fatalf("node: %q", got)
	}
	if got := (jsRuntime{Path: "Paseo", Electron: true}).env(base); !slices.Equal(got, []string{"A=1", "B=2", "ELECTRON_RUN_AS_NODE=1"}) {
		t.Fatalf("app binary: %q", got)
	}
	if !slices.Equal(base, []string{"A=1", "ELECTRON_RUN_AS_NODE=1", "B=2"}) {
		t.Fatalf("base changed: %q", base)
	}
}

// The bridge is told the node's mcp.json, the one beside the runtime socket, to give its new Agents the Persona tools,
// and which of them its Agents call without a card: the same as a Claude Agent's start allows (#4966).
func TestPaseoBridgeArgsNameTheNodesMCPConfig(t *testing.T) {
	got := paseoBridgeArgs("/pkg", "/data/persona/runtime.sock", "/home/u/.paseo")
	want := []string{"/pkg/paseo/bridge.mjs", "--socket", "/data/persona/runtime.sock", "--paseo-home", "/home/u/.paseo",
		"--mcp-config", "/data/persona/mcp.json", "--allow-tools", ownToolsRule}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// fakeBridge makes the bridge a shell script that marks each start in dir and lives until stopped (or exits at once
// when quit is set).
func fakeBridge(t *testing.T, quit bool) string {
	t.Helper()
	dir := t.TempDir()
	old := paseoBridgeCommand
	t.Cleanup(func() { paseoBridgeCommand = old })
	paseoBridgeCommand = func(ctx context.Context, rh *RuntimeHost) (*exec.Cmd, error) {
		script := fmt.Sprintf("echo started >> %q; echo hello from the bridge", filepath.Join(dir, "starts"))
		if !quit {
			script += "; trap 'echo stopped >> " + filepath.Join(dir, "starts") + "; exit 0' TERM; while :; do sleep 0.05; done"
		}
		return exec.Command("/bin/sh", "-c", script), nil
	}
	oldMin := paseoBridgeRestartMin
	paseoBridgeRestartMin = 20 * time.Millisecond
	t.Cleanup(func() { paseoBridgeRestartMin = oldMin })
	return filepath.Join(dir, "starts")
}

func starts(path string) []string {
	b, _ := os.ReadFile(path)
	return strings.Fields(string(b))
}

// The bridge runs while the engine is Paseo: engine.use starts it, another engine stops it; one that exits is run
// again.
func TestPaseoBridgeFollowsTheEngine(t *testing.T) {
	marks := fakeBridge(t, false)
	n, _ := runtimeNode(t)
	if _, err := nodeCall(t, n, "engine.use", map[string]any{"engine": proto.EnginePaseo}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the bridge started", func() bool { return len(starts(marks)) == 1 })
	if _, err := nodeCall(t, n, "engine.use", map[string]any{"engine": proto.EnginePaseo}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if len(starts(marks)) != 1 {
		t.Fatal("started twice for the same engine")
	}
	if _, err := nodeCall(t, n, "engine.use", map[string]any{"engine": proto.EngineHerdr}); err != nil {
		t.Fatal(err)
	}
	if got := starts(marks); len(got) != 2 || got[1] != "stopped" {
		t.Fatalf("not stopped: %v", got)
	}

	quitting := fakeBridge(t, true)
	if _, err := nodeCall(t, n, "engine.use", map[string]any{"engine": proto.EnginePaseo}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "run again after it exited", func() bool { return len(starts(quitting)) >= 2 })
	n.runtimeHost.setPaseoBridge(false)
}

// node.info's Paseo: available when its daemon answers at paseo.pid's address, or its bridge is connected (then with
// Paseo's version). Never at a default port: that may be another home's daemon.
func TestPaseoEngineDetected(t *testing.T) {
	n, _ := runtimeNode(t)
	home := t.TempDir()
	t.Setenv("PASEO_HOME", home)
	e := n.paseoEngine(context.Background())
	if e.ID != proto.EnginePaseo || e.Name != "Paseo" || e.Available {
		t.Fatalf("without Paseo: %+v", e)
	}
	_ = os.WriteFile(filepath.Join(home, "paseo.pid"), []byte(`{"pid":1,"listen":null}`), 0o600)
	if e := n.paseoEngine(context.Background()); e.Available {
		t.Fatalf("a Paseo still starting: %+v", e)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	b, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "listen": ln.Addr().String()})
	_ = os.WriteFile(filepath.Join(home, "paseo.pid"), b, 0o600)
	if e := n.paseoEngine(context.Background()); !e.Available {
		t.Fatalf("Paseo listening: %+v", e)
	}
	hello := paseoEngineHello()
	hello["version"] = "0.14.0"
	dialRuntime(t, n, hello)
	if e := n.paseoEngine(context.Background()); !e.Available || e.Version != "0.14.0" {
		t.Fatalf("bridge connected: %+v", e)
	}
	found := false
	for _, x := range n.engines(context.Background(), "") {
		found = found || x.ID == proto.EnginePaseo
	}
	if !found {
		t.Fatal("node.info's engines leave Paseo out")
	}
}

// scriptBridge makes the bridge a shell script (run again soon after it exits).
func scriptBridge(t *testing.T, script string) {
	t.Helper()
	old := paseoBridgeCommand
	t.Cleanup(func() { paseoBridgeCommand = old })
	paseoBridgeCommand = func(ctx context.Context, rh *RuntimeHost) (*exec.Cmd, error) {
		return exec.Command("/bin/sh", "-c", script), nil
	}
	oldMin := paseoBridgeRestartMin
	paseoBridgeRestartMin = 20 * time.Millisecond
	t.Cleanup(func() { paseoBridgeRestartMin = oldMin })
}

// paseoAnswering is a Paseo home whose daemon answers at its paseo.pid's address, as PASEO_HOME.
func paseoAnswering(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	home := t.TempDir()
	b, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "listen": ln.Addr().String()})
	_ = os.WriteFile(filepath.Join(home, "paseo.pid"), b, 0o600)
	t.Setenv("PASEO_HOME", home)
}

// A bridge that exits says why in node.info's note: the error Node.js printed, without its source line, stack and
// trailer, rather than "exited: exit status 1". It is the canonical Chinese the hub words for each reader.
func TestPaseoBridgeExitSaysWhy(t *testing.T) {
	scriptBridge(t, `{ echo 'file:///pkg/paseo/bridge.mjs:7'; echo "import { parseArgs } from 'node:util'"; echo '         ^^^^^^^^^';
echo "SyntaxError: The requested module 'node:util' does not provide an export named 'parseArgs'";
echo '    at ModuleJob._instantiate (node:internal/modules/esm/module_job:123:21)'; echo; echo 'Node.js v16.20.2'; } >&2; exit 1`)
	paseoAnswering(t)
	n, _ := runtimeNode(t)
	if _, err := nodeCall(t, n, "engine.use", map[string]any{"engine": proto.EnginePaseo}); err != nil {
		t.Fatal(err)
	}
	defer n.runtimeHost.setPaseoBridge(false)
	want := i18n.T(i18n.Default, "node-engine.paseo_unreachable",
		i18n.Args{"reason": "SyntaxError: The requested module 'node:util' does not provide an export named 'parseArgs'"})
	waitFor(t, "the bridge's error as the note", func() bool { return n.paseoEngine(context.Background()).Note == want })
	if e := n.paseoEngine(context.Background()); !e.Available {
		t.Fatalf("Paseo answers: %+v", e)
	}
}

// A bridge that runs without connecting says so once it has had its time, with the last thing it printed; before that,
// and once it is connected, there is no note.
func TestPaseoBridgeRunningButNotConnectedSaysSo(t *testing.T) {
	scriptBridge(t, `echo 'paseo: Paseo refused the connection (4401 unauthorized); trying again' >&2; trap 'exit 0' TERM; while :; do sleep 0.05; done`)
	oldWait := paseoBridgeConnectWait
	t.Cleanup(func() { paseoBridgeConnectWait = oldWait })
	paseoBridgeConnectWait = time.Hour
	paseoAnswering(t)
	n, _ := runtimeNode(t)
	if _, err := nodeCall(t, n, "engine.use", map[string]any{"engine": proto.EnginePaseo}); err != nil {
		t.Fatal(err)
	}
	defer n.runtimeHost.setPaseoBridge(false)
	waitFor(t, "the bridge's line", func() bool {
		n.runtimeHost.paseo.mu.Lock()
		defer n.runtimeHost.paseo.mu.Unlock()
		return len(n.runtimeHost.paseo.tail) == 1
	})
	if e := n.paseoEngine(context.Background()); !e.Available || e.Note != "" {
		t.Fatalf("a bridge still starting: %+v", e)
	}
	paseoBridgeConnectWait = 0
	want := i18n.T(i18n.Default, "node-engine.paseo_not_connected_why",
		i18n.Args{"reason": "paseo: Paseo refused the connection (4401 unauthorized); trying again"})
	if e := n.paseoEngine(context.Background()); !e.Available || e.Note != want {
		t.Fatalf("a bridge that does not connect: %+v", e)
	}
	n.runtimeHost.paseo.mu.Lock()
	n.runtimeHost.paseo.tail = nil // one that printed nothing
	n.runtimeHost.paseo.mu.Unlock()
	if e := n.paseoEngine(context.Background()); e.Note != i18n.T(i18n.Default, "node-engine.paseo_not_connected") {
		t.Fatalf("a silent bridge: %+v", e)
	}
	hello := paseoEngineHello()
	hello["version"] = "0.15.4"
	dialRuntime(t, n, hello)
	if e := n.paseoEngine(context.Background()); !e.Available || e.Version != "0.15.4" || e.Note != "" {
		t.Fatalf("connected: %+v", e)
	}
}

// A Paseo older than the oldest Persona is tested with still works, with a note asking to update it; a version that
// cannot be read gets none.
func TestPaseoOutdatedNote(t *testing.T) {
	n, _ := runtimeNode(t)
	hello := paseoEngineHello()
	hello["version"] = "0.14.0"
	dialRuntime(t, n, hello)
	want := i18n.T(i18n.Default, "node-engine.paseo_outdated", i18n.Args{"version": "0.14.0", "minimum": "0.15.0"})
	if e := n.paseoEngine(context.Background()); !e.Available || e.Version != "0.14.0" || e.Note != want {
		t.Fatalf("Paseo 0.14.0: %+v", e)
	}
	for v, outdated := range map[string]bool{"0.10.3": true, "0.14.9-beta.1": true, "0.15.0": false, "0.15.4": false,
		"1.0.0": false, "": false, "unknown": false, "v0.14.0": false} {
		if got := paseoVersionNote(v) != ""; got != outdated {
			t.Errorf("%q: outdated %v", v, got)
		}
	}
}

// What a bridge that exited printed about it: the last error Node.js or the bridge printed, with its message's other
// lines and without its stack or the object Node.js shows after it; else its last line that says anything.
func TestBridgeErrorLines(t *testing.T) {
	long := strings.Repeat("é", 400)
	for _, c := range []struct {
		lines []string
		want  string
	}{
		{[]string{"paseo: connected to Paseo 0.15.4", "node:internal/modules/esm/resolve:873", "  throw new ERR_MODULE_NOT_FOUND(packageName, fileURLToPath(base), null);",
			"        ^", "", "Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'ws' imported from /pkg/paseo/lib/ws.js",
			"    at packageResolve (node:internal/modules/esm/resolve:873:9)", "    at moduleResolve (node:internal/modules/esm/resolve:946:18) {",
			"  code: 'ERR_MODULE_NOT_FOUND'", "}", "", "Node.js v22.20.0"},
			"Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'ws' imported from /pkg/paseo/lib/ws.js"},
		{[]string{"uncaught: Error: Paseo said no", "while it was asked twice", "    at onPaseo (file:///pkg/paseo/lib/bridge.js:540:11)"},
			"uncaught: Error: Paseo said no while it was asked twice"},
		{[]string{"persona: connected to the node", "paseo: away; leaving the node until it is back", ""}, "paseo: away; leaving the node until it is back"},
		{[]string{"boom", "(Use `node --trace-uncaught ...` to show where the exception was thrown)", "", "Node.js v22.20.0"}, "boom"},
		{[]string{long}, strings.Repeat("é", 300) + "…"},
		{[]string{"", "    "}, ""},
		{nil, ""},
	} {
		if got := bridgeErrorLines(c.lines); got != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.lines, got, c.want)
		}
	}
}

// A line longer than the buffer is cut and the lines after it still come (#3959): the program's log is read to its end.
func TestLogLinesReadsPastALongLine(t *testing.T) {
	var got []string
	long := strings.Repeat("y", 2<<20) // past any buffer the reader may have
	logLines(strings.NewReader("one\r\n"+long+"\n"+strings.Repeat("z", 3000)+"\ntwo"), func(l string) { got = append(got, l) })
	want := []string{"one", long[:2000] + "…", strings.Repeat("z", 2000) + "…", "two"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %d lines, want %d: %.60q", len(got), len(want), got)
	}
}
