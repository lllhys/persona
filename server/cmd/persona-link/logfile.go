package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/link"
)

// logMax is how big the log may grow before it is moved to <log>.1 and started over: about the menu bar app's
// limit for the helper's log (a setting).
const logMax = 1 << 20

// rotating is the log of `persona-link run --log`: appended to, moved aside to <path>.1 when it reaches max.
type rotating struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

func openRotating(path string, max int64) (*rotating, error) {
	r := &rotating{path: path, max: max}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotating) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, fi.Size()
	return nil
}

func (r *rotating) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size > 0 && r.size+int64(len(p)) > r.max {
		r.f.Close()
		_ = os.Rename(r.path, r.path+".1")
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotating) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

// logs prints the end of persona-link's log and, with -f, what is added to it after, across a rotation, until
// interrupted. Without a log file and with the systemd service installed, the journal has it.
func logs(e *env, args []string) error {
	n, follow := 100, false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "-f", "--follow":
			follow = true
			continue
		}
		v, next, ok, err := flagValue(args, i, "-n")
		if !ok && err == nil {
			v, next, ok, err = flagValue(args, i, "--lines")
		}
		switch {
		case err != nil:
			return err
		case !ok:
			return errUsage
		}
		if n, err = strconv.Atoi(v); err != nil || n < 0 {
			return link.NewTextError("link-cli.log_lines_invalid", nil, i18n.Args{"value": strconv.Quote(v)})
		}
		i = next
	}
	path := e.logPath()
	if _, err := os.Stat(path); err != nil {
		if e.goos == "linux" && fileExists(e.unitPath()) {
			args := []string{"--user", "-u", unitName, "-n", strconv.Itoa(n), "--no-pager"}
			if follow {
				args = append(args, "-f")
			}
			return e.passthrough("journalctl", args...)
		}
		fmt.Fprintln(e.stdout, terminalText("link-cli.log_missing", i18n.Args{"path": path}))
		return nil
	}
	ctx, stop := e.interruptible()
	defer stop()
	return tailFile(ctx, e.stdout, path, n, follow, time.Second/2)
}

// tailFile writes the last n lines of path and, when follow is set, what is appended until ctx ends; a file moved
// aside and started over (rotating) is followed from its start.
func tailFile(ctx context.Context, w io.Writer, path string, n int, follow bool, poll time.Duration) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, 64<<20))
	if err != nil {
		return err
	}
	if _, err := w.Write(lastLines(b, n)); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	offset := int64(len(b))
	at, err := f.Stat()
	if err != nil {
		return err
	}
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		now, err := os.Stat(path)
		if err != nil { // moved aside, the new one not there yet
			continue
		}
		if !os.SameFile(at, now) || now.Size() < offset {
			nf, err := os.Open(path)
			if err != nil {
				continue
			}
			if !os.SameFile(at, now) {
				// Moved aside: what was written to it after the last look comes before the new file's lines.
				if old, err := f.Stat(); err == nil && old.Size() > offset {
					if _, err := io.Copy(w, io.NewSectionReader(f, offset, old.Size()-offset)); err != nil {
						nf.Close()
						return err
					}
				}
			}
			f.Close()
			f, at, offset = nf, now, 0
		}
		if now.Size() > offset {
			r := bufio.NewReader(io.NewSectionReader(f, offset, now.Size()-offset))
			copied, err := io.Copy(w, r)
			offset += copied
			if err != nil {
				return err
			}
		}
	}
}

// lastLines is the end of b: its last n lines.
func lastLines(b []byte, n int) []byte {
	if n <= 0 {
		return nil
	}
	end := len(b)
	if end > 0 && b[end-1] == '\n' {
		end--
	}
	for i := end; i > 0; {
		j := bytes.LastIndexByte(b[:i], '\n')
		if j < 0 {
			break
		}
		if n--; n == 0 {
			return b[j+1:]
		}
		i = j
	}
	return b
}
