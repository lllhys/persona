package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- devices ----

type Device struct {
	DeviceID     string `json:"device_id"`
	CredentialID string `json:"-"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	APNSToken    string `json:"apns_token,omitempty"`
	APNSEnv      string `json:"apns_env,omitempty"`
	Topic        string `json:"topic,omitempty"`
	// Pushing through the relay (docs/operations/push-relay.md): the phone's public key (base64 X9.63), the relay's URL and the
	// grant it gave the phone.
	PushKey    string `json:"push_key,omitempty"`
	RelayURL   string `json:"relay_url,omitempty"`
	RelayGrant string `json:"relay_grant,omitempty"`
	// RelayError: why the phone got no grant from its relay, in its own words (the relay's status and answer, a
	// redirect, a network error); empty when it got one or names no relay.
	RelayError string `json:"relay_error,omitempty"`
	// Urgent calls (docs/assistant/urgent-call.md): the phone's PushKit token, the relay grant for it (topic
	// "<bundle id>.voip") and why it got none. Empty: this device cannot be rung.
	VoIPToken string `json:"voip_token,omitempty"`
	VoIPGrant string `json:"voip_grant,omitempty"`
	VoIPError string `json:"voip_error,omitempty"`
	// Features: what the app on it understands beyond the first version's pushes ("withdraw": a background push
	// naming the cards that are over, hub/push_withdraw.go). The hub keeps only names it knows.
	Features []string `json:"features,omitempty"`
	// Locale: the interface language it registered with (zh-Hans / en / ja, docs/protocol/i18n.md); "" is the user's.
	Locale string `json:"locale,omitempty"`
	// Credential: the device token (device_tokens.id) that registered it, set by the hub, never from the request;
	// empty for the pairing token. Revoking it forgets the device (hub: Pusher.signedOut).
	Credential   string    `json:"-"`
	UpdatedAt    time.Time `json:"updated_at"`
	RegisteredAt time.Time `json:"-"`
	LastPushAt   time.Time `json:"-"`
	Registering  bool      `json:"-"` // only a fresh authenticated enrollment may recreate a row
	// Prefs is the device's notification settings as JSON (hub decodes it); empty = defaults. Registration
	// (UpsertDevice) never touches it, only SetDevicePrefs does.
	Prefs json.RawMessage `json:"-"`
}

func (d *DB) UpsertDevice(ctx context.Context, v Device) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if err := lockPushRegistrations(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('persona.push_writer',$1,true)`, v.DeviceID); err != nil {
			return err
		}
		if !v.Registering && !v.RegisteredAt.IsZero() {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE device_id=$1)`, v.DeviceID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrPushRegistrationConflict
			}
		}
		if v.Registering {
			if _, err := tx.Exec(ctx, `SELECT set_config('persona.push_enrollment',$1,true)`, v.DeviceID); err != nil {
				return err
			}
			rows, err := devicesIn(ctx, tx)
			if err != nil {
				return err
			}
			for _, current := range rows {
				if current.DeviceID != v.DeviceID && SamePushInstallation(current, v) && current.RegisteredAt.After(v.RegisteredAt) {
					return ErrPushRegistrationConflict
				}
				// The same installation registering with another credential (the phone was paired again) moves its
				// row to that credential, so revoking the old one no longer retires a phone still in use. The fence
				// (migration 0541) checks the proof again and that the new credential is live.
				if current.DeviceID == v.DeviceID && pushOwner(current) != "" && pushOwner(current) != pushOwner(v) && sameInstallationTarget(current, v) {
					if _, err := tx.Exec(ctx, `SELECT set_config('persona.push_transfer',$1,true)`, v.DeviceID); err != nil {
						return err
					}
				}
			}
		}
		if err := upsertPushDevice(ctx, tx, v); err != nil {
			return err
		}
		if v.Registering {
			if _, err := reconcilePushDevices(ctx, tx, v.RegisteredAt, 90*24*time.Hour); err != nil {
				return err
			}
			var kept bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE device_id=$1)`, v.DeviceID).Scan(&kept); err != nil {
				return err
			}
			if !kept {
				return ErrPushRegistrationConflict
			}
		}
		return nil
	})
}

func (d *DB) Devices(ctx context.Context) ([]Device, error) {
	rows, err := d.pool.Query(ctx, `SELECT device_id, name, kind, apns_token, apns_env, topic, push_key, relay_url, relay_grant, credential, updated_at, prefs, credential_id, locale, relay_error, features, registered_at, last_push_at, voip_token, voip_grant, voip_error FROM devices ORDER BY registered_at DESC, device_id DESC`)
	if err != nil {
		return nil, err
	}
	return collectDevices(rows)
}

func collectDevices(rows pgx.Rows) ([]Device, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Device, error) {
		var v Device
		var prefs []byte
		var lastPush *time.Time
		err := r.Scan(&v.DeviceID, &v.Name, &v.Kind, &v.APNSToken, &v.APNSEnv, &v.Topic, &v.PushKey, &v.RelayURL, &v.RelayGrant, &v.Credential, &v.UpdatedAt, &prefs, &v.CredentialID, &v.Locale, &v.RelayError, &v.Features, &v.RegisteredAt, &lastPush, &v.VoIPToken, &v.VoIPGrant, &v.VoIPError)
		if lastPush != nil {
			v.LastPushAt = *lastPush
		}
		if len(prefs) > 0 {
			v.Prefs = prefs
		}
		return v, err
	})
}

// SetDevicePrefs stores a device's notification settings. The device may not have registered for push yet (no
// permission, the simulator, a browser, which never does): the row is created then and filled in when it does.
// credential is the device token that wrote them: a row without one takes it, so revoking that token (Pusher.signedOut)
// also removes a row only prefs ever made; registering sets it as usual.
func (d *DB) SetDevicePrefs(ctx context.Context, id, credential string, prefs json.RawMessage) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO devices (device_id, name, kind, apns_token, apns_env, topic, updated_at, prefs, credential)
		VALUES ($1, '', '', '', '', '', now(), $2, $3)
		ON CONFLICT (device_id) DO UPDATE SET prefs = $2,
			credential = CASE WHEN devices.credential = '' THEN EXCLUDED.credential ELSE devices.credential END`, id, []byte(prefs), credential)
	// A retired registration id is fenced until the app registers again (POST /api/v1/devices).
	return pushRegistrationConflict(err)
}

// DeleteDevice forgets a device (e.g. APNs says the token is no longer valid).
func (d *DB) DeleteDevice(ctx context.Context, id string) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if err := lockPushRegistrations(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM devices WHERE device_id=$1 FOR UPDATE`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM live_activities WHERE device_id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('persona.push_retire',$1,true)`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM devices WHERE device_id=$1`, id)
		return err
	})
}

// LiveActivity is a device's Fleet activity (hub/liveactivity.go).
type LiveActivity struct {
	DeviceID         string
	ActivityID       string
	PushToken        string
	PushToStartToken string
	StartedAt        time.Time
}

func (d *DB) LiveActivities(ctx context.Context) ([]LiveActivity, error) {
	rows, err := d.pool.Query(ctx, `SELECT device_id, activity_id, push_token, push_to_start_token, started_at FROM live_activities`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LiveActivity, error) {
		var v LiveActivity
		var started *time.Time
		err := r.Scan(&v.DeviceID, &v.ActivityID, &v.PushToken, &v.PushToStartToken, &started)
		if started != nil {
			v.StartedAt = *started
		}
		return v, err
	})
}

// SaveLiveActivity writes the whole row (the hub keeps the current state in memory and saves it on every change).
func (d *DB) SaveLiveActivity(ctx context.Context, v LiveActivity) error {
	var started *time.Time
	if !v.StartedAt.IsZero() {
		started = &v.StartedAt
	}
	_, err := d.pool.Exec(ctx, `INSERT INTO live_activities (device_id, activity_id, push_token, push_to_start_token, started_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (device_id) DO UPDATE SET activity_id = $2, push_token = $3, push_to_start_token = $4, started_at = $5, updated_at = now()`,
		v.DeviceID, v.ActivityID, v.PushToken, v.PushToStartToken, started)
	return pushRegistrationConflict(err)
}
