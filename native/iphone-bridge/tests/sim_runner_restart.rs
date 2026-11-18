#![cfg(target_os = "macos")]

use std::fs;
use std::io::{BufRead, BufReader, Write};
use std::os::unix::fs::PermissionsExt;
use std::path::PathBuf;
use std::process::{Child, ChildStdin, Command, ExitStatus, Stdio};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::mpsc::{self, Receiver};
use std::thread;
use std::time::{Duration, Instant};

use serde_json::{Value, json};

static NEXT_FIXTURE: AtomicU64 = AtomicU64::new(0);

const XCRUN: &str = r#"#!/bin/bash
set -u
printf '%s\n' "$*" >> "$SIM_RESTART_COMMANDS"
if [ "$1" = simctl ] && [ "$2" = list ]; then
  printf '%s\n' '{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-27-2":[{"udid":"SIM-1","name":"Test Phone","state":"Booted","deviceTypeIdentifier":"com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro"}]}}'
elif [ "$1" = simctl ] && [ "$2" = getenv ]; then
  case "$4" in
    SIMULATOR_MODEL_IDENTIFIER) echo iPhone18,1 ;;
    SIMULATOR_RUNTIME_BUILD_VERSION) echo 25A1 ;;
    SIMULATOR_MAINSCREEN_WIDTH) echo 1206 ;;
    SIMULATOR_MAINSCREEN_HEIGHT) echo 2622 ;;
    SIMULATOR_MAINSCREEN_SCALE) echo 3 ;;
  esac
elif [ "$1" = xcodebuild ]; then
  starts=$(grep -c '^xcodebuild ' "$SIM_RESTART_COMMANDS")
  scratch=
  previous=
  for argument in "$@"; do
    if [ "$previous" = -resultBundlePath ]; then
      scratch=$argument
    fi
    previous=$argument
  done
  scratch=${scratch%/result.xcresult}
  printf '%s %s %s\n' "$starts" "$$" "$scratch" >> "$SIM_RESTART_STARTS"
  export SIM_RESTART_LAUNCH=$starts
  case "$SIM_RESTART_SCENARIO" in
    transient)
      if [ "$starts" -eq 1 ]; then
        exit 42
      fi
      ;;
    delayed)
      if [ "$starts" -eq 1 ]; then
        sleep 16
      fi
      ;;
    disconnect)
      if [ "$starts" -eq 1 ]; then
        export SIM_RESTART_DROP=1
      fi
      ;;
  esac
  exec /usr/bin/python3 -I "$SIM_RESTART_SERVER"
fi
"#;

const SERVER: &str = r#"import json
import os
import socket
import sys

port = int(os.environ["TEST_RUNNER_PERSONA_RUNNER_PORT"])
token = os.environ["TEST_RUNNER_PERSONA_RUNNER_TOKEN"]
launch = int(os.environ["SIM_RESTART_LAUNCH"])
request_log = os.environ["SIM_RESTART_REQUESTS"]
with socket.socket() as listener:
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(("127.0.0.1", port))
    listener.listen()
    while True:
        conn, _ = listener.accept()
        with conn:
            stream = conn.makefile("rwb", buffering=0)
            hello = json.loads(stream.readline())
            if hello.get("token") != token:
                continue
            stream.write(b'{"ok":true}\n')
            for raw in stream:
                request = json.loads(raw)
                with open(request_log, "a") as log:
                    log.write(json.dumps({"launch": launch, "op": request.get("op")}) + "\n")
                if request.get("op") == "quit":
                    sys.exit(0)
                if os.environ.get("SIM_RESTART_DROP") == "1":
                    sys.exit(0)
                response = {"id": request.get("id"), "ok": True, "result": {"launch": launch}}
                stream.write((json.dumps(response) + "\n").encode())
"#;

struct Fixture {
    root: PathBuf,
    runner: PathBuf,
    record: PathBuf,
    starts: PathBuf,
    requests: PathBuf,
}

impl Fixture {
    fn new(scenario: &str) -> Self {
        let serial = NEXT_FIXTURE.fetch_add(1, Ordering::Relaxed);
        let root = std::env::temp_dir().join(format!(
            "persona-sim-restart-{}-{}-{}",
            scenario,
            std::process::id(),
            serial
        ));
        let bin = root.join("bin");
        let runner = root.join("runner");
        fs::create_dir_all(&bin).unwrap();
        fs::create_dir_all(&runner).unwrap();
        fs::write(runner.join("Dummy.xctestrun"), b"").unwrap();
        let server = root.join("runner_server.py");
        fs::write(&server, SERVER).unwrap();
        let xcrun = bin.join("xcrun");
        fs::write(&xcrun, XCRUN).unwrap();
        fs::set_permissions(&xcrun, fs::Permissions::from_mode(0o755)).unwrap();
        let starts = root.join("starts.log");
        let requests = root.join("requests.log");
        let commands = root.join("commands.log");
        for file in [&starts, &requests, &commands] {
            fs::write(file, b"").unwrap();
        }
        Self {
            record: root.join("sim.record"),
            root,
            runner,
            starts,
            requests,
        }
    }

    fn command(&self, subcommand: &str) -> Command {
        let mut command = Command::new(env!("CARGO_BIN_EXE_persona-iphone-bridge"));
        command
            .arg(subcommand)
            .env("PATH", format!("{}:/usr/bin:/bin", self.root.join("bin").display()))
            .env("PERSONA_IPHONE_SIM_RUNNER", &self.runner)
            .env("SIM_RESTART_SERVER", self.root.join("runner_server.py"))
            .env("SIM_RESTART_COMMANDS", self.root.join("commands.log"))
            .env("SIM_RESTART_STARTS", &self.starts)
            .env("SIM_RESTART_REQUESTS", &self.requests);
        command
    }

    fn serve(&self, scenario: &str) -> Serve {
        let paired = self
            .command("pair")
            .args(["--udid", "SIM-1", "--out"])
            .arg(&self.record)
            .output()
            .unwrap();
        assert!(
            paired.status.success(),
            "pair failed: {}",
            String::from_utf8_lossy(&paired.stderr)
        );
        Serve::start(self, scenario)
    }

    fn starts(&self) -> Vec<Start> {
        fs::read_to_string(&self.starts)
            .unwrap()
            .lines()
            .map(|line| {
                let mut fields = line.splitn(3, ' ');
                Start {
                    launch: fields.next().unwrap().parse().unwrap(),
                    pid: fields.next().unwrap().parse().unwrap(),
                    scratch: PathBuf::from(fields.next().unwrap()),
                }
            })
            .collect()
    }

    fn operations(&self, launch: u32) -> Vec<String> {
        fs::read_to_string(&self.requests)
            .unwrap()
            .lines()
            .map(|line| serde_json::from_str::<Value>(line).unwrap())
            .filter(|request| request["launch"] == launch)
            .map(|request| request["op"].as_str().unwrap().to_owned())
            .collect()
    }
}

impl Drop for Fixture {
    fn drop(&mut self) {
        if thread::panicking() {
            eprintln!("failed fixture preserved at {}", self.root.display());
        } else {
            let _ = fs::remove_dir_all(&self.root);
        }
    }
}

#[derive(Debug)]
struct Start {
    launch: u32,
    pid: u32,
    scratch: PathBuf,
}

struct Serve {
    child: Child,
    stdin: Option<ChildStdin>,
    replies: Receiver<Value>,
    reader: Option<thread::JoinHandle<()>>,
    events: Vec<Value>,
    finished: bool,
}

struct Closed {
    status: ExitStatus,
    events: Vec<Value>,
}

impl Serve {
    fn start(fixture: &Fixture, scenario: &str) -> Self {
        let stderr = fs::File::create(fixture.root.join("serve.stderr")).unwrap();
        let mut child = fixture
            .command("serve")
            .args(["--udid", "SIM-1", "--record"])
            .arg(&fixture.record)
            .env("SIM_RESTART_SCENARIO", scenario)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::from(stderr))
            .spawn()
            .unwrap();
        let stdin = child.stdin.take().unwrap();
        let stdout = child.stdout.take().unwrap();
        let (sender, replies) = mpsc::channel();
        let reader = thread::spawn(move || {
            for line in BufReader::new(stdout).lines() {
                let line = line.unwrap();
                if sender.send(serde_json::from_str(&line).unwrap()).is_err() {
                    break;
                }
            }
        });
        let mut serve = Self {
            child,
            stdin: Some(stdin),
            replies,
            reader: Some(reader),
            events: Vec::new(),
            finished: false,
        };
        let ready = serve.next(Duration::from_secs(15));
        assert_eq!(ready["event"], "ready", "serve did not become ready: {ready}");
        serve
    }

    fn next(&mut self, timeout: Duration) -> Value {
        let value = self
            .replies
            .recv_timeout(timeout)
            .unwrap_or_else(|error| panic!("no serve output within {timeout:?}: {error}"));
        if value.get("event").is_some() {
            self.events.push(value.clone());
        }
        value
    }

    fn tap(&mut self, id: &str, timeout_ms: u64) -> Value {
        writeln!(
            self.stdin.as_mut().unwrap(),
            "{}",
            json!({"id": id, "op": "tap", "args": {"x": 1, "y": 1}, "timeout_ms": timeout_ms})
        )
        .unwrap();
        self.stdin.as_mut().unwrap().flush().unwrap();
        let deadline = Instant::now() + Duration::from_millis(timeout_ms + 5000);
        loop {
            let remaining = deadline.saturating_duration_since(Instant::now());
            let value = self.next(remaining);
            if value["id"] == id {
                return value;
            }
            assert!(value.get("event").is_some(), "unexpected serve answer: {value}");
        }
    }

    fn close(mut self) -> Closed {
        self.stdin.take();
        let deadline = Instant::now() + Duration::from_secs(15);
        let status = loop {
            if let Some(status) = self.child.try_wait().unwrap() {
                break status;
            }
            assert!(Instant::now() < deadline, "serve did not exit after stdin EOF");
            thread::sleep(Duration::from_millis(20));
        };
        self.reader.take().unwrap().join().unwrap();
        while let Ok(value) = self.replies.try_recv() {
            if value.get("event").is_some() {
                self.events.push(value);
            }
        }
        self.finished = true;
        Closed {
            status,
            events: self.events.clone(),
        }
    }
}

impl Drop for Serve {
    fn drop(&mut self) {
        if self.finished {
            return;
        }
        self.stdin.take();
        let deadline = Instant::now() + Duration::from_secs(15);
        loop {
            match self.child.try_wait() {
                Ok(Some(_)) => break,
                Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(20)),
                _ => {
                    let _ = self.child.kill();
                    let _ = self.child.wait();
                    break;
                }
            }
        }
        if let Some(reader) = self.reader.take() {
            let _ = reader.join();
        }
    }
}

fn process_alive(pid: u32) -> bool {
    unsafe { libc::kill(pid as i32, 0) == 0 }
}

fn assert_clean(start: &Start) {
    assert!(!process_alive(start.pid), "launch {} is still running", start.launch);
    assert!(
        !start.scratch.exists(),
        "launch {} scratch remains at {}",
        start.launch,
        start.scratch.display()
    );
}

fn assert_stayed_connected(closed: &Closed) {
    assert!(closed.status.success(), "serve exited with {}", closed.status);
    assert!(
        !closed.events.iter().any(|event| event["event"] == "disconnected"),
        "serve disconnected: {:?}",
        closed.events
    );
}

#[test]
fn pre_handshake_exit_is_cleaned_and_the_next_tap_starts_a_replacement() {
    let fixture = Fixture::new("transient");
    let mut serve = fixture.serve("transient");

    let first = serve.tap("1", 5_000);
    let mut initial_starts = fixture.starts();
    assert_eq!(initial_starts.len(), 1);
    let first_start = initial_starts.remove(0);
    let failed_cleaned = !process_alive(first_start.pid) && !first_start.scratch.exists();
    let second = serve.tap("2", 5_000);
    let starts = fixture.starts();
    let replacement_live = starts.get(1).is_some_and(|start| process_alive(start.pid));
    let second_tap_count = fixture.operations(2).iter().filter(|op| *op == "tap").count();
    let closed = serve.close();

    assert_eq!(first["ok"], false);
    assert_eq!(first["error"]["code"], "service_unavailable");
    assert!(first["error"]["message"].as_str().unwrap().contains("exit status: 42"));
    assert_eq!(second["ok"], true, "the failed runner was not replaced: {second}");
    assert_eq!(second["result"], json!({}));
    assert_eq!(
        starts.len(),
        2,
        "each recovery request must start exactly one replacement"
    );
    assert!(
        failed_cleaned,
        "the exited launch was not cleaned before the first error returned"
    );
    assert_clean(&first_start);
    assert!(replacement_live, "the healthy replacement was killed while cached");
    assert_eq!(second_tap_count, 1, "the first tap must not be replayed automatically");
    assert_eq!(fixture.operations(1), Vec::<String>::new());
    assert_eq!(fixture.operations(2), ["tap", "quit"]);
    assert_clean(&starts[1]);
    assert_stayed_connected(&closed);
}

#[test]
fn a_slow_live_start_stays_cached() {
    let fixture = Fixture::new("delayed");
    let mut serve = fixture.serve("delayed");

    let first = serve.tap("1", 22_000);
    let initial_starts = fixture.starts();
    assert_eq!(initial_starts.len(), 1);
    let start = &initial_starts[0];
    let live_after_timeout = process_alive(start.pid);
    let scratch_after_timeout = start.scratch.exists();
    let second = serve.tap("2", 7_000);
    let starts = fixture.starts();
    let live_after_success = process_alive(start.pid);
    let tap_count = fixture.operations(1).iter().filter(|op| *op == "tap").count();
    let closed = serve.close();

    assert_eq!(first["ok"], false);
    assert_eq!(first["error"]["code"], "service_unavailable");
    assert!(first["error"]["message"].as_str().unwrap().contains("still starting"));
    assert!(
        live_after_timeout,
        "a live slow process was killed at the readiness timeout"
    );
    assert!(scratch_after_timeout, "a live slow runner's scratch was removed");
    assert_eq!(
        second["ok"], true,
        "the cached slow runner did not become ready: {second}"
    );
    assert_eq!(second["result"], json!({}));
    assert_eq!(starts.len(), 1, "a live slow start must not be replaced");
    assert!(
        live_after_success,
        "the live runner was killed after its successful call"
    );
    assert_eq!(tap_count, 1, "only the second tap reaches the delayed runner");
    assert_eq!(fixture.operations(1), ["tap", "quit"]);
    assert_clean(start);
    assert_stayed_connected(&closed);
}

#[test]
fn an_established_runner_disconnect_is_replaced_without_a_serve_disconnect() {
    let fixture = Fixture::new("disconnect");
    let mut serve = fixture.serve("disconnect");

    let first = serve.tap("1", 5_000);
    let mut initial_starts = fixture.starts();
    assert_eq!(initial_starts.len(), 1);
    let first_start = initial_starts.remove(0);
    let failed_cleaned = !process_alive(first_start.pid) && !first_start.scratch.exists();
    let second = serve.tap("2", 5_000);
    let starts = fixture.starts();
    let first_taps = fixture.operations(1).iter().filter(|op| *op == "tap").count();
    let second_taps = fixture.operations(2).iter().filter(|op| *op == "tap").count();
    let closed = serve.close();

    assert_eq!(first["ok"], false);
    assert_eq!(first["error"]["code"], "service_unavailable");
    assert_eq!(
        first["error"]["message"],
        "the simulator test runner stopped; the next request starts it again"
    );
    assert!(
        failed_cleaned,
        "the disconnected runner was not cleaned before the error returned"
    );
    assert_eq!(second["ok"], true, "the replacement did not answer: {second}");
    assert_eq!(second["result"], json!({}));
    assert_eq!(starts.len(), 2);
    assert_clean(&first_start);
    assert_eq!(first_taps, 1);
    assert_eq!(second_taps, 1, "the dropped tap must not be replayed automatically");
    assert_eq!(fixture.operations(1), ["tap"]);
    assert_eq!(fixture.operations(2), ["tap", "quit"]);
    assert_clean(&starts[1]);
    assert_stayed_connected(&closed);
}
