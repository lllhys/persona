package node

import (
	"os"
	"path/filepath"
	"testing"
)

// Commands the assistant ran that still asked on the phone after #8119 although they write nothing to Feishu
// (#8148): lark-cli named in a here-document program, a one-command group or a note, a command path from ~ or $HOME,
// arithmetic read as a subshell. And the writes that must still ask. Only the classifier and the approval adapters
// see these strings; nothing runs them. The conversation is in bypass mode (assertAssistantBypassClassification).
func TestLarkProgramsAsWritten(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, command string
		cards         int
	}{
		// A program in a quoted here-document is read as the same code given with -c.
		{"python stdin program", "python3 -I - <<'EOF'\nprint('lark-cli profile')\nEOF", 0},
		{"heredoc edit of a note", "cd ~/reports && python3 -I - <<'EOF'\np='plan.md'\ns=open(p,encoding='utf-8').read()\na=\"old line\"\nassert s.count(a)==1, s.count(a)\ns=s.replace(a,a+\" (the gate took `lark-cli` for a write; checked with docker exec)\")\nopen(p,'w',encoding='utf-8').write(s)\nEOF", 0},
		{"git diff, heredoc edit, git diff", "cd ~/reports && git diff --stat brief.md; python3 - <<'EOF'\np='brief.md'\nL=open(p).read().split('\\n')\ndef rep(i, old, new):\n    assert old in L[i], (i+1, L[i][:120])\n    L[i]=L[i].replace(old,new)\nrep(3,'send with the bot','send with the bot (`lark-cli --as bot`)')\nopen(p,'w').write('\\n'.join(L))\nEOF\ngrep -c 'bot' brief.md; git diff --stat", 0},
		{"heredoc reader then a process list", "d=/tmp/tasks; date '+now %H:%M:%S'; timeout 20 python3 -I - \"$d/a.output\" <<'EOF'\nimport json,sys\nrows=[]\nfor line in open(sys.argv[1],encoding='utf-8',errors='replace'):\n    try:o=json.loads(line)\n    except Exception: continue\n    rows.append(o)\nfor o in rows[-4:]:\n    print('---',o.get('type'))\nEOF\necho ---PROCS; ps -eo pid,etimes,args 2>/dev/null | grep -e 'some-cli' -e 'lark-cli' | grep -v grep | cut -c1-200 | head", 0},

		// A group of one command is judged as that command.
		{"which and a help in a subshell", `which some-cli lark-cli 2>/dev/null; echo "---"; (timeout 10 some-cli --help 2>&1 | head -30) ; ls ~/skills/ 2>/dev/null | grep -i -E "feishu|lark" | head`, 0},

		// Notes: a program runs one only when it names it; in a sequence only after it is written.
		{"note then git", `echo '- send with lark-cli --as bot' >> ~/reports/notes.md; git diff --stat`, 0},
		{"program then a file written", `git status --short; echo 'send:; lark-cli im +messages-send' > /tmp/x.mk`, 0},

		// A command path from the home directory, also with its output piped.
		{"tilde command variable into head", `ML=~/tools/notes.py; timeout 60 $ML --help 2>&1 | head -40`, 0},
		{"tilde command variable into cut", `ML=~/tools/notes.py; timeout 90 $ML list 2>&1 | cut -c1-260 | head -60`, 0},
		{"HOME command variable", `T=$HOME/.cache/tool/bin/some-cli; timeout 60 $T query x 2>&1 | head`, 0},
		{"quoted HOME command variable", `T="$HOME/.cache/tool/bin/some-cli"; $T query x`, 0},

		// $((…)) is arithmetic, not a subshell.
		{"arithmetic around date", `OLD=abc; end=$(( $(date +%s) + 3000 )); while [ $(date +%s) -lt $end ]; do o=$(timeout 25 ssh -o BatchMode=yes host.example 'uptime'); if [ -n "$o" ] && ! echo "$o" | grep -q $OLD; then echo DONE; exit 0; fi; sleep 20; done; echo TIMEOUT`, 0},

		// Still asked: programs that run something, text that reaches one, names the gate cannot pin down.
		{"heredoc program starts a process", "python3 - <<'EOF'\nimport subprocess\nsubprocess.run(['lark-cli', 'im', '+messages-send'])\nEOF", 1},
		{"heredoc program calls a name from nowhere", "python3 - <<'EOF'\nrun(['lark-cli', 'im', '+messages-send'])\nEOF", 1},
		{"inline code calls a name from nowhere", `python3 -c 'run("lark-cli im +messages-send")'`, 1},
		{"heredoc program reaches os through a module", "python3 - <<'EOF'\nimport pathlib\npathlib.os.system('lark-cli im +messages-send')\nEOF", 1},
		{"heredoc program reaches a private module", "python3 - <<'EOF'\nimport random\nrandom._os.system('lark-cli im +messages-send')\nEOF", 1},
		{"heredoc program imports a module that runs programs", "python3 - <<'EOF'\nimport shutil\nprint('lark-cli')\nEOF", 1},
		{"heredoc program imports everything", "python3 - <<'EOF'\nfrom json import *\nprint('lark-cli')\nEOF", 1},
		{"heredoc program with dynamic lookup", "python3 - <<'EOF'\nprint(vars()['lark-cli'])\nEOF", 1},
		{"node program naming the CLI, by its words as before", "node - <<'EOF'\nconsole.log('lark-cli')\nEOF", 1},
		{"heredoc shell text given to python", "python3 - <<'EOF'\nlark-cli im +messages-send\nEOF", 1},
		{"unquoted heredoc substitution", "python3 - <<EOF\nprint('$(lark-cli im +messages-send)')\nEOF", 1},
		{"interactive python reads on", "python3 -i - <<'EOF'\nprint('lark-cli')\nEOF", 1},
		{"heredoc program piped to a shell", "python3 - <<'EOF' | sh\nprint('lark-cli im +messages-send')\nEOF", 1},
		{"heredoc program writes a Makefile, then make", "python3 - <<'EOF'\nopen('Makefile','w').write('all: ; lark-cli im +messages-send\\n')\nEOF\nmake", 1},
		{"heredoc program writes through a variable path", "python3 - <<'EOF'\nimport sys\nopen(sys.argv[0],'w').write('lark-cli im +messages-send')\nEOF\nmake", 1},
		{"heredoc program writes a note a program names", "python3 - <<'EOF'\nopen('x.md','w').write('all: ; lark-cli im +messages-send\\n')\nEOF\nmake -f x.md", 1},
		{"note a program names", `echo 'all:; lark-cli im +messages-send' > x.md; make -f x.md`, 1},
		{"trap runs after the write", "trap 'make' EXIT; python3 - <<'EOF'\nopen('Makefile','w').write('all: ; lark-cli im +messages-send\\n')\nEOF", 1},
		{"send in a subshell", `(cd /tmp && lark-cli im +messages-send --text hi)`, 1},
		{"send in a group", `{ lark-cli im +messages-send --text hi; }`, 1},
		{"group that assigns", `A=lark-cli; (A=/bin/ls); $A im +messages-send --text hi`, 1},
		{"home path to lark-cli", `L=~/.local/bin/lark-cli; $L im +messages-send --text hi`, 1},
		{"HOME set in the command", `HOME=/tmp/x; T=$HOME/tool; $T im +messages-send --text hi`, 1},
		{"assignment in a pipe", `T=~/tools/a.py | true; $T im +messages-send`, 1},
		{"last command of a pipe reads the variable", `T=~/tools/a.py; echo lark-cli | read T; $T im +messages-send`, 1},
		{"dynamic command inside arithmetic", `n=$(( $($CMD) + 1 ))`, 1},
		{"send inside arithmetic", `n=$(( $(lark-cli im +messages-send --text hi | wc -l) + 1 ))`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) { assertAssistantBypassClassification(t, tc.command, tc.cards) })
	}
}
