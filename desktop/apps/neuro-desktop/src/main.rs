// ============================================================
// desktop/apps/neuro-desktop/src/main.rs
// ============================================================

mod controller;
mod ipc_handler;
mod go_manager;

use controller::Controller;
use ipc_handler::IPCHandler;
use go_manager::GoProcessManager;
use std::env;

use std::path::PathBuf;
use std::fs;
use serde::{Deserialize};

#[derive(Debug, Deserialize)]
struct IntegrationConfig {
    connection: ConnectionConfig,
}

#[derive(Debug, Deserialize)]
struct ConnectionConfig {
    #[serde(rename = "neuro-backend")]
    neuro_backend: String,
}

fn load_config() -> Result<IntegrationConfig, Box<dyn std::error::Error>> {
    let exe_dir = env::current_exe()?
        .parent()
        .ok_or("No parent dir")?
        .to_path_buf();
    
    let config_path = exe_dir.join("config").join("integration-config.yml");
    
    if !config_path.exists() {
        // Fallback to development path
        let dev_config = PathBuf::from("config/integration-config.yml");
        if dev_config.exists() {
            let content = fs::read_to_string(dev_config)?;
            return Ok(serde_yaml::from_str(&content)?);
        }
    }
    
    let content = fs::read_to_string(config_path)?;
    Ok(serde_yaml::from_str(&content)?)
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    println!("=======================================================");
    println!("           Neuro Desktop Control System");
    println!("=======================================================");
    println!();

    // Load config
    let config = load_config().unwrap_or_else(|e| {
        eprintln!("Warning: Could not load config file: {}", e);
        eprintln!("Using default values...");
        IntegrationConfig {
            connection: ConnectionConfig {
                neuro_backend: "ws://localhost:8000".to_string(),
            }
        }
    });

    // Configuration
    let ws_url = env::var("NEURO_SDK_WS_URL")
        .unwrap_or_else(|_| config.connection.neuro_backend.clone());

    let ipc_path = env::var("NEURO_IPC_FILE")
        .unwrap_or_else(|_| {
            env::current_exe()
                .ok()
                .and_then(|p| p.parent().map(|p| p.join("neuro_ipc.json")))
                .unwrap_or_else(|| std::path::PathBuf::from("./neuro_ipc.json"))
                .to_string_lossy()
                .to_string()
        });

    println!("Configuration:");
    println!("  - Neuro WebSocket: {}", ws_url);
    println!("  - IPC File:        {}", ipc_path);
    println!();

    // Initialize Python controller drivers
    println!("[1/4] Initializing Python controller drivers...");
    let controller = Controller::initialize_drivers()
        .expect("Failed to initialize controller drivers");
    println!("      ✓ Python drivers loaded");
    println!();

    // Initialize Go process manager
    println!("[2/4] Initializing Neuro integration...");
    let mut go_manager = GoProcessManager::new()
        .expect("Failed to create Go manager");
    println!("      ✓ Go integration ready");
    println!();

    // Start IPC handler
    println!("[3/4] Starting IPC handler...");
    let ipc = IPCHandler::new(&ipc_path);
    let ipc_handler = ipc.start(controller);
    println!("      ✓ IPC handler running on: {}", ipc_path);
    println!();

    // Start Go integration
    println!("[4/4] Starting Neuro Integration Code...");
    go_manager.start(&ws_url, &ipc_path)
        .expect("Failed to start Go integration");
    println!("      ✓ Go integration connected to Neuro");
    println!();

    println!("=======================================================");
    println!("  Neuro Desktop is ready!");
    println!("  Neuro can now control your computer.");
    println!("=======================================================");
    println!();
    println!("Press Ctrl+C to stop");
    println!();

    // Monitor Go process and restart if needed
    let mut check_interval = tokio::time::interval(tokio::time::Duration::from_secs(5));
    
    loop {
        tokio::select! {
            _ = check_interval.tick() => {
                // Check for IPC shutdown first
                if !ipc_handler.load(std::sync::atomic::Ordering::SeqCst) {
                    println!(); // Print out a space for clarity
                    println!("Shutdown signal received, Neuro Desktop is stopping fully...");
                    go_manager.stop();
                    break;
                }

                // Check if Go process crashed
                if !go_manager.is_running() {
                    eprintln!("⚠ Neuro integration crashed! Attempting restart...");
                    if let Err(e) = go_manager.restart(&ws_url, &ipc_path) {
                        eprintln!("✗ Failed to restart Neuro integration: {}", e);
                        break;
                    }
                    println!("✓ Neuro integration restarted");
                }
            }

            _ = tokio::signal::ctrl_c() => {
                println!();
                println!("Shutting down...");
                go_manager.stop();
                break;
            }
        }
    }

    println!("Neuro Desktop stopped");
    Ok(())
}