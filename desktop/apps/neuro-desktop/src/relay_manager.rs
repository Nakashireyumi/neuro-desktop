use anyhow::{Context, Result};
use std::env;
use std::path::PathBuf;
use std::process::{Child, Command};

pub struct RelayProcessManager {
    child: Option<Child>,
    binary_path: PathBuf,
}

impl RelayProcessManager {
    pub fn new() -> Result<Self> {
        let exe_dir = env::current_exe()
            .context("Failed to get current executable path")?
            .parent()
            .context("Failed to get parent directory")?
            .to_path_buf();

        #[cfg(target_os = "windows")]
        let candidates = ["neuro-relay.exe", "neurorelay.exe"];
        #[cfg(not(target_os = "windows"))]
        let candidates = ["neuro-relay", "neurorelay"];

        let explicit_path = env::var("NEURO_RELAY_BINARY")
            .ok()
            .map(PathBuf::from)
            .filter(|path| path.exists());

        let binary_path = if let Some(path) = explicit_path {
            path
        } else {
            candidates
                .iter()
                .map(|name| exe_dir.join(name))
                .find(|path| path.exists())
                .ok_or_else(|| {
                    anyhow::anyhow!(
                        "Neuro relay binary not found. Checked {:?} in {}. Set NEURO_RELAY_BINARY to override.",
                        candidates,
                        exe_dir.display()
                    )
                })?
        };

        Ok(Self {
            child: None,
            binary_path,
        })
    }

    pub fn start(&mut self, relay_name: &str, neuro_url: &str, emulated_addr: &str) -> Result<()> {
        if self.child.is_some() {
            println!("Neuro relay already running");
            return Ok(());
        }

        println!("Starting Neuro relay at: {}", self.binary_path.display());

        let child = Command::new(&self.binary_path)
            .arg("-name")
            .arg(relay_name)
            .arg("-neuro-url")
            .arg(neuro_url)
            .arg("-emulated-addr")
            .arg(emulated_addr)
            .spawn()
            .context("Failed to start Neuro relay")?;

        self.child = Some(child);
        println!(
            "Neuro relay started with PID: {}",
            self.child
                .as_ref()
                .expect("relay process should exist")
                .id()
        );

        Ok(())
    }

    pub fn is_running(&mut self) -> bool {
        if let Some(child) = &mut self.child {
            match child.try_wait() {
                Ok(Some(_)) => {
                    println!("Neuro relay has exited");
                    self.child = None;
                    false
                }
                Ok(None) => true,
                Err(e) => {
                    eprintln!("Error checking Neuro relay status: {}", e);
                    false
                }
            }
        } else {
            false
        }
    }

    pub fn restart(
        &mut self,
        relay_name: &str,
        neuro_url: &str,
        emulated_addr: &str,
    ) -> Result<()> {
        println!("Restarting Neuro relay...");
        self.stop();
        std::thread::sleep(std::time::Duration::from_millis(500));
        self.start(relay_name, neuro_url, emulated_addr)
    }

    pub fn stop(&mut self) {
        if let Some(mut child) = self.child.take() {
            println!("Stopping Neuro relay...");
            match child.kill() {
                Ok(_) => {
                    let _ = child.wait();
                    println!("Neuro relay stopped");
                }
                Err(e) => {
                    eprintln!("Failed to kill Neuro relay process: {}", e);
                }
            }
        }
    }
}

impl Drop for RelayProcessManager {
    fn drop(&mut self) {
        self.stop();
    }
}
