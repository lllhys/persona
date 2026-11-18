package node

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/streams"
)

// An FLV stream as ffmpeg writes it: the header, a sequence header, then frames (0x17 keyframe, 0x27 not).
func flvOf(frames ...byte) []byte {
	b := []byte{'F', 'L', 'V', 1, 1, 0, 0, 0, 9, 0, 0, 0, 0}
	tag := func(body []byte) {
		n := len(body)
		b = append(b, 9, byte(n>>16), byte(n>>8), byte(n), 0, 0, 0, 0, 0, 0, 0)
		b = append(b, body...)
		b = append(b, 0, 0, 0, byte(11+n))
	}
	tag([]byte{0x17, 0, 0, 0, 0, 1, 0x42})
	for _, f := range frames {
		tag([]byte{f, 1, 0, 0, 0, 0, 0, 0, 2, 0x65, 0x88})
	}
	return b
}

func TestFLVVideoFrames(t *testing.T) {
	if n, key := flvVideoFrames(flvOf(0x17, 0x27, 0x27)); n != 3 || !key {
		t.Fatalf("frames = %d, first key %v; want 3, true", n, key)
	}
	if n, key := flvVideoFrames(flvOf(0x27, 0x17)); n != 2 || key {
		t.Fatalf("frames = %d, first key %v; want 2, false", n, key)
	}
	cut := flvOf(0x17, 0x27)
	if n, _ := flvVideoFrames(cut[:len(cut)-8]); n != 1 {
		t.Fatalf("a cut-off last tag counted: %d", n)
	}
	if n, _ := flvVideoFrames([]byte("not an flv stream")); n != 0 {
		t.Fatal("not FLV, yet frames")
	}
}

func TestFFmpegListAndVersion(t *testing.T) {
	list := "Encoders:\n V..... = Video\n ------\n V....D libx264              libx264 H.264 / AVC\n V....D libx264rgb   x\n"
	if !hasEncoder(list, "libx264") || hasEncoder(list, "libx265") || hasEncoder("libx264 alone", "libx264") {
		t.Fatal("hasEncoder")
	}
	for out, want := range map[string]string{
		"ffmpeg version 9.0.2 Copyright (c) 2000-2026 the FFmpeg developers\nbuilt with …": "ffmpeg 9.0.2",
		"ffmpeg version n8.1.3-12-gabc Copyright":                                          "ffmpeg n8.1.3-12-gabc",
		"something else": "ffmpeg",
	} {
		if got := ffmpegVersion(out); got != want {
			t.Errorf("ffmpegVersion(%q) = %q, want %q", out, got, want)
		}
	}
}

// fakeFFmpeg is an executable at dir/name; the probe itself is faked, so its content only gives it a size.
func fakeFFmpeg(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// The probe runs once per file: again after the file changes (a brew upgrade), and never on PATH.
func TestTheX264ProbeIsKeptPerFile(t *testing.T) {
	dir := t.TempDir()
	ff := fakeFFmpeg(t, dir, "#!/bin/sh\n")
	var probes atomic.Int32
	verdict := "ffmpeg 9.0.2"
	swap(t, &probeFFmpeg, func(_ context.Context, path string) (string, string) {
		if path != ff {
			t.Errorf("probed %s", path)
		}
		probes.Add(1)
		return verdict, "made up"
	})
	swap(t, &screenFFmpegPaths, []string{filepath.Join(dir, "missing"), ff})
	p := &x264Probe{}
	ctx := context.Background()
	if runtime.GOOS != "darwin" {
		if path, _ := p.get(ctx); path != "" || probes.Load() != 0 {
			t.Fatal("looked for ffmpeg off a Mac")
		}
		return
	}
	for range 2 {
		if path, v := p.get(ctx); path != ff || v != "ffmpeg 9.0.2" || probes.Load() != 1 {
			t.Fatalf("get = %q %q after %d probes, want the answer from 1", path, v, probes.Load())
		}
	}
	// Upgraded, and this one fails: tested again, nothing found.
	fakeFFmpeg(t, dir, "#!/bin/sh\n# another version\n")
	verdict = ""
	if path, v := p.get(ctx); path != "" || v != "" || probes.Load() != 2 {
		t.Fatalf("a failing ffmpeg = %q %q after %d probes", path, v, probes.Load())
	}
	// None installed: nothing, and nothing probed.
	swap(t, &screenFFmpegPaths, []string{filepath.Join(dir, "missing")})
	if path, _ := p.get(ctx); path != "" || probes.Load() != 2 {
		t.Fatalf("no ffmpeg = %q after %d probes", path, probes.Load())
	}
}

// A failed probe stands for screenX264Retry, then the same file is tried again; a probe never runs on a caller's
// context, so a status call that was already cancelled still gets (and keeps) the right answer (#8859).
func TestAFailedX264ProbeIsTriedAgain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	ff := fakeFFmpeg(t, t.TempDir(), "#!/bin/sh\n")
	swap(t, &screenFFmpegPaths, []string{ff})
	now := time.Unix(1_800_000_000, 0)
	swap(t, &screenX264Now, func() time.Time { return now })
	var probes atomic.Int32
	pass := false
	swap(t, &probeFFmpeg, func(ctx context.Context, _ string) (string, string) {
		probes.Add(1)
		if ctx.Err() != nil {
			return "", "cancelled"
		}
		if !pass {
			return "", "made up"
		}
		return "ffmpeg 9.0.2", ""
	})
	p := &x264Probe{}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if path, _ := p.get(cancelled); path != "" || probes.Load() != 1 {
		t.Fatalf("first, failing: %q after %d probes", path, probes.Load())
	}
	pass = true
	now = now.Add(screenX264Retry - time.Second)
	if path, _ := p.get(cancelled); path != "" || probes.Load() != 1 {
		t.Fatalf("within the retry time: %q after %d probes, want the failure kept", path, probes.Load())
	}
	now = now.Add(time.Second)
	if path, v := p.get(cancelled); path != ff || v != "ffmpeg 9.0.2" || probes.Load() != 2 {
		t.Fatalf("after it, on a cancelled caller's context: %q %q after %d probes", path, v, probes.Load())
	}
	now = now.Add(time.Hour)
	if path, _ := p.get(cancelled); path != ff || probes.Load() != 2 {
		t.Fatalf("a pass stands until the file changes: %q after %d probes", path, probes.Load())
	}
}

// A program without libx264 fails the probe (no self-test run); the real thing, named by PERSONA_TEST_FFMPEG (e.g.
// Homebrew's), passes it.
func TestProbeFFmpeg(t *testing.T) {
	dir := t.TempDir()
	nox264 := fakeFFmpeg(t, dir, "#!/bin/sh\necho ' V....D libx265   libx265 H.265'\n")
	if v, why := probeFFmpeg(context.Background(), nox264); v != "" || why != "no libx264" {
		t.Fatalf("an ffmpeg without libx264 = %q (%s)", v, why)
	}
	real := os.Getenv("PERSONA_TEST_FFMPEG")
	if real == "" {
		t.Skip("set PERSONA_TEST_FFMPEG to an ffmpeg with libx264 for the self-test")
	}
	if v, why := probeFFmpeg(context.Background(), real); !strings.HasPrefix(v, "ffmpeg ") {
		t.Fatalf("probe of %s = %q (%s)", real, v, why)
	}
}

func TestScreenDialPassesX264(t *testing.T) {
	n := &Node{screens: newScreenLogins()}
	var got []bool
	n.screenStart = func(_ context.Context, _, _ string, _, _, x264 bool, _ func()) (bool, error) {
		got = append(got, x264)
		return false, nil
	}
	n.screens.byID["K"] = &screenPair{result: rfbLoginSignedIn, open: true}
	for _, on := range []bool{false, true} {
		body, _ := json.Marshal(map[string]any{"id": "v1", "pair": "K", "x264": on})
		if _, err := n.screenDial(context.Background(), body); err != nil {
			t.Fatal(err)
		}
		n.screens.leave()
	}
	if !slices.Equal(got, []bool{false, true}) {
		t.Fatalf("screenStart got x264 %v", got)
	}
}

// With x264 on for this Mac the helper gets `--x264 <ffmpeg>` after its own arguments, when ffmpeg passed the probe;
// without one it starts as before.
func TestTheHelperGetsTheUsersFFmpeg(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	installedHelper(t, "granted")
	dir := t.TempDir()
	ff := fakeFFmpeg(t, dir, "#!/bin/sh\n")
	swap(t, &screenFFmpegPaths, []string{ff})
	swap(t, &probeFFmpeg, func(context.Context, string) (string, string) { return "ffmpeg 9.0.2", "" })
	var launched [][]string
	connect := fakeHelper(t, func(token string) string { return token }, "")
	swap(t, &screenLaunch, func(ctx context.Context, helper string, args ...string) error {
		launched = append(launched, args)
		return connect(ctx, helper, args...)
	})
	start := func(x264 bool) {
		t.Helper()
		n := &Node{screens: newScreenLogins(), streams: streams.New(func(string, any) error { return nil })}
		rfb, peer := net.Pipe()
		defer peer.Close()
		tee := n.screens.observe("K", rfb)
		defer tee.Close()
		n.screens.mu.Lock()
		n.screens.byID["K"].result = rfbLoginSignedIn
		n.screens.mu.Unlock()
		if _, err := n.startScreenHelper(context.Background(), "v1", "K", false, false, x264, func() {}); err != nil {
			t.Fatal(err)
		}
	}
	start(false)
	start(true)
	swap(t, &probeFFmpeg, func(context.Context, string) (string, string) { return "", "made up" })
	swap(t, &screenX264, &x264Probe{})
	start(true)
	if len(launched) != 3 {
		t.Fatalf("launches = %d", len(launched))
	}
	tail := func(args []string) string { return strings.Join(args[4:], " ") } // after --socket s --token t
	if tail(launched[0]) != "" || tail(launched[1]) != "--x264 "+ff || tail(launched[2]) != "" {
		t.Fatalf("helper arguments after its own: %q, %q, %q", tail(launched[0]), tail(launched[1]), tail(launched[2]))
	}
}
