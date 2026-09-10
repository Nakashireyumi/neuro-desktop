// ============================================================
// apps/neuro-desktop/src/ipc_handler_v2.rs
// ============================================================

use anyhow::{Context, Result};
use log::{error, info};
use serde::{Deserialize, Serialize};
use std::collections::BTreeSet;
use std::fs;
use std::path::PathBuf;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::{Duration, Instant};

use crate::controller::Controller;

// ============================================================
// Command Types with Validation
// ============================================================

#[derive(Debug, Deserialize, Clone)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum IPCCommand {
    MoveMouseTo {
        params: MoveMouseToParams,
        #[serde(default = "default_true")]
        execute_now: bool,
        #[serde(default = "default_true")]
        clear_after: bool,
    },
    MouseClick {
        params: MouseClickParams,
        #[serde(default = "default_true")]
        execute_now: bool,
        #[serde(default = "default_true")]
        clear_after: bool,
    },
    KeyPress {
        params: KeyPressParams,
        #[serde(default = "default_true")]
        execute_now: bool,
        #[serde(default = "default_true")]
        clear_after: bool,
    },
    TypeText {
        params: TypeTextParams,
        #[serde(default = "default_true")]
        execute_now: bool,
        #[serde(default = "default_true")]
        clear_after: bool,
    },
    RunScript {
        params: RunScriptParams,
        #[serde(default = "default_true")]
        execute_now: bool,
        #[serde(default = "default_true")]
        clear_after: bool,
    },
    ExecuteQueue,
    ClearActionQueue,
    GetStatus {
        #[serde(default)]
        params: GetStatusParams,
    },
    Heartbeat,
    ShutdownGracefully,
    ShutdownImmediately,
}

fn default_true() -> bool {
    true
}

fn default_max_open_windows() -> usize {
    15
}

fn default_max_processes() -> usize {
    20
}

fn default_max_actions() -> usize {
    20
}

// Parameter validation
impl IPCCommand {
    pub fn validate(&self) -> Result<()> {
        match self {
            Self::MoveMouseTo { params, .. } => {
                if params.x < 0 || params.y < 0 {
                    anyhow::bail!("Mouse coordinates must be non-negative");
                }
                if params.x > 10000 || params.y > 10000 {
                    anyhow::bail!("Mouse coordinates out of reasonable range");
                }
            }
            Self::TypeText { params, .. } => {
                if params.text.len() > 10000 {
                    anyhow::bail!("Text too long (max 10000 characters)");
                }
            }
            Self::RunScript { params, .. } => {
                if params.script.len() > 50000 {
                    anyhow::bail!("Script too long (max 50000 characters)");
                }
            }
            Self::GetStatus { params } => {
                if params.max_open_windows == 0 || params.max_open_windows > 200 {
                    anyhow::bail!("max_open_windows must be between 1 and 200");
                }
                if params.max_processes == 0 || params.max_processes > 500 {
                    anyhow::bail!("max_processes must be between 1 and 500");
                }
                if params.max_actions == 0 || params.max_actions > 200 {
                    anyhow::bail!("max_actions must be between 1 and 200");
                }
            }
            _ => {}
        }
        Ok(())
    }
}

#[derive(Debug, Deserialize, Clone)]
pub struct MoveMouseToParams {
    pub x: i32,
    pub y: i32,
}

#[derive(Debug, Deserialize, Clone)]
pub struct MouseClickParams {
    pub button: Option<String>,
}

#[derive(Debug, Deserialize, Clone)]
pub struct KeyPressParams {
    pub key: String,
}

#[derive(Debug, Deserialize, Clone)]
pub struct TypeTextParams {
    pub text: String,
}

#[derive(Debug, Deserialize, Clone)]
pub struct RunScriptParams {
    pub script: String,
}

#[derive(Debug, Deserialize, Clone)]
pub struct GetStatusParams {
    #[serde(default)]
    pub capture_screenshot: bool,
    #[serde(default)]
    pub screenshot_path: Option<String>,
    #[serde(default = "default_max_open_windows")]
    pub max_open_windows: usize,
    #[serde(default = "default_max_processes")]
    pub max_processes: usize,
    #[serde(default = "default_max_actions")]
    pub max_actions: usize,
}

impl Default for GetStatusParams {
    fn default() -> Self {
        Self {
            capture_screenshot: false,
            screenshot_path: None,
            max_open_windows: default_max_open_windows(),
            max_processes: default_max_processes(),
            max_actions: default_max_actions(),
        }
    }
}

// ============================================================
// Response with More Information
// ============================================================

#[derive(Debug, Serialize, Clone)]
pub struct IPCResponse {
    pub success: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    pub timestamp: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub execution_time_ms: Option<u64>,
}

impl IPCResponse {
    pub fn success() -> Self {
        Self {
            success: true,
            data: None,
            error: None,
            timestamp: current_timestamp(),
            execution_time_ms: None,
        }
    }

    pub fn success_with_data(data: serde_json::Value) -> Self {
        Self {
            success: true,
            data: Some(data),
            error: None,
            timestamp: current_timestamp(),
            execution_time_ms: None,
        }
    }

    pub fn failure(error: String) -> Self {
        Self {
            success: false,
            data: None,
            error: Some(error),
            timestamp: current_timestamp(),
            execution_time_ms: None,
        }
    }

    pub fn with_execution_time(mut self, time_ms: u64) -> Self {
        self.execution_time_ms = Some(time_ms);
        self
    }
}

fn current_timestamp() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_secs()
}

// ============================================================
// Statistics and Monitoring
// ============================================================

#[derive(Debug, Clone)]
pub struct IPCStatistics {
    pub total_commands: u64,
    pub successful_commands: u64,
    pub failed_commands: u64,
    pub average_execution_time_ms: f64,
    pub uptime_seconds: u64,
}

// ============================================================
// IPC Handler
// ============================================================

pub struct IPCHandler {
    ipc_file: PathBuf,
    response_file: PathBuf,
    running: Arc<AtomicBool>,
    stats: Arc<parking_lot::Mutex<IPCStatistics>>,
    start_time: Instant,
}

impl IPCHandler {
    pub fn new(ipc_path: &str) -> Self {
        let ipc_file = PathBuf::from(ipc_path);
        let response_file = PathBuf::from(format!("{}.response", ipc_path));

        Self {
            ipc_file,
            response_file,
            running: Arc::new(AtomicBool::new(false)),
            stats: Arc::new(parking_lot::Mutex::new(IPCStatistics {
                total_commands: 0,
                successful_commands: 0,
                failed_commands: 0,
                average_execution_time_ms: 0.0,
                uptime_seconds: 0,
            })),
            start_time: Instant::now(),
        }
    }

    pub fn start(self, controller: Controller) -> Arc<AtomicBool> {
        let running = Arc::clone(&self.running);
        let running_clone = Arc::clone(&self.running);
        running.store(true, Ordering::SeqCst);

        let ipc_file = self.ipc_file.clone();
        let response_file = self.response_file.clone();
        let stats = Arc::clone(&self.stats);
        let start_time = self.start_time;

        thread::spawn(move || {
            info!("IPC Handler started");
            let mut consecutive_errors = 0;
            const MAX_CONSECUTIVE_ERRORS: u32 = 10;

            loop {
                if !running_clone.load(Ordering::SeqCst) {
                    info!("IPC Handler stopping");
                    break;
                }

                match Self::process_once(
                    &ipc_file,
                    &response_file,
                    &controller,
                    &running_clone,
                    &stats,
                    start_time,
                ) {
                    Ok(processed) => {
                        if processed {
                            consecutive_errors = 0;
                        }
                    }
                    Err(e) => {
                        consecutive_errors += 1;
                        error!("IPC processing error ({}): {}", consecutive_errors, e);

                        if consecutive_errors >= MAX_CONSECUTIVE_ERRORS {
                            error!("Too many consecutive errors, stopping IPC handler");
                            running_clone.store(false, Ordering::SeqCst);
                            break;
                        }
                    }
                }

                thread::sleep(Duration::from_millis(50));
            }

            info!("IPC Handler stopped");
        });

        running
    }

    fn process_once(
        ipc_file: &PathBuf,
        response_file: &PathBuf,
        controller: &Controller,
        running: &Arc<AtomicBool>,
        stats: &Arc<parking_lot::Mutex<IPCStatistics>>,
        start_time: Instant,
    ) -> Result<bool> {
        // Check if command file exists
        if !ipc_file.exists() {
            return Ok(false);
        }

        let execution_start = Instant::now();

        // Read command
        let data = fs::read_to_string(&ipc_file).context("Failed to read IPC file")?;

        // Parse command
        let command: IPCCommand =
            serde_json::from_str(&data).context("Failed to parse IPC command")?;

        // Validate command
        if let Err(e) = command.validate() {
            error!("Command validation failed: {}", e);
            let response = IPCResponse::failure(format!("Validation error: {}", e));
            Self::write_response(response_file, &response)?;
            fs::remove_file(&ipc_file)?;
            return Ok(true);
        }

        // Delete command file immediately
        fs::remove_file(&ipc_file).context("Failed to remove IPC file")?;

        // Execute command
        let response = Self::execute_command(controller, command);

        // Update statistics
        let execution_time_ms = execution_start.elapsed().as_millis() as u64;
        let response_with_time = response.with_execution_time(execution_time_ms);

        {
            let mut stats_lock = stats.lock();
            stats_lock.total_commands += 1;
            if response_with_time.success {
                stats_lock.successful_commands += 1;
            } else {
                stats_lock.failed_commands += 1;
            }

            // Update rolling average
            let n = stats_lock.total_commands as f64;
            stats_lock.average_execution_time_ms =
                (stats_lock.average_execution_time_ms * (n - 1.0) + execution_time_ms as f64) / n;

            stats_lock.uptime_seconds = start_time.elapsed().as_secs();
        }

        // Check for shutdown signal
        if let Some(data) = &response_with_time.data {
            if data
                .get("shutdown")
                .and_then(|s| s.as_bool())
                .unwrap_or(false)
            {
                info!("Shutdown signal received");
                Self::write_response(response_file, &response_with_time)?;
                running.store(false, Ordering::SeqCst);
                return Ok(true);
            }
        }

        // Write response
        Self::write_response(response_file, &response_with_time)?;

        Ok(true)
    }

    fn write_response(path: &PathBuf, response: &IPCResponse) -> Result<()> {
        let json = serde_json::to_string(response).context("Failed to serialize response")?;

        fs::write(path, json).context("Failed to write response file")?;

        Ok(())
    }

    pub fn execute_command(controller: &Controller, command: IPCCommand) -> IPCResponse {
        fn execute_and_maybe_clear(
            execute_now: bool,
            clear_after: bool,
            controller: &Controller,
        ) -> Result<()> {
            if execute_now {
                controller.execute_instructions()?;
            }
            if clear_after {
                controller.clear_action_queue()?;
            }

            Ok(())
        }

        let result = match command {
            IPCCommand::MoveMouseTo {
                params,
                execute_now,
                clear_after,
            } => controller
                .mouse_move(params.x, params.y)
                .and_then(|_| execute_and_maybe_clear(execute_now, clear_after, controller)),

            IPCCommand::MouseClick {
                params,
                execute_now,
                clear_after,
            } => {
                let button = params.button.as_deref().unwrap_or("left");

                controller
                    .mouse_click(button)
                    .and_then(|_| execute_and_maybe_clear(execute_now, clear_after, controller))
            }

            IPCCommand::TypeText {
                params,
                execute_now,
                clear_after,
            } => controller
                .type_text(&params.text)
                .and_then(|_| execute_and_maybe_clear(execute_now, clear_after, controller)),

            IPCCommand::KeyPress {
                params,
                execute_now,
                clear_after,
            } => controller
                .press_key(&params.key)
                .and_then(|_| execute_and_maybe_clear(execute_now, clear_after, controller)),

            IPCCommand::RunScript {
                params,
                execute_now,
                clear_after,
            } => controller
                .run_script(&params.script)
                .and_then(|_| execute_and_maybe_clear(execute_now, clear_after, controller)),

            IPCCommand::ExecuteQueue => controller.execute_instructions(),

            IPCCommand::ClearActionQueue => controller.clear_action_queue(),

            IPCCommand::GetStatus { params } => {
                let mouse_position = controller
                    .get_current_mouse_position()
                    .ok()
                    .map(|(x, y)| serde_json::json!({ "x": x, "y": y }))
                    .unwrap_or(serde_json::Value::Null);

                let active_window = controller.get_active_window().ok().flatten();

                let mut open_windows = controller.get_open_windows().unwrap_or_default();
                open_windows.truncate(params.max_open_windows);

                let screen = controller
                    .get_screen_size()
                    .ok()
                    .map(|(width, height)| serde_json::json!({ "width": width, "height": height }))
                    .unwrap_or(serde_json::Value::Null);

                let running_processes_raw = controller.get_running_processes().unwrap_or_default();
                let mut deduped_processes: Vec<String> = running_processes_raw
                    .into_iter()
                    .collect::<BTreeSet<_>>()
                    .into_iter()
                    .collect();
                deduped_processes.truncate(params.max_processes);

                let recent_actions = controller
                    .action_history_json()
                    .ok()
                    .and_then(|v| v.as_array().cloned())
                    .map(|mut actions| {
                        if actions.len() > params.max_actions {
                            actions = actions.split_off(actions.len() - params.max_actions);
                        }
                        actions
                    })
                    .unwrap_or_default();

                let screenshot_path = if params.capture_screenshot {
                    let path = params.screenshot_path.unwrap_or_else(|| {
                        let mut tmp = std::env::temp_dir();
                        tmp.push(format!("nd-capture-{}.png", current_timestamp()));
                        tmp.to_string_lossy().to_string()
                    });

                    controller.capture_screen_to_file(&path).ok()
                } else {
                    None
                };

                return IPCResponse::success_with_data(serde_json::json!({
                    "status": "running",
                    "timestamp": current_timestamp(),
                    "active_window": active_window,
                    "open_windows": open_windows,
                    "screen": screen,
                    "mouse_position": mouse_position,
                    "running_processes": deduped_processes,
                    "recent_actions": recent_actions,
                    "screenshot_path": screenshot_path,
                }));
            }

            IPCCommand::Heartbeat => {
                return IPCResponse::success_with_data(serde_json::json!({
                    "heartbeat": "alive",
                    "timestamp": current_timestamp(),
                }));
            }

            IPCCommand::ShutdownGracefully | IPCCommand::ShutdownImmediately => {
                controller.shutdown().ok();
                return IPCResponse::success_with_data(serde_json::json!({
                    "shutdown": true
                }));
            }
        };

        match result {
            Ok(_) => IPCResponse::success(),
            Err(e) => IPCResponse::failure(format!("Execution error: {}", e)),
        }
    }

    pub fn get_statistics(&self) -> IPCStatistics {
        self.stats.lock().clone()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_command_validation() {
        let valid = IPCCommand::MoveMouseTo {
            params: MoveMouseToParams { x: 100, y: 200 },
            execute_now: true,
            clear_after: true,
        };
        assert!(valid.validate().is_ok());

        let invalid = IPCCommand::MoveMouseTo {
            params: MoveMouseToParams { x: -100, y: 200 },
            execute_now: true,
            clear_after: true,
        };
        assert!(invalid.validate().is_err());
    }

    #[test]
    fn test_response_serialization() {
        let response = IPCResponse::success();
        let json = serde_json::to_string(&response).unwrap();
        assert!(json.contains("\"success\":true"));
    }
}
