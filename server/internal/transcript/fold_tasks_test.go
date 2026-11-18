package transcript

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestFoldTaskSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name, tool, input, output, replace string
		failed, noResult                   bool
		count                              int
	}{
		{name: "todo", tool: "TodoWrite", input: `{"todos":[{"content":"检查表单","status":"in_progress","activeForm":"检查中"}]}`, output: "OK", replace: "todo", count: 1},
		{name: "failed", tool: "TodoWrite", input: `{"todos":[]}`, output: "failed", failed: true},
		{name: "rejected", tool: "TodoWrite", input: `{"todos":[]}`, output: `{"success":false}`},
		{name: "error", tool: "TodoWrite", input: `{"todos":[]}`, output: `{"error":"failed"}`},
		{name: "unconfirmed", tool: "TodoWrite", input: `{"todos":[]}`, noResult: true},
		{name: "malformed", tool: "TodoWrite", input: `{"todos":[{"content":""}]}`, output: "OK"},
		{name: "clear", tool: "TodoWrite", input: `{"todos":[]}`, output: "OK", replace: "todo", count: 1},
		{name: "create", tool: "TaskCreate", input: `{"subject":"检查表单","description":"覆盖空输入"}`, output: "Task #7 created successfully", count: 1},
		{name: "update", tool: "TaskUpdate", input: `{"taskId":"7","status":"completed"}`, output: "OK", count: 1},
		{name: "delete", tool: "TaskUpdate", input: `{"taskId":"7","status":"deleted"}`, output: "OK", count: 1},
		{name: "list", tool: "TaskList", input: `{}`, output: "#7 [completed] 检查表单", replace: "task", count: 1},
		{name: "list_json", tool: "TaskList", input: `{}`, output: `{"tasks":[{"id":"7","subject":"检查表单"}]}`, replace: "task", count: 1},
		{name: "list_clear", tool: "TaskList", input: `{}`, output: "[]", replace: "task", count: 1},
		{name: "list_invalid", tool: "TaskList", input: `{}`, output: "malformed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			use := proto.Item{ID: "use#0", Seq: 2, Kind: proto.KindToolUse, ToolUseID: "call", ToolName: tc.tool, ToolInput: json.RawMessage(tc.input)}
			items := []proto.Item{{ID: "thought#0", Seq: 1, Kind: proto.KindThinking}, use}
			if !tc.noResult {
				items = append(items, proto.Item{ID: "result#0", Seq: 3, Kind: proto.KindToolResult, ToolUseID: "call", Text: tc.output, IsError: tc.failed})
			}
			var snapshots []proto.FoldTaskSnapshot
			for _, item := range FoldWork(items) {
				if item.Fold != nil {
					snapshots = append(snapshots, item.Fold.Tasks...)
				}
			}
			if len(snapshots) != tc.count {
				t.Fatalf("got %+v, want %d snapshots", snapshots, tc.count)
			}
			if tc.count == 0 {
				return
			}
			snapshot := snapshots[0]
			if snapshot.SourceItemID != use.ID || snapshot.SourceToolUseID != use.ToolUseID || snapshot.SourceSeq != use.Seq || snapshot.Replace != tc.replace {
				t.Fatalf("source or replacement lost: %+v", snapshot)
			}
			if tc.name == "todo" {
				entry := snapshot.Entries[0]
				if entry.ID != "todo:0" || entry.Subject == nil || *entry.Subject != "检查表单" || entry.Status == nil || *entry.Status != "in_progress" {
					t.Fatalf("wrong todo: %+v", entry)
				}
			}
		})
	}
}

func TestFoldTaskSnapshotsPreserveCrossRunSources(t *testing.T) {
	use := proto.Item{ID: "use#0", Seq: 1, Kind: proto.KindToolUse, ToolUseID: "call", ToolName: "TaskUpdate", ToolInput: json.RawMessage(`{"taskId":"7","status":"completed"}`)}
	items := []proto.Item{use, {ID: "reply#0", Seq: 2, Kind: proto.KindText},
		{ID: "thought#0", Seq: 3, Kind: proto.KindThinking},
		{ID: "thought#1", Seq: 4, Kind: proto.KindThinking},
		{ID: "result#0", Seq: 5, Kind: proto.KindToolResult, ToolUseID: "call", Text: "OK"}}
	folded := FoldWork(items)
	snapshot := folded[2].Fold.Tasks[0]
	if snapshot.SourceItemID != use.ID || snapshot.Entries[0].Subject != nil || *snapshot.Entries[0].Status != "completed" {
		t.Fatalf("partial update/source lost: %+v", snapshot)
	}
}

func TestFoldTaskSnapshotsAcceptStructuralClipMarker(t *testing.T) {
	todos := make([]any, 2000)
	for i := range todos {
		todos[i] = map[string]any{"content": "Do the work", "activeForm": "Doing the work", "status": "pending"}
	}
	raw, _ := json.Marshal(map[string]any{"todos": todos})
	use := proto.Item{ID: "use#0", Seq: 1, Kind: proto.KindToolUse, ToolUseID: "call", ToolName: "TodoWrite", ToolInput: clipJSON(raw)}
	result := proto.Item{ID: "result#0", Seq: 2, Kind: proto.KindToolResult, ToolUseID: "call", Text: "OK"}
	snapshots := foldTaskSnapshots([]proto.Item{use, result}, map[string]proto.Item{"call": use})
	if len(snapshots) != 1 || len(snapshots[0].Entries) == 0 || len(snapshots[0].Entries) == len(todos) {
		t.Fatalf("structurally clipped to-do snapshots = %+v", snapshots)
	}
	for i, entry := range snapshots[0].Entries {
		if entry.Subject == nil || *entry.Subject != "Do the work" || entry.ID != "todo:"+strconv.Itoa(i) {
			t.Fatalf("retained to-do %d changed: %+v", i, entry)
		}
	}
}
