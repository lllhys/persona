package devices

// A computer's diagnosis and cache cleanup (#8037, docs/server/machines.md "Diagnosis and cleanup"): the routes the
// machine page's alert sheets read, relayed to the node's machine.diagnose and machine.cleanup. The manager checks
// what it can before the node is asked (the role, the sections and ids against the fixed lists, the capability), and
// passes the node's answer through with the machine's id. Only a device with full credentials gets here: the hub keeps
// answer-only devices and extensions' tokens out, and diagnosisRoleOK checks the role it relays again.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/nodeproto"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// How long the manager waits on the node: a diagnosis's disk walk stops after 20 s on the node; a cleanup removes
// whole caches, which on a slow disk takes a while.
const (
	diagnosisTimeout = 30 * time.Second
	cleanupTimeout   = 120 * time.Second
)

// codeBadRequest is the code of a request the manager refuses as malformed.
const codeBadRequest = "bad_request"

// getDiagnosis: GET /api/v1/machines/{mid}/diagnosis?sections=processes,disk,tools → proto.Diagnosis.
func (d *Devices) getDiagnosis(w http.ResponseWriter, r *http.Request, m *Machine) {
	if !diagnosisRoleOK(w, r) {
		return
	}
	var sections []string
	if q := r.URL.Query().Get("sections"); q != "" {
		for _, s := range strings.Split(q, ",") {
			s = strings.TrimSpace(s)
			if !slices.Contains(proto.DiagnoseSections, s) {
				writeErrCode(w, http.StatusBadRequest, codeBadRequest, i18n.Err("devices-diagnosis.bad_section", i18n.Args{"section": s}))
				return
			}
			if !slices.Contains(sections, s) {
				sections = append(sections, s)
			}
		}
	}
	if !diagnosisReady(w, m) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), diagnosisTimeout)
	defer cancel()
	var out proto.Diagnosis
	if err := m.call(ctx, proto.MethodMachineDiagnose, proto.DiagnoseParams{Sections: sections}, &out); err != nil {
		d.writeDiagnosisCallErr(w, m, "diagnose", err)
		return
	}
	out.MachineID = m.cfg.ID
	writeJSON(w, out)
}

// postCleanup: POST /api/v1/machines/{mid}/cleanup {"ids":[…]} → proto.CleanupResult.
func (d *Devices) postCleanup(w http.ResponseWriter, r *http.Request, m *Machine) {
	if !diagnosisRoleOK(w, r) {
		return
	}
	var in proto.CleanupParams
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil || len(in.IDs) == 0 {
		writeErrCode(w, http.StatusBadRequest, codeBadRequest, i18n.Err("devices-diagnosis.need_ids"))
		return
	}
	for _, id := range in.IDs {
		if !slices.Contains(proto.CacheIDs, id) {
			writeErrCode(w, http.StatusBadRequest, codeBadRequest, i18n.Err("devices-diagnosis.bad_cache", i18n.Args{"id": id}))
			return
		}
	}
	if !diagnosisReady(w, m) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), cleanupTimeout)
	defer cancel()
	var out proto.CleanupResult
	if err := m.call(ctx, proto.MethodMachineCleanup, proto.CleanupParams{IDs: in.IDs}, &out); err != nil {
		d.writeDiagnosisCallErr(w, m, "cleanup", err)
		return
	}
	cleaned := 0
	for _, it := range out.Items {
		if it.Status == proto.CleanupCleaned {
			cleaned++
		}
	}
	d.logf("[%s] cleanup from the app: %d of %d caches emptied, %d bytes freed", m.cfg.ID, cleaned, len(in.IDs), out.FreedBytes)
	writeJSON(w, out)
}

// diagnosisRoleOK refuses a device that may only read and answer, and an extension's token (the hub already does;
// this is the manager's own check of the role it relays): a diagnosis lists what runs on the computer, and a cleanup
// removes files.
func diagnosisRoleOK(w http.ResponseWriter, r *http.Request) bool {
	switch r.Header.Get(assistantwire.RoleHeader) {
	case "", "full":
		return true
	}
	writeErrCode(w, http.StatusForbidden, proto.CodeReadOnly, i18n.Err("devices-diagnosis.read_only"))
	return false
}

// diagnosisReady answers the request itself, and false, when the node cannot be asked: it is offline (503), or too old
// to have the methods (409). A node reconnecting (warm) is waited for by the call.
func diagnosisReady(w http.ResponseWriter, m *Machine) bool {
	m.mu.Lock()
	reachable := m.online || m.warmLocked(time.Now())
	info := m.info
	m.mu.Unlock()
	if !reachable {
		writeErrCode(w, http.StatusServiceUnavailable, devicewire.CodeMachineOffline, i18n.Err("devices-diagnosis.offline", i18n.Args{"name": m.name()}))
		return false
	}
	if !nodeHas(info, nodeproto.MachineDiagnose) {
		writeErrCode(w, http.StatusConflict, proto.CodeNodeOutdated, i18n.Err("devices-diagnosis.node_outdated"))
		return false
	}
	return true
}

// writeDiagnosisCallErr answers a failed call by what went wrong; the node's own words are logged, not shown.
func (d *Devices) writeDiagnosisCallErr(w http.ResponseWriter, m *Machine, op string, err error) {
	var coded *rpc.Error
	errors.As(err, &coded)
	switch {
	case isOffline(err):
		writeErrCode(w, http.StatusServiceUnavailable, devicewire.CodeMachineOffline, i18n.Err("devices-diagnosis.offline", i18n.Args{"name": m.name()}))
	case coded != nil && (coded.Code == proto.CodeNodeOutdated || strings.HasPrefix(coded.Message, "unknown method")):
		writeErrCode(w, http.StatusConflict, proto.CodeNodeOutdated, i18n.Err("devices-diagnosis.node_outdated"))
	case errors.Is(err, context.DeadlineExceeded) || coded != nil && coded.Message == context.DeadlineExceeded.Error():
		writeErrCode(w, http.StatusGatewayTimeout, devicewire.CodeDeadline, i18n.Err("devices-diagnosis.deadline", i18n.Args{"name": m.name()}))
	default:
		d.logf("[%s] %s: %v", m.cfg.ID, op, err)
		writeErrCode(w, http.StatusBadGateway, "diagnosis_failed", i18n.Err("devices-diagnosis.failed", i18n.Args{"name": m.name()}))
	}
}
