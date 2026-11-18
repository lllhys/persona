package extension

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"testing"
)

const (
	tenToTwentyMinusOne = "99999999999999999999"
	tenToTwenty         = "100000000000000000000"
)

func maxIntPlusOne() string {
	return new(big.Int).SetUint64(uint64(math.MaxInt) + 1).String()
}

func mustParsePrecisionVersion(t *testing.T, s string) Version {
	t.Helper()
	v, err := ParseVersion(s)
	if err != nil {
		t.Fatalf("ParseVersion(%q): %v", s, err)
	}
	return v
}

func assertPrecisionVersionOrder(t *testing.T, older, newer string) {
	t.Helper()
	a, b := mustParsePrecisionVersion(t, older), mustParsePrecisionVersion(t, newer)
	if got := a.Compare(b); got != -1 {
		t.Errorf("%q.Compare(%q) = %d, want -1", older, newer, got)
	}
	if got := b.Compare(a); got != 1 {
		t.Errorf("%q.Compare(%q) = %d, want 1", newer, older, got)
	}
	if a.Compare(b) == 0 || b.Compare(a) == 0 {
		t.Errorf("%q and %q must not compare equal", older, newer)
	}
	if !NewerVersion(newer, older) {
		t.Errorf("NewerVersion(%q, %q) = false, want true", newer, older)
	}
	if NewerVersion(older, newer) {
		t.Errorf("NewerVersion(%q, %q) = true, want false", older, newer)
	}
}

func TestVersionCompareArbitraryPrecisionCore(t *testing.T) {
	for _, boundary := range []struct {
		name, older, newer string
	}{
		{name: "max_int_plus_one", older: strconv.Itoa(math.MaxInt), newer: maxIntPlusOne()},
		{name: "ten_to_twenty", older: tenToTwentyMinusOne, newer: tenToTwenty},
	} {
		for component := 0; component < 3; component++ {
			older, newer := []string{"0", "0", "0"}, []string{"0", "0", "0"}
			older[component], newer[component] = boundary.older, boundary.newer
			t.Run(boundary.name+"/"+[]string{"major", "minor", "patch"}[component], func(t *testing.T) {
				assertPrecisionVersionOrder(t, strings.Join(older, "."), strings.Join(newer, "."))
			})
		}
	}
}

func TestVersionComparePrereleasePrecisionAndSemVerOrder(t *testing.T) {
	ordinary := []string{
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
	}
	for i := 1; i < len(ordinary); i++ {
		assertPrecisionVersionOrder(t, ordinary[i-1], ordinary[i])
	}

	for _, pair := range [][2]string{
		{"1.0.0-0", "1.0.0-0.0"},
		{"1.0.0-" + tenToTwentyMinusOne, "1.0.0-" + tenToTwenty},
		{"1.0.0-100000000000000000000", "1.0.0-100000000000000000001"},
		{"1.0.0-" + tenToTwenty, "1.0.0-0a"},
		{"1.0.0-0", "1.0.0-0a"},
		{"1.0.0-0", "1.0.0--1"},
		{"1.0.0--1", "1.0.0-0a"},
	} {
		assertPrecisionVersionOrder(t, pair[0], pair[1])
	}

	for _, pair := range [][2]string{
		{"1.0.0-" + tenToTwenty, "1.0.0-" + tenToTwenty},
		{"1.0.0-000" + tenToTwenty, "1.0.0-" + tenToTwenty},
		{"1.0.0-000000000000000000000", "1.0.0-0"},
	} {
		a, b := mustParsePrecisionVersion(t, pair[0]), mustParsePrecisionVersion(t, pair[1])
		if got := a.Compare(b); got != 0 {
			t.Errorf("%q.Compare(%q) = %d, want 0", pair[0], pair[1], got)
		}
		if got := b.Compare(a); got != 0 {
			t.Errorf("%q.Compare(%q) = %d, want 0", pair[1], pair[0], got)
		}
	}
}

func TestParseVersionCoreValidationAndCompatibilityView(t *testing.T) {
	zero := mustParsePrecisionVersion(t, "0.0.0")
	if zero.Major != 0 || zero.Minor != 0 || zero.Patch != 0 {
		t.Fatalf("zero version fields = %+v", zero)
	}
	for _, bad := range []string{"00.0.0", "01.0.0", "1.01.0", "1.0.01"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) accepted a leading-zero core identifier", bad)
		}
	}

	huge := mustParsePrecisionVersion(t, tenToTwenty+"."+maxIntPlusOne()+".7")
	for name, got := range map[string]int{"major": huge.Major, "minor": huge.Minor, "patch": huge.Patch} {
		raw := map[string]string{"major": tenToTwenty, "minor": maxIntPlusOne(), "patch": "7"}[name]
		want, _ := strconv.Atoi(raw)
		if got != want {
			t.Errorf("%s compatibility field = %d, want strconv.Atoi result %d", name, got, want)
		}
	}

	// Prerelease parsing intentionally remains more permissive than core parsing.
	for _, valid := range []string{"1.0.0-01", "1.0.0-alpha..1"} {
		mustParsePrecisionVersion(t, valid)
	}
}

func TestVersionManualConstructionFallback(t *testing.T) {
	manual := Version{Major: 1, Minor: 2, Patch: 3, Pre: "rc.1"}
	parsed := mustParsePrecisionVersion(t, "1.2.3-rc.1")
	if got := manual.Compare(parsed); got != 0 {
		t.Errorf("manual.Compare(parsed) = %d, want 0", got)
	}
	if got := parsed.Compare(manual); got != 0 {
		t.Errorf("parsed.Compare(manual) = %d, want 0", got)
	}

	manualMax := Version{Major: math.MaxInt}
	parsedNext := mustParsePrecisionVersion(t, maxIntPlusOne()+".0.0")
	if got := manualMax.Compare(parsedNext); got != -1 {
		t.Errorf("manual MaxInt.Compare(parsed MaxInt+1) = %d, want -1", got)
	}
	if got := parsedNext.Compare(manualMax); got != 1 {
		t.Errorf("parsed MaxInt+1.Compare(manual MaxInt) = %d, want 1", got)
	}

	manualPre := Version{Major: 1, Pre: tenToTwentyMinusOne}
	parsedPre := mustParsePrecisionVersion(t, "1.0.0-"+tenToTwenty)
	if got := manualPre.Compare(parsedPre); got != -1 {
		t.Errorf("manual huge prerelease.Compare(parsed huge prerelease) = %d, want -1", got)
	}
}

func TestNewerVersionInvalidCandidateAndInstalledSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, candidate, installed string
		want                       bool
	}{
		{name: "invalid candidate", candidate: "invalid", installed: "1.0.0", want: false},
		{name: "invalid installed", candidate: "1.0.1", installed: "invalid", want: true},
		{name: "both invalid", candidate: "invalid", installed: "also invalid", want: false},
		{name: "valid candidate not newer", candidate: "1.0.0", installed: "1.0.1", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewerVersion(tc.candidate, tc.installed); got != tc.want {
				t.Fatalf("NewerVersion(%q, %q) = %v, want %v", tc.candidate, tc.installed, got, tc.want)
			}
		})
	}
}

func TestVersionRangeArbitraryPrecision(t *testing.T) {
	for _, tc := range []struct {
		name, versionRange, lower, upper string
	}{
		{name: "max int", versionRange: ">=" + strconv.Itoa(math.MaxInt) + ".0.0 <" + maxIntPlusOne() + ".0.0", lower: strconv.Itoa(math.MaxInt) + ".0.0", upper: maxIntPlusOne() + ".0.0"},
		{name: "core", versionRange: ">=" + tenToTwentyMinusOne + ".0.0 <" + tenToTwenty + ".0.0", lower: tenToTwentyMinusOne + ".0.0", upper: tenToTwenty + ".0.0"},
		{name: "prerelease", versionRange: ">=1.0.0-" + tenToTwentyMinusOne + " <1.0.0-" + tenToTwenty, lower: "1.0.0-" + tenToTwentyMinusOne, upper: "1.0.0-" + tenToTwenty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := ParseVersionRange(tc.versionRange)
			if err != nil {
				t.Fatal(err)
			}
			if !r.Allows(tc.lower) {
				t.Errorf("range %q rejected its inclusive lower bound %q", tc.versionRange, tc.lower)
			}
			if r.Allows(tc.upper) {
				t.Errorf("range %q accepted its exclusive upper bound %q", tc.versionRange, tc.upper)
			}
		})
	}
}
