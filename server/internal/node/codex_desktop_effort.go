package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/nwverse/persona/server/internal/proto"
)

// The effort a chat of the Codex App runs its next turn with, changed from the phone (#5575) as with the App's own
// picker: the window that owns the chat is asked over the App's bus (codex_desktop.go) to update the thread's settings
// for its next turn (thread-follower-update-thread-settings), so a running turn goes on as it was, and the App's picker
// shows the new effort too. The change counts once the window's read-model-settings shows it.
//
// The levels offered are what the chat's model takes on the App's own codex (model/list), as for a thread on an
// app-server (codex_effort.go); the phone's choice is shown until a turn starts after it, whose record then tells.

// codexAppPick is the effort the phone set for a chat of the Codex App's next turn, and when.
type codexAppPick struct {
	effort string
	at     time.Time
}

// codexAppModelsTTL: the App's model list is asked again after this long, in the background (the rows meanwhile offer
// the list kept).
const codexAppModelsTTL = 10 * time.Minute

var codexAppModelLists = struct {
	sync.Mutex
	at     map[string]time.Time
	got    map[string]map[string]codexModelEfforts // home → model → its efforts
	failed map[string]string                       // home → the failure last logged, not logged again on each retry (#9845)
	flight singleflight.Group
}{at: map[string]time.Time{}, got: map[string]map[string]codexModelEfforts{}, failed: map[string]string{}}

// readCodexAppModels asks the codex the App bundles (codexAppCLI) for the models it lists for home, hidden ones too (a
// chat may run one). A variable: tests answer their own.
var readCodexAppModels = func(ctx context.Context, home string) (map[string]codexModelEfforts, error) {
	cli, err := codexAppCLI()
	if err != nil {
		return nil, err
	}
	s, err := startCodexStdio(ctx, cli, home, os.TempDir())
	if err != nil {
		return nil, err
	}
	defer s.close()
	return readCodexModels(ctx, s.call)
}

// codexAppEfforts are the levels a chat of the Codex App running model may be set to: the model's on the App's codex,
// else (a model it does not list, a list not read yet or that could not be read) Codex's whole list, as on an
// app-server (codex_effort.go). Empty: the model takes no effort. The list is read in the background, never here: the
// rows are listed again once it comes.
func (n *Node) codexAppEfforts(home, model string) []string {
	codexAppModelLists.Lock()
	at, ok := codexAppModelLists.at[home]
	models := codexAppModelLists.got[home]
	codexAppModelLists.Unlock()
	if !ok || time.Since(at) >= codexAppModelsTTL {
		codexAppModelLists.flight.DoChan(home, func() (any, error) {
			ctx, cancel := context.WithTimeout(context.Background(), codexAppSeedWait)
			defer cancel()
			got, err := readCodexAppModels(ctx, home)
			codexAppModelLists.Lock()
			repeated := err != nil && codexAppModelLists.failed[home] == err.Error()
			if err != nil {
				got = models // the list kept, if any; asked again after the TTL
				codexAppModelLists.failed[home] = err.Error()
			} else {
				delete(codexAppModelLists.failed, home)
			}
			codexAppModelLists.Unlock()
			if err != nil && !repeated {
				n.logf("codex app %s: model/list: %v; its chats offer every effort level", home, err)
			}
			codexAppModelLists.Lock()
			codexAppModelLists.at[home], codexAppModelLists.got[home] = time.Now(), got
			codexAppModelLists.Unlock()
			if err == nil && !ok {
				n.notifyCodex() // the rows can offer their models' own levels
			}
			return nil, nil
		})
	}
	if e, found := models[model]; model != "" && found {
		return e.levels
	}
	return codexEffortLevels
}

// codexDesktopEffortRow lets the phone change the effort of a chat of the Codex App (setCodexDesktopEffort) as on a
// thread of an app-server (codexEffortRow): the levels its model takes, which the apps take for effort alone (no
// capabilities.modes: that would offer the permission mode too), and the effort its next turn runs with — the phone's
// choice until a turn starts after it, else what the rollout's last turn ran with (m: its meta), else what the App's
// database keeps for the chat (a chat whose history is there has no turn_context to read, #6501). The row's model
// comes from the database too when nothing else said it.
func (n *Node) codexDesktopEffortRow(s *proto.Session, src codexSource, m quickMeta) {
	app := codexAppThreads(src.home)[src.thread]
	if s.Model == "" {
		s.Model = app.Model
	}
	levels := n.codexAppEfforts(src.home, firstNonEmpty(s.Model, m.model))
	if len(levels) == 0 {
		return
	}
	s.Efforts, s.Effort = levels, firstNonEmpty(m.effort, app.Effort)
	if p := n.codexAppPickOf(s.SessionID); p != nil && p.at.After(m.effortAt) {
		s.Effort = p.effort
	}
}

func (n *Node) codexAppPickOf(id string) *codexAppPick {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.modes[id].App
}

func (n *Node) setCodexAppPick(id, effort string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.modes == nil {
		n.modes = map[string]sessionModes{}
	}
	m := n.modes[id]
	m.App = &codexAppPick{effort: effort, at: time.Now()}
	n.modes[id] = m
}

// codexDesktopModes is session.modes for a chat of the Codex App: its effort only. The model and the permission mode
// are the App's to change.
func (n *Node) codexDesktopModes(ctx context.Context, id string, src codexSource, model, mode, effort string) error {
	switch {
	case model != "":
		return fmt.Errorf("Codex App 的对话不支持在手机上切换模型")
	case mode != "":
		return fmt.Errorf("Codex 的会话在手机上只能改思考强度，权限模式要在电脑上改")
	case effort == "":
		return nil
	}
	return n.setCodexDesktopEffort(ctx, id, src, effort)
}

// setCodexDesktopEffort sets the effort of the next turn of a chat of the Codex App, in the App window that owns it
// (the App is asked to open it first, as for a message). It is set once the window reads it back; the running turn,
// if any, goes on with its own.
func (n *Node) setCodexDesktopEffort(ctx context.Context, id string, src codexSource, effort string) error {
	levels := n.codexAppEfforts(src.home, n.metaOf(id).model)
	if len(levels) == 0 {
		return fmt.Errorf("这个会话的模型不支持设置思考强度")
	}
	if !slices.Contains(levels, effort) {
		return fmt.Errorf("这个会话的模型不支持思考强度 %s（可选：%s）", effort, strings.Join(levels, " / "))
	}
	ctx, cancel := context.WithTimeout(ctx, codexDesktopTimeout)
	defer cancel()
	c, err := dialCodexDesktop(ctx, src.home)
	if err != nil {
		return refusal("codex_read_only", "连不上电脑上的 Codex App："+err.Error(), nil)
	}
	defer c.close()
	owner, err := c.loadedOwner(ctx, src.thread)
	if err != nil {
		return err
	}
	f, err := c.request("thread-follower-update-thread-settings", map[string]any{"conversationId": src.thread, "threadSettings": map[string]any{"effort": effort}}, owner.HandledByClient)
	if err != nil {
		if f.Type == "response" { // the window answered: it did not take it
			return refusal("codex_refused", "思考强度没有改成：Codex App 回复「"+f.Error+"」", nil)
		}
		return fmt.Errorf("思考强度可能没有改成：和电脑上的 Codex App 断开了（%v），会话里显示的是它现在的设置", err)
	}
	var updated struct {
		Applied bool `json:"applied"`
	}
	if json.Unmarshal(f.Result, &updated) != nil || !updated.Applied {
		return refusal("codex_refused", "Codex App 没有接受这个思考强度", nil)
	}
	f, err = c.request("thread-follower-read-model-settings", map[string]any{"conversationId": src.thread}, owner.HandledByClient)
	var read struct {
		Settings *struct {
			ReasoningEffort *string `json:"reasoningEffort"`
		} `json:"settings"`
	}
	if err != nil || json.Unmarshal(f.Result, &read) != nil || read.Settings == nil || read.Settings.ReasoningEffort == nil || *read.Settings.ReasoningEffort != effort {
		return fmt.Errorf("思考强度可能没有改成：Codex App 读回的设置不是 %s", effort)
	}
	n.setCodexAppPick(id, effort)
	n.notifyCodex()
	return nil
}
