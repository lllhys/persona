package node

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// KERN_PROCARGS2 contains argc, the executable path and padding, followed by
// exactly argc NUL-terminated arguments. Never parse the environment after them.
func updateDarwinArgv(data []byte) ([]string, error) {
	invalid := errors.New("incomplete process arguments")
	if len(data) < 4 {
		return nil, invalid
	}
	argc := int(binary.NativeEndian.Uint32(data[:4]))
	if argc < 1 || argc > len(data)-4 {
		return nil, invalid
	}
	data = data[4:]
	end := bytes.IndexByte(data, 0)
	if end < 1 {
		return nil, invalid
	}
	data = bytes.TrimLeft(data[end:], "\x00")
	args := make([]string, 0, argc)
	for range argc {
		end = bytes.IndexByte(data, 0)
		if end < 0 {
			return nil, invalid
		}
		args = append(args, string(data[:end]))
		data = data[end+1:]
	}
	return args, nil
}

// darwinExecPath is the executable path of KERN_PROCARGS2: the file the kernel ran, kept ahead of the arguments.
// A process that renames itself rewrites only its arguments (Node.js's process.title: Paseo's supervisor is "Paseo
// Supervisor", which is also what `ps -o comm=` shows), so this still says what it runs.
func darwinExecPath(data []byte) (string, error) {
	if len(data) < 4 {
		return "", errors.New("incomplete process arguments")
	}
	data = data[4:]
	end := bytes.IndexByte(data, 0)
	if end < 1 {
		return "", errors.New("incomplete process arguments")
	}
	return string(data[:end]), nil
}
