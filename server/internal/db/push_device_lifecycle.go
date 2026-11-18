package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrPushRegistrationConflict = errors.New("push registration ownership changed or identity was superseded")

// PushDeviceRetirement contains identities only; callers must hash them in logs.
type PushDeviceRetirement struct{ DeviceID, CanonicalID, Reason string }

const pushRegistrationLock = int64(0x706572736f6e61)

func lockPushRegistrations(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, pushRegistrationLock)
	return err
}

func upsertPushDevice(ctx context.Context, tx pgx.Tx, v Device) error {
	features := v.Features
	if features == nil {
		features = []string{}
	}
	var registered, delivered *time.Time
	if !v.RegisteredAt.IsZero() {
		at := v.RegisteredAt.UTC().Truncate(time.Microsecond)
		registered = &at
	}
	if !v.LastPushAt.IsZero() {
		at := v.LastPushAt.UTC().Truncate(time.Microsecond)
		delivered = &at
	}
	tag, err := tx.Exec(ctx, `INSERT INTO devices (device_id,name,kind,apns_token,apns_env,topic,push_key,relay_url,relay_grant,credential,updated_at,credential_id,locale,relay_error,features,registered_at,last_push_at,voip_token,voip_grant,voip_error)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now(),$11,$12,$13,$14,COALESCE($15,now()),$16,$17,$18,$19)
		ON CONFLICT(device_id) DO UPDATE SET name=$2,kind=$3,apns_token=$4,apns_env=$5,topic=$6,push_key=$7,relay_url=$8,relay_grant=$9,credential=$10,updated_at=now(),credential_id=$11,locale=$12,relay_error=$13,features=$14,voip_token=$17,voip_grant=$18,voip_error=$19,
		registered_at=GREATEST(devices.registered_at,COALESCE($15,devices.registered_at)),last_push_at=GREATEST(devices.last_push_at,$16)
		WHERE (COALESCE(NULLIF(devices.credential_id,''),devices.credential)='' OR COALESCE(NULLIF(devices.credential_id,''),devices.credential)=COALESCE(NULLIF($11,''),$10)
			OR current_setting('persona.push_transfer',true)=devices.device_id)
		AND ($15::timestamptz IS NULL OR devices.registered_at <= $15)`, v.DeviceID, v.Name, v.Kind, v.APNSToken, v.APNSEnv, v.Topic, v.PushKey, v.RelayURL, v.RelayGrant, v.Credential, v.CredentialID, v.Locale, v.RelayError, features, registered, delivered, v.VoIPToken, v.VoIPGrant, v.VoIPError)
	if err != nil {
		return pushRegistrationConflict(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPushRegistrationConflict
	}
	return nil
}

// pushRegistrationConflict maps the fence's refusals (migrations 0435 and 0541) to ErrPushRegistrationConflict, so
// callers can tell "register again" apart from a database that is down.
func pushRegistrationConflict(err error) error {
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23514" && (pgerr.Message == "push registration was superseded" || pgerr.Message == "push registration ownership changed") {
		return ErrPushRegistrationConflict
	}
	return err
}

func pushOwner(d Device) string {
	if d.CredentialID != "" {
		return d.CredentialID
	}
	return d.Credential
}

// sameInstallationTarget is SamePushInstallation without the owner: the proof a re-paired phone gives that it is the
// installation a row already holds (its encryption key or exact APNs target, scoped by topic and environment).
func sameInstallationTarget(a, b Device) bool {
	return a.Topic != "" && a.Topic == b.Topic && a.APNSEnv == b.APNSEnv &&
		(a.PushKey != "" && a.PushKey == b.PushKey || a.APNSToken != "" && a.APNSToken == b.APNSToken)
}

// SamePushInstallation requires a registered owner and either its encryption key
// or exact APNs target, scoped by environment/topic. Names and IPs are never proof.
func SamePushInstallation(a, b Device) bool {
	owner := pushOwner(a)
	return owner != "" && owner == pushOwner(b) && sameInstallationTarget(a, b)
}

func devicesIn(ctx context.Context, tx pgx.Tx) ([]Device, error) {
	rows, err := tx.Query(ctx, `SELECT device_id,name,kind,apns_token,apns_env,topic,push_key,relay_url,relay_grant,credential,updated_at,prefs,credential_id,locale,relay_error,features,registered_at,last_push_at,voip_token,voip_grant,voip_error FROM devices ORDER BY registered_at DESC, device_id DESC FOR UPDATE`)
	if err != nil {
		return nil, err
	}
	return collectDevices(rows)
}

func pushProof(value string) string {
	if value == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func retirePushDevice(ctx context.Context, tx pgx.Tx, old Device, canonical string, reason string) error {
	if canonical != "" {
		// A new ID with no settings inherits the proven installation's choices;
		// never overwrite settings explicitly stored on the canonical registration.
		if _, err := tx.Exec(ctx, `UPDATE devices target SET prefs=source.prefs FROM devices source
			WHERE target.device_id=$2 AND source.device_id=$1 AND COALESCE(target.prefs,'{}'::jsonb)='{}'::jsonb AND COALESCE(source.prefs,'{}'::jsonb)<>'{}'::jsonb`, old.DeviceID, canonical); err != nil {
			return err
		}
		// Preserve the longest existing start deadline when identity rolls over.
		var budgetExists bool
		if err := tx.QueryRow(ctx, `SELECT to_regclass('live_activity_start_budget') IS NOT NULL`).Scan(&budgetExists); err != nil {
			return err
		}
		if budgetExists {
			_, err := tx.Exec(ctx, `INSERT INTO live_activity_start_budget (device_id,claimed_at,next_start_at)
				SELECT $2,claimed_at,next_start_at FROM live_activity_start_budget WHERE device_id=$1
				ON CONFLICT(device_id) DO UPDATE SET claimed_at=GREATEST(live_activity_start_budget.claimed_at,EXCLUDED.claimed_at),next_start_at=GREATEST(live_activity_start_budget.next_start_at,EXCLUDED.next_start_at)`, old.DeviceID, canonical)
			if err != nil {
				return err
			}
		}
	}
	owner := old.CredentialID
	if owner == "" {
		owner = old.Credential
	}
	if _, err := tx.Exec(ctx, `INSERT INTO push_device_retirements(device_id,canonical_id,credential_id,reason,push_key_hash,target_hash,topic,apns_env) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT(device_id) DO UPDATE SET canonical_id=EXCLUDED.canonical_id,reason=EXCLUDED.reason`, old.DeviceID, canonical, owner, reason, pushProof(old.PushKey), pushProof(old.APNSToken), old.Topic, old.APNSEnv); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM live_activities WHERE device_id=$1`, old.DeviceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('persona.push_retire',$1,true)`, old.DeviceID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM devices WHERE device_id=$1`, old.DeviceID)
	return err
}

// reconcilePushDevices runs under the same database lock as registration and
// renewal. The token ledger keeps active owners safe from a stale timestamp alone.
func reconcilePushDevices(ctx context.Context, tx pgx.Tx, at time.Time, maxIdle time.Duration) ([]PushDeviceRetirement, error) {
	devices, err := devicesIn(ctx, tx)
	if err != nil {
		return nil, err
	}
	var retired []PushDeviceRetirement
	var kept []Device
	for _, device := range devices {
		canonical := ""
		for _, candidate := range kept {
			if SamePushInstallation(candidate, device) {
				canonical = candidate.DeviceID
				break
			}
		}
		reason := "superseded"
		if canonical == "" {
			owner := device.CredentialID
			if owner == "" {
				owner = device.Credential
			}
			var seen, revoked *time.Time
			if owner != "" {
				err := tx.QueryRow(ctx, `SELECT last_seen,revoked_at FROM device_tokens WHERE id=$1`, owner).Scan(&seen, &revoked)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return nil, err
				}
			}
			latest := device.RegisteredAt
			if device.LastPushAt.After(latest) {
				latest = device.LastPushAt
			}
			if seen != nil && seen.After(latest) {
				latest = *seen
			}
			if revoked != nil {
				reason = "revoked"
			} else if !latest.IsZero() && at.Sub(latest) >= maxIdle {
				reason = "inactive"
			} else {
				kept = append(kept, device)
				continue
			}
		}
		if err := retirePushDevice(ctx, tx, device, canonical, reason); err != nil {
			return nil, err
		}
		retired = append(retired, PushDeviceRetirement{device.DeviceID, canonical, reason})
	}
	return retired, nil
}

// ReconcilePushDevices retires proven duplicates and stale/revoked owners in one
// transaction. Missing historical delivery data receives migration-time grace.
func (d *DB) ReconcilePushDevices(ctx context.Context, at time.Time, maxIdle time.Duration) ([]PushDeviceRetirement, error) {
	var retired []PushDeviceRetirement
	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if err := lockPushRegistrations(ctx, tx); err != nil {
			return err
		}
		var err error
		retired, err = reconcilePushDevices(ctx, tx, at, maxIdle)
		return err
	})
	if err != nil {
		return nil, err
	}
	return retired, nil
}
