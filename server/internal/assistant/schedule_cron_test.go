package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func mustCron(t *testing.T, expr string) cronRule {
	t.Helper()
	r, err := parseCron(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	return r
}

// runsAfter are the n runs of expr after `after`, as the wall clock in loc shows them ("2026-11-12 09:00 Thu").
func runsAfter(t *testing.T, expr string, after time.Time, loc *time.Location, n int) []string {
	t.Helper()
	var out []string
	for _, at := range mustCron(t, expr).nextRuns(after, loc, n) {
		out = append(out, at.In(loc).Format("2006-01-02 15:04 Mon"))
	}
	return out
}

func sameRuns(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("%s:\n got  %q\n want %q", what, got, want)
	}
}

func TestParseCron(t *testing.T) {
	for _, c := range []struct{ in, expr, fields string }{
		{"*/30 9-18 * * 1-5", "*/30 9-18 * * 1-5", "*/30 9-18 * * 1-5"},
		{"  0   10 1 * *  ", "0 10 1 * *", "0 10 1 * *"},
		{"0 9 * * 7", "0 9 * * 7", "0 9 * * 0"},                   // 7 is Sunday too
		{"0 9 * * 5-7", "0 9 * * 5-7", "0 9 * * 5-6,0"},           // as is a range up to it
		{"0 9 * * MON-FRI", "0 9 * * MON-FRI", "0 9 * * MON-FRI"}, // names
		{"0 9 * JAN,JUL *", "0 9 * JAN,JUL *", "0 9 * JAN,JUL *"},
		{"@daily", "@daily", "0 0 * * *"},
		{"@HOURLY", "@HOURLY", "0 * * * *"},
		{"@weekly", "@weekly", "0 0 * * 0"},
		{"@monthly", "@monthly", "0 0 1 * *"},
		{"@yearly", "@yearly", "0 0 1 1 *"},
		{"@annually", "@annually", "0 0 1 1 *"},
		{"@midnight", "@midnight", "0 0 * * *"},
		{"*/5 * * * *", "*/5 * * * *", "*/5 * * * *"}, // the shortest gap allowed
		{"0,55 * * * *", "0,55 * * * *", "0,55 * * * *"},
	} {
		r, err := parseCron(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if r.expr != c.expr || strings.Join(r.fields[:], " ") != c.fields {
			t.Fatalf("%q: stored %q, read as %q", c.in, r.expr, strings.Join(r.fields[:], " "))
		}
	}
}

func TestParseCronRefusesWithAReason(t *testing.T) {
	for _, c := range []struct{ in, key, says string }{
		{"", "cron_empty", "0 9 * * 1-5"},
		{"   ", "cron_empty", "minute hour day-of-month month day-of-week"},
		{"0 9 * *", "cron_fields", "has 4 fields"},
		{"0 0 9 * * *", "cron_fields", "seconds aren’t supported"},
		{"61 * * * *", "cron_field", `the minute (0–59) can’t be "61"`},
		{"0 24 * * *", "cron_field", `the hour (0–23) can’t be "24"`},
		{"0 9 32 * *", "cron_field", `day of month (1–31) can’t be "32"`},
		{"0 9 * 13 *", "cron_field", `month (1–12 or JAN–DEC) can’t be "13"`},
		{"0 9 * * 8", "cron_field", `day of week (0–7 or SUN–SAT, 0 and 7 are Sunday) can’t be "8"`},
		{"0 9 * * FUN", "cron_field", `can’t be "FUN"`},
		{"x 9 * * *", "cron_field", `the minute (0–59) can’t be "x"`},
		{"0 18-9 * * *", "cron_field", `the hour (0–23) can’t be "18-9"`},
		{"*/3 * * * *", "cron_often", "runs 3 minutes apart"},
		{"0-4 * * * *", "cron_often", "runs 1 minutes apart"},
		{"0,58 9,10 * * *", "cron_often", "runs 2 minutes apart"}, // 9:58, then 10:00
		{"@every 5m", "cron_every", "use an interval"},
		{"@reboot", "cron_alias", "isn’t a cron alias"},
		{"@daily 9", "cron_alias", "isn’t a cron alias"},
		{"TZ=Asia/Tokyo 0 9 * * *", "cron_zone", "run in your time zone"},
		{"CRON_TZ=UTC 0 9 * * *", "cron_zone", "Leave out TZ="},
		{"0 0 30 2 *", "cron_never", "never runs"},
	} {
		_, err := parseCron(c.in)
		var m *i18n.Message
		if !errors.As(err, &m) || m.Key != "assistant-schedule."+c.key {
			t.Fatalf("%q: want %s, got %v", c.in, c.key, err)
		}
		if !strings.Contains(err.Error(), c.says) { // the tools' English
			t.Fatalf("%q: %q does not say %q", c.in, err.Error(), c.says)
		}
		for _, l := range []i18n.Lang{i18n.ZhHans, i18n.Ja} { // and the apps' languages
			if text := m.Text(l); text == "" || text == m.Key || strings.Contains(text, "{") {
				t.Fatalf("%q in %s: %q", c.in, l, text)
			}
		}
	}
}

// The user's examples, on Taipei's wall clock: every 30 minutes 9:00–18:30 on weekdays, and 10:00 on the 1st.
func TestCronNextInTheUsersTimeZone(t *testing.T) {
	tpe := mustZone(t, "Asia/Taipei")
	wed := time.Date(2026, 11, 11, 18, 10, 0, 0, tpe)
	sameRuns(t, "weekdays 9–18", runsAfter(t, "*/30 9-18 * * 1-5", wed, tpe, 4),
		[]string{"2026-11-11 18:30 Wed", "2026-11-12 09:00 Thu", "2026-11-12 09:30 Thu", "2026-11-12 10:00 Thu"})
	fri := time.Date(2026, 11, 13, 18, 30, 0, 0, tpe) // exactly at a run: the next one is strictly after
	sameRuns(t, "over the weekend", runsAfter(t, "*/30 9-18 * * 1-5", fri, tpe, 1), []string{"2026-11-16 09:00 Mon"})
	sameRuns(t, "the 1st", runsAfter(t, "0 10 1 * *", wed, tpe, 3),
		[]string{"2026-12-01 10:00 Tue", "2027-01-01 10:00 Fri", "2027-02-01 10:00 Mon"})

	// The same expression in another zone is another moment: it is read on the user's clock, not the hub's.
	at := mustCron(t, "0 9 * * *").next(wed, tpe)
	if want := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC); !at.Equal(want) {
		t.Fatalf("09:00 in Taipei is 01:00 UTC: %v", at)
	}
	if ny := mustCron(t, "0 9 * * *").next(wed, mustZone(t, "America/New_York")); !ny.Equal(time.Date(2026, 11, 11, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("09:00 in New York: %v", ny)
	}
}

func TestCronNextOverMonthEnds(t *testing.T) {
	tpe := mustZone(t, "Asia/Taipei")
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, tpe)
	// The 31st skips the months without one.
	sameRuns(t, "the 31st", runsAfter(t, "0 10 31 * *", nov, tpe, 4),
		[]string{"2026-12-31 10:00 Thu", "2027-01-31 10:00 Sun", "2027-03-31 10:00 Wed", "2027-05-31 10:00 Mon"})
	// From the last minute of a month to the first day of the next, and of the year.
	sameRuns(t, "the 1st at midnight", runsAfter(t, "@monthly", time.Date(2026, 12, 31, 23, 59, 30, 0, tpe), tpe, 2),
		[]string{"2027-01-01 00:00 Fri", "2027-02-01 00:00 Mon"})
	// February 29th waits for a leap year.
	sameRuns(t, "leap day", runsAfter(t, "0 9 29 2 *", nov, tpe, 2), []string{"2028-02-29 09:00 Tue", "2032-02-29 09:00 Sun"})
	// 28–31: every day at the end of a month, however long it is.
	sameRuns(t, "month ends", runsAfter(t, "0 9 28-31 2 *", time.Date(2027, 2, 1, 0, 0, 0, 0, tpe), tpe, 2),
		[]string{"2027-02-28 09:00 Sun", "2028-02-28 09:00 Mon"})
}

// Day of month and day of week, as Vixie cron reads them: both restricted, either one; one starting with *, both.
func TestCronDayOfMonthOrDayOfWeek(t *testing.T) {
	tpe := mustZone(t, "Asia/Taipei")
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, tpe) // a Sunday
	sameRuns(t, "the 13th or a Friday", runsAfter(t, "0 9 13 * 5", nov, tpe, 6), []string{
		"2026-11-06 09:00 Fri", "2026-11-13 09:00 Fri", "2026-11-20 09:00 Fri", "2026-11-27 09:00 Fri",
		"2026-12-04 09:00 Fri", "2026-12-11 09:00 Fri"})
	sameRuns(t, "the 13th or a Friday, into the next month", runsAfter(t, "0 9 13 * 5", time.Date(2026, 12, 11, 10, 0, 0, 0, tpe), tpe, 2),
		[]string{"2026-12-13 09:00 Sun", "2026-12-18 09:00 Fri"})
	sameRuns(t, "Mondays only", runsAfter(t, "0 9 * * 1", nov, tpe, 2), []string{"2026-11-02 09:00 Mon", "2026-11-09 09:00 Mon"})
	sameRuns(t, "the 1st only", runsAfter(t, "0 9 1 * *", nov, tpe, 2), []string{"2026-11-01 09:00 Sun", "2026-12-01 09:00 Tue"})
	// "*/2" starts with *: odd days that are also Mondays (robfig alone would take either).
	sameRuns(t, "odd-day Mondays", runsAfter(t, "0 9 */2 * 1", nov, tpe, 3),
		[]string{"2026-11-09 09:00 Mon", "2026-11-23 09:00 Mon", "2026-12-07 09:00 Mon"})
	sameRuns(t, "Sunday as 7", runsAfter(t, "0 9 * * 7", nov, tpe, 2), []string{"2026-11-01 09:00 Sun", "2026-11-08 09:00 Sun"})
	sameRuns(t, "the weekend as 6-7", runsAfter(t, "0 9 * * 6-7", time.Date(2026, 11, 2, 0, 0, 0, 0, tpe), tpe, 3),
		[]string{"2026-11-07 09:00 Sat", "2026-11-08 09:00 Sun", "2026-11-14 09:00 Sat"})
}

// A clock change never runs one time twice: the hour a fall-back repeats runs once, a time the clock skips runs
// shifted by the gap, once.
func TestCronNextAcrossClockChanges(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	utc := func(at time.Time) string { return at.UTC().Format("01-02 15:04Z") }
	list := func(expr string, after time.Time, loc *time.Location, n int) []string {
		var out []string
		for _, at := range mustCron(t, expr).nextRuns(after, loc, n) {
			out = append(out, utc(at)+" "+at.In(loc).Format("15:04 MST"))
		}
		return out
	}
	// 2026-11-01: 02:00 EDT goes back to 01:00 EST.
	fall := time.Date(2026, 11, 1, 0, 0, 0, 0, ny)
	sameRuns(t, "01:30 on the fall-back day", list("30 1 * * *", fall, ny, 2),
		[]string{"11-01 05:30Z 01:30 EDT", "11-02 06:30Z 01:30 EST"})
	sameRuns(t, "every 30 minutes over the fall-back", list("*/30 * * * *", fall.Add(45*time.Minute), ny, 4),
		[]string{"11-01 05:00Z 01:00 EDT", "11-01 05:30Z 01:30 EDT", "11-01 07:00Z 02:00 EST", "11-01 07:30Z 02:30 EST"})
	second := time.Date(2026, 11, 1, 6, 10, 0, 0, time.UTC) // 01:10 EST, the hour's second pass
	sameRuns(t, "from the second pass", list("*/30 * * * *", second, ny, 1), []string{"11-01 07:00Z 02:00 EST"})
	// 2026-03-08: 02:00 EST jumps to 03:00 EDT.
	spring := time.Date(2026, 3, 8, 0, 0, 0, 0, ny)
	sameRuns(t, "02:30 on the spring-forward day", list("30 2 * * *", spring, ny, 2),
		[]string{"03-08 07:30Z 03:30 EDT", "03-09 06:30Z 02:30 EDT"})
	sameRuns(t, "every 30 minutes over the spring-forward", list("*/30 * * * *", spring.Add(90*time.Minute), ny, 3),
		[]string{"03-08 07:00Z 03:00 EDT", "03-08 07:30Z 03:30 EDT", "03-08 08:00Z 04:00 EDT"})
	// East of UTC Go picks the second pass of an ambiguous time; the run still takes the first.
	berlin := mustZone(t, "Europe/Berlin") // 2026-10-25: 03:00 CEST goes back to 02:00 CET
	sameRuns(t, "02:30 in Berlin on the fall-back day", list("30 2 * * *", time.Date(2026, 10, 25, 0, 0, 0, 0, berlin), berlin, 2),
		[]string{"10-25 00:30Z 02:30 CEST", "10-26 01:30Z 02:30 CET"})
}

func TestCronGap(t *testing.T) {
	for expr, want := range map[string]int{
		"*/5 * * * *":     5,
		"*/30 9-18 * * *": 30,
		"0 * * * *":       60,
		"0 9 * * *":       24 * 60, // one hour, one minute: no two runs in an hour
		"0,58 9 * * *":    58,      // 9:58, then 9:00 the next day
		"0,55 9,10 * * *": 5,       // 9:55, then 10:00
		"58 */1 * * *":    60,      // */1 is every hour: hh:58, then (hh+1):58
		"55 23,0 * * *":   60,      // 23:55, then 00:55
		"0,30 23,0 * * *": 30,
	} {
		if g := mustCron(t, expr).gap(); g != want {
			t.Fatalf("%s: gap %d, want %d", expr, g, want)
		}
	}
}

func TestCronText(t *testing.T) {
	for _, c := range []struct {
		expr string
		lang i18n.Lang
		want string
	}{
		{"*/30 9-18 * * 1-5", i18n.En, "Every 30 minutes, between 09:00 and 18:59, Monday through Friday"},
		{"0 10 1 * *", i18n.En, "At 10:00, on day 1 of the month"},
		{"@daily", i18n.En, "At 00:00"},
		{"0 9 * * 7", i18n.En, "At 09:00, only on Sunday"},
		{"*/30 9-18 * * 1-5", i18n.ZhHans, "每隔 30 分钟, 在 09:00 和 18:59 之间, 星期一至星期五"},
		{"0 9 */2 * 1", i18n.En, ""}, // Vixie wants both days, the words would read as either
	} {
		if got := mustCron(t, c.expr).text(c.lang); got != c.want {
			t.Fatalf("%s in %s: %q, want %q", c.expr, c.lang, got, c.want)
		}
	}
	if got := mustCron(t, "0 9 * * 1-5").text(i18n.Ja); got == "" {
		t.Fatal("Japanese has words too")
	}
}

// Cron tasks sit beside the other three in one list: checkTask tidies each to its own fields, and what was stored before
// reads as it did.
func TestCronTasksBesideTheOtherRules(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	cronTask := Task{Title: "巡检", Prompt: "看看", Repeat: repeatCron, Cron: "  */30   9-18 * * 1-5 ", Time: "09:00", Weekdays: []int{1},
		EveryMinutes: 30, At: now, Enabled: true}
	if err := checkTask(&cronTask, now); err != nil {
		t.Fatal(err)
	}
	if cronTask.Cron != "*/30 9-18 * * 1-5" || cronTask.Time != "" || cronTask.Weekdays != nil || cronTask.EveryMinutes != 0 || !cronTask.At.IsZero() {
		t.Fatalf("a cron task keeps only its expression: %+v", cronTask)
	}
	bad := Task{Title: "x", Prompt: "y", Repeat: repeatCron, Cron: "*/1 * * * *", Enabled: true}
	if err := checkTask(&bad, now); err == nil || !strings.Contains(err.Error(), "at most every 5 minutes") {
		t.Fatalf("a cron task too often is refused: %v", err)
	}
	none := Task{Title: "x", Prompt: "y", Repeat: repeatCron, Enabled: true}
	if err := checkTask(&none, now); err == nil {
		t.Fatal("a cron task needs an expression")
	}
	for _, other := range []Task{
		{Title: "早报", Prompt: "x", Repeat: repeatDaily, Time: "09:00", Cron: "0 9 * * *", Enabled: true},
		{Title: "巡检", Prompt: "x", Repeat: repeatInterval, EveryMinutes: 30, Cron: "0 9 * * *", Enabled: true},
		{Prompt: "x", Repeat: repeatOnce, At: now.Add(time.Hour), Cron: "0 9 * * *", Enabled: true},
	} {
		if err := checkTask(&other, now); err != nil || other.Cron != "" {
			t.Fatalf("a %s task drops an expression: %+v %v", other.Repeat, other, err)
		}
	}
	// A list stored before cron tasks (no cron field) reads the same, and writes nothing new for them.
	stored := `[{"id":"morning","title":"早报","prompt":"x","repeat":"daily","time":"09:00","weekdays":[1,2,3,4,5],"enabled":true},` +
		`{"id":"p","title":"巡检","prompt":"y","repeat":"interval","every_minutes":30,"at":"2026-11-11T10:30:00Z","enabled":true}]`
	var tasks []Task
	if err := json.Unmarshal([]byte(stored), &tasks); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(tasks)
	if strings.Contains(string(again), `"cron`) || strings.Contains(string(again), `"next_runs"`) {
		t.Fatalf("old tasks gain no cron fields: %s", again)
	}
	tpe := mustZone(t, "Asia/Taipei")
	if n := tasks[0].next(now, tpe); !n.Equal(time.Date(2026, 11, 12, 9, 0, 0, 0, tpe)) {
		t.Fatalf("the daily task: %v", n)
	}
	if n := tasks[1].next(now, tpe); !n.Equal(time.Date(2026, 11, 11, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("the interval task: %v", n)
	}
	cronTask.ID = "c"
	if n := cronTask.next(now, tpe); !n.Equal(time.Date(2026, 11, 11, 18, 30, 0, 0, tpe)) { // 10:00 UTC is 18:00 in Taipei
		t.Fatalf("the cron task: %v", n)
	}
	// Changing the expression, or switching the task on, arms it as a daily one is armed.
	on := cronTask
	rearm(cronTask, &on, now)
	if !on.ArmedAt.IsZero() {
		t.Fatal("nothing changed: not rearmed")
	}
	on.Cron = "0 9 * * *"
	if rearm(cronTask, &on, now); on.ArmedAt.IsZero() {
		t.Fatal("another expression rearms it")
	}
}

// The scheduler runs a cron task at its time, in the user's time zone, once per time; a time missed while the hub was
// down runs if it is less than 30 minutes ago and is skipped and recorded otherwise.
func TestSchedulerRunsCronTasks(t *testing.T) {
	f := readyHub()
	f.settings = &Settings{Timezone: "Asia/Taipei"}
	h := newStorelessService(t, f)
	tpe := mustZone(t, "Asia/Taipei")
	at := func(d, hh, mm int) time.Time { return time.Date(2026, 11, d, hh, mm, 0, 0, tpe) }
	armed := at(11, 8, 0) // created Wednesday 08:00
	setTasks(h,
		Task{ID: "c", Title: "巡检", Prompt: "看看部署", Repeat: repeatCron, Cron: "*/30 9-18 * * 1-5", Enabled: true, ArmedAt: armed},
		Task{ID: "m", Title: "月报", Prompt: "写月报", Repeat: repeatCron, Cron: "0 10 1 * *", Enabled: true, ArmedAt: armed},
		Task{ID: "r", Title: "早报", Prompt: "早报", Repeat: repeatDaily, Time: "09:00", Enabled: true, ArmedAt: armed})

	if next := h.nextDue(context.Background(), at(11, 8, 10)); !next.Equal(at(11, 9, 0)) {
		t.Fatalf("the scheduler wakes for 09:00: %v", next)
	}
	h.runDue(context.Background(), at(11, 8, 59))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.IsZero() {
		t.Fatalf("not yet: %+v", c)
	}
	// 09:00:20: both the cron task and the daily one run; the cron task's run is due 09:00.
	nine := at(11, 9, 0).Add(20 * time.Second)
	h.runDue(context.Background(), nine)
	h.sched.inflight.Wait()
	runs := waitRuns(t, h, 2)
	var cronRun *ScheduleRun
	for i := range runs {
		if runs[i].RefID == "c" {
			cronRun = &runs[i]
		}
	}
	if cronRun == nil || !cronRun.Due.Equal(at(11, 9, 0)) || cronRun.Status != runOK || cronRun.Kind != "routine" {
		t.Fatalf("the cron task ran at 09:00: %+v", runs)
	}
	sentPrompt := false
	f.mu.Lock()
	for _, p := range f.sends {
		if text, _ := p["text"].(string); strings.Contains(text, "看看部署") && strings.Contains(text, "⏰ 巡检（定时任务）") {
			sentPrompt = true
		}
	}
	f.mu.Unlock()
	if !sentPrompt {
		t.Fatal("its run sends its prompt as a scheduled task's")
	}
	// Not again in the same half hour; again at 09:30.
	h.runDue(context.Background(), at(11, 9, 10))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(nine) {
		t.Fatalf("one run per time: %+v", c)
	}
	h.runDue(context.Background(), at(11, 9, 30))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(at(11, 9, 30)) {
		t.Fatalf("09:30 runs: %+v", c)
	}
	// Down from 09:40 to 10:20: 10:00 is missed, the latest time (10:00) is less than 30 minutes ago and runs once now.
	h.runDue(context.Background(), at(11, 10, 20))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(at(11, 10, 20)) || c.LastStatus == runSkipped {
		t.Fatalf("a time missed by 20 minutes runs late: %+v", c)
	}
	// Down from 10:20 to 12:45 on a task whose last time is long past: 12:30 is 15 minutes ago and runs.
	h.runDue(context.Background(), at(11, 12, 45))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(at(11, 12, 45)) || c.LastStatus == runSkipped {
		t.Fatalf("the latest missed time runs: %+v", c)
	}
	// Saturday is not a weekday.
	sc := h.loadSchedule(context.Background())
	sc.mu.Lock()
	sc.tasks[0].LastRun = at(13, 18, 30)
	sc.mu.Unlock()
	h.runDue(context.Background(), at(14, 9, 0))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(at(13, 18, 30)) {
		t.Fatalf("no run on Saturday: %+v", c)
	}
	// Switched off, it does not run on Monday either.
	sc.mu.Lock()
	sc.tasks[0].Enabled, sc.tasks[2].Enabled = false, false
	sc.tasks[1].LastRun = time.Date(2026, 11, 1, 10, 0, 5, 0, tpe) // the monthly one ran last month
	sc.mu.Unlock()
	h.runDue(context.Background(), at(16, 9, 0))
	h.sched.inflight.Wait()
	if c, _ := taskByID(h, "c"); !c.LastRun.Equal(at(13, 18, 30)) {
		t.Fatalf("off: %+v", c)
	}
	// The monthly one, a month after its last run, waits for the 1st; down over 10:00 until 11:00 it is skipped and
	// recorded, not sent an hour late, and only once.
	h.runDue(context.Background(), at(30, 23, 0))
	h.sched.inflight.Wait()
	if m, _ := taskByID(h, "m"); m.LastRun.Day() != 1 {
		t.Fatalf("not before the 1st: %+v", m)
	}
	dec := time.Date(2026, 12, 1, 11, 0, 0, 0, tpe)
	h.runDue(context.Background(), dec)
	h.sched.inflight.Wait()
	if m, _ := taskByID(h, "m"); !m.LastRun.Equal(dec) || m.LastStatus != runSkipped {
		t.Fatalf("the monthly run an hour late is skipped: %+v", m)
	}
	h.runDue(context.Background(), dec.Add(time.Minute))
	h.sched.inflight.Wait()
	runs = waitRuns(t, h, 1)
	if last := runs[len(runs)-1]; last.RefID != "m" || last.Status != runSkipped || !last.Due.Equal(time.Date(2026, 12, 1, 10, 0, 0, 0, tpe)) {
		t.Fatalf("one skip, due 10:00: %+v", last)
	}
	skips := 0
	for _, r := range runs {
		if r.RefID == "m" {
			skips++
		}
	}
	if skips != 1 {
		t.Fatalf("skipped once: %+v", runs)
	}
}

func TestCronTaskRoutes(t *testing.T) {
	f := newFakeHub()
	f.settings = &Settings{Timezone: "Asia/Taipei"}
	h := newStorelessService(t, f)
	setTasks(h, Task{ID: "morning", Title: "早报", Prompt: "x", Repeat: repeatDaily, Time: "09:00", Enabled: true})
	type answer struct {
		Code int
		Body *bytes.Buffer
	}
	// As the hub relays an app's request (bus.go serveHTTP): the errors read in its Accept-Language.
	in := func(lang, method, target, body string) answer {
		path, query, _ := strings.Cut(target, "?")
		res, err := serveHTTP(context.Background(), h, assistantwire.HTTPRequest{Method: method, Path: path, Query: query,
			Header: map[string][]string{"Accept-Language": {lang}}, Body: []byte(body)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return answer{res.Status, bytes.NewBuffer(res.Body)}
	}
	do := func(method, target, body string) answer { return in("en", method, target, body) }
	rec := do("POST", "/api/v1/assistant/tasks", `{"title":"巡检","prompt":"看看部署","repeat":"cron","cron":"*/30 9-18 * * 1-5","enabled":true}`)
	var created struct{ Task Task }
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != 200 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	c := created.Task
	if c.Repeat != repeatCron || c.Cron != "*/30 9-18 * * 1-5" || len(c.NextRuns) != cronRuns || !c.NextRun.Equal(c.NextRuns[0]) ||
		c.CronText != "Every 30 minutes, between 09:00 and 18:59, Monday through Friday" {
		t.Fatalf("created: %+v", c)
	}
	tpe := mustZone(t, "Asia/Taipei")
	for _, at := range c.NextRuns {
		if l := at.In(tpe); l.Minute()%30 != 0 || l.Hour() < 9 || l.Hour() > 18 || l.Weekday() == time.Saturday || l.Weekday() == time.Sunday {
			t.Fatalf("a next run off the rule: %v", l)
		}
	}
	if stored, _ := taskByID(h, c.ID); stored.CronText != "" || stored.NextRuns != nil || stored.ArmedAt.IsZero() {
		t.Fatalf("words and next runs are the view's, the task is armed: %+v", stored)
	}
	// A bad expression: 400, saying what is wrong, in the app's language.
	rec = do("POST", "/api/v1/assistant/tasks", `{"title":"x","prompt":"y","repeat":"cron","cron":"0 25 * * *","enabled":true}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `the hour (0–23) can’t be \"25\"`) {
		t.Fatalf("bad: %d %s", rec.Code, rec.Body)
	}
	// An app that does not know cron tasks (an older iOS app sends it back as a one-off without an expression) keeps the rule.
	rec = do("PUT", "/api/v1/assistant/tasks/"+c.ID, `{"title":"巡检","prompt":"看看部署和 CI","repeat":"once","time":"09:00","weekdays":[],"every_minutes":0,"enabled":false}`)
	if rec.Code != 200 {
		t.Fatalf("old app's put: %d %s", rec.Code, rec.Body)
	}
	if stored, _ := taskByID(h, c.ID); stored.Repeat != repeatCron || stored.Cron != "*/30 9-18 * * 1-5" || stored.Enabled || stored.Prompt != "看看部署和 CI" {
		t.Fatalf("the rule stays, the rest changes: %+v", stored)
	}
	// One that does changes it, back to daily as well.
	rec = do("PUT", "/api/v1/assistant/tasks/"+c.ID, `{"title":"巡检","prompt":"y","repeat":"cron","cron":"@hourly","enabled":true}`)
	if stored, _ := taskByID(h, c.ID); rec.Code != 200 || stored.Cron != "@hourly" || !stored.Enabled {
		t.Fatalf("new expression: %d %s", rec.Code, rec.Body)
	}
	rec = do("PUT", "/api/v1/assistant/tasks/"+c.ID, `{"title":"巡检","prompt":"y","repeat":"daily","time":"08:30","cron":"","enabled":true}`)
	if stored, _ := taskByID(h, c.ID); rec.Code != 200 || stored.Repeat != repeatDaily || stored.Cron != "" || stored.Time != "08:30" {
		t.Fatalf("back to daily: %d %+v", rec.Code, stored)
	}
	// The list has the other tasks as they were.
	rec = do("GET", "/api/v1/assistant/tasks", "")
	if !strings.Contains(rec.Body.String(), `"id":"morning"`) || strings.Contains(rec.Body.String(), `"cron"`) {
		t.Fatalf("list: %s", rec.Body)
	}

	// The preview: words, the time zone and the next runs; or what is wrong.
	rec = do("GET", "/api/v1/assistant/tasks/cron?cron="+url.QueryEscape("0 10 1 * *"), "")
	var preview struct {
		Cron, Text, Timezone string
		NextRuns             []time.Time `json:"next_runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil || rec.Code != 200 {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	if preview.Cron != "0 10 1 * *" || preview.Text != "At 10:00, on day 1 of the month" || preview.Timezone != "Asia/Taipei" || len(preview.NextRuns) != cronRuns {
		t.Fatalf("preview: %+v", preview)
	}
	for _, at := range preview.NextRuns {
		if l := at.In(tpe); l.Day() != 1 || l.Hour() != 10 || l.Minute() != 0 {
			t.Fatalf("a preview run off the rule: %v", l)
		}
	}
	rec = do("GET", "/api/v1/assistant/tasks/cron?cron="+url.QueryEscape("*/2 * * * *"), "")
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "at most every 5 minutes") {
		t.Fatalf("preview of a bad one: %d %s", rec.Code, rec.Body)
	}
	rec = in("zh-Hans", "GET", "/api/v1/assistant/tasks/cron?cron="+url.QueryEscape("0 9 * *"), "")
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "cron 表达式是 5 段") {
		t.Fatalf("in Chinese: %d %s", rec.Code, rec.Body)
	}
}

func TestCronInTheTools(t *testing.T) {
	f := newFakeHub()
	f.settings = &Settings{Timezone: "Asia/Taipei"}
	h := newStorelessService(t, f)
	ctx := context.Background()
	out, err := h.toolScheduleRoutine(ctx, "", "", json.RawMessage(`{"title":"月报","prompt":"写月报","cron":"0 10 1 * *"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cron `0 10 1 * *` (At 10:00, on day 1 of the month)", "Asia/Taipei (UTC+08:00)", "next runs 12-01 10:00 Tue, 01-01 10:00 Fri"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the reply says %q: %s", want, out)
		}
	}
	sc := h.loadSchedule(ctx)
	r := sc.tasks[len(sc.tasks)-1]
	if r.Repeat != repeatCron || r.Cron != "0 10 1 * *" || r.CreatedBy != "assistant" || r.ArmedAt.IsZero() {
		t.Fatalf("routine: %+v", r)
	}
	if !strings.Contains(h.describeSchedule(ctx, ""), "cron `0 10 1 * *` (At 10:00, on day 1 of the month), next ") {
		t.Fatalf("list_followups: %s", h.describeSchedule(ctx, ""))
	}
	// Back to a time of day by id; a bad expression changes nothing and says why.
	if _, err := h.toolScheduleRoutine(ctx, "", "", json.RawMessage(`{"id":"`+r.ID+`","time":"09:00"}`)); err != nil {
		t.Fatal(err)
	}
	if r2, _ := taskByID(h, r.ID); r2.Repeat != repeatDaily || r2.Cron != "" || r2.Time != "09:00" {
		t.Fatalf("back to daily: %+v", r2)
	}
	if _, err := h.toolScheduleRoutine(ctx, "", "", json.RawMessage(`{"id":"`+r.ID+`","cron":"0 9 31 2 *"}`)); err == nil || !strings.Contains(err.Error(), "never runs") {
		t.Fatalf("never: %v", err)
	}
	if r2, _ := taskByID(h, r.ID); r2.Repeat != repeatDaily {
		t.Fatalf("a refused change is not saved: %+v", r2)
	}
	if _, err := h.toolScheduleRoutine(ctx, "", "", json.RawMessage(`{"title":"x","prompt":"y","cron":"0 9 * * *","time":"09:00"}`)); err == nil {
		t.Fatal("cron and time together are refused")
	}

	// A repeating follow-up on cron, into this conversation.
	out, err = h.toolScheduleFollowup(ctx, "server", "s1", json.RawMessage(`{"prompt":"看看 CI","title":"CI","cron":"*/30 9-18 * * 1-5"}`))
	if err != nil || !strings.Contains(out, "on cron `*/30 9-18 * * 1-5`") || !strings.Contains(out, "It repeats by itself") {
		t.Fatalf("follow-up: %q %v", out, err)
	}
	fu := sc.tasks[len(sc.tasks)-1]
	if fu.Repeat != repeatCron || fu.SessionID != "s1" || !fu.At.IsZero() || fu.EveryMinutes != 0 || fu.ArmedAt.IsZero() {
		t.Fatalf("follow-up task: %+v", fu)
	}
	for _, bad := range []string{`{"prompt":"x","cron":"0 9 * * *","every":"30m"}`, `{"prompt":"x","cron":"0 9 * * *","at":"2h"}`, `{"prompt":"x","cron":"*/1 * * * *"}`} {
		if _, err := h.toolScheduleFollowup(ctx, "server", "s1", json.RawMessage(bad)); err == nil {
			t.Fatalf("refused: %s", bad)
		}
	}
	// The snapshot lists it with the routines, by its rule.
	_, _, routines, _, _, _ := h.snapshotSchedule(ctx, mustZone(t, "Asia/Taipei"), time.Now())
	found := false
	for _, rt := range routines {
		found = found || strings.HasPrefix(rt.Rule, "cron `*/30 9-18 * * 1-5`")
	}
	if !found {
		t.Fatalf("snapshot: %+v", routines)
	}
}
