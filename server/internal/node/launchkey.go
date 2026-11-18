package node

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/nwverse/persona/server/internal/nodeclient"
)

// The launch key (#5588): a random value the node keeps in a 0600 file beside its socket (nodeclient.LaunchKeyPath).
// A managed launcher's settings hold its provider key in plain text, and every agent's hooks and MCP servers must be
// able to reach node.sock, so being able to connect cannot be what lets a caller have them. `personad launch` reads
// the key wherever it runs: started by the node, typed into a herdr pane, run again from herdr's resume_argv after a
// herdr restart, or typed by the user in a terminal. A process that cannot read the data directory (an agent in a
// sandbox that hides it) cannot. The key stays across node restarts: a launch that read it just before one still
// gets its answer.

// launchKeyLen is the key's length: 32 random bytes in hex.
const launchKeyLen = 64

var errMalformedLaunchKey = errors.New("malformed launch key")

// readLaunchKey is the launch key in path; an error for a missing file or one that does not hold a key.
func readLaunchKey(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	k := strings.TrimSpace(string(b))
	if _, err := hex.DecodeString(k); err != nil || len(k) != launchKeyLen {
		return "", fmt.Errorf("%w in %s", errMalformedLaunchKey, path)
	}
	return k, nil
}

// ensureLaunchKey is the launch key in path, written (0600) when the file is missing or does not hold one. Any other
// error is returned as is: a key the node failed to read is not replaced under a launch that has just read it.
func ensureLaunchKey(path string) (string, error) {
	k, err := readLaunchKey(path)
	if err == nil {
		return k, nil
	}
	if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, errMalformedLaunchKey) {
		return "", err
	}
	b := make([]byte, launchKeyLen/2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	k = hex.EncodeToString(b)
	if err := writeFileAtomic(path, []byte(k+"\n")); err != nil {
		return "", err
	}
	return k, nil
}

// launcherEnvFor answers launcher.env: the launcher's settings for a request that carries the machine's launch key.
// Any other request gets the same error whatever it names, with none of the settings in it.
func (n *Node) launcherEnvFor(req sockRequest) map[string]any {
	if refused := n.launchKeyRefusal(req); refused != nil {
		return refused
	}
	return n.launcherEnv(req.Name)
}

// launchKeyRefusal is the answer to a request for a provider's secrets (launcher.env, launcher.pi) that does not
// carry the machine's launch key; nil for one that does.
func (n *Node) launchKeyRefusal(req sockRequest) map[string]any {
	path := nodeclient.LaunchKeyPath()
	key, err := ensureLaunchKey(path)
	if err != nil {
		n.logf("%s %q: launch key: %v", req.Kind, req.Name, err)
		return map[string]any{"error": "the node cannot read its launch key " + path + ": " + err.Error()}
	}
	if subtle.ConstantTimeCompare([]byte(req.LaunchKey), []byte(key)) != 1 {
		n.logf("%s %q refused: the request did not carry this machine's launch key", req.Kind, req.Name)
		return map[string]any{"error": "refused: launcher settings go only to a personad launch that can read " + path +
			" (a sandboxed process cannot, and a personad older than the node does not send it)"}
	}
	return nil
}

// launcherEnvRequest is what `personad launch` asks the node for: name's settings, with the launch key when this
// process can read it. When it cannot, the request goes without one and the node's answer says why.
func launcherEnvRequest(name string) sockRequest {
	key, _ := readLaunchKey(nodeclient.LaunchKeyPath())
	return sockRequest{Kind: "launcher.env", Name: name, LaunchKey: key}
}
