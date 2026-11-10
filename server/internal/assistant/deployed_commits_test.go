package assistant

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// writeDeployed writes a deployed-commits file of repo o/r whose first commit is head, then the given lines.
func writeDeployed(t *testing.T, s *Service, head string, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), deployedCommitsName)
	text := deployedCommitsHeader + "\n# repo: o/r\n" + head + " fix: the deployed one (#20)\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	s.deploy.listPaths = []string{p}
	return p
}

func TestParseDeployedCommits(t *testing.T) {
	l := parseDeployedCommits(deployedCommitsHeader + "\n# repo: Owner/Repo.git\n" +
		newHub + " feat: the newest (#12)\n" +
		mergeSHA + " Merge pull request #9 from someone/branch\n" +
		oldHub + " fix: an older one (#12)\n" +
		"not a line\n")
	if l == nil || l.repo != "owner/repo" || l.head != newHub || l.count != 3 {
		t.Fatalf("%+v", l)
	}
	if l.prs[12] != newHub || l.prs[9] != mergeSHA || len(l.prs) != 2 {
		t.Fatalf("prs %+v", l.prs)
	}
	if !l.runs(newHub[:9]) || !l.runs(strings.ToUpper(newHub)) || l.runs(oldHub) || l.runs("cccc") {
		t.Fatal("runs")
	}
	for _, bad := range []string{"", "# repo: o/r\n" + newHub + " x (#1)\n", deployedCommitsHeader + "\n" + newHub + " x (#1)\n",
		deployedCommitsHeader + "\n# repo: o/r\n"} {
		if parseDeployedCommits(bad) != nil {
			t.Errorf("read %q as a file", bad)
		}
	}
}

// GitHub unreachable (the hub's machine has no way out): the deployed-commits file decides. The PR named in the running
// hub's file: deployed. Not named: merged, not deployed, and GitHub is not asked again for a while; once a deploy
// brings a file that names it, deployed.
func TestDeployedCommitsWithoutGitHub(t *testing.T) {
	s, f, posts, _, forge := deployService(t)
	forge.err = errors.New("dial tcp: connect: network is unreachable")
	writeDeployed(t, s, oldHub, mergeSHA+" fix: the task's PR (#9)")
	todo := startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitFor(t, "accepted", func() bool {
		a := askOf(s, todo.ID)
		return a != nil && !a.AcceptedAt.IsZero() && todoNow(s, todo.ID).Stage == "verify"
	})
	if forge.calls != 0 || verifyCards(posts()) != 0 {
		t.Fatalf("%d GitHub calls, %d cards", forge.calls, verifyCards(posts()))
	}

	s2, f2, _, hub2, forge2 := deployService(t)
	forge2.err = errors.New("dial tcp: connect: network is unreachable")
	writeDeployed(t, s2, oldHub, "dddddddddddddddddddddddddddddddddddddddd fix: something else (#8)")
	todo2 := startedTask(t, s2, f2)
	agentTurn(s2, f2, proto.StatusWorking, "", 0)
	agentTurn(s2, f2, proto.StatusDone, deployedReply, 0)
	waitFor(t, "held", func() bool { return todoNow(s2, todo2.ID).Delivery != nil })
	got := todoNow(s2, todo2.ID)
	if got.Delivery.State != deliveryMerged || got.Stage != "merge" || !strings.Contains(got.Delivery.Gate, "还没有这个 PR") {
		t.Fatalf("%q %+v", got.Stage, got.Delivery)
	}
	if askOf(s2, todo2.ID) != nil {
		t.Fatal("asked to verify")
	}
	calls := forge2.calls
	s2.sweepDeploys(time.Now())
	if forge2.calls != calls {
		t.Fatalf("GitHub asked again %d times while it is down", forge2.calls-calls)
	}
	hub2.deploy(newHub)
	writeDeployed(t, s2, newHub, mergeSHA+" fix: the task's PR (#9)", oldHub+" the one before")
	s2.sweepDeploys(time.Now())
	if got := todoNow(s2, todo2.ID); got.Delivery.State != deliveryLive || got.Stage != "verify" {
		t.Fatalf("after the deploy: %q %+v", got.Stage, got.Delivery)
	}
}

// A file of another deploy (its first commit is not the running hub's) is not believed: GitHub decides, and when it
// cannot, nothing says deployed.
func TestStaleDeployedCommitsIgnored(t *testing.T) {
	s, f, _, _, forge := deployService(t)
	forge.err = errors.New("timeout")
	writeDeployed(t, s, newHub, mergeSHA+" fix: the task's PR (#9)") // the hub runs oldHub
	todo := startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitFor(t, "held", func() bool { return todoNow(s, todo.ID).Delivery != nil })
	if got := todoNow(s, todo.ID); got.Delivery.State != deliveryMerged || !strings.Contains(got.Delivery.Gate, "等确认部署") {
		t.Fatalf("%+v", got.Delivery)
	}
}

// The open to-dos' sweep with GitHub unreachable: a PR the file names is deployed (Just finished); one it does not name
// is left as it is (it may be open still), never "merged, not deployed".
func TestSweepPRsWithDeployedCommits(t *testing.T) {
	s, _, _, _, forge := deployService(t)
	forge.err = errors.New("unreachable")
	writeDeployed(t, s, oldHub, mergeSHA+" fix: progress for every task (#9)")
	ctx := t.Context()
	putTodo(s, Todo{ID: "todo_in", Title: "主对话任务栏显示全部进度", Origin: "conv1", CreatedAt: time.Now().Add(-time.Hour)})
	s.setTaskStage(ctx, "todo_in", "review", "https://github.com/o/r/pull/9")
	putTodo(s, Todo{ID: "todo_open", Title: "开着的", Origin: "conv1", CreatedAt: time.Now().Add(-time.Hour)})
	s.setTaskStage(ctx, "todo_open", "review", "https://github.com/o/r/pull/31")
	s.sweepDeploys(time.Now())
	if got := todoNow(s, "todo_in"); got.Delivery == nil || got.Delivery.State != deliveryLive || got.Stage != "deploy" {
		t.Fatalf("named: %q %+v", got.Stage, got.Delivery)
	}
	if got := todoNow(s, "todo_open"); got.Delivery != nil || got.Stage != "review" {
		t.Fatalf("not named: %q %+v", got.Stage, got.Delivery)
	}
}
