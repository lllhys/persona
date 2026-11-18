package devices

import "testing"

// BenchmarkViews: the computers as the extensions manager reads them on each of its lookups (Devices.Views), 20
// computers of 200 sessions each. The views carried a copy of every computer's sessions before #3197.
func BenchmarkViews(b *testing.B) {
	d := ListedMachinesWithSessions(b, 20, 200)
	b.ReportAllocs()
	for b.Loop() {
		if len(d.Views()) != 20 {
			b.Fatal("not 20 views")
		}
	}
}
