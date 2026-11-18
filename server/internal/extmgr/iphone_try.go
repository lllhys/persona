package extmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// iphoneTryApp is the app Try It opens: one every iPhone has, harmless to open.
const iphoneTryApp = "com.apple.weather"

// iphoneTryTimeout bounds Try It as a whole: a look, then one launch.
const iphoneTryTimeout = 90 * time.Second

// iphoneTryResult is what Try It answers: the screenshot the look took (nil when it took none) and whether the app
// opened, with the reason when it didn't.
type iphoneTryResult struct {
	Screenshot *proto.Image `json:"screenshot,omitempty"`
	Opened     bool         `json:"opened"`
	App        string       `json:"app"`
	Error      string       `json:"error,omitempty"`
}

// postIPhoneTry: POST /api/v1/iphone/try {"device"}: the guided setup's Try It (#8199), from the owner's own app on
// that iPhone, without the model: a look (its screenshot), then Weather opened, through the same path the assistant's
// tools take (the iPhone allowed, its computer on, one do at a time). A real iPhone asks the owner before every input it
// can't check (unchecked, iphone-control.md "Without an element tree"): the owner's tap on Try It is the answer for
// exactly that one step, so the launch of Weather is approved at once and anything else asked is not. Only the
// user's own signed-in apps may (403 otherwise); the extension off 404; an iPhone not allowed, or its computer offline,
// 409.
func (h *Manager) postIPhoneTry(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Device string `json:"device"`
	}
	if err := decodeJSONBody(w, r, &in, 4<<10); err != nil {
		h.writeComputerBodyErr(w, r, err)
		return
	}
	if !byOwnDevice(r.Context()) || principalOf(r.Context()).Extension != "" {
		writeErr(w, http.StatusForbidden, i18n.Err("hub-iphone.err_consent_device"))
		return
	}
	if !h.iphone().On {
		writeErr(w, http.StatusNotFound, i18n.Err("hub-iphone.err_off"))
		return
	}
	in.Device = strings.TrimSpace(in.Device)
	if in.Device == "" || len(in.Device) > 200 {
		writeErr(w, http.StatusBadRequest, i18n.Err("hub-iphone.err_no_device"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), iphoneTryTimeout)
	defer cancel()
	d, m, err := h.iphoneTarget(ctx, in.Device)
	if err != nil || d.ID != in.Device {
		writeErr(w, http.StatusConflict, i18n.Err("hub-iphone.err_try_not_ready"))
		return
	}
	release, err := h.iphoneTurn(ctx, d.ID)
	if err != nil {
		writeErr(w, http.StatusConflict, i18n.Err("hub-iphone.err_try_busy"))
		return
	}
	defer release()
	set := h.iphone()
	base := proto.IPhoneCall{Device: d.ID, Session: "persona-setup", SelfApps: set.SelfApps, SelfNames: set.SelfNames}
	out := iphoneTryResult{App: iphoneTryApp}

	look := base
	look.Op, look.Args = "look", json.RawMessage(`{"screenshot":true}`)
	res, err := h.iphoneCall(ctx, m, look)
	if err != nil {
		h.logf("[%s] iphone try: look failed (%s)", m.cfg.ID, iphoneCode(err))
		out.Error = i18n.T(h.requestLang(r), "hub-iphone.try_look_failed")
		writeJSON(w, out)
		return
	}
	out.Screenshot = res.Image

	open := base
	open.Op = "do"
	open.Args, _ = json.Marshal(map[string]any{"steps": []map[string]any{{"action": "launch", "app": iphoneTryApp}}, "screenshot": false})
	res, err = h.iphoneCall(ctx, m, open)
	if err == nil && res.NeedsApproval {
		a := res.Approval
		if a == nil || res.ApprovalID == "" || a.Action != "launch" || a.LaunchApp != iphoneTryApp || len(a.Steps) > 1 {
			h.logf("[%s] iphone try: the node asked about something else; not approved", m.cfg.ID)
			out.Error = i18n.T(h.requestLang(r), "hub-iphone.try_open_failed")
			writeJSON(w, out)
			return
		}
		resume := base
		resume.Op, resume.Approved = "do", res.ApprovalID
		res, err = h.iphoneCall(ctx, m, resume)
	}
	switch {
	case err != nil:
		h.logf("[%s] iphone try: open failed (%s)", m.cfg.ID, iphoneCode(err))
		out.Error = i18n.T(h.requestLang(r), "hub-iphone.try_open_failed")
	case res.NeedsApproval:
		h.logf("[%s] iphone try: asked again after the approval; stopped", m.cfg.ID)
		out.Error = i18n.T(h.requestLang(r), "hub-iphone.try_open_failed")
	case !iphoneLaunchSent(res.Output):
		h.logf("[%s] iphone try: the launch was not sent", m.cfg.ID)
		out.Error = i18n.T(h.requestLang(r), "hub-iphone.try_open_failed")
	default:
		out.Opened = true
	}
	h.logf("[%s] iphone try: look ok, open %v", m.cfg.ID, out.Opened) // never which iPhone
	writeJSON(w, out)
}

// iphoneLaunchSent says whether a do's result (the core's DoResult) has its launch step sent to the phone: a step
// that fails comes back inside the result, not as an error.
func iphoneLaunchSent(output json.RawMessage) bool {
	var r struct {
		Steps []struct {
			Action   string `json:"action"`
			Delivery string `json:"delivery"`
		} `json:"steps"`
	}
	if json.Unmarshal(output, &r) != nil {
		return false
	}
	for _, st := range r.Steps {
		if st.Action == "launch" && st.Delivery == "sent" {
			return true
		}
	}
	return false
}
