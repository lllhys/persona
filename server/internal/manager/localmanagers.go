package manager

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
)

// managersRoot is where the managers of this machine are by default (~/.persona/managers).
func managersRoot() string { return filepath.Dir(assistant.DefaultManagerDir("x")) }

// localManagers are the directories under managersRoot with a manager.json, but the built-in manager's (the
// assistant the hub runs, managerconf.DefaultDir: its manager.json holds only its configuration).
func localManagers() []string {
	entries, _ := os.ReadDir(managersRoot())
	var out []string
	for _, ent := range entries {
		d := filepath.Join(managersRoot(), ent.Name())
		if ent.Name() == assistantwire.AssistantID {
			continue
		}
		if ent.IsDir() && fileExists(filepath.Join(d, assistant.ManagerFile)) {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// onlyManager is the one manager of this machine, for a command without --dir.
func onlyManager(l i18n.Lang) (string, error) {
	list := localManagers()
	switch len(list) {
	case 1:
		return list[0], nil
	case 0:
		return "", errors.New(i18n.T(l.Or(i18n.En), "manager-cli.only_manager_none", i18n.Args{"directory": managersRoot()}))
	}
	return "", errors.New(i18n.T(l.Or(i18n.En), "manager-cli.only_manager_many", i18n.Args{"count": len(list), "directories": strings.Join(list, i18n.T(l.Or(i18n.En), "manager-cli.list_separator"))}))
}
