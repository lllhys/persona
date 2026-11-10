package extmgr

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/errreport"
	"github.com/nwverse/persona/server/internal/extwire"
)

// The error reports (docs/extensions/extensions.md "Error reports"): the apps' (POST /api/v1/error-reports), the hub's
// (extwire.EventErrorReports) and the manager's own (its log's error lines) go through one pipeline here: checked,
// scrubbed, fingerprinted, deduplicated and rate limited, then given in batches to the running extensions with an
// error_reports contribution. With none, nothing is taken: the feature is off and the apps send nothing.

const (
	errorReportsFeature  = "error_reports"
	errorReportsBodyMost = 256 << 10
	errorReportsMost     = 20 // reports in one request, and in one delivery
	errorReportsBatch    = 2 * time.Second
	errorReportsQueue    = 500
	errorReportsMethod   = "error_reports.deliver"
)

// errorReports is the pipeline's state.
type errorReports struct {
	once  sync.Once
	gate  *errreport.Gate
	queue chan wire.ErrorReport
	// own: the manager's own reports, taken on another goroutine (ReportOwn is called from inside a log write, maybe
	// under a lock the pipeline takes); on: ErrorReportsOn as the last report to the hub said, read without a lock.
	own chan wire.ErrorReport
	on  atomic.Bool
}

func (h *Manager) errorPipe() *errorReports {
	h.errReports.once.Do(func() {
		h.errReports.gate = errreport.NewGate(nil)
		h.errReports.queue = make(chan wire.ErrorReport, errorReportsQueue)
		h.errReports.own = make(chan wire.ErrorReport, errorReportsQueue)
		go h.deliverErrorReports()
		go func() {
			for r := range h.errReports.own {
				h.takeErrorReport(r, "hub", []string{wire.ErrorSourceManager}, nil)
			}
		}()
	})
	return &h.errReports
}

// ErrorReportsOn says whether an extension takes error reports now: one enabled and running with an error_reports
// contribution switched on. Only then are reports collected, here and on the apps (the feature).
func (h *Manager) ErrorReportsOn() bool {
	if h == nil || h.extensions == nil {
		return false
	}
	for _, t := range h.extensions.reg.ErrorReportTakers("") {
		if hp := h.extensions.get(t.Extension); hp != nil && hp.proc != nil {
			return true
		}
	}
	return false
}

// ReportOwn takes a report of the manager itself (its log's error lines, a panic it recovered). Never blocks and
// never logs: it is called from inside the log's writer.
func (h *Manager) ReportOwn(r wire.ErrorReport) {
	if h == nil || !h.errReports.on.Load() {
		return
	}
	select {
	case h.errorPipe().own <- r:
	default:
	}
}

// ErrorReportsAsked says, without a lock, whether the manager's last report said error_reports: what its log's
// writer asks before it builds a report.
func (h *Manager) ErrorReportsAsked() bool { return h != nil && h.errReports.on.Load() }

// takeErrorReport puts one report through the pipeline; sources are the ones this sender may say, names the
// sender's own names to hide too. Says whether it was taken.
func (h *Manager) takeErrorReport(r wire.ErrorReport, sender string, sources []string, names []string) bool {
	p := h.errorPipe()
	if !slices.Contains(sources, r.Source) {
		return false
	}
	if errreport.Normalize(&r, time.Now()) != nil {
		return false
	}
	h.errorScrubber(names).Report(&r)
	r.Fingerprint = errreport.Fingerprint(&r)
	if !p.gate.Admit(&r, sender) {
		return false
	}
	select {
	case p.queue <- r:
		return true
	default: // the extension is slower than the errors come: the next one of this problem counts it
		p.gate.Unsent(&r)
		return false
	}
}

// errorScrubber hides the names of this machine, of every computer and of their users, and extra.
func (h *Manager) errorScrubber(extra []string) *errreport.Scrubber {
	names := slices.Clone(extra)
	if host, err := os.Hostname(); err == nil {
		names = append(names, host)
	}
	if home, err := os.UserHomeDir(); err == nil {
		names = append(names, filepath.Base(home))
	}
	for _, m := range h.machinesNow() {
		m.mu.Lock()
		names = append(names, m.cfg.Name, m.cfg.ID, m.cfg.SSHHost, m.info.Hostname, filepath.Base(m.info.Home))
		m.mu.Unlock()
	}
	return errreport.NewScrubber(names...)
}

// deliverErrorReports gives the queued reports to the extensions that take them, in batches.
func (h *Manager) deliverErrorReports() {
	p := &h.errReports
	tick := time.NewTicker(errorReportsBatch)
	defer tick.Stop()
	var batch []wire.ErrorReport
	for {
		select {
		case r := <-p.queue:
			batch = append(batch, r)
			if len(batch) < errorReportsMost {
				continue
			}
		case <-tick.C:
			if len(batch) == 0 {
				continue
			}
		}
		h.deliverErrorBatch(batch)
		batch = nil
	}
}

// deliverErrorBatch gives each contribution the reports of the sources it takes.
func (h *Manager) deliverErrorBatch(batch []wire.ErrorReport) {
	ps := h.extensions
	if ps == nil {
		return
	}
	for _, t := range ps.reg.ErrorReportTakers("") {
		hp := ps.get(t.Extension)
		if hp == nil || hp.proc == nil {
			continue
		}
		p := ps.reg.Get(t.Extension)
		if p == nil || p.Manifest == nil {
			continue
		}
		for _, it := range p.Manifest.ErrorReports() {
			if !slices.Contains(t.Contributions, it.ID) {
				continue
			}
			var mine []wire.ErrorReport
			for _, r := range batch {
				if len(it.Sources) == 0 || slices.Contains(it.Sources, r.Source) {
					mine = append(mine, r)
				}
			}
			if len(mine) > 0 {
				_ = hp.proc.Notify(errorReportsMethod, wire.ErrorReportsDeliver{Contribution: it.ID, Reports: mine})
			}
		}
	}
}

// postErrorReports is POST /api/v1/error-reports: an app's reports. Only the user's own apps send them (a full
// device, not a pairing token, not an extension, not the watch).
func (h *Manager) postErrorReports(w http.ResponseWriter, r *http.Request) {
	who := principalOf(r.Context())
	if who.Role != roleFull || who.Extension != "" || who.Paired {
		writeErrCode(w, http.StatusForbidden, "permission_denied", errors.New("error reports come from the user's own apps"))
		return
	}
	var body struct {
		Reports []json.RawMessage `json:"reports"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, errorReportsBodyMost)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("body: {\"reports\": [...]}, at most 256 KB"))
		return
	}
	if len(body.Reports) > errorReportsMost {
		writeErr(w, http.StatusBadRequest, errors.New("at most 20 reports at once"))
		return
	}
	if !h.ErrorReportsOn() {
		writeJSON(w, map[string]any{"enabled": false, "accepted": 0, "dropped": len(body.Reports)})
		return
	}
	sender := "device:" + who.ID
	accepted := 0
	for _, raw := range body.Reports {
		var rep wire.ErrorReport
		if json.Unmarshal(raw, &rep) != nil {
			continue
		}
		rep.Fingerprint, rep.Count, rep.ReceivedAt = "", 0, "" // the manager's to set
		if h.takeErrorReport(rep, sender, appErrorSources, []string{who.Name}) {
			accepted++
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"enabled": true, "accepted": accepted, "dropped": len(body.Reports) - accepted})
}

// appErrorSources are the sources an app may say (not the hub's or the manager's).
var appErrorSources = []string{wire.ErrorSourceWeb, wire.ErrorSourceAndroid, wire.ErrorSourceIos, wire.ErrorSourceMac,
	wire.ErrorSourceWatch, wire.ErrorSourceVision}

// hubErrorReports takes the hub's own reports (extwire.EventErrorReports).
func (h *Manager) hubErrorReports(params json.RawMessage) {
	if !h.ErrorReportsOn() {
		return
	}
	var e extwire.ErrorReports
	if json.Unmarshal(params, &e) != nil {
		return
	}
	for _, r := range e.Reports {
		h.takeErrorReport(r, "hub", []string{wire.ErrorSourceHub}, nil)
	}
}
