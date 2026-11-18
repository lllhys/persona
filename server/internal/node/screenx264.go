package node

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Optional x264 for the live screen (docs/extensions/computer-use.md "x264 on a direct LAN (#8859)"). When the user
// has installed ffmpeg with libx264 themselves and x264 is on for this Mac (screen.dial's x264), the node starts the helper
// with `--x264 <ffmpeg>`, and the helper encodes with it while the stream is on a direct LAN route. Persona ships, links,
// downloads and installs nothing of x264's or ffmpeg's: this only finds the user's ffmpeg and checks that it works.
//
// ffmpeg is looked for at fixed paths, never on PATH. One counts when its encoder list has libx264 and a 3-frame
// self-test through the helper's command line gives back 3 FLV video frames, the first a keyframe. The answer is kept
// for that file's path, size and modification time, so a `brew upgrade` is tested again.

// screenFFmpegPaths are where ffmpeg is looked for, in order: Homebrew on Apple silicon and on Intel, then MacPorts. A
// variable for tests.
var screenFFmpegPaths = []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg", "/opt/local/bin/ffmpeg"}

// screenX264Wait bounds one probe (an encoder list, a self-test and a version; about 0.15–0.3 s). A variable for tests.
var screenX264Wait = 5 * time.Second

// screenX264Retry is how long a failed probe stands before the same file is tried again; a passed one stands until the
// file changes. A variable for tests.
var screenX264Retry = time.Minute

// screenX264Now is the probe's clock. A variable for tests.
var screenX264Now = time.Now

// screenX264 is this node's probe.
var screenX264 = &x264Probe{}

// x264Probe keeps the last answer for one file. A caller that comes while a probe runs waits for it, so two statuses
// never disagree about the same ffmpeg. The probe runs on its own deadline, never the caller's: a status call cut short
// must not leave "no ffmpeg" behind, and a failure is tried again after screenX264Retry (on the first node built with
// it, macstudio's answer stayed empty after its first probe failed, #8859).
type x264Probe struct {
	mu       sync.Mutex
	key      string // the file the answer is for: path, size, modification time; "" before any, and with none found
	path     string // the ffmpeg that passed, "" when none did
	version  string
	failedAt time.Time // when the probe of key last failed; zero after a pass
}

// candidate is the first executable ffmpeg at screenFFmpegPaths and its key, or "" for both.
func (p *x264Probe) candidate() (path, key string) {
	if runtime.GOOS != "darwin" {
		return "", ""
	}
	for _, c := range screenFFmpegPaths {
		st, err := os.Stat(c) // follows Homebrew's symlink to the version installed now
		if err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0 {
			return c, fmt.Sprintf("%s|%d|%d", c, st.Size(), st.ModTime().UnixNano())
		}
	}
	return "", ""
}

// get is the ffmpeg that passed the probe and its version ("ffmpeg 9.0.2"), or "" for both; probed first when the file
// changed since the last answer, or when the last probe of it failed more than screenX264Retry ago. Each probe's
// outcome goes to the node's log once.
func (p *x264Probe) get(ctx context.Context) (path, version string) {
	c, key := p.candidate()
	p.mu.Lock()
	defer p.mu.Unlock()
	retry := !p.failedAt.IsZero() && screenX264Now().Sub(p.failedAt) >= screenX264Retry
	if key == p.key && !retry {
		return p.path, p.version
	}
	p.key, p.path, p.version, p.failedAt = key, "", "", time.Time{}
	if c == "" {
		return "", ""
	}
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), screenX264Wait)
	v, why := probeFFmpeg(pctx, c)
	cancel()
	if v == "" {
		p.failedAt = screenX264Now()
		log.Printf("screen x264: %s doesn't qualify: %s", c, why)
		return "", ""
	}
	p.path, p.version = c, v
	log.Printf("screen x264: %s qualifies (%s)", c, v)
	return p.path, p.version
}

// probeFFmpeg is "ffmpeg <version>" when the ffmpeg at path has libx264 and passes the self-test, else "" and why not.
// A variable for tests.
var probeFFmpeg = func(ctx context.Context, path string) (version, why string) {
	out, err := exec.CommandContext(ctx, path, "-hide_banner", "-encoders").Output()
	if err != nil {
		return "", "-encoders: " + err.Error()
	}
	if !hasEncoder(string(out), "libx264") {
		return "", "no libx264"
	}
	if err := x264SelfTest(ctx, path); err != nil {
		return "", "self-test: " + err.Error()
	}
	out, err = exec.CommandContext(ctx, path, "-hide_banner", "-version").Output()
	if err != nil {
		return "", "-version: " + err.Error()
	}
	return ffmpegVersion(string(out)), ""
}

// hasEncoder: ffmpeg's `-encoders` list has name (lines like " V....D libx264  libx264 H.264 / AVC …").
func hasEncoder(list, name string) bool {
	for _, line := range strings.Split(list, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[1] == name {
			return true
		}
	}
	return false
}

// ffmpegVersion is "ffmpeg 9.0.2" from `-version`'s first line ("ffmpeg version 9.0.2 Copyright …"), else "ffmpeg".
func ffmpegVersion(out string) string {
	first, _, _ := strings.Cut(out, "\n")
	if f := strings.Fields(first); len(f) >= 3 && f[0] == "ffmpeg" && f[1] == "version" {
		return "ffmpeg " + f[2]
	}
	return "ffmpeg"
}

// x264SelfTest runs ffmpeg as the helper does (PersonaScreenCore's X264Plan.arguments, kept in step) on three small
// gray frames and checks that three FLV video frames come back, the first a keyframe.
func x264SelfTest(ctx context.Context, path string) error {
	const w, h, n = 64, 64, 3
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "rawvideo", "-pix_fmt", "nv12", "-video_size", fmt.Sprintf("%dx%d", w, h), "-framerate", "60",
		"-i", "pipe:0",
		"-fps_mode", "passthrough",
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-threads", "1",
		"-x264-params", "sliced-threads=1:keyint=infinite:colorprim=bt709:transfer=bt709:colormatrix=bt709:range=tv",
		"-crf", "20", "-maxrate", "2000k", "-bufsize", "100k",
		"-flush_packets", "1", "-f", "flv", "pipe:1",
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{} // as the helper starts it: nothing from this process's environment
	cmd.Stdin = bytes.NewReader(bytes.Repeat([]byte{128}, w*h*3/2*n))
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	if frames, firstKey := flvVideoFrames(out); frames != n || !firstKey {
		return fmt.Errorf("%d frames back, the first a keyframe: %v", frames, firstKey)
	}
	return nil
}

// flvVideoFrames counts an FLV stream's AVC frames (not its sequence header) and says whether the first is a keyframe.
func flvVideoFrames(b []byte) (frames int, firstKey bool) {
	if len(b) < 13 || string(b[:3]) != "FLV" {
		return 0, false
	}
	off := int(binary.BigEndian.Uint32(b[5:9])) + 4
	for off+11 <= len(b) {
		size := int(b[off+1])<<16 | int(b[off+2])<<8 | int(b[off+3])
		if off+11+size > len(b) {
			break
		}
		body := b[off+11 : off+11+size]
		if b[off] == 9 && len(body) >= 2 && body[0]&0x0F == 7 && body[1] == 1 {
			if frames == 0 {
				firstKey = body[0]>>4 == 1
			}
			frames++
		}
		off += 11 + size + 4
	}
	return frames, firstKey
}
