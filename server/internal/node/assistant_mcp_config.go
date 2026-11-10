package node

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
)

// Kept replaceable so launch-boundary tests exercise the Windows file lifecycle on any host.
var assistantCLIMCPFlags = func(flags []string) ([]string, func(), error) {
	return assistantCLIMCPFlagsForOS(runtime.GOOS, flags)
}

// assistantCLIMCPFlagsForOS keeps JSON out of Windows shell argv: Git Bash/MSYS can consume its doubled
// backslashes while parsing the incoming native command line (even builtin printf sees the loss).
// This happens before Bash launches a child; reproduced with native Claude, not an npm shim (#3402).
// Only the CLI calls this helper; the SDK carries its config over stdin. The caller removes the file
// on failed start or process exit. Use a slash path for argv, without changing the JSON inside the file.
func assistantCLIMCPFlagsForOS(goos string, flags []string) ([]string, func(), error) {
	cleanup := func() {}
	if goos != "windows" {
		return flags, cleanup, nil
	}
	i := slices.Index(flags, "--mcp-config")
	if i < 0 || i+1 >= len(flags) {
		return nil, cleanup, errors.New("missing assistant MCP config")
	}
	dir, err := privateCacheDir("assistant-mcp")
	if err != nil {
		return nil, cleanup, err
	}
	// Resolve before launch: cmd.Dir is the conversation directory, not the node's cwd.
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, cleanup, err
	}
	f, err := os.CreateTemp(dir, "mcp-*.json") // 0600, unique even for overlapping resumes of one session
	if err != nil {
		return nil, cleanup, err
	}
	cleanup = func() { _ = os.Remove(f.Name()) }
	_, err = f.WriteString(flags[i+1]) // Preserve the JSON bytes, including paths and extension environment values.
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		cleanup()
		return nil, cleanup, err
	}
	args := slices.Clone(flags)
	args[i+1] = filepath.ToSlash(f.Name())
	return args, cleanup, nil
}
