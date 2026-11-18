package extension

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/lazyre"
)

var semverPattern = lazyre.New(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)

// Version is a plugin's MAJOR.MINOR.PATCH[-pre]. The integer fields are a
// best-effort compatibility view; Compare retains the full parsed precision.
type Version struct {
	Major, Minor, Patch int
	Pre                 string
	rawCore             [3]string
}

// ParseVersion reads a semantic version (build metadata not allowed).
func ParseVersion(s string) (Version, error) {
	m := semverPattern.FindStringSubmatch(s)
	if m == nil {
		return Version{}, errors.New("not MAJOR.MINOR.PATCH")
	}
	var v Version
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	v.Pre = strings.TrimPrefix(m[4], "-")
	v.rawCore = [3]string{m[1], m[2], m[3]}
	return v, nil
}

// Compare is -1, 0 or 1 as a is older than, the same as or newer than b (a pre-release is older than its release).
func (a Version) Compare(b Version) int {
	aCore := []int{a.Major, a.Minor, a.Patch}
	bCore := []int{b.Major, b.Minor, b.Patch}
	for i := range aCore {
		aRaw, bRaw := a.rawCore[i], b.rawCore[i]
		if aRaw == "" {
			aRaw = strconv.Itoa(aCore[i])
		}
		if bRaw == "" {
			bRaw = strconv.Itoa(bCore[i])
		}
		if c := compareNumeric(aRaw, bRaw); c != 0 {
			return c
		}
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	}
	return comparePre(a.Pre, b.Pre)
}

func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		aNumeric, bNumeric := isASCIIDigits(as[i]), isASCIIDigits(bs[i])
		switch {
		case aNumeric && bNumeric:
			if c := compareNumeric(as[i], bs[i]); c != 0 {
				return c
			}
		case aNumeric:
			return -1
		case bNumeric:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	return sign(len(as) - len(bs))
}

func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// compareNumeric compares valid decimal integers without narrowing them to int.
// Core strings come from semverPattern; fallback strings come from strconv.Itoa.
func compareNumeric(a, b string) int {
	ai, _ := new(big.Int).SetString(a, 10)
	bi, _ := new(big.Int).SetString(b, 10)
	return ai.Cmp(bi)
}

func sign(d int) int {
	switch {
	case d < 0:
		return -1
	case d > 0:
		return 1
	}
	return 0
}

// NewerVersion says whether version a is newer than b (both valid; an invalid one is never newer).
func NewerVersion(a, b string) bool {
	va, err := ParseVersion(a)
	if err != nil {
		return false
	}
	vb, err := ParseVersion(b)
	if err != nil {
		return true
	}
	return va.Compare(vb) > 0
}

// Range is a range of hub build numbers: every bound must hold.
type Range struct {
	bounds []bound
}

type bound struct {
	op string // ">=" or "<"
	n  int64
}

// ParseRange reads ">=N", "<N" or both, separated by spaces.
func ParseRange(s string) (Range, error) {
	var r Range
	for _, f := range strings.Fields(s) {
		var b bound
		switch {
		case strings.HasPrefix(f, ">="):
			b.op = ">="
		case strings.HasPrefix(f, "<") && !strings.HasPrefix(f, "<="):
			b.op = "<"
		default:
			return Range{}, fmt.Errorf("%q: only >=N and <N", f)
		}
		n, err := strconv.ParseInt(strings.TrimPrefix(f, b.op), 10, 64)
		if err != nil || n <= 0 {
			return Range{}, fmt.Errorf("%q: N is a build number", f)
		}
		b.n = n
		r.bounds = append(r.bounds, b)
	}
	if len(r.bounds) == 0 {
		return Range{}, errors.New("empty")
	}
	return r, nil
}

// Allows says whether build is in the range.
func (r Range) Allows(build int64) bool {
	for _, b := range r.bounds {
		if b.op == ">=" && build < b.n || b.op == "<" && build >= b.n {
			return false
		}
	}
	return true
}

// CheckEngines says why m cannot run on the hub of build (nil when it can). A development build (0) runs anything;
// the warning says so.
func CheckEngines(m *Manifest, build int64) (warning string, err error) {
	if m.Engines.Persona == "" {
		return "", nil
	}
	r, err := ParseRange(m.Engines.Persona)
	if err != nil {
		return "", err
	}
	if build == 0 {
		return i18n.T(i18n.Default, "extension-status.dev_engines", i18n.Args{"required": m.Engines.Persona}), nil
	}
	if !r.Allows(build) {
		args := i18n.Args{"required": m.Engines.Persona, "current": build}
		for _, b := range r.bounds {
			if b.op == "<" && build >= b.n {
				return "", i18n.Err("extension-version.hub_incompatible", args)
			}
		}
		return "", i18n.Err("extension-version.hub_too_old", args)
	}
	return "", nil
}
