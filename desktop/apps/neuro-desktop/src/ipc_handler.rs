// desktop/apps/neuro-desktop/src/ipc_handler.rs
use anyhow::Result;
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use crate::controller::Controller;

#[derive(Debug, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum IPCCommand {
    MouseMove { 
        params: MouseMoveParams,
        execute_now: Option<bool>,
        clear_after: Option<bool>,
    },
    MouseClick { 
        params: MouseClickParams,
        execute_now: Option<bool>,
        clear_after: Option<bool>,
    },
    KeyPress { 
        params: KeyPressParams,
        execute_now: Option<bool>,
        clear_after: Option<bool>,
    },
    KeyType { 
        params: KeyTypeParams,
        execute_now: Option<bool>,
        clear_after: Option<bool>,
    },
    RunScript { 
        params: RunScriptParams,
        execute_now: Option<bool>,
        clear_after: Option<bool>,
    },
    
    ExecuteQueue,
    ClearActionQueue,
    ShutdownGracefully,
    ShutdownImmediately,
}

// Add these new params structs to handle the nested structure:
#[derive(Debug, Deserialize)]
pub struct MouseMoveParams {
    pub x: i32,
    pub y: i32,
}

#[derive(Debug, Deserialize)]
pub struct MouseClickParams {
    pub x: Option<i32>,
    pub y: Option<i32>,
    pub button: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct KeyPressParams {
    pub key: String,
    pub modifiers: Option<Vec<String>>,
}

#[derive(Debug, Deserialize)]
pub struct KeyTypeParams {
    pub text: String,
}

#[derive(Debug, Deserialize)]
pub struct RunScriptParams {
    pub script: String,
}

#[derive(Debug, Serialize)]
pub struct IPCResponse {
    pub success: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

impl IPCResponse {
    pub fn success() -> Self {
        Self {
            success: true,
            data: None,
            error: None,
        }
    }

    pub fn failure(error: String) -> Self {
        Self {
            success: false,
            data: None,
            error: Some(error),
        }
    }

    pub fn shutdown() -> IPCResponse {
        IPCResponse {
            success: true,
            data: Some(serde_json::json!({"shutdown": true})),
            error: None,
        }
    }
}

pub struct IPCHandler {
    ipc_file: PathBuf,
    response_file: PathBuf,
    running: Arc<AtomicBool>,
}

fn process_once(
    ipc_file: &PathBuf,
    response_file: &PathBuf,
    controller: &Controller,
    running: &Arc<AtomicBool>
) -> Result<()> {
    // Check if command file exists
    if !ipc_file.exists() {
        return Ok(());
    }

    // Read command
    let data = fs::read_to_string(&ipc_file)?;
    let command: IPCCommand = serde_json::from_str(&data)?;

    // Delete command file immediately
    fs::remove_file(&ipc_file)?;

    // Execute command
    let response = execute_command(controller, command);

    // Check for shutdown signal before writing response
    let should_shutdown = response.data.as_ref()
        .and_then(|d| d.get("shutdown"))
        .and_then(|s| s.as_bool())
        .unwrap_or(false);

    // Write response
    let response_json = serde_json::to_string(&response)?;
    fs::write(&response_file, response_json)?;

    // Handle shutdown after writing response
    if should_shutdown {
        println!();
        println!("Shutdown signal received, stopping IPC handler...");
        running.store(false, Ordering::SeqCst);
    }

    Ok(())
}

fn execute_command(controller: &Controller, command: IPCCommand) -> IPCResponse {
    match command {
        IPCCommand::MouseMove { params, execute_now, clear_after } => {
            let execute = execute_now.unwrap_or(true);
            let clear = clear_after.unwrap_or(true);
            
            match controller.mouse_move(params.x, params.y) {
                Ok(_) => {
                    if execute {
                        controller.execute_instructions().ok();
                        if clear {
                            controller.clear_action_queue().ok();
                        }
                    }
                    IPCResponse::success()
                }
                Err(e) => IPCResponse::failure(format!("Mouse move failed: {}", e)),
            }
        }

        IPCCommand::MouseClick { params, execute_now, clear_after } => {
            let execute = execute_now.unwrap_or(true);
            let clear = clear_after.unwrap_or(true);
            
            // If coordinates provided, move first
            if let (Some(x), Some(y)) = (params.x, params.y) {
                if let Err(e) = controller.mouse_move(x, y) {
                    return IPCResponse::failure(format!("Mouse move failed: {}", e));
                }
            }

            // Then click
            let x = params.x.unwrap_or(0);
            let y = params.y.unwrap_or(0);
            let button = params.button.as_deref().unwrap_or("left");

            match controller.mouse_click(x, y, button) {
                Ok(_) => {
                    if execute {
                        controller.execute_instructions().ok();
                        if clear {
                            controller.clear_action_queue().ok();
                        }
                    }
                    IPCResponse::success()
                }
                Err(e) => IPCResponse::failure(format!("Mouse click failed: {}", e)),
            }
        }

        IPCCommand::KeyPress { params, execute_now, clear_after } => {
            let execute = execute_now.unwrap_or(true);
            let clear = clear_after.unwrap_or(true);
            
            let script = if let Some(modifiers) = params.modifiers {
                let mods = modifiers.join(" ");
                format!("SHORTCUT {} {}", mods, params.key)
            } else {
                format!("PRESS {}", params.key)
            };

            match controller.run_script(&script) {
                Ok(_) => {
                    if !execute {
                        // Script auto-executes, so this shouldn't happen
                        // but we keep the flag for consistency
                    }
                    if execute && clear {
                        controller.clear_action_queue().ok();
                    }
                    IPCResponse::success()
                }
                Err(e) => IPCResponse::failure(format!("Key press failed: {}", e)),
            }
        }

        IPCCommand::KeyType { params, execute_now, clear_after } => {
            let execute = execute_now.unwrap_or(true);
            let clear = clear_after.unwrap_or(true);
            
            match controller.type_text(&params.text) {
                Ok(_) => {
                    if execute {
                        controller.execute_instructions().ok();
                        if clear {
                            controller.clear_action_queue().ok();
                        }
                    }
                    IPCResponse::success()
                }
                Err(e) => IPCResponse::failure(format!("Type text failed: {}", e)),
            }
        }

        IPCCommand::RunScript { params, execute_now, clear_after } => {
            let execute = execute_now.unwrap_or(true);
            let clear = clear_after.unwrap_or(true);
            
            match controller.run_script(&params.script) {
                Ok(_) => {
                    // run_script auto-executes in Python
                    if execute && clear {
                        controller.clear_action_queue().ok();
                    }
                    IPCResponse::success()
                }
                Err(e) => IPCResponse::failure(format!("Script execution failed: {}", e)),
            }
        }

        IPCCommand::ExecuteQueue => {
            match controller.execute_instructions() {
                Ok(_) => IPCResponse::success(),
                Err(e) => IPCResponse::failure(format!("Execute queue failed: {}", e)),
            }
        }

        IPCCommand::ClearActionQueue => {
            let _ = controller.clear_action_queue();
            IPCResponse::success()
        }

        IPCCommand::ShutdownGracefully | IPCCommand::ShutdownImmediately => {
            let _ = controller.shutdown();
            IPCResponse::shutdown()
        }
    }
}

impl IPCHandler {
    pub fn new(ipc_path: &str) -> Self {
        let ipc_file = PathBuf::from(ipc_path);
        let response_file = PathBuf::from(format!("{}.response", ipc_path));

        Self {
            ipc_file,
            response_file,
            running: Arc::new(AtomicBool::new(false)),
        }
    }

    pub fn start(self, controller: Controller) -> Arc<AtomicBool> {
        let running = Arc::clone(&self.running);
        let running_clone = Arc::clone(&self.running);
        running.store(true, Ordering::SeqCst);
        
        let ipc_file = self.ipc_file.clone();
        let response_file = self.response_file.clone();
        
        thread::spawn(move || {
            loop {
                if !running_clone.load(Ordering::SeqCst) {
                    break;
                }
                
                let result = process_once(&ipc_file, &response_file, &controller, &running_clone);
                if let Err(e) = result {
                    eprintln!("IPC processing error: {}", e);
                }
                thread::sleep(Duration::from_millis(50));
            }
            println!("Stopped IPC handler");
        });

        running
    }
}