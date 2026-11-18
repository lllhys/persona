package node

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// "Back to the configured model" after a refusal fallback (#2528, the follow-up of #2289): Claude Code moved a session
// to another model after an upstream refusal and keeps it there for later turns (proto.Session.ModelFallback). For a
// Persona conversation (headless claude -p) the node can close the idle process, so the next message starts it again
// with the model the hub configures for the conversation (assistantSend → spawnClaude --model). That is all it does:
// no message is sent, nothing is replayed, no key, account, quota or route changes, and the transcript (history, todo
// links, the permission mode, the refusal record itself) stays as it is. What it says is "the next start uses the
// configured model": whether that model then runs is for a newer reply in the transcript to show, and the same
// request may be refused again. Terminal (TUI) sessions are refused with the manual step; nothing is typed there.

// Refusal codes of session.restore_model (409 at the hub, the node's words in error).
const (
	restoreBusy        = "restore_busy"        // the process is working, waiting on a hook or control, or held by a send
	restoreUnsupported = "restore_unsupported" // not a headless Claude Code conversation of this node
	restoreNothing     = "restore_nothing"     // no active refusal fallback in the session
	restoreUnknown     = "restore_unknown"     // the transcript or the configured model can't be read now
	restoreGone        = "restore_gone"        // no record of the session
)

// restoreMark is a restore the user asked for, shown as proto.Session.ModelRestore until newer model evidence: the
// fallback record it was asked against is gone or replaced, or a real reply (not an API error line) was written after
// it. Kept in memory only: after a node restart the session shows its fallback again with the button, never a claim.
type restoreMark struct {
	to      string    // the configured model the node read when it closed the process
	at      time.Time // when it was asked
	raw     string    // the fallback record (ModelRefusal.Raw) it was asked against
	size    int64     // the transcript's size then: a reply after it ends the mark
	scanned int64     // how far a reply was looked for
}

type restoreMarks struct {
	mu sync.Mutex
	m  map[string]*restoreMark
}

func (r *restoreMarks) set(id string, mark restoreMark) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = map[string]*restoreMark{}
	}
	mark.scanned = mark.size
	r.m[id] = &mark
}

// restoreReplyScan bounds how much written after a restore is looked through for a reply; more than that is taken as
// model evidence (the mark ends and the card offers the button again: never a claim either way).
const restoreReplyScan = 16 << 20

// setModelRestore puts a pending restore on s while the transcript at path (read as m) has no newer model evidence.
func (n *Node) setModelRestore(s *proto.Session, path string, m quickMeta) {
	n.restores.mu.Lock()
	defer n.restores.mu.Unlock()
	mark := n.restores.m[s.SessionID]
	if mark == nil {
		return
	}
	if m.modelFallback == nil || m.modelFallback.Raw != mark.raw || replyAfter(path, mark) {
		delete(n.restores.m, s.SessionID)
		return
	}
	s.ModelRestore = &proto.ModelRestore{State: proto.ModelRestoreNextStart, To: mark.to, At: mark.at.UTC()}
}

// replyAfter: a real reply was written after the mark (or the file was rewritten, or too much to look through).
// Advances mark.scanned over whole lines it has looked at.
func replyAfter(path string, mark *restoreMark) bool {
	st, err := os.Stat(path)
	if err != nil || st.Size() < mark.size {
		return true
	}
	if st.Size()-mark.size > restoreReplyScan {
		return true
	}
	if st.Size() == mark.scanned {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	r := bufio.NewReaderSize(io.NewSectionReader(f, mark.scanned, st.Size()-mark.scanned), 64<<10)
	off := mark.scanned
	for {
		line, err := r.ReadBytes('\n')
		if err != nil { // the last line still being written: looked at again on the next read
			return false
		}
		off += int64(len(line))
		mark.scanned = off
		if isModelReply(line) {
			return true
		}
	}
}

// isModelReply: line is the main conversation's assistant line from a named model, not an API error line Claude Code
// writes itself (its model is "<synthetic>", or it is marked isApiErrorMessage) nor a subagent's.
func isModelReply(line []byte) bool {
	if !bytes.Contains(line, []byte(`"assistant"`)) {
		return false
	}
	var e struct {
		Type              string `json:"type"`
		IsSidechain       bool   `json:"isSidechain"`
		IsAPIErrorMessage bool   `json:"isApiErrorMessage"`
		Message           *struct {
			Model string `json:"model"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "assistant" || e.IsSidechain || e.IsAPIErrorMessage || e.Message == nil {
		return false
	}
	return e.Message.Model != "" && e.Message.Model != "<synthetic>"
}

// hookFlights counts each session's hook calls in progress (handleHook): a permission, question or plan card, or any
// other hook Claude Code waits on, means its process is not idle, whatever its turn state says.
type hookFlights struct {
	mu sync.Mutex
	n  map[string]int
}

func (h *hookFlights) enter(id string) func() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n == nil {
		h.n = map[string]int{}
	}
	h.n[id]++
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.n[id]--; h.n[id] <= 0 {
			delete(h.n, id)
		}
	}
}

func (h *hookFlights) active(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n[id] > 0
}

// stopIdleAssistant closes id's process only if it is still p and idle (no turn, background task, compaction,
// control request waiting for its answer, or hook call in progress), checked and taken out of n.procs under n.mu
// then p.mu (the reaper's order): a process a send started or made busy meanwhile stays.
func (n *Node) stopIdleAssistant(id string, p *assistantProc) bool {
	n.mu.Lock()
	if n.procs[id] != p {
		n.mu.Unlock()
		return false
	}
	p.mu.Lock()
	idle := !p.busy && len(p.tasks) == 0 && !p.compacting && !p.manual && len(p.controls) == 0
	p.mu.Unlock()
	if !idle || n.hookFlights.active(id) {
		n.mu.Unlock()
		return false
	}
	delete(n.procs, id)
	n.mu.Unlock()
	p.stop()
	return true
}

// restoreModel is session.restore_model → {restore: "next_start", restore_to: the configured model read now, closed:
// whether an idle process was closed (false: none was running)}.
func (n *Node) restoreModel(ctx context.Context, id string) (map[string]any, error) {
	if id == "" || !validID(id) {
		return nil, refusal(restoreGone, "找不到这个会话的记录", nil)
	}
	if n.runtimeAssistant(id) || n.externalCodex(id) || n.codexPath(id) != "" || n.traexPath(id) != "" {
		return nil, refusal(restoreUnsupported, "只有 Persona 里的 Claude Code 对话能这样回到配置的模型", nil)
	}
	path := n.transcriptPath(id)
	if path == "" {
		return nil, refusal(restoreGone, "找不到这个会话的记录", nil)
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, refusal(restoreGone, "找不到这个会话的记录", nil)
	}
	m := n.meta(path, st)
	// The runtime's own fallback, or the node's bounded-recovery marker projected onto the same shape (#7824). Either
	// one means the conversation answers on a model that is not its configured one and a "back to configured" is valid.
	if err := fallbackToRestore(m); err != nil {
		if r := n.modelRecovery(id); modelRecoverySwitched(r) {
			// A node-driven switch has no transcript fallback: synthesize what restore needs from the marker.
			m.modelFallback = &proto.ModelRefusal{Subtype: "model_refusal_fallback", OriginalModel: r.From, FallbackModel: r.To, Raw: ""}
		} else {
			return nil, err
		}
	}
	if !n.isAssistantSession(id) {
		// A typed /model may also become the default for new sessions (model_switch.go): not done behind the user's back.
		hint := "/model"
		if m.modelFallback.OriginalModel != "" {
			hint += " " + m.modelFallback.OriginalModel
		}
		return nil, refusal(restoreUnsupported, "终端会话不支持一键恢复：可以在 Claude Code 里自己输入 "+hint+"。注意 Claude Code 可能把它同时存成新会话的默认模型",
			map[string]any{"hint": hint})
	}
	// In turn with assistantSend, assistantCompact and compactIdle (assistantLife). Not waited for: whoever holds it is
	// working, and a wait could outlive the request (a cancelled one would still close the process after).
	unlock, ok := n.assistantLife.tryLock(id)
	if !ok {
		return nil, errRestoreBusy()
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Read again under the lock: a reply since (the fallback over) or a new refusal changes what there is to restore.
	if st, err = os.Stat(path); err != nil {
		return nil, refusal(restoreGone, "找不到这个会话的记录", nil)
	}
	n.mu.Lock()
	if prev, ok := n.metas[path]; ok {
		prev.readAt = time.Time{} // past metaMinInterval: read what was written since
		n.metas[path] = prev
	}
	n.mu.Unlock()
	m = n.meta(path, st)
	if err := fallbackToRestore(m); err != nil {
		if r := n.modelRecovery(id); modelRecoverySwitched(r) {
			m.modelFallback = &proto.ModelRefusal{Subtype: "model_refusal_fallback", OriginalModel: r.From, FallbackModel: r.To, Raw: ""}
		} else {
			return nil, err
		}
	}
	// The configured model as the hub says it now: what the next start asks for unless the setting changes again.
	// A valid launcher with an empty model means its default; an all-zero answer means the hub could not say.
	launcher, exec := n.assistantModel(ctx, id)
	if launcher == "" {
		return nil, refusal(restoreUnknown, "Persona 读不到这个对话配置的模型，说不准下次启动会用哪个，所以没有关闭任何东西", nil)
	}
	if err := ctx.Err(); err != nil { // given up while the hub was asked: closes nothing
		return nil, err
	}
	n.mu.Lock()
	p := n.procs[id]
	n.mu.Unlock()
	if p != nil && !n.stopIdleAssistant(id, p) {
		return nil, errRestoreBusy()
	}
	n.restores.set(id, restoreMark{to: exec.Model, at: time.Now(), raw: m.modelFallback.Raw, size: st.Size()})
	// A node-driven switch is released here too, so the conversation actually leaves its fallback and the marker and
	// 30-minute hold do not pin it to the cheaper model (#7824).
	if r := n.modelRecovery(id); modelRecoverySwitched(r) {
		n.releaseModelRecovery(id, r.From)
	}
	n.logger.Printf("assistant %s: back to the configured model asked: closed %v, next start uses %s", short(id), p != nil, exec.Model)
	n.notifySessions()
	return map[string]any{"restore": proto.ModelRestoreNextStart, "restore_to": exec.Model, "closed": p != nil}, nil
}

func errRestoreBusy() error {
	return refusal(restoreBusy, "这个对话正在工作（这一轮、后台任务、压缩或等你回答的卡片还没结束），没有关闭任何东西；结束后再试", nil)
}

// fallbackToRestore: m says the session runs on a refusal fallback. A transcript still being read in the background
// is not taken for "no fallback".
func fallbackToRestore(m quickMeta) error {
	if m.modelFallback == nil && m.bgWait {
		return refusal(restoreUnknown, "这个会话的记录还没读完，Persona 说不准它现在用哪个模型，所以没有关闭任何东西；稍后再试", nil)
	}
	if m.modelFallback == nil {
		return refusal(restoreNothing, "这个会话没有在用拒绝后自动换的模型，不用恢复", nil)
	}
	return nil
}
