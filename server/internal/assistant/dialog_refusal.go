package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/screen"
)

// dialogRefusal is err, a send to task agent c, as the card that sent it needs it when the node refused it because a
// dialog has the agent's terminal (dialog_open): with the agent (machine_id, session_id), so the card can open its
// terminal, and the dialog's first line (dialog) when the node did not say it. A Codex node says none (codexReady),
// and the hub reads it off the screen: Codex draws its pickers without the rules DialogHead goes by (#7343). Any other
// error is err as it was.
func (s *Service) dialogRefusal(ctx context.Context, c proto.Session, err error) error {
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != screen.DialogOpen {
		return err
	}
	data := map[string]any{}
	if len(re.Data) > 0 && json.Unmarshal(re.Data, &data) != nil {
		return err
	}
	data["machine_id"], data["session_id"] = c.MachineID, c.SessionID
	if head, _ := data["dialog"].(string); head == "" {
		if head = s.dialogHead(ctx, c); head != "" {
			data["dialog"] = head
		}
	}
	raw, jerr := json.Marshal(data)
	if jerr != nil {
		return err
	}
	return &rpc.Error{Code: re.Code, Message: re.Message, Data: raw}
}

// dialogHead reads the first line of the dialog on agent c's screen, "" when it cannot.
func (s *Service) dialogHead(ctx context.Context, c proto.Session) string {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	var scr struct {
		Text string `json:"text"`
	}
	if err := s.call(rctx, c.MachineID, "session.screen", map[string]any{"session_id": c.SessionID, "lines": 60}, &scr); err != nil {
		return ""
	}
	if head := screen.CodexDialogHead(scr.Text); head != "" {
		return clip(head, 120)
	}
	return clip(screen.DialogHead(screen.Plain(scr.Text)), 120)
}
