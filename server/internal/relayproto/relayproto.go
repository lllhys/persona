// Package relayproto is what a hub and the push relay (internal/relay, docs/operations/push-relay.md) say to each other, so the
// hub does not link the relay's server.
package relayproto

// PushRequest is what a hub posts to /relay/v1/push.
type PushRequest struct {
	Type       string         `json:"type"` // alert | background | liveactivity | voip
	Priority   string         `json:"priority,omitempty"`
	CollapseID string         `json:"collapse_id,omitempty"`
	Sealed     string         `json:"sealed,omitempty"` // alert / background / voip: base64 HPKE payload (pushcrypto)
	Token      string         `json:"token,omitempty"`  // liveactivity: the activity's or the push-to-start token
	APS        map[string]any `json:"aps,omitempty"`    // liveactivity
	// TTL is how many seconds APNs keeps trying a voip push (an urgent call ringing the phone) before it
	// drops it, so a phone that was offline never rings for a call that is long over: 1 to MaxVoIPTTL, 0 for
	// DefaultVoIPTTL. Other types ignore it.
	TTL int `json:"ttl,omitempty"`
}

// VoIPTopicSuffix ends the topic of a PushKit (VoIP) device token: the phone registers that token with
// "<bundle id>.voip", and its grant carries only voip pushes.
const VoIPTopicSuffix = ".voip"

// DefaultVoIPTTL and MaxVoIPTTL bound how long APNs keeps an undelivered voip push (PushRequest.TTL).
const (
	DefaultVoIPTTL = 30
	MaxVoIPTTL     = 60
)

// PushTypes are the push types this relay version takes; GET /relay/v1/health lists them so a hub or phone can tell
// an older relay that cannot ring a phone.
var PushTypes = []string{"alert", "background", "liveactivity", "voip"}

// MaxSealed is the longest base64 sealed payload the relay takes: with its envelope (about 130 bytes) the push stays
// under APNs' 4 KB. The hub shortens a push's texts until it fits (hub/push_transport.go sealFor).
const MaxSealed = 3800

// RegisterRequest is what /relay/v1/register takes (the phone at first, the hub again when a grant stops working);
// the answer is {"grant": "..."}.
type RegisterRequest struct {
	APNSToken string `json:"apns_token"`
	Env       string `json:"env"`
	Topic     string `json:"topic"` // the bundle id, or "<bundle id>.voip" for a PushKit token
}
