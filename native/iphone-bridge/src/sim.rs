//! The simulator backend, for development and testing: the same operations on an iOS simulator on this
//! computer, through `xcrun simctl`, so everything above the bridge can run end to end without a phone.
//!
//! It never boots, shuts down or erases a simulator: booting is `scripts/app/sim-boot.sh`'s job. The
//! accessibility tree and input go through the test runner (`runner.rs`, `sim-runner/`), started on first use
//! (or with `serve`) and stopped with the bridge.

use std::path::Path;
use std::time::Duration;

use serde_json::{Value, json};
use tokio::time::Instant;

use crate::error::{BridgeError, Code};
use crate::geometry::{self, Orientation, Point, Screen};
use crate::ops::{Action, AppTarget, KeyInput, ShotOptions, SwipeSpec};
use crate::record::SimRecord;
use crate::run::Phone;
use crate::runner::Runner as TestRunner;
use crate::shot;

/// What one command printed.
#[derive(Debug, Clone, Default)]
pub struct Output {
    pub ok: bool,
    pub stdout: Vec<u8>,
    pub stderr: String,
}

/// Runs `xcrun` and `plutil`; tests use a stand-in.
#[allow(async_fn_in_trait)]
pub trait Commands {
    async fn run(&self, program: &str, args: &[String], stdin: Option<&[u8]>, env: &[(String, String)]) -> Output;
}

/// The real commands, killed if the request they serve is abandoned.
pub struct System;

impl Commands for System {
    async fn run(&self, program: &str, args: &[String], stdin: Option<&[u8]>, env: &[(String, String)]) -> Output {
        use tokio::io::AsyncWriteExt as _;
        let mut cmd = tokio::process::Command::new(program);
        cmd.args(args)
            .envs(env.iter().map(|(k, v)| (k, v)))
            .stdin(if stdin.is_some() {
                std::process::Stdio::piped()
            } else {
                std::process::Stdio::null()
            })
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped())
            .kill_on_drop(true);
        let mut child = match cmd.spawn() {
            Ok(c) => c,
            Err(e) => {
                return Output {
                    ok: false,
                    stdout: Vec::new(),
                    stderr: format!("run {program}: {e}"),
                };
            }
        };
        if let (Some(input), Some(mut pipe)) = (stdin, child.stdin.take()) {
            let _ = pipe.write_all(input).await;
        }
        match child.wait_with_output().await {
            Ok(out) => Output {
                ok: out.status.success(),
                stdout: out.stdout,
                stderr: String::from_utf8_lossy(&out.stderr).into_owned(),
            },
            Err(e) => Output {
                ok: false,
                stdout: Vec::new(),
                stderr: format!("run {program}: {e}"),
            },
        }
    }
}

fn strings(args: &[&str]) -> Vec<String> {
    args.iter().map(|s| (*s).to_owned()).collect()
}

/// The last non-empty line of a command's complaint, for messages.
fn complaint(o: &Output) -> String {
    o.stderr
        .lines()
        .map(str::trim)
        .rfind(|l| !l.is_empty())
        .unwrap_or("no output")
        .to_owned()
}

/// A simulator as `simctl list devices -j` describes it.
#[derive(Debug, Clone, PartialEq)]
pub struct SimInfo {
    pub udid: String,
    pub name: String,
    pub state: String,
    /// "27.2" from the runtime "com.apple.CoreSimulator.SimRuntime.iOS-27-2".
    pub os_version: Option<String>,
    pub device_type: String,
}

async fn find<R: Commands>(runner: &R, udid: &str) -> Result<Option<SimInfo>, BridgeError> {
    let o = runner
        .run("xcrun", &strings(&["simctl", "list", "devices", "-j"]), None, &[])
        .await;
    if !o.ok {
        return Err(BridgeError::new(
            Code::ServiceUnavailable,
            format!("simctl list: {}", complaint(&o)),
        ));
    }
    let v: Value = serde_json::from_slice(&o.stdout)
        .map_err(|e| BridgeError::internal(format!("simctl list: unreadable answer: {e}")))?;
    Ok(parse_devices(&v)
        .into_iter()
        .find(|d| d.udid.eq_ignore_ascii_case(udid)))
}

fn parse_devices(v: &Value) -> Vec<SimInfo> {
    let Some(runtimes) = v.get("devices").and_then(Value::as_object) else {
        return Vec::new();
    };
    let mut out = Vec::new();
    for (runtime, devices) in runtimes {
        let os_version = runtime
            .rsplit('.')
            .next()
            .and_then(|r| r.split_once('-'))
            .map(|(_, v)| v.replace('-', "."));
        for d in devices.as_array().into_iter().flatten() {
            let text = |k: &str| d.get(k).and_then(Value::as_str).unwrap_or_default().to_owned();
            out.push(SimInfo {
                udid: text("udid"),
                name: text("name"),
                state: text("state"),
                os_version: os_version.clone(),
                device_type: text("deviceTypeIdentifier"),
            });
        }
    }
    out
}

/// Whether `udid` is one of this computer's simulators: `pair` then makes a simulator record.
pub async fn is_simulator<R: Commands>(runner: &R, udid: &str) -> bool {
    matches!(find(runner, udid).await, Ok(Some(_)))
}

/// `pair` for a simulator: writes a record naming it. No USB or trust involved.
pub async fn pair<R: Commands>(runner: &R, udid: &str, out: &Path) -> Result<Value, BridgeError> {
    let info = find(runner, udid)
        .await?
        .ok_or_else(|| BridgeError::new(Code::NotFound, format!("no simulator {udid} on this computer")))?;
    SimRecord {
        udid: info.udid.clone(),
    }
    .write(out)?;
    let model = getenv(runner, &info.udid, "SIMULATOR_MODEL_IDENTIFIER").await;
    Ok(json!({
        "udid": info.udid,
        "name": info.name,
        "model": model,
        "product_version": info.os_version,
        "transport": "simulator",
    }))
}

async fn getenv<R: Commands>(runner: &R, udid: &str, name: &str) -> Option<String> {
    let o = runner
        .run("xcrun", &strings(&["simctl", "getenv", udid, name]), None, &[])
        .await;
    let v = String::from_utf8_lossy(&o.stdout).trim().to_owned();
    (o.ok && !v.is_empty()).then_some(v)
}

pub struct SimDevice<R: Commands> {
    cmd: R,
    info: SimInfo,
    model: Option<String>,
    build: Option<String>,
    screen: Option<Screen>,
    /// The built test runner, `None` when it is not built.
    runner_build: Option<std::path::PathBuf>,
    runner: Option<TestRunner>,
    /// How long an operation waits for the runner to start listening.
    runner_wait: Duration,
    /// The app last seen in front, asked about first next time.
    front: Option<String>,
    /// Bundle IDs of the apps a user can see, read once: helper processes that also run as apps (widget
    /// renderers, Spotlight) are never the app in front.
    visible: Option<std::collections::HashSet<String>>,
}

/// How the simulator backend should handle its test runner.
pub struct RunnerPlan {
    /// The built runner (`runner::locate`), `None` when it is not built.
    pub build: Option<std::path::PathBuf>,
    /// Start it right away (`serve`) rather than on first use (`call`).
    pub start_now: bool,
    /// How long an operation waits for it to start listening.
    pub wait: Duration,
}

impl<R: Commands> SimDevice<R> {
    /// Opens the simulator a record names; it has to be booted already.
    pub async fn connect(cmd: R, record: &SimRecord, udid: &str, plan: RunnerPlan) -> Result<Self, BridgeError> {
        if !record.udid.eq_ignore_ascii_case(udid) {
            return Err(BridgeError::new(
                Code::WrongDevice,
                format!("the record is for simulator {}, not {udid}", record.udid),
            ));
        }
        let info = find(&cmd, &record.udid).await?.ok_or_else(|| {
            BridgeError::new(
                Code::PairingRejected,
                format!(
                    "simulator {} no longer exists on this computer; pair another",
                    record.udid
                ),
            )
        })?;
        if info.state != "Booted" {
            return Err(BridgeError::new(
                Code::Unreachable,
                format!(
                    "simulator {} is {}; boot it with scripts/app/sim-boot.sh",
                    info.udid,
                    info.state.to_lowercase()
                ),
            ));
        }
        let env = |name| getenv(&cmd, &info.udid, name);
        let model = env("SIMULATOR_MODEL_IDENTIFIER").await;
        let build = env("SIMULATOR_RUNTIME_BUILD_VERSION").await;
        let num = |v: Option<String>| v.and_then(|v| v.parse::<f64>().ok()).filter(|n| *n > 0.0);
        let w = num(env("SIMULATOR_MAINSCREEN_WIDTH").await);
        let h = num(env("SIMULATOR_MAINSCREEN_HEIGHT").await);
        let scale = num(env("SIMULATOR_MAINSCREEN_SCALE").await);
        let screen = match (w, h, scale) {
            (Some(w), Some(h), Some(scale)) => Some(Screen {
                pixel_width: w.min(h) as u32,
                pixel_height: w.max(h) as u32,
                scale,
            }),
            _ => None,
        };
        let runner = match (&plan.build, plan.start_now) {
            // A runner that fails to start says why at first use.
            (Some(b), true) => TestRunner::start(b, &info.udid).ok(),
            _ => None,
        };
        Ok(Self {
            cmd,
            info,
            model,
            build,
            screen,
            runner_build: plan.build,
            runner,
            runner_wait: plan.wait,
            front: None,
            visible: None,
        })
    }

    /// The test runner, started and connected.
    async fn runner(&mut self) -> Result<&mut TestRunner, BridgeError> {
        let Some(build) = &self.runner_build else {
            return Err(BridgeError::new(
                Code::Unsupported,
                "the simulator test runner is not built: run native/iphone-bridge/build.sh --sim-runner",
            ));
        };
        if self.runner.is_none() {
            self.runner = Some(TestRunner::start(build, &self.info.udid)?);
        }
        let (ready, exited) = {
            let runner = self.runner.as_mut().expect("just started");
            let ready = runner.ready(Instant::now() + self.runner_wait).await;
            // Only a confirmed pre-handshake exit can never become ready. A live slow start stays cached.
            let exited = ready.is_err() && runner.exited_before_ready();
            (ready, exited)
        };
        if let Err(error) = ready {
            if exited {
                self.runner.take().expect("cached runner").stop().await;
            }
            return Err(error);
        }
        Ok(self.runner.as_mut().expect("cached runner"))
    }

    /// Apps running on the simulator, most recently started first, the last one seen in front before them:
    /// the runner asks each whether it is in front, so the likely ones go first.
    async fn candidates(&mut self) -> Vec<String> {
        if self.visible.is_none() {
            self.visible = self
                .apps()
                .await
                .ok()
                .map(|apps| apps.into_iter().map(|a| a.0).collect());
        }
        let o = self
            .simctl(&["simctl", "spawn", &self.info.udid, "launchctl", "list"])
            .await;
        let mut apps = running_apps(&String::from_utf8_lossy(&o.stdout));
        apps.sort_by_key(|a| std::cmp::Reverse(a.1));
        let mut ids: Vec<String> = apps
            .into_iter()
            .map(|(id, _)| id)
            .filter(|id| self.visible.as_ref().is_none_or(|v| v.contains(id)))
            .collect();
        ids.dedup();
        if let Some(front) = &self.front
            && let Some(i) = ids.iter().position(|id| id == front)
        {
            let id = ids.remove(i);
            ids.insert(0, id);
        }
        ids
    }

    /// One runner request; a dropped runner is forgotten, so the next request starts another.
    async fn ask(&mut self, req: Value) -> Result<Value, BridgeError> {
        let result = self.runner().await?.call(req).await;
        if result.as_ref().is_err_and(|e| e.code == Code::Disconnected) {
            if let Some(r) = self.runner.take() {
                r.stop().await;
            }
            return Err(BridgeError::new(
                Code::ServiceUnavailable,
                "the simulator test runner stopped; the next request starts it again",
            ));
        }
        result
    }

    fn frame(&self) -> (f64, f64) {
        self.screen
            .map_or((f64::MAX, f64::MAX), |s| s.frame(Orientation::Portrait))
    }

    /// A point must be on screen, in either orientation (the runner knows which).
    fn check(&self, p: Point) -> Result<Value, BridgeError> {
        let (w, h) = self.frame();
        let side = w.max(h);
        if !p.x.is_finite() || !p.y.is_finite() || p.x < 0.0 || p.y < 0.0 || p.x > side || p.y > side {
            return Err(BridgeError::bad_request(format!(
                "({}, {}) is outside the screen",
                p.x, p.y
            )));
        }
        Ok(json!({"x": p.x, "y": p.y}))
    }

    async fn ax_tree(&mut self, max_depth: u32, max_nodes: u32, include_offscreen: bool) -> Result<Value, BridgeError> {
        let candidates = self.candidates().await;
        let tree = self
            .ask(json!({
                "op": "ax_tree", "candidates": candidates,
                "max_depth": max_depth, "max_nodes": max_nodes, "include_offscreen": include_offscreen,
            }))
            .await?;
        self.front = tree["app"]["bundle_id"].as_str().map(str::to_owned);
        Ok(tree)
    }

    async fn input(&mut self, action: Action) -> Result<Value, BridgeError> {
        let req = match action {
            Action::Tap { at, count } => {
                let mut r = self.check(at)?;
                r["op"] = json!("tap");
                r["count"] = json!(count);
                r
            }
            Action::LongPress { at, duration_ms } => {
                let mut r = self.check(at)?;
                r["op"] = json!("long_press");
                r["duration_ms"] = json!(duration_ms);
                r
            }
            Action::Drag {
                from,
                to,
                duration_ms,
                hold_ms,
            } => {
                json!({"op": "drag", "from": self.check(from)?, "to": self.check(to)?, "duration_ms": duration_ms, "hold_ms": hold_ms})
            }
            Action::Swipe { spec, duration_ms } => {
                let (a, b) = match spec {
                    SwipeSpec::Ends(a, b) => (a, b),
                    SwipeSpec::Direction { dir, origin, distance } => {
                        geometry::swipe_ends(dir, origin, distance, self.frame())
                    }
                };
                json!({"op": "drag", "from": self.check(a)?, "to": self.check(b)?, "duration_ms": duration_ms, "hold_ms": 50})
            }
            Action::Keys(KeyInput::Text(text)) => {
                json!({"op": "type", "text": text, "candidates": self.candidates().await})
            }
            Action::Keys(KeyInput::Key { name, repeat, .. }) => {
                json!({"op": "key", "key": name, "repeat": repeat, "candidates": self.candidates().await})
            }
            Action::Button { button, .. } => match crate::keys::button_name(&button) {
                Some("home") => json!({"op": "home", "candidates": self.candidates().await}),
                _ => {
                    return Err(BridgeError::new(
                        Code::Unsupported,
                        "a simulator has no lock or volume buttons to press",
                    ));
                }
            },
            _ => unreachable!("only input comes here"),
        };
        self.ask(req).await?;
        Ok(json!({}))
    }

    async fn simctl(&self, args: &[&str]) -> Output {
        self.cmd.run("xcrun", &strings(args), None, &[]).await
    }

    fn device_info(&self) -> Value {
        let runner = self.runner_build.is_some();
        let mut notes = serde_json::Map::new();
        notes.insert(
            "simulated".into(),
            json!("an iOS simulator on this computer: the development and testing backend"),
        );
        if !runner {
            notes.insert("ax_tree".into(), json!(Code::Unsupported.as_str()));
            notes.insert("input".into(), json!(Code::Unsupported.as_str()));
        }
        let frame = self.screen.map(|s| s.frame(crate::geometry::Orientation::Portrait));
        json!({
            "udid": self.info.udid,
            "name": self.info.name,
            "product_type": self.model,
            "os_version": self.info.os_version,
            "build": self.build,
            "point_width": frame.map(|f| f.0),
            "point_height": frame.map(|f| f.1),
            "scale": self.screen.map(|s| s.scale),
            "orientation": "portrait",
            "locked": Value::Null,
            "developer_mode": Value::Null,
            "ddi_mounted": Value::Null,
            "battery": Value::Null,
            "simulated": true,
            "transport": "simulator",
            "capabilities": {"screenshot": true, "ax_tree": runner, "input": runner, "apps": true},
            "notes": notes,
        })
    }

    async fn screenshot(&self, opts: ShotOptions) -> Result<Value, BridgeError> {
        let dir = std::env::temp_dir().join(format!("persona-iphone-bridge-{}", uuid::Uuid::new_v4().simple()));
        std::fs::create_dir(&dir).map_err(|e| BridgeError::internal(format!("temporary directory: {e}")))?;
        let file = dir.join("screen.png");
        let o = self
            .simctl(&[
                "simctl",
                "io",
                &self.info.udid,
                "screenshot",
                "--type=png",
                &file.to_string_lossy(),
            ])
            .await;
        let raw = std::fs::read(&file);
        let _ = std::fs::remove_dir_all(&dir);
        if !o.ok {
            return Err(BridgeError::new(
                Code::ServiceUnavailable,
                format!("screenshot: {}", complaint(&o)),
            ));
        }
        let raw = raw.map_err(|e| BridgeError::internal(format!("screenshot: {e}")))?;
        // The simulator's orientation cannot be read without the test runner; a capture wider than tall is
        // reported as landscape, and is never turned.
        let image = shot::prepare(raw, &opts, crate::geometry::Orientation::Portrait)?;
        let scale = self.screen.map_or(3.0, |s| s.scale);
        let landscape = image.width > image.height;
        let (pw, ph) = match self.screen {
            Some(s) => s.frame(if landscape {
                crate::geometry::Orientation::LandscapeRight
            } else {
                crate::geometry::Orientation::Portrait
            }),
            None => (f64::from(image.width) / scale, f64::from(image.height) / scale),
        };
        let mut out = json!({
            "mime": image.mime,
            "width": image.width,
            "height": image.height,
            "point_width": pw,
            "point_height": ph,
            "orientation": if landscape { "landscape_right" } else { "portrait" },
        });
        match &opts.out_file {
            Some(path) => crate::files::write_private(Path::new(path), &image.bytes)?,
            None => {
                use base64::Engine as _;
                out["data"] = json!(base64::engine::general_purpose::STANDARD.encode(&image.bytes));
            }
        }
        Ok(out)
    }

    /// Installed apps: `(bundle_id, name, version, system)`.
    async fn apps(&self) -> Result<Vec<App>, BridgeError> {
        let o = self.simctl(&["simctl", "listapps", &self.info.udid]).await;
        if !o.ok {
            return Err(BridgeError::new(
                Code::ServiceUnavailable,
                format!("listapps: {}", complaint(&o)),
            ));
        }
        // simctl prints an old-style (OpenStep) property list; plutil turns it into XML.
        let xml = self
            .cmd
            .run(
                "plutil",
                &strings(&["-convert", "xml1", "-o", "-", "-"]),
                Some(&o.stdout),
                &[],
            )
            .await;
        if !xml.ok {
            return Err(BridgeError::internal(format!("listapps: {}", complaint(&xml))));
        }
        parse_apps(&xml.stdout)
    }

    async fn list_apps(&self, include_system: bool) -> Result<Value, BridgeError> {
        let apps: Vec<Value> = self
            .apps()
            .await?
            .into_iter()
            .filter(|a| include_system || !a.3)
            .map(|(bundle_id, name, version, system)| {
                json!({"bundle_id": bundle_id, "name": name, "version": version, "type": if system { "system" } else { "user" }})
            })
            .collect();
        Ok(json!({ "apps": apps }))
    }

    async fn installed(&self, bundle_id: &str) -> Result<bool, BridgeError> {
        Ok(self.apps().await?.iter().any(|a| a.0 == bundle_id))
    }

    async fn launch_app(
        &mut self,
        bundle_id: String,
        args: Vec<String>,
        env: Vec<(String, String)>,
        kill_existing: bool,
    ) -> Result<Value, BridgeError> {
        let mut cmd = strings(&["simctl", "launch"]);
        if kill_existing {
            cmd.push("--terminate-running-process".into());
        }
        cmd.push(self.info.udid.clone());
        cmd.push(bundle_id.clone());
        cmd.extend(args);
        // simctl hands SIMCTL_CHILD_ variables to the app without the prefix.
        let env: Vec<(String, String)> = env.into_iter().map(|(k, v)| (format!("SIMCTL_CHILD_{k}"), v)).collect();
        let o = self.cmd.run("xcrun", &cmd, None, &env).await;
        if !o.ok {
            if !self.installed(&bundle_id).await.unwrap_or(true) {
                return Err(BridgeError::new(
                    Code::AppNotFound,
                    format!("no app {bundle_id} on the simulator"),
                ));
            }
            return Err(BridgeError::internal(format!("launch {bundle_id}: {}", complaint(&o))));
        }
        if self.visible.as_ref().is_some_and(|v| !v.contains(&bundle_id)) {
            self.visible = None; // installed since it was read
        }
        let pid = parse_launch(&String::from_utf8_lossy(&o.stdout), &bundle_id);
        Ok(json!({"bundle_id": bundle_id, "pid": pid}))
    }

    async fn terminate_app(&self, target: AppTarget) -> Result<Value, BridgeError> {
        let bundle_id = match target {
            AppTarget::BundleId(id) => id,
            AppTarget::Pid(pid) => {
                // Only an app process of this simulator, never anything else on the computer.
                let o = self
                    .simctl(&["simctl", "spawn", &self.info.udid, "launchctl", "list"])
                    .await;
                if !o.ok {
                    return Err(BridgeError::internal(format!("launchctl list: {}", complaint(&o))));
                }
                match app_of_pid(&String::from_utf8_lossy(&o.stdout), pid) {
                    Some(id) => id,
                    None => return Ok(json!({"terminated": false})),
                }
            }
        };
        let o = self.simctl(&["simctl", "terminate", &self.info.udid, &bundle_id]).await;
        if o.ok {
            return Ok(json!({"terminated": true}));
        }
        if !self.installed(&bundle_id).await.unwrap_or(true) {
            return Err(BridgeError::new(
                Code::AppNotFound,
                format!("no app {bundle_id} on the simulator"),
            ));
        }
        if o.stderr.contains("found nothing to terminate") {
            return Ok(json!({"terminated": false}));
        }
        Err(BridgeError::internal(format!(
            "terminate {bundle_id}: {}",
            complaint(&o)
        )))
    }
}

/// An installed app: bundle ID, name, version, and whether it is Apple's own.
type App = (String, String, Option<String>, bool);

fn parse_apps(xml: &[u8]) -> Result<Vec<App>, BridgeError> {
    let v: plist::Value =
        plist::from_bytes(xml).map_err(|e| BridgeError::internal(format!("listapps: unreadable answer: {e}")))?;
    let d = v
        .as_dictionary()
        .ok_or_else(|| BridgeError::internal("listapps: not a dictionary"))?;
    let mut apps: Vec<_> = d
        .iter()
        .filter_map(|(id, app)| {
            let app = app.as_dictionary()?;
            let text = |k: &str| app.get(k).and_then(plist::Value::as_string).map(str::to_owned);
            let name = text("CFBundleDisplayName")
                .or_else(|| text("CFBundleName"))
                .unwrap_or_else(|| id.clone());
            let system = text("ApplicationType").as_deref() == Some("System");
            Some((id.clone(), name, text("CFBundleShortVersionString"), system))
        })
        .collect();
    apps.sort();
    Ok(apps)
}

/// App processes in `launchctl list` inside the simulator: `(bundle ID, pid)`.
fn running_apps(list: &str) -> Vec<(String, u32)> {
    list.lines()
        .filter_map(|l| {
            let mut f = l.split_whitespace();
            let pid: u32 = f.next()?.parse().ok()?;
            let app = f.nth(1)?.strip_prefix("UIKitApplication:")?;
            Some((app.split('[').next().unwrap_or(app).to_owned(), pid))
        })
        .collect()
}

/// "com.example.App: 2047" → 2047.
fn parse_launch(out: &str, bundle_id: &str) -> Option<u32> {
    out.lines()
        .find_map(|l| l.trim().strip_prefix(bundle_id)?.strip_prefix(':')?.trim().parse().ok())
}

/// The app whose process is `pid`, from `launchctl list` inside the simulator
/// ("2047  0  UIKitApplication:com.apple.Preferences[9791][rb-legacy]").
fn app_of_pid(list: &str, pid: u32) -> Option<String> {
    running_apps(list).into_iter().find(|a| a.1 == pid).map(|a| a.0)
}

impl<R: Commands> Phone for SimDevice<R> {
    async fn run(&mut self, action: Action) -> Result<Value, BridgeError> {
        match action {
            Action::DeviceInfo => Ok(self.device_info()),
            Action::Screenshot(opts) => self.screenshot(opts).await,
            Action::ListApps { include_system } => self.list_apps(include_system).await,
            Action::LaunchApp {
                bundle_id,
                args,
                env,
                kill_existing,
            } => self.launch_app(bundle_id, args, env, kill_existing).await,
            Action::TerminateApp(t) => self.terminate_app(t).await,
            Action::AxTree {
                max_depth,
                max_nodes,
                include_offscreen,
            } => self.ax_tree(max_depth, max_nodes, include_offscreen).await,
            Action::MountDdi { .. } => Err(BridgeError::new(
                Code::Unsupported,
                "a simulator has its developer services already; there is no disk image to mount",
            )),
            input => self.input(input).await,
        }
    }

    fn idle_at(&self) -> Option<Instant> {
        None
    }

    async fn idle(&mut self) {}

    async fn close(&mut self) {
        if let Some(r) = self.runner.take() {
            r.stop().await;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::RefCell;

    const UDID: &str = "11111111-2222-3333-4444-555555555555";

    /// Answers by the command's first words; records what ran.
    #[derive(Default)]
    struct Fake {
        answers: Vec<(String, Output)>,
        ran: RefCell<Vec<String>>,
    }

    impl Fake {
        fn answer(mut self, prefix: impl Into<String>, ok: bool, stdout: &str, stderr: &str) -> Self {
            self.answers.push((
                prefix.into(),
                Output {
                    ok,
                    stdout: stdout.as_bytes().to_vec(),
                    stderr: stderr.into(),
                },
            ));
            self
        }
    }

    impl Commands for Fake {
        async fn run(&self, program: &str, args: &[String], stdin: Option<&[u8]>, env: &[(String, String)]) -> Output {
            let line = format!("{program} {}", args.join(" "));
            let mut logged = line.clone();
            for (k, v) in env {
                logged.push_str(&format!(" [{k}={v}]"));
            }
            self.ran.borrow_mut().push(logged);
            if program == "plutil" {
                // Stand in for the conversion: the fixtures are XML already.
                return Output {
                    ok: true,
                    stdout: stdin.unwrap_or_default().to_vec(),
                    stderr: String::new(),
                };
            }
            if let Some(path) = line.strip_prefix(&format!("xcrun simctl io {UDID} screenshot --type=png ")) {
                std::fs::write(path, png(30, 60)).unwrap();
                return Output {
                    ok: true,
                    ..Output::default()
                };
            }
            self.answers
                .iter()
                .find(|(p, _)| line.starts_with(p.as_str()))
                .map(|(_, o)| o.clone())
                .unwrap_or_default()
        }
    }

    fn png(w: u32, h: u32) -> Vec<u8> {
        let mut out = Vec::new();
        image::DynamicImage::new_rgb8(w, h)
            .write_to(&mut std::io::Cursor::new(&mut out), image::ImageFormat::Png)
            .unwrap();
        out
    }

    fn devices(state: &str) -> String {
        json!({"devices": {"com.apple.CoreSimulator.SimRuntime.iOS-27-2": [
            {"udid": UDID, "name": "Test Phone", "state": state, "deviceTypeIdentifier": "com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro"}
        ]}})
        .to_string()
    }

    const APPS: &str = r#"<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>com.apple.Preferences</key><dict><key>ApplicationType</key><string>System</string><key>CFBundleDisplayName</key><string>Settings</string><key>CFBundleShortVersionString</key><string>1.0</string></dict>
<key>com.example.App</key><dict><key>ApplicationType</key><string>User</string><key>CFBundleName</key><string>Example</string><key>CFBundleShortVersionString</key><string>2.1</string></dict>
</dict></plist>"#;

    fn booted() -> Fake {
        Fake::default()
            .answer("xcrun simctl list devices", true, &devices("Booted"), "")
            .answer(
                format!("xcrun simctl getenv {UDID} SIMULATOR_MODEL_IDENTIFIER"),
                true,
                "iPhone18,1\n",
                "",
            )
            .answer(
                format!("xcrun simctl getenv {UDID} SIMULATOR_MAINSCREEN_WIDTH"),
                true,
                "1206\n",
                "",
            )
            .answer(
                format!("xcrun simctl getenv {UDID} SIMULATOR_MAINSCREEN_HEIGHT"),
                true,
                "2622\n",
                "",
            )
            .answer(
                format!("xcrun simctl getenv {UDID} SIMULATOR_MAINSCREEN_SCALE"),
                true,
                "3.000000\n",
                "",
            )
            .answer("xcrun simctl listapps", true, APPS, "")
    }

    fn no_runner() -> RunnerPlan {
        RunnerPlan {
            build: None,
            start_now: false,
            wait: Duration::from_secs(1),
        }
    }

    async fn open(fake: Fake) -> SimDevice<Fake> {
        SimDevice::connect(fake, &SimRecord { udid: UDID.into() }, UDID, no_runner())
            .await
            .unwrap()
    }

    #[tokio::test]
    async fn describes_itself_as_a_simulator() {
        let mut d = open(booted()).await;
        let info = d.run(Action::DeviceInfo).await.unwrap();
        assert_eq!(info["simulated"], true);
        assert_eq!(info["transport"], "simulator");
        assert_eq!(info["product_type"], "iPhone18,1");
        assert_eq!(info["os_version"], "27.2");
        assert_eq!(info["point_width"], 402.0);
        assert_eq!(info["point_height"], 874.0);
        assert_eq!(info["capabilities"]["input"], false);
        assert_eq!(info["notes"]["input"], "unsupported");
    }

    #[tokio::test]
    async fn needs_a_booted_simulator_and_never_boots_one() {
        let fake = Fake::default().answer("xcrun simctl list devices", true, &devices("Shutdown"), "");
        let err = SimDevice::connect(fake, &SimRecord { udid: UDID.into() }, UDID, no_runner())
            .await
            .err()
            .unwrap();
        assert_eq!(err.code, Code::Unreachable);
        assert!(err.message.contains("sim-boot.sh"));

        let fake = Fake::default().answer("xcrun simctl list devices", true, r#"{"devices":{}}"#, "");
        let err = SimDevice::connect(fake, &SimRecord { udid: UDID.into() }, UDID, no_runner())
            .await
            .err()
            .unwrap();
        assert_eq!(err.code, Code::PairingRejected);

        let err = SimDevice::connect(Fake::default(), &SimRecord { udid: UDID.into() }, "OTHER", no_runner())
            .await
            .err()
            .unwrap();
        assert_eq!(err.code, Code::WrongDevice);
    }

    #[tokio::test]
    async fn takes_screenshots() {
        let mut d = open(booted()).await;
        let shot = d
            .run(Action::Screenshot(ShotOptions {
                format: crate::ops::ImageFormat::Png,
                quality: 85,
                max_dimension: 0,
                out_file: None,
            }))
            .await
            .unwrap();
        assert_eq!(shot["mime"], "image/png");
        assert_eq!((shot["width"].as_u64(), shot["height"].as_u64()), (Some(30), Some(60)));
        assert_eq!(shot["point_width"], 402.0);
        assert!(shot["data"].as_str().is_some_and(|d| !d.is_empty()));
    }

    #[tokio::test]
    async fn lists_launches_and_terminates_apps() {
        let fake = booted()
            .answer(
                format!("xcrun simctl launch {UDID} com.example.App"),
                true,
                "com.example.App: 2047\n",
                "",
            )
            .answer(
                format!("xcrun simctl spawn {UDID} launchctl list"),
                true,
                "PID\tStatus\tLabel\n2047\t0\tUIKitApplication:com.example.App[9791][rb-legacy]\n12\t0\tcom.apple.other\n",
                "",
            )
            .answer(
                format!("xcrun simctl terminate {UDID} com.example.App"),
                true,
                "",
                "",
            )
            .answer(
                format!("xcrun simctl terminate {UDID} com.apple.Preferences"),
                false,
                "",
                "An error…\nfound nothing to terminate\n",
            );
        let mut d = open(fake).await;
        let apps = d.run(Action::ListApps { include_system: false }).await.unwrap();
        assert_eq!(
            apps,
            json!({"apps": [{"bundle_id": "com.example.App", "name": "Example", "version": "2.1", "type": "user"}]})
        );
        let launched = d
            .run(Action::LaunchApp {
                bundle_id: "com.example.App".into(),
                args: vec!["-x".into()],
                env: vec![("A".into(), "1".into())],
                kill_existing: false,
            })
            .await
            .unwrap();
        assert_eq!(launched, json!({"bundle_id": "com.example.App", "pid": 2047}));
        assert!(
            d.cmd
                .ran
                .borrow()
                .iter()
                .any(|l| l.ends_with("com.example.App -x [SIMCTL_CHILD_A=1]"))
        );
        let t = d.run(Action::TerminateApp(AppTarget::Pid(2047))).await.unwrap();
        assert_eq!(t, json!({"terminated": true}));
        let t = d.run(Action::TerminateApp(AppTarget::Pid(12))).await.unwrap();
        assert_eq!(t, json!({"terminated": false}), "not an app process: left alone");
        let t = d
            .run(Action::TerminateApp(AppTarget::BundleId(
                "com.apple.Preferences".into(),
            )))
            .await
            .unwrap();
        assert_eq!(t, json!({"terminated": false}));
        let err = d
            .run(Action::TerminateApp(AppTarget::BundleId("com.example.None".into())))
            .await
            .unwrap_err();
        assert_eq!(err.code, Code::AppNotFound);
        let err = d
            .run(Action::LaunchApp {
                bundle_id: "com.example.None".into(),
                args: vec![],
                env: vec![],
                kill_existing: false,
            })
            .await
            .unwrap_err();
        assert_eq!(err.code, Code::AppNotFound);
    }

    #[tokio::test]
    async fn input_and_ax_tree_need_the_built_runner() {
        let mut d = open(booted()).await;
        let tap = Action::Tap {
            at: crate::geometry::Point { x: 1.0, y: 1.0 },
            count: 1,
        };
        assert_eq!(d.run(tap).await.unwrap_err().code, Code::Unsupported);
        let ax = Action::AxTree {
            max_depth: 0,
            max_nodes: 10,
            include_offscreen: false,
        };
        let err = d.run(ax).await.unwrap_err();
        assert_eq!(err.code, Code::Unsupported);
        assert!(err.message.contains("--sim-runner"), "says how to get it");
    }

    #[tokio::test]
    async fn pairs_a_simulator() {
        let dir = std::env::temp_dir().join(format!("persona-iphone-bridge-sim-{}", uuid::Uuid::new_v4()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("pairing.record");
        let fake = booted();
        assert!(is_simulator(&fake, UDID).await);
        assert!(!is_simulator(&fake, "00000000-0000-0000-0000-000000000000").await);
        let out = pair(&fake, UDID, &path).await.unwrap();
        assert_eq!(out["transport"], "simulator");
        assert_eq!(out["model"], "iPhone18,1");
        assert!(matches!(
            crate::record::Stored::read(&path).unwrap(),
            crate::record::Stored::Simulator(r) if r.udid == UDID
        ));
        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[tokio::test]
    async fn only_visible_apps_can_be_in_front() {
        let fake = booted().answer(
            format!("xcrun simctl spawn {UDID} launchctl list"),
            true,
            "PID\tStatus\tLabel\n50\t0\tUIKitApplication:com.apple.chrono.WidgetRenderer-Default[1][x]\n40\t0\tUIKitApplication:com.example.App[2][x]\n30\t0\tUIKitApplication:com.apple.Preferences[3][x]\n",
            "",
        );
        let mut d = open(fake).await;
        assert_eq!(d.candidates().await, ["com.example.App", "com.apple.Preferences"]);
        d.front = Some("com.apple.Preferences".into());
        assert_eq!(d.candidates().await, ["com.apple.Preferences", "com.example.App"]);
    }

    #[test]
    fn reads_simctl_output() {
        assert_eq!(parse_launch("com.a.b: 12\n", "com.a.b"), Some(12));
        assert_eq!(parse_launch("other: 12\n", "com.a.b"), None);
        assert_eq!(
            app_of_pid("PID\tStatus\tLabel\n5\t0\tUIKitApplication:com.a[1][x]\n", 5).as_deref(),
            Some("com.a")
        );
        let d = parse_devices(&serde_json::from_str(&devices("Booted")).unwrap());
        assert_eq!(d[0].os_version.as_deref(), Some("27.2"));
    }
}
