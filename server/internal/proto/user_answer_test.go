package proto

import "testing"

func TestUserAllowedAndDenied(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		name          string
		r             Resolution
		allow, denied bool
	}{
		{"the user allows", Resolution{Allow: &yes}, true, false},
		{"an allow kept from always", Resolution{Allow: &yes, Remembered: true}, true, false},
		{"the user denies", Resolution{Allow: &no}, false, true},
		{"expired", Resolution{Dismiss: true, Reason: DismissedExpired}, false, false},
		{"dismissed", Resolution{Dismiss: true}, false, false},
		{"cancelled", Resolution{Cancel: true}, false, false},
		{"nothing", Resolution{}, false, false},
		{"allowed by the assistant", Resolution{Allow: &yes, AnsweredBy: AnsweredByAssistant}, false, false},
		{"denied by the assistant", Resolution{Allow: &no, AnsweredBy: AnsweredByAssistant}, false, false},
		{"an allow with a dismiss", Resolution{Allow: &yes, Dismiss: true}, false, false},
	} {
		if c.r.UserAllowed() != c.allow || c.r.UserDenied() != c.denied {
			t.Errorf("%s: allowed %v denied %v, want %v %v", c.name, c.r.UserAllowed(), c.r.UserDenied(), c.allow, c.denied)
		}
	}
}
