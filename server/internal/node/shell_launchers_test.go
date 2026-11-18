package node

import (
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// shellLaunchers sets the login shell's launcher list the node has from a minute ago.
func shellLaunchers(t *testing.T, names ...string) {
	t.Helper()
	var list []proto.Launcher
	for _, name := range names {
		list = append(list, proto.Launcher{Name: name})
	}
	if list == nil {
		list = []proto.Launcher{}
	}
	launcherCache.Lock()
	oldAt, oldList := launcherCache.at, launcherCache.list
	launcherCache.at, launcherCache.list = time.Now(), list
	launcherCache.Unlock()
	t.Cleanup(func() {
		launcherCache.Lock()
		launcherCache.at, launcherCache.list = oldAt, oldList
		launcherCache.Unlock()
	})
}
