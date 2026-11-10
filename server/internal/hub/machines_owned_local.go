package hub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// addOwnedLocalMachine adds a dedicated local machine: only ever owner's, never the carrier's (OwnerRequired). It is
// exposed only through the authenticated admin machine endpoint.
func (h *Hub) addOwnedLocalMachine(ctx context.Context, mc *MachineConfig, owner string) error {
	if mc.Kind != "local" || owner == "" || owner == devicesOwner {
		return refuseAdd(http.StatusBadRequest, "invalid_manager", errors.New("a local machine requires an explicit non-carrier manager"))
	}
	return h.addMachineTo(ctx, mc, owner, "admin")
}

// addMachineTo adds mc as manager owner's (not the carrier) from the start: the owner is reserved durably before the
// entry can reach any manager. A crash between the writes leaves either an unused owner reservation or an entry with
// its owner already stored, never one the carrier ran first. A local machine is a dedicated one (OwnerRequired); any
// other is owner's as one moved to it is (moveMachine): owner offline, it waits in hub.json until owner connects. by
// is who added it, for the log.
func (h *Hub) addMachineTo(ctx context.Context, mc *MachineConfig, owner, by string) error {
	if owner == "" || owner == devicesOwner {
		return refuseAdd(http.StatusBadRequest, "invalid_manager", errors.New("a machine added to a manager names one other than the carrier"))
	}
	if mc.Kind == "local" {
		mc.OwnerRequired = true
	}
	h.moving.managers.RLock()
	defer h.moving.managers.RUnlock()
	d := &h.devices
	d.moving.Lock()
	defer d.moving.Unlock()
	if !h.knownManager(ctx, owner) {
		return refuseAdd(http.StatusBadRequest, "no_manager", fmt.Errorf("%w: %q", errUnknownManager, owner))
	}
	store := h.managerStore()
	if store == nil {
		return refuseAdd(http.StatusServiceUnavailable, "ownership_unavailable", errOwnersUnknown)
	}
	if err := h.readOwnersMoving(ctx); err != nil {
		return err
	}
	if h.machineIDTaken(mc.ID) {
		return refuseAdd(http.StatusConflict, "exists", errors.New("machine already exists"))
	}
	d.mu.Lock()
	previous := d.assign
	next := make(map[string]string, len(previous)+1)
	for k, v := range previous {
		next[k] = v
	}
	next[mc.ID] = owner
	d.mu.Unlock()
	if err := store.PutSetting(ctx, machineOwnersKey, next); err != nil {
		return err
	}
	d.mu.Lock()
	d.assign = next
	d.mu.Unlock()
	if err := h.addMachineMoving(mc, by); err != nil {
		// The entry was not published. Restore the reservation without a cancelled
		// request preventing cleanup; if storage is down, keep the durable-safe owner.
		if rollbackErr := store.PutSetting(context.WithoutCancel(ctx), machineOwnersKey, previous); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("restore machine ownership: %w", rollbackErr))
		}
		d.mu.Lock()
		d.assign = previous
		d.mu.Unlock()
		return err
	}
	return nil
}

// releaseDedicatedMachineMoving revokes a removed manager's ownership without
// handing its local process to the carrier. d.moving is held by releaseMachines.
func (h *Hub) releaseDedicatedMachineMoving(ctx context.Context, mid string) error {
	d := &h.devices
	d.mu.Lock()
	next := make(map[string]string, len(d.assign))
	for k, v := range d.assign {
		if k != mid {
			next[k] = v
		}
	}
	d.mu.Unlock()
	store := h.managerStore()
	if store == nil {
		return errOwnersUnknown
	}
	if err := store.PutSetting(ctx, machineOwnersKey, next); err != nil {
		return err
	}
	d.mu.Lock()
	d.assign = next
	d.mu.Unlock()
	h.machineHandedOver(mid, unknownMachineOwner)
	return nil
}

// releaseManagerDedicatedMachines runs before identity revocation while the
// manager-removal lock is held. A failed owner write must leave the identity in
// place, rather than report a completed removal with a reusable stale assignment.
func (h *Hub) releaseManagerDedicatedMachines(ctx context.Context, owner string) error {
	d := &h.devices
	d.moving.Lock()
	defer d.moving.Unlock()
	relevant := false
	for _, mc := range h.cfgEntries() {
		if mc.OwnerRequired && (h.entryOwner(mc.ID) == owner || h.entryOwner(mc.ID) == unknownMachineOwner) {
			relevant = true
			break
		}
	}
	if !relevant {
		return nil
	}
	if err := h.readOwnersMoving(ctx); err != nil {
		return err
	}
	for _, mc := range h.cfgEntries() {
		if mc.OwnerRequired && h.entryOwner(mc.ID) == owner {
			if err := h.releaseDedicatedMachineMoving(ctx, mc.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
