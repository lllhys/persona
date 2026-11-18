package subscriptions

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// A continuation is the bounded, durable position of a multi-page pull. WindowSince and WindowEnd are the original
// run's window; later pages must not move it. NewestAt and the item flags accumulate what terminal completion will
// commit to SinceAt. Cursors and PageHistory include the position requested next, so a repeated or cycling token is
// refused before another page can loop forever.
type Continuation struct {
	WindowSince time.Time  `json:"window_since"`
	WindowEnd   time.Time  `json:"window_end"`
	Cursor      string     `json:"cursor,omitempty"`
	Page        int        `json:"page,omitempty"`
	Pages       int        `json:"pages"`
	Cursors     []string   `json:"cursors,omitempty"`
	PageHistory []int      `json:"page_history,omitempty"`
	NewestAt    *time.Time `json:"newest_at,omitempty"`
	SawItems    bool       `json:"saw_items,omitempty"`
	SawTimed    bool       `json:"saw_timed,omitempty"`
	// Manual continues a Run Now that started while both destinations were off; it never enables ordinary polling.
	Manual bool `json:"manual,omitempty"`
}

const (
	maxContinuationPages  = 100
	continuationHistory   = 8
	continuationCursorMax = 2048
	maxContinuationPage   = 1000000000
)

func cloneContinuation(c *Continuation) *Continuation {
	if c == nil {
		return nil
	}
	out := *c
	out.Cursors = slices.Clone(c.Cursors)
	out.PageHistory = slices.Clone(c.PageHistory)
	if c.NewestAt != nil {
		t := *c.NewestAt
		out.NewestAt = &t
	}
	return &out
}

func cursorPagination(r Recipe) bool { return r.CursorArg != "" || r.NextCursorPath != "" }
func pagePagination(r Recipe) bool   { return r.PageArg != "" || r.NextPagePath != "" }

// checkPagination holds the optional pagination mappings to one unambiguous strategy. A page mapping without an
// explicit next page uses the recipe's requested page size: a full page continues and a short page is terminal.
func checkPagination(r Recipe) *Problem {
	bad := func(message string) *Problem { return &Problem{CodeBadRecipe, message} }
	cursor, page := cursorPagination(r), pagePagination(r)
	if !cursor && !page {
		return nil
	}
	if r.Mode == "snapshot" {
		return bad("Pagination is available for items recipes, not snapshots.")
	}
	if cursor && page {
		return bad("A recipe can use cursor or page pagination, not both.")
	}
	if cursor {
		if r.CursorArg == "" || r.NextCursorPath == "" {
			return bad("Cursor pagination needs both cursor_arg and next_cursor_path.")
		}
		if !keyRE.MatchString(r.CursorArg) {
			return bad("cursor_arg must be a top-level argument name.")
		}
		return nil
	}
	if r.PageArg == "" {
		return bad("Page pagination needs page_arg.")
	}
	if !keyRE.MatchString(r.PageArg) {
		return bad("page_arg must be a top-level argument name.")
	}
	if r.NextPagePath == "" {
		args, err := fillArgs(r.Args, vars(time.Now(), time.UTC))
		if err == nil && pageLimit(args) == 0 {
			return bad("Page pagination without next_page_path needs a numeric page-size argument.")
		}
	}
	return nil
}

func checkContinuation(r Recipe, c *Continuation) *Problem {
	if c == nil {
		return nil
	}
	bad := func(message string) *Problem { return &Problem{CodePagination, message} }
	if c.WindowSince.IsZero() || c.WindowEnd.IsZero() || c.WindowEnd.Before(c.WindowSince) {
		return bad("The saved pagination window is invalid; edit the recipe to start again.")
	}
	if c.Pages <= 0 || len(c.Cursors) > continuationHistory || len(c.PageHistory) > continuationHistory {
		return bad("The saved pagination position is invalid; edit the recipe to start again.")
	}
	if c.Pages >= maxContinuationPages {
		return bad(fmt.Sprintf("The source exceeded the %d-page safety limit; edit the recipe to start again.", maxContinuationPages))
	}
	switch {
	case cursorPagination(r) && !pagePagination(r):
		if c.Cursor == "" || len(c.Cursor) > continuationCursorMax || c.Page != 0 || len(c.PageHistory) != 0 {
			return bad("The saved cursor is invalid; edit the recipe to start again.")
		}
		for _, token := range c.Cursors {
			if len(token) > continuationCursorMax {
				return bad("The saved cursor history is invalid; edit the recipe to start again.")
			}
		}
	case pagePagination(r) && !cursorPagination(r):
		if c.Page <= 0 || c.Page > maxContinuationPage || c.Cursor != "" || len(c.Cursors) != 0 {
			return bad("The saved page is invalid; edit the recipe to start again.")
		}
	default:
		return bad("The saved pagination position does not fit the recipe; edit the recipe to start again.")
	}
	return nil
}

// fillPullArgs fills the time window and applies the requested cursor or page without converting either through a
// string template. Page arguments therefore remain JSON integers under the tool's input schema.
func fillPullArgs(r Recipe, since time.Time, loc *time.Location, c *Continuation) (map[string]any, error) {
	args, err := fillArgs(r.Args, vars(since, loc))
	if err != nil {
		return nil, err
	}
	if err := applyPaginationArgs(r, args, c); err != nil {
		return nil, err
	}
	return args, nil
}

func applyPaginationArgs(r Recipe, args map[string]any, c *Continuation) error {
	if c != nil {
		switch {
		case cursorPagination(r):
			args[r.CursorArg] = c.Cursor
		case pagePagination(r):
			args[r.PageArg] = c.Page
		default:
			return fmt.Errorf("the recipe has no pagination mapping for its saved position")
		}
		return nil
	}
	if !pagePagination(r) {
		return nil
	}
	page := 1
	if v, ok := args[r.PageArg]; ok && v != nil {
		n, ok := positivePage(v)
		if !ok {
			return fmt.Errorf("%s must be a positive integer page number", r.PageArg)
		}
		page = n
	}
	args[r.PageArg] = page
	return nil
}

func positivePage(v any) (int, bool) {
	var n int64
	switch x := v.(type) {
	case json.Number:
		var err error
		n, err = x.Int64()
		if err != nil {
			return 0, false
		}
	case string:
		var err error
		n, err = strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, false
		}
	case int:
		return x, x > 0 && x <= maxContinuationPage
	case int64:
		n = x
	case float64:
		if x != math.Trunc(x) || x > maxContinuationPage {
			return 0, false
		}
		n = int64(x)
	default:
		return 0, false
	}
	return int(n), n > 0 && n <= maxContinuationPage
}

// nextContinuation reads the next position from a successful result. Missing, null, false or an empty token is the
// terminal page. A page's has-more boolean may be mapped with next_page_path; true requests the following number.
func nextContinuation(r Recipe, res result, args map[string]any, current *Continuation, since, until time.Time, itemCount int) (*Continuation, *Problem) {
	if !cursorPagination(r) && !pagePagination(r) {
		return nil, nil
	}
	next := &Continuation{WindowSince: since, WindowEnd: until, Pages: 1}
	if current != nil {
		next = cloneContinuation(current)
		next.Pages++
	}
	bad := func(message string) (*Continuation, *Problem) { return nil, &Problem{CodePagination, message} }
	if cursorPagination(r) {
		v, ok := at(res.json, r.NextCursorPath)
		if !ok || v == nil {
			return nil, nil
		}
		token, isString := v.(string)
		if !isString {
			return bad("The source returned a cursor that is not a string; edit the pagination recipe.")
		}
		if strings.TrimSpace(token) == "" {
			return nil, nil
		}
		if len(token) > continuationCursorMax {
			return bad("The source returned an oversized cursor; edit the pagination recipe.")
		}
		if current == nil {
			if initial, ok := args[r.CursorArg].(string); ok && strings.TrimSpace(initial) != "" {
				if len(initial) > continuationCursorMax {
					return bad("The initial cursor is oversized; edit the pagination recipe.")
				}
				next.Cursors = append(next.Cursors, initial)
			}
		}
		if slices.Contains(next.Cursors, token) {
			return bad("The source repeated a pagination cursor; edit the pagination recipe.")
		}
		next.Cursors = append(next.Cursors, token)
		if len(next.Cursors) > continuationHistory {
			next.Cursors = slices.Clone(next.Cursors[len(next.Cursors)-continuationHistory:])
		}
		next.Cursor, next.Page, next.PageHistory = token, 0, nil
		return next, nil
	}

	page, ok := positivePage(args[r.PageArg])
	if !ok {
		return bad("The current page number is invalid; edit the pagination recipe.")
	}
	nextPage := 0
	if r.NextPagePath == "" {
		if limit := pageLimit(args); limit > 0 && itemCount >= limit {
			nextPage = page + 1
		}
	} else {
		v, found := at(res.json, r.NextPagePath)
		if !found || v == nil {
			return nil, nil
		}
		if more, isBool := v.(bool); isBool {
			if more {
				nextPage = page + 1
			}
		} else if s, isString := v.(string); isString && strings.TrimSpace(s) == "" {
			return nil, nil
		} else {
			n, valid := positivePage(v)
			if !valid {
				return bad("The source returned an invalid next page; edit the pagination recipe.")
			}
			nextPage = n
		}
	}
	if nextPage == 0 {
		return nil, nil
	}
	if nextPage > maxContinuationPage {
		return bad("The source returned an out-of-range next page; edit the pagination recipe.")
	}
	if current == nil {
		next.PageHistory = append(next.PageHistory, page)
	}
	if slices.Contains(next.PageHistory, nextPage) {
		return bad("The source repeated a pagination page; edit the pagination recipe.")
	}
	next.PageHistory = append(next.PageHistory, nextPage)
	if len(next.PageHistory) > continuationHistory {
		next.PageHistory = slices.Clone(next.PageHistory[len(next.PageHistory)-continuationHistory:])
	}
	next.Page, next.Cursor, next.Cursors = nextPage, "", nil
	return next, nil
}
