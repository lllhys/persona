package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPRReviewWakeForgeEvidence(t *testing.T) {
	type reviewRow struct {
		State       string `json:"state"`
		CommitID    string `json:"commit_id,omitempty"`
		SubmittedAt string `json:"submitted_at,omitempty"`
		User        struct {
			Login string `json:"login,omitempty"`
		} `json:"user"`
	}
	makeReview := func(login, state, commit, at string) reviewRow {
		r := reviewRow{State: state, CommitID: commit, SubmittedAt: at}
		r.User.Login = login
		return r
	}
	marshalReviews := func(t *testing.T, rows []reviewRow) string {
		t.Helper()
		b, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	const (
		head = "abc123"
		t1   = "2026-11-17T01:02:03Z"
		t2   = "2026-11-17T02:03:04Z"
		t3   = "2026-11-17T03:04:05Z"
	)
	cases := []struct {
		name       string
		reviews    func(*testing.T) string
		wantAt     string
		wantReview string
	}{
		{
			name: "current head active request",
			reviews: func(t *testing.T) string {
				return marshalReviews(t, []reviewRow{makeReview("reviewer-a", "CHANGES_REQUESTED", head, t1)})
			},
			wantAt: t1, wantReview: "changes_requested",
		},
		{
			name: "latest active request across reviewers",
			reviews: func(t *testing.T) string {
				return marshalReviews(t, []reviewRow{
					makeReview("reviewer-a", "CHANGES_REQUESTED", head, t1),
					makeReview("reviewer-b", "CHANGES_REQUESTED", head, t3),
					makeReview("reviewer-c", "CHANGES_REQUESTED", head, t2),
				})
			},
			wantAt: t3, wantReview: "changes_requested",
		},
		{
			name: "later approval clears request",
			reviews: func(t *testing.T) string {
				return marshalReviews(t, []reviewRow{
					makeReview("reviewer-a", "CHANGES_REQUESTED", head, t1),
					makeReview("reviewer-a", "APPROVED", head, t2),
				})
			},
			wantReview: "approved",
		},
		{
			name: "later dismissal clears request",
			reviews: func(t *testing.T) string {
				return marshalReviews(t, []reviewRow{
					makeReview("reviewer-a", "CHANGES_REQUESTED", head, t1),
					makeReview("reviewer-a", "DISMISSED", head, t2),
				})
			},
			wantReview: "pending",
		},
		{
			name: "old head request has no wake time",
			reviews: func(t *testing.T) string {
				return marshalReviews(t, []reviewRow{makeReview("reviewer-a", "CHANGES_REQUESTED", "oldhead", t1)})
			},
			wantReview: "changes_requested",
		},
		{
			name: "missing commit has no wake time",
			reviews: func(t *testing.T) string {
				return marshalReviews(t, []reviewRow{makeReview("reviewer-a", "CHANGES_REQUESTED", "", t1)})
			},
			wantReview: "changes_requested",
		},
		{
			name: "missing submission time has no wake time",
			reviews: func(t *testing.T) string {
				return marshalReviews(t, []reviewRow{makeReview("reviewer-a", "CHANGES_REQUESTED", head, "")})
			},
			wantReview: "changes_requested",
		},
		{
			name: "missing login has no wake time",
			reviews: func(t *testing.T) string {
				return marshalReviews(t, []reviewRow{makeReview("", "CHANGES_REQUESTED", head, t1)})
			},
			wantReview: "changes_requested",
		},
		{
			name: "blank login has no wake time",
			reviews: func(t *testing.T) string {
				return marshalReviews(t, []reviewRow{makeReview("  \t", "CHANGES_REQUESTED", head, t1)})
			},
			wantReview: "changes_requested",
		},
		{
			name: "commented does not clear active request",
			reviews: func(t *testing.T) string {
				return marshalReviews(t, []reviewRow{
					makeReview("reviewer-a", "CHANGES_REQUESTED", head, t1),
					makeReview("reviewer-a", "COMMENTED", head, t2),
				})
			},
			wantAt: t1, wantReview: "changes_requested",
		},
		{
			name: "saturated review page has no wake time",
			reviews: func(t *testing.T) string {
				rows := make([]reviewRow, 100)
				for i := range rows {
					rows[i] = makeReview(fmt.Sprintf("reviewer-%03d", i), "CHANGES_REQUESTED", head, t1)
				}
				return marshalReviews(t, rows)
			},
			wantReview: "changes_requested",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reviews := tc.reviews(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body string
				switch r.URL.RequestURI() {
				case "/repos/example/app/pulls/12":
					body = `{"state":"open","head":{"sha":"` + head + `"}}`
				case "/repos/example/app/pulls/12/reviews?per_page=100":
					body = reviews
				case "/repos/example/app/commits/abc123/status":
					body = `{"state":"success","total_count":0}`
				case "/repos/example/app/commits/abc123/check-runs?per_page=100&filter=latest":
					body = `{"total_count":0,"check_runs":[]}`
				default:
					t.Errorf("unexpected request: %s", r.URL)
					http.Error(w, "unexpected", http.StatusNotFound)
					return
				}
				_, _ = fmt.Fprint(w, body)
			}))
			defer srv.Close()

			got, err := (githubForge{base: srv.URL, client: srv.Client()}).PRStatus(context.Background(), "example/app", 12)
			if err != nil {
				t.Fatalf("PRStatus: %v", err)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal status: %v", err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("decode status JSON: %v", err)
			}
			var reviewLabel string
			if err := json.Unmarshal(payload["review"], &reviewLabel); err != nil {
				t.Fatalf("decode existing review label: %v", err)
			}
			if reviewLabel != tc.wantReview {
				t.Errorf("review label = %q, want %q", reviewLabel, tc.wantReview)
			}
			value, present := payload["changes_requested_at"]
			if tc.wantAt == "" {
				if present {
					t.Errorf("changes_requested_at unexpectedly present: %s", value)
				}
				return
			}
			if !present {
				t.Fatalf("changes_requested_at missing from JSON: %s", raw)
			}
			var gotAt string
			if err := json.Unmarshal(value, &gotAt); err != nil {
				t.Fatalf("decode changes_requested_at: %v", err)
			}
			if gotAt != tc.wantAt {
				t.Errorf("changes_requested_at = %q, want %q", gotAt, tc.wantAt)
			}
			if strings.TrimSpace(gotAt) != gotAt {
				t.Errorf("changes_requested_at is not normalized: %q", gotAt)
			}
		})
	}
}
