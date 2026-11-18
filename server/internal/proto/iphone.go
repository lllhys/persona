package proto

import (
	"encoding/json"
	"time"
)

// IPhone is what a computer has of the iPhones it reaches over RemotePairing through the device bridge, which the
// assistant may operate (the built-in extension iphone, docs/extensions/iphone.md). In proto.Machine only while the
// extension is on. Enabled is the manager's: the user turned it on for this computer; the rest is what the node found
// when last asked (the iphone program's status).
type IPhone struct {
	Enabled bool `json:"enabled"`
	// Installed: the device bridge (persona-iphone-bridge) is on this computer; Version, its version.
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	// Devices: the paired iPhones, and those plugged in over USB that can be paired.
	Devices []IPhoneDevice `json:"devices"`
	// Note: what is missing on this computer as a whole (no bridge, nothing paired, an old node), in Chinese for the
	// hub to translate.
	Note string `json:"note,omitempty"`
	// Code is Note's reason as a code, for an app to act on without reading the words: bridge_missing, none_paired.
	Code string `json:"code,omitempty"`
	// DDIPresent: this computer has the developer disk image a phone's Prepare mounts (Xcode's, or
	// PERSONA_IPHONE_DDI_DIR's); absent from a node that doesn't say (#8199).
	DDIPresent *bool     `json:"ddi_present,omitempty"`
	Checked    time.Time `json:"checked,omitempty"`
	// Job: pairing or forgetting an iPhone from the app (kind pair | forget), the one running or the last one since the
	// node started.
	Job *ComputerJob `json:"job,omitempty"`
}

// IPhoneDevice is one iPhone as the node sees it. ID is the node's stable id for it (from its pairing record, the same
// on every computer that paired it); ProductType ("iPhone16,2") and IOS ("18.1") come from the record and the device.
type IPhoneDevice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ProductType string `json:"product_type,omitempty"`
	IOS         string `json:"ios,omitempty"`
	// Paired: this computer holds a pairing record for it; USB: it is plugged in now (one that is can be paired).
	Paired bool `json:"paired"`
	USB    bool `json:"usb,omitempty"`
	// State of the link to a paired one: idle | connecting | connected | backoff | failed; Transport: network | usb.
	State     string `json:"state,omitempty"`
	Transport string `json:"transport,omitempty"`
	// Connected: the link is up and the device answered just now; Locked: it said its screen is locked.
	Connected bool `json:"connected"`
	Locked    bool `json:"locked"`
	// Capabilities: which groups of operations work now (screenshot, ax_tree, input, apps), as the device said.
	Capabilities map[string]bool `json:"capabilities,omitempty"`
	// Ready: it can be operated now. Note says what is missing and what to do, in Chinese for the hub to translate.
	Ready bool   `json:"ready"`
	Note  string `json:"note,omitempty"`
	// Code is Note's reason as a code (#8199), for an app to act on without reading the words: the bridge's or the
	// session's (developer_mode_off, ddi_not_mounted, unsupported, locked, unreachable, pairing_rejected, …), or
	// usb_unpaired (on the cable, not paired), connecting, failed (no better word). Empty with no note.
	Code string `json:"code,omitempty"`
	// DeveloperMode is what the iPhone last said of its Developer Mode (at pairing, then whenever it answered); absent
	// when it never said.
	DeveloperMode *bool `json:"developer_mode,omitempty"`
	// AutoPrepare: its owner prepared it once from the app (its developer disk image mounted), so this computer mounts
	// the image again by itself whenever it comes back without it, as after a restart (#8199).
	AutoPrepare bool `json:"auto_prepare,omitempty"`
	// Allowed is the manager's: the computer is on and this iPhone consented.
	Allowed bool `json:"allowed"`
}

// IPhoneCall is one call of the iphone program: op status | look | do on a device ("" the only paired one), in the
// assistant's conversation session, with the tool's arguments. Approved is the approval id the user allowed (passed
// back as the node gave it). TimeoutMS bounds the node's answer (it clamps it to 1–120 s, 30 s when 0).
type IPhoneCall struct {
	Device    string          `json:"device,omitempty"`
	Session   string          `json:"session,omitempty"`
	Op        string          `json:"op"`
	Args      json.RawMessage `json:"args,omitempty"`
	Approved  string          `json:"approved,omitempty"`
	TimeoutMS int             `json:"timeout_ms,omitempty"`
	// Confirm is the extension's "ask before every action": the node asks step by step (the manager does not ask
	// again first).
	Confirm bool `json:"confirm,omitempty"`
	// SelfApps and SelfNames are Persona's own apps on the iPhone, bundle ids and names, as the hub knows them (never
	// written in code): no step touches or launches them, since that is where the owner approves steps. The node keeps
	// them while it runs, so a call without them (a resume) never lifts the rule.
	SelfApps  []string `json:"self_apps,omitempty"`
	SelfNames []string `json:"self_names,omitempty"`
}

// IPhoneResult is what an iphone call answered: its JSON (Output), or Text, and a screenshot; or that a step needs the
// user's approval first (Approval says what, as data the manager words for the card; the call is made again with only
// Approved = ApprovalID once given). The JSON name approval_summary is the node core's (iphonectl.Response).
type IPhoneResult struct {
	Output        json.RawMessage `json:"output,omitempty"`
	Text          string          `json:"text,omitempty"`
	Image         *Image          `json:"image,omitempty"`
	NeedsApproval bool            `json:"needs_approval,omitempty"`
	Approval      *IPhoneApproval `json:"approval_summary,omitempty"`
	ApprovalID    string          `json:"approval_id,omitempty"`
}

// IPhoneApproval is the step that waits for the owner, as the node core gives it (iphonectl.Approval): which step of
// the do, what it does, why it needs approval (Needs: payment | delete | send_message | password | confirm | unchecked,
// unchecked being a screen without elements, so the target can't be identified), in which app, on what (Target) or
// where (At), what it types (Text, never a password; TextLen always), and with confirm the steps it covers. Evidence
// is English facts for the log, never shown.
type IPhoneApproval struct {
	Step      int                 `json:"step"`
	Action    string              `json:"action"`
	Needs     []string            `json:"needs"`
	App       IPhoneApp           `json:"app"`
	Target    *IPhoneElementRef   `json:"target,omitempty"`
	At        *IPhonePoint        `json:"at,omitempty"`
	Text      string              `json:"text,omitempty"`
	TextLen   int                 `json:"text_len,omitempty"`
	Key       string              `json:"key,omitempty"`
	Button    string              `json:"button,omitempty"`
	LaunchApp string              `json:"launch_app,omitempty"`
	Evidence  []string            `json:"evidence,omitempty"`
	Steps     []IPhoneStepSummary `json:"steps,omitempty"`
}

// IPhoneStepSummary is one step an approval with confirm covers.
type IPhoneStepSummary struct {
	Action  string            `json:"action"`
	Target  *IPhoneElementRef `json:"target,omitempty"`
	At      *IPhonePoint      `json:"at,omitempty"`
	TextLen int               `json:"text_len,omitempty"`
	Key     string            `json:"key,omitempty"`
	Button  string            `json:"button,omitempty"`
	App     string            `json:"app,omitempty"`
}

// IPhoneApp is the app in front: zero on the home or lock screen when the bridge can't tell.
type IPhoneApp struct {
	BundleID string `json:"bundle_id,omitempty"`
	Name     string `json:"name,omitempty"`
}

// IPhoneElementRef is an element of the screen by its number in the latest read, role and label.
type IPhoneElementRef struct {
	N     int    `json:"n,omitempty"`
	Role  string `json:"role"`
	Label string `json:"label,omitempty"`
}

// IPhonePoint is a position on the screen in points, origin at the top left.
type IPhonePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// A failed iphone call is an RPC error whose data says whether the operation may have reached the iPhone:
// IPhoneNotSent when it was refused before anything was handed to the device (safe to try again), IPhoneMaybeSent
// otherwise (never retried by itself).
const (
	IPhoneNotSent   = "no"
	IPhoneMaybeSent = "unknown"
)

// IPhoneErrorData is a failed iphone call's error data.
type IPhoneErrorData struct {
	Sent string `json:"sent"`
}
