package devices

import (
	"reflect"
	"testing"
	"time"
)

// viewsByAppend is Views before its result was allocated with the known machine count.
func viewsByAppend(d *Devices) []View {
	out := []View{}
	for _, m := range d.machinesNow() {
		m.mu.Lock()
		v := View{Summary: Summary{ID: m.cfg.ID, Name: m.name(), Kind: m.cfg.Kind, SSHHost: m.cfg.SSHHost, Home: m.info.Home,
			DataDir: m.info.DataDir, Online: m.online || m.warmLocked(time.Now())}, Info: m.info}
		m.mu.Unlock()
		v.Trust = d.machineTrust(m)
		v.Reachable = m.reachable()
		out = append(out, v)
	}
	return out
}

func TestViewsMatchAppendPath(t *testing.T) {
	d := newTestDevices(t)
	check := func(step string) {
		t.Helper()
		got, want := d.Views(), viewsByAppend(d)
		if got == nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: Views = %+v, previous path = %+v", step, got, want)
		}
	}
	check("empty before regroup")
	box := summaryMachine(d, "box", "ssh", "work", true)
	link := summaryMachine(d, "box-link", kindLink, "personal", false)
	mac := summaryMachine(d, "mac", "local", "", true)
	for _, m := range []*Machine{box, link, mac} {
		d.addEntry(m)
	}
	check("configured order")
	far := summaryMachine(d, "far", "ssh", "", true)
	far.derived = true
	f := &d.fleet
	f.mu.Lock()
	f.init()
	f.absorbed[link] = box
	f.paths[box] = []*nodePath{cfgPath(link), cfgPath(box)}
	f.derived["uid:far"] = far
	f.order = []*Machine{mac, box, far, box}
	f.mu.Unlock()
	check("reordered, absorbed, derived and duplicate")
	got := d.Views()
	if len(got) != 4 || got[0].ID != "mac" || got[1].ID != "box" || got[2].ID != "far" || got[3].ID != "box" {
		t.Fatalf("listed order changed: %+v", got)
	}
	d.addEntry(summaryMachine(d, "late", "ssh", "", true))
	d.mu.Lock()
	delete(d.byID, "mac")
	d.mu.Unlock()
	check("entries differ from listed order")
	f.mu.Lock()
	f.order = []*Machine{}
	f.mu.Unlock()
	check("empty after regroup")
}
