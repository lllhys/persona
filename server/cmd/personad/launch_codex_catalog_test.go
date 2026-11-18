package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// answeringNode listens on the node socket under home and answers every request with answer, as a node answers
// launcher.env.
func answeringNode(t *testing.T, home string, answer map[string]any) {
	t.Helper()
	dir := filepath.Join(home, ".persona")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", filepath.Join(dir, "node.sock"))
	if err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(answer)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				if _, err := bufio.NewReader(c).ReadString('\n'); err == nil {
					c.Write(append(line, '\n'))
				}
			}()
		}
	}()
	t.Cleanup(func() { l.Close() })
}

// `personad launch` of a managed Codex provider gives codex the gateway's model catalog when the node found one, and
// otherwise exactly the arguments it always gave; arguments that pick their own provider keep Persona's out. When the
// node checked another codex than the one this launch runs (another PATH), this one is asked its version, and one too
// old for a catalog gets none. The codex here is a fake that writes the arguments it got. The child is this test binary
// running main().
func TestLaunchCodexGatewayCatalog(t *testing.T) {
	if os.Getenv("PERSONAD_TEST_LAUNCH_CODEX") == "1" {
		var args []string
		_ = json.Unmarshal([]byte(os.Getenv("PERSONAD_TEST_LAUNCH_ARGS")), &args)
		os.Args = append([]string{"personad", "launch", "gw"}, args...)
		main()
		os.Exit(0)
	}
	if runtime.GOOS == "windows" {
		t.Skip("a shell script as codex")
	}
	provider := `model_providers.persona-launcher={name="Persona launcher",base_url="http://127.0.0.1:9/v1",wire_api="responses",env_key="OPENAI_API_KEY"`
	withCatalog := []string{"-c", `model_provider="persona-launcher"`, "-c", provider + `,model_catalog_url="http://127.0.0.1:9/v1/models"}`, "-m", "example-big", "app-server"}
	without := []string{"-c", `model_provider="persona-launcher"`, "-c", provider + "}", "-m", "example-big", "app-server"}
	for _, c := range []struct {
		name    string
		catalog string // what the node answers: "" is an older node, or a gateway with no catalog, or a probe that failed
		checked string // the codex the node checked: "codex" is this launch's, "other" another file
		version string // what this launch's codex says it is
		args    []string
		want    []string
		asked   bool // whether this launch ran codex --version
	}{
		{"catalog", "http://127.0.0.1:9/v1/models", "codex", "0.160.0", []string{"app-server"}, withCatalog, false},
		{"no catalog", "", "", "0.167.0", []string{"app-server"}, without, false},
		{"its own provider", "http://127.0.0.1:9/v1/models", "codex", "0.167.0", []string{"-c", "model_provider=mine", "app-server"},
			[]string{"-m", "example-big", "-c", "model_provider=mine", "app-server"}, false},
		{"another codex that reads it", "http://127.0.0.1:9/v1/models", "other", "0.167.0", []string{"app-server"}, withCatalog, true},
		{"another codex too old for it", "http://127.0.0.1:9/v1/models", "other", "0.160.0", []string{"app-server"}, without, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := shortHome(t)
			bin := filepath.Join(home, "bin")
			if err := os.MkdirAll(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			got, asked := filepath.Join(home, "codex-args.txt"), filepath.Join(home, "codex-version-asked")
			codex := filepath.Join(bin, "codex")
			script := "#!/bin/sh\nif [ \"$1\" = --version ]; then touch '" + asked + "'; echo 'codex-cli " + c.version + "'; exit 0; fi\n" +
				"for a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + got + "'\n"
			if err := os.WriteFile(codex, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			other := filepath.Join(home, "other-codex")
			if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			answer := map[string]any{"agent": "codex", "model": "example-big",
				"env": []string{"OPENAI_BASE_URL=http://127.0.0.1:9/v1", "OPENAI_API_KEY=sk-test"}}
			if c.catalog != "" {
				answer["codex_catalog_url"] = c.catalog
				answer["codex_catalog_codex"] = map[string]string{"codex": codex, "other": other}[c.checked]
			}
			answeringNode(t, home, answer)
			argsJSON, _ := json.Marshal(c.args)
			cmd := exec.Command(os.Args[0], "-test.run=^TestLaunchCodexGatewayCatalog$")
			env := []string{"PERSONAD_TEST_LAUNCH_CODEX=1", "PERSONAD_TEST_LAUNCH_ARGS=" + string(argsJSON)}
			for _, kv := range launchEnv(home) {
				if !strings.HasPrefix(kv, "PATH=") {
					env = append(env, kv)
				}
			}
			cmd.Env = append(env, "PATH="+bin+":/usr/bin:/bin")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("personad launch: %v\n%s", err, out)
			}
			b, err := os.ReadFile(got)
			if err != nil {
				t.Fatal(err)
			}
			if args := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"); !slices.Equal(args, c.want) {
				t.Fatalf("codex got\n%q\nwant\n%q", args, c.want)
			}
			if _, err := os.Stat(asked); (err == nil) != c.asked {
				t.Fatalf("ran codex --version: %v, want %v", err == nil, c.asked)
			}
		})
	}
}
