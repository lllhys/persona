//! The simulator's test runner (`sim-runner/`): started with `xcodebuild test-without-building` against one
//! simulator, it listens on 127.0.0.1 at a free port the bridge picked and answers only a connection that
//! first sends the bridge's random token. It provides the accessibility tree and input. It ends when the
//! bridge disconnects or says `quit`; the bridge also kills `xcodebuild` if it lingers, so nothing stays
//! running after the bridge.

use std::path::{Path, PathBuf};
use std::time::Duration;

use serde_json::{Value, json};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::TcpStream;
use tokio::net::tcp::{OwnedReadHalf, OwnedWriteHalf};
use tokio::process::Child;
use tokio::time::Instant;

use crate::error::{BridgeError, Code};

/// Where the built runner is: `$PERSONA_IPHONE_SIM_RUNNER`, else `persona-iphone-sim-runner/` next to the bridge
/// (where `build.sh --sim-runner` and `--install` put it). It holds an `.xctestrun` file and the products.
pub fn locate() -> Option<PathBuf> {
    let dir = match std::env::var_os("PERSONA_IPHONE_SIM_RUNNER") {
        Some(d) => PathBuf::from(d),
        None => std::env::current_exe()
            .ok()?
            .parent()?
            .join("persona-iphone-sim-runner"),
    };
    xctestrun(&dir)
}

fn xctestrun(dir: &Path) -> Option<PathBuf> {
    std::fs::read_dir(dir)
        .ok()?
        .filter_map(Result::ok)
        .map(|e| e.path())
        .find(|p| p.extension().is_some_and(|x| x == "xctestrun"))
}

fn not_ready(why: impl std::fmt::Display) -> BridgeError {
    BridgeError::new(Code::ServiceUnavailable, format!("simulator test runner: {why}"))
}

pub struct Runner {
    child: Child,
    port: u16,
    token: String,
    /// Where xcodebuild keeps its result bundle and log; removed when the runner stops.
    scratch: PathBuf,
    link: Option<(BufReader<OwnedReadHalf>, OwnedWriteHalf)>,
    next_id: u64,
}

impl Runner {
    /// Starts the runner against simulator `udid`; it takes about half a minute to start listening.
    pub fn start(xctestrun: &Path, udid: &str) -> Result<Self, BridgeError> {
        let port = {
            let l = std::net::TcpListener::bind("127.0.0.1:0").map_err(not_ready)?;
            l.local_addr().map_err(not_ready)?.port()
        };
        let token = uuid::Uuid::new_v4().simple().to_string();
        let scratch = std::env::temp_dir().join(format!("persona-iphone-sim-runner-{}", uuid::Uuid::new_v4().simple()));
        std::fs::create_dir(&scratch).map_err(not_ready)?;
        let log = std::fs::File::create(scratch.join("xcodebuild.log")).map_err(not_ready)?;
        let child = tokio::process::Command::new("xcrun")
            .arg("xcodebuild")
            .arg("test-without-building")
            .arg("-xctestrun")
            .arg(xctestrun)
            .arg("-destination")
            .arg(format!("platform=iOS Simulator,id={udid}"))
            .arg("-resultBundlePath")
            .arg(scratch.join("result.xcresult"))
            // xcodebuild hands TEST_RUNNER_ variables to the runner without the prefix.
            .env("TEST_RUNNER_PERSONA_RUNNER_TOKEN", &token)
            .env("TEST_RUNNER_PERSONA_RUNNER_PORT", port.to_string())
            .stdin(std::process::Stdio::null())
            .stdout(log.try_clone().map_err(not_ready)?)
            .stderr(log)
            .kill_on_drop(true)
            .spawn()
            .map_err(|e| not_ready(format!("start xcodebuild: {e}")))?;
        Ok(Self {
            child,
            port,
            token,
            scratch,
            link: None,
            next_id: 0,
        })
    }

    /// Waits until the runner answers with the token accepted, up to `until`.
    pub async fn ready(&mut self, until: Instant) -> Result<(), BridgeError> {
        while self.link.is_none() {
            if let Ok(Some(status)) = self.child.try_wait() {
                return Err(not_ready(format!(
                    "xcodebuild ended ({status}) before the runner listened; see {}",
                    self.scratch.join("xcodebuild.log").display()
                )));
            }
            if let Ok(stream) = TcpStream::connect(("127.0.0.1", self.port)).await {
                let (r, mut w) = stream.into_split();
                let mut r = BufReader::new(r);
                let hello = format!("{}\n", json!({"token": self.token}));
                if w.write_all(hello.as_bytes()).await.is_ok() {
                    let mut line = String::new();
                    let answered = tokio::time::timeout(Duration::from_secs(5), r.read_line(&mut line)).await;
                    if matches!(answered, Ok(Ok(n)) if n > 0)
                        && serde_json::from_str::<Value>(&line).is_ok_and(|v| v["ok"] == true)
                    {
                        self.link = Some((r, w));
                        break;
                    }
                }
            }
            if Instant::now() >= until {
                return Err(not_ready("still starting; try again in a few seconds"));
            }
            tokio::time::sleep(Duration::from_millis(500)).await;
        }
        Ok(())
    }

    /// Whether xcodebuild exited before a runner connection was established.
    pub fn exited_before_ready(&mut self) -> bool {
        self.link.is_none() && matches!(self.child.try_wait(), Ok(Some(_)))
    }

    /// One request; the runner's `result`, or its error.
    pub async fn call(&mut self, mut req: Value) -> Result<Value, BridgeError> {
        let (r, w) = self.link.as_mut().ok_or_else(|| not_ready("not connected"))?;
        self.next_id += 1;
        let id = self.next_id.to_string();
        req["id"] = json!(id);
        let lost = |e: std::io::Error| BridgeError::new(Code::Disconnected, format!("simulator test runner: {e}"));
        w.write_all(format!("{req}\n").as_bytes()).await.map_err(lost)?;
        let mut line = String::new();
        if r.read_line(&mut line).await.map_err(lost)? == 0 {
            self.link = None;
            return Err(BridgeError::new(Code::Disconnected, "simulator test runner stopped"));
        }
        let reply: Value = serde_json::from_str(&line)
            .map_err(|e| BridgeError::internal(format!("simulator test runner: unreadable answer: {e}")))?;
        if reply["ok"] == true {
            return Ok(reply["result"].clone());
        }
        let code = match reply["error"]["code"].as_str() {
            Some("bad_request") => Code::BadRequest,
            Some("unsupported") => Code::Unsupported,
            _ => Code::Internal,
        };
        Err(BridgeError::new(
            code,
            reply["error"]["message"]
                .as_str()
                .unwrap_or("the runner gave no reason"),
        ))
    }

    /// Asks the runner to end, waits a little for xcodebuild, then kills what is left.
    pub async fn stop(mut self) {
        let mut grace = Duration::ZERO;
        if let Some((_, w)) = self.link.as_mut()
            && w.write_all(b"{\"op\":\"quit\"}\n").await.is_ok()
        {
            grace = Duration::from_secs(10);
        }
        self.link = None;
        if tokio::time::timeout(grace, self.child.wait()).await.is_err() {
            let _ = self.child.kill().await;
        }
        let _ = std::fs::remove_dir_all(&self.scratch);
    }
}

impl Drop for Runner {
    fn drop(&mut self) {
        // `stop` was not reached (an abandoned request): kill_on_drop ends xcodebuild; clean up its files.
        let _ = std::fs::remove_dir_all(&self.scratch);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn finds_the_xctestrun_file() {
        let dir = std::env::temp_dir().join(format!("persona-runner-{}", uuid::Uuid::new_v4().simple()));
        std::fs::create_dir_all(&dir).unwrap();
        assert!(xctestrun(&dir).is_none());
        std::fs::write(dir.join("PersonaSimRunner_iphonesimulator.xctestrun"), b"").unwrap();
        assert!(xctestrun(&dir).is_some());
        assert!(xctestrun(Path::new("/nonexistent/persona")).is_none());
        std::fs::remove_dir_all(&dir).unwrap();
    }

    /// A stand-in runner: checks the token, answers one request, then closes.
    #[tokio::test]
    async fn speaks_the_runner_protocol() {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let port = listener.local_addr().unwrap().port();
        let server = tokio::spawn(async move {
            // A wrong token is dropped; the right one is accepted.
            loop {
                let (s, _) = listener.accept().await.unwrap();
                let (r, mut w) = s.into_split();
                let mut r = BufReader::new(r);
                let mut line = String::new();
                r.read_line(&mut line).await.unwrap();
                if !line.contains("right") {
                    continue;
                }
                w.write_all(b"{\"ok\":true}\n").await.unwrap();
                line.clear();
                r.read_line(&mut line).await.unwrap();
                let req: Value = serde_json::from_str(&line).unwrap();
                assert_eq!(req["op"], "tap");
                let reply = json!({"id": req["id"], "ok": false, "error": {"code": "bad_request", "message": "no"}});
                w.write_all(format!("{reply}\n").as_bytes()).await.unwrap();
                return;
            }
        });
        let child = tokio::process::Command::new("sleep")
            .arg("30")
            .kill_on_drop(true)
            .spawn()
            .unwrap();
        let scratch = std::env::temp_dir().join(format!("persona-runner-{}", uuid::Uuid::new_v4().simple()));
        std::fs::create_dir_all(&scratch).unwrap();
        let mut wrong = Runner {
            child,
            port,
            token: "wrong".into(),
            scratch: scratch.clone(),
            link: None,
            next_id: 0,
        };
        let err = wrong
            .ready(Instant::now() + Duration::from_millis(200))
            .await
            .unwrap_err();
        assert_eq!(err.code, Code::ServiceUnavailable);
        wrong.token = "right".into();
        wrong.ready(Instant::now() + Duration::from_secs(5)).await.unwrap();
        let err = wrong.call(json!({"op": "tap", "x": 1, "y": 1})).await.unwrap_err();
        assert_eq!(err.code, Code::BadRequest);
        let err = wrong.call(json!({"op": "tap", "x": 1, "y": 1})).await.unwrap_err();
        assert_eq!(err.code, Code::Disconnected);
        wrong.stop().await;
        assert!(!scratch.exists());
        server.await.unwrap();
    }
}
