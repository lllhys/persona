package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// pushCredential adds a live phone credential created at created and, when seen is not zero, last seen then.
func pushCredential(t *testing.T, d *DB, id string, created, seen time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := d.CreateDeviceToken(ctx, DeviceToken{ID: id, Name: "iPhone", Kind: "phone", Role: "full"}, "hash-"+id); err != nil {
		t.Fatal(err)
	}
	var last *time.Time
	if !seen.IsZero() {
		last = &seen
	}
	if _, err := d.pool.Exec(ctx, `UPDATE device_tokens SET created_at=$2, last_seen=$3 WHERE id=$1`, id, created, last); err != nil {
		t.Fatal(err)
	}
}

func pushRegistrationOwner(t *testing.T, d *DB, id string) (string, bool) {
	t.Helper()
	list, err := d.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range list {
		if v.DeviceID == id {
			return v.CredentialID, true
		}
	}
	return "", false
}

func phoneRegistration(owner string, at time.Time) Device {
	return Device{DeviceID: "phone", CredentialID: owner, Credential: owner, Name: "iPhone", Kind: "iphone", Topic: "app.example",
		APNSEnv: "production", PushKey: "key", APNSToken: "token", RelayURL: "https://relay.example", RelayGrant: "grant",
		Registering: true, RegisteredAt: at}
}

// A phone paired again keeps its registration id and registers with its new credential: the registration moves to it,
// so revoking the old credential (unused since) leaves the phone's pushes alone, and revoking the one it uses still
// stops them.
func TestRepairedPhoneRegistrationMovesToItsNewCredential(t *testing.T) {
	d, ctx := open(t), context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	pushCredential(t, d, "old-cred", at.Add(-48*time.Hour), at.Add(-24*time.Hour-6*time.Second))
	pushCredential(t, d, "new-cred", at.Add(-24*time.Hour), at.Add(-time.Minute))
	if err := d.UpsertDevice(ctx, phoneRegistration("old-cred", at.Add(-48*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := d.SetDevicePrefs(ctx, "phone", "old-cred", json.RawMessage(`{"input":true,"done":"always"}`)); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveLiveActivity(ctx, LiveActivity{DeviceID: "phone", PushToStartToken: "start"}); err != nil {
		t.Fatal(err)
	}

	// The re-paired phone registers again; its APNs token may have rotated, its push key proves the installation.
	again := phoneRegistration("new-cred", at)
	again.APNSToken = "rotated-token"
	if err := d.UpsertDevice(ctx, again); err != nil {
		t.Fatalf("re-paired registration refused: %v", err)
	}
	if owner, ok := pushRegistrationOwner(t, d, "phone"); !ok || owner != "new-cred" {
		t.Fatalf("registration owner after re-pairing = %q (present %v), want new-cred", owner, ok)
	}

	// Cleaning up the old, unused credential no longer retires the phone in use, nor its settings or Live Activity.
	if ok, err := d.RevokeDeviceToken(ctx, "old-cred"); err != nil || !ok {
		t.Fatalf("revoke old: %v %v", ok, err)
	}
	list, err := d.Devices(ctx)
	if err != nil || len(list) != 1 || list[0].DeviceID != "phone" || list[0].APNSToken != "rotated-token" || list[0].RelayGrant != "grant" {
		t.Fatalf("registration after revoking the old credential: %+v %v", list, err)
	}
	if string(list[0].Prefs) != `{"done": "always", "input": true}` {
		t.Fatalf("prefs lost: %s", list[0].Prefs)
	}
	if activities, err := d.LiveActivities(ctx); err != nil || len(activities) != 1 {
		t.Fatalf("live activity lost: %+v %v", activities, err)
	}
	if retired, err := d.ReconcilePushDevices(ctx, at, 90*24*time.Hour); err != nil || len(retired) != 0 {
		t.Fatalf("maintenance retired the live registration: %+v %v", retired, err)
	}

	// Revoking the credential the phone uses now still stops its pushes.
	if ok, err := d.RevokeDeviceToken(ctx, "new-cred"); err != nil || !ok {
		t.Fatalf("revoke new: %v %v", ok, err)
	}
	if _, ok := pushRegistrationOwner(t, d, "phone"); ok {
		t.Fatal("a revoked device kept its push registration")
	}
	var reason string
	if err := d.pool.QueryRow(ctx, `SELECT reason FROM push_device_retirements WHERE device_id='phone'`).Scan(&reason); err != nil || reason != "revoked" {
		t.Fatalf("retirement = %q %v", reason, err)
	}
	t.Log("before: the re-paired phone's registration stayed under the old credential; after: it moves, survives the old one's revocation, and goes with the new one's")
}

// The move needs the installation's proof, a live new credential, and an old credential that went quiet when the new
// one was issued; anything else is refused as before (409), and the row keeps its owner.
func TestPushRegistrationTransferNeedsProof(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond)
	for _, tc := range []struct {
		name string
		edit func(*Device)
		seen time.Duration // the old credential's last use, relative to the new one's creation
		new  string
	}{
		{name: "different installation", edit: func(v *Device) { v.PushKey, v.APNSToken = "other-key", "other-token" }, seen: -time.Second, new: "new-cred"},
		{name: "different topic", edit: func(v *Device) { v.Topic = "other.example" }, seen: -time.Second, new: "new-cred"},
		{name: "different environment", edit: func(v *Device) { v.APNSEnv = "sandbox" }, seen: -time.Second, new: "new-cred"},
		{name: "old credential still in use", seen: time.Hour, new: "new-cred"},
		{name: "unknown credential", seen: -time.Second, new: "not-issued"},
		{name: "revoked credential", seen: -time.Second, new: "revoked-cred"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, ctx := open(t), context.Background()
			created := at.Add(-24 * time.Hour)
			pushCredential(t, d, "old-cred", at.Add(-48*time.Hour), created.Add(tc.seen))
			pushCredential(t, d, "new-cred", created, time.Time{})
			pushCredential(t, d, "revoked-cred", created, time.Time{})
			if err := d.UpsertDevice(ctx, phoneRegistration("old-cred", at.Add(-48*time.Hour))); err != nil {
				t.Fatal(err)
			}
			if _, err := d.RevokeDeviceToken(ctx, "revoked-cred"); err != nil {
				t.Fatal(err)
			}
			v := phoneRegistration(tc.new, at)
			if tc.edit != nil {
				tc.edit(&v)
			}
			if err := d.UpsertDevice(ctx, v); !errors.Is(err, ErrPushRegistrationConflict) {
				t.Fatalf("transfer allowed: %v", err)
			}
			if owner, ok := pushRegistrationOwner(t, d, "phone"); !ok || owner != "old-cred" {
				t.Fatalf("owner = %q (present %v), want old-cred", owner, ok)
			}
			// A background save (not an enrollment) never moves the row either, even with the proof.
			bg := phoneRegistration("new-cred", at)
			bg.Registering = false
			if err := d.UpsertDevice(ctx, bg); !errors.Is(err, ErrPushRegistrationConflict) {
				t.Fatalf("background save moved the row: %v", err)
			}
		})
	}
}

// After a registration was retired, the app's settings and Live Activity writes say so (ErrPushRegistrationConflict,
// which the hub answers as 409 push_registration_required) instead of failing as a database error, and the app's next
// registration with the same installation restores it.
func TestRetiredRegistrationWritesConflictUntilTheAppRegistersAgain(t *testing.T) {
	d, ctx := open(t), context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	pushCredential(t, d, "old-cred", at.Add(-48*time.Hour), at.Add(-24*time.Hour-time.Second))
	pushCredential(t, d, "new-cred", at.Add(-24*time.Hour), at.Add(-time.Minute))
	// Before the fix: the registration stayed under the old credential, which was then revoked.
	if err := d.UpsertDevice(ctx, phoneRegistration("old-cred", at.Add(-48*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RevokeDeviceToken(ctx, "old-cred"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetDevicePrefs(ctx, "phone", "new-cred", json.RawMessage(`{"input":true}`)); !errors.Is(err, ErrPushRegistrationConflict) {
		t.Fatalf("prefs write on a retired registration: %v", err)
	}
	if err := d.SaveLiveActivity(ctx, LiveActivity{DeviceID: "phone", PushToStartToken: "start"}); !errors.Is(err, ErrPushRegistrationConflict) {
		t.Fatalf("live activity write on a retired registration: %v", err)
	}
	if err := d.UpsertDevice(ctx, phoneRegistration("new-cred", at)); err != nil {
		t.Fatalf("registering again after retirement: %v", err)
	}
	if owner, ok := pushRegistrationOwner(t, d, "phone"); !ok || owner != "new-cred" {
		t.Fatalf("owner = %q (present %v), want new-cred", owner, ok)
	}
	if err := d.SetDevicePrefs(ctx, "phone", "new-cred", json.RawMessage(`{"input":true}`)); err != nil {
		t.Fatalf("prefs write after registering again: %v", err)
	}
}
