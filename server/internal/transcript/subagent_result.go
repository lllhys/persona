package transcript

import (
	"encoding/json"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

type agentCall struct {
	Title  string
	Status string
}

func agentTerminal(status string) bool {
	switch status {
	case "completed", "failed", "killed", "stopped", "ended":
		return true
	}
	return false
}

func (m *Meta) rememberAgentCall(call, title string) {
	if call == "" {
		return
	}
	if m.agentCalls == nil {
		m.agentCalls = map[string]agentCall{}
	}
	m.agentCalls[call] = agentCall{Title: strings.Clone(oneLine(title, noteTitleLen)), Status: "running"}
}

func (m *Meta) agentResult(toolUseID, text string, isError bool, line []byte) *proto.TaskNote {
	call, ok := m.agentCalls[toolUseID]
	if !ok {
		return nil
	}
	var e struct {
		ToolUseResult struct {
			AgentID string `json:"agentId"`
			IsAsync bool   `json:"isAsync"`
			Status  string `json:"status"`
		} `json:"toolUseResult"`
	}
	_ = unmarshalLine(line, &e)
	agent, async := e.ToolUseResult.AgentID, e.ToolUseResult.IsAsync || e.ToolUseResult.Status == "async_launched"
	if agent == "" {
		if match := bgIDRe.FindStringSubmatch(text); match != nil {
			agent = match[1]
		}
	}
	if agent != "" {
		if m.taskCalls == nil {
			m.taskCalls = map[string]string{}
		}
		m.taskCalls[strings.Clone(agent)] = toolUseID
	}
	status := "completed"
	if isError {
		status = "failed"
	} else if async || strings.Contains(text, "Async agent launched") {
		status = "running"
	}
	call.Status = status
	m.agentCalls[toolUseID] = call
	return &proto.TaskNote{Kind: "agent", TaskID: strings.Clone(agent), ToolUseID: toolUseID, Title: call.Title, Status: status}
}

func (m *Meta) agentPeer(mk func(int, string, string) proto.Item, origin, content json.RawMessage) (proto.Item, bool) {
	if len(origin) == 0 || len(m.agentCalls) == 0 {
		return proto.Item{}, false
	}
	var o struct {
		Kind               string `json:"kind"`
		From               string `json:"from"`
		SenderTaskID       string `json:"senderTaskId"`
		Name               string `json:"name"`
		Body               string `json:"body"`
		Handback           bool   `json:"handback"`
		HandbackReportText string `json:"handbackReportText"`
	}
	if json.Unmarshal(origin, &o) != nil || o.Kind != "peer" {
		return proto.Item{}, false
	}
	agent := o.SenderTaskID
	if agent == "" {
		agent = o.From
	}
	id := m.taskCalls[agent]
	call, ok := m.agentCalls[id]
	if !ok {
		return proto.Item{}, false
	}
	text := o.HandbackReportText
	if !o.Handback {
		peer, found := peerMessage(mk, origin, content)
		if !found {
			return proto.Item{}, false
		}
		text = peer.Text
	}
	it := mk(0, "user", proto.KindToolResult)
	it.Text = clip(strings.TrimSpace(text))
	it.ToolUseID = id
	status := call.Status
	if o.Handback {
		status = "completed"
		call.Status = status
		m.agentCalls[id] = call
		m.removeBackground(agent)
		m.handedBack(agent, "")
	}
	it.IsError = status == "failed"
	it.Task = &proto.TaskNote{Kind: "agent", TaskID: strings.Clone(agent), ToolUseID: id, Title: call.Title, Status: status,
		Detail: clipTo(it.Text, pageOutputLen)}
	return it, true
}

func (m *Meta) agentNotification(it *proto.Item) {
	n := it.Task
	if n == nil || n.Kind != "agent" || n.ToolUseID == "" {
		return
	}
	call, ok := m.agentCalls[n.ToolUseID]
	if !ok {
		call.Title = n.Title
	}
	call.Status = n.Status
	if m.agentCalls == nil {
		m.agentCalls = map[string]agentCall{}
	}
	m.agentCalls[n.ToolUseID] = call
	it.Role, it.Kind, it.ToolUseID = "user", proto.KindToolResult, n.ToolUseID
	it.IsError = n.Status == "failed"
	it.Text = n.Detail
	n.Detail = clipTo(n.Detail, pageOutputLen)
}
