// desktop/apps/neuro-desktop/src/go_manager.rs
use anyhow::{Context, Result};
use std::process::{Child, Command};
use std::env;
use std::path::PathBuf;

pub struct GoProcessManager {
    child: Option<Child>,
    binary_path: PathBuf,
}

impl GoProcessManager {
    pub fn new() -> Result<Self> {
        // Get path to the Go binary
        let exe_dir = env::current_exe()
            .context("Failed to get current executable path")?
            .parent()
            .context("Failed to get parent directory")?
            .to_path_buf();

        #[cfg(target_os = "windows")]
        let binary_name = "neuro-integration.exe";
        #[cfg(not(target_os = "windows"))]
        let binary_name = "neuro-integration";

        let binary_path = exe_dir.join(binary_name);

        if !binary_path.exists() {
            anyhow::bail!(
                "Neuro integration binary not found at: {}",
                binary_path.display()
            );
        }

        Ok(Self {
            child: None,
            binary_path,
        })
    }

    pub fn start(&mut self, ws_url: &str, ipc_file: &str) -> Result<()> {
        if self.child.is_some() {
            println!("Neuro integration already running");
            return Ok(());
        }

        println!("Starting Neuro integration at: {}", self.binary_path.display());

        let child = Command::new(&self.binary_path)
            .env("NEURO_SDK_WS_URL", ws_url)
            .env("NEURO_IPC_FILE", ipc_file)
            .spawn()
            .context("Failed to start Neuro integration")?;

        self.child = Some(child);
        println!("Neuro integration started with PID: {}", self.child.as_ref().unwrap().id());

        Ok(())
    }

    pub fn is_running(&mut self) -> bool {
        if let Some(child) = &mut self.child {
            match child.try_wait() {
                Ok(Some(_)) => {
                    println!("Neuro integration has exited");
                    self.child = None;
                    false
                }
                Ok(None) => true,
                Err(e) => {
                    eprintln!("Error checking Neuro process status: {}", e);
                    false
                }
            }
        } else {
            false
        }
    }

    pub fn restart(&mut self, ws_url: &str, ipc_file: &str) -> Result<()> {
        println!("Restarting Neuro integration...");
        self.stop();
        std::thread::sleep(std::time::Duration::from_millis(500));
        self.start(ws_url, ipc_file)
    }

    pub fn stop(&mut self) {
        if let Some(mut child) = self.child.take() {
            println!("Stopping Neuro integration...");
            
            match child.kill() {
                Ok(_) => {
                    let _ = child.wait();
                    println!("Neuro integration stopped");
                }
                Err(e) => {
                    eprintln!("Failed to kill Neuro integration process: {}", e);
                }
            }
        }
    }
}

impl Drop for GoProcessManager {
    fn drop(&mut self) {
        self.stop();
    }
}