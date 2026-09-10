//! TCP executor client — connects to the Go bridge hub and runs commands locally.
//!
//! Protocol (JSON lines):
//!   client → {"type":"hello","role":"executor","version":"1"}
//!   server → {"type":"hello_ack",...}
//!   server → {"type":"command","id":"...","command":{...}}
//!   client → {"type":"result","id":"...","success":true,"data":{...}}

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::io::{BufRead, BufReader, Write};
use std::net::TcpStream;
use std::time::Duration;

use crate::controller::Controller;
use crate::ipc_handler::{IPCCommand, IPCHandler};

#[derive(Debug, Serialize, Deserialize)]
struct Envelope {
    #[serde(rename = "type")]
    kind: String,
    #[serde(default)]
    id: Option<String>,
    #[serde(default)]
    command: Option<Value>,
    #[serde(default)]
    success: Option<bool>,
    #[serde(default)]
    data: Option<Value>,
    #[serde(default)]
    error: Option<String>,
    #[serde(default)]
    role: Option<String>,
    #[serde(default)]
    version: Option<String>,
}

pub fn run_executor_client(server_addr: &str, controller: Controller) -> Result<()> {
    println!("[executor] Connecting to bridge at {}…", server_addr);
    let stream = TcpStream::connect(server_addr)
        .with_context(|| format!("Failed to connect to bridge at {}", server_addr))?;
    stream.set_read_timeout(Some(Duration::from_secs(120)))?;
    stream.set_write_timeout(Some(Duration::from_secs(30)))?;

    let mut writer = stream.try_clone().context("clone tcp stream")?;
    let mut reader = BufReader::new(stream);

    let hello = Envelope {
        kind: "hello".into(),
        id: None,
        command: None,
        success: None,
        data: None,
        error: None,
        role: Some("executor".into()),
        version: Some("1".into()),
    };
    write_line(&mut writer, &hello)?;

    let mut line = String::new();
    line.clear();
    reader.read_line(&mut line)?;
    let ack: Envelope = serde_json::from_str(line.trim())
        .context("invalid hello_ack from bridge")?;
    if ack.kind != "hello_ack" {
        anyhow::bail!("expected hello_ack, got {}", ack.kind);
    }
    println!("[executor] Connected to bridge — waiting for commands");

    loop {
        line.clear();
        match reader.read_line(&mut line) {
            Ok(0) => {
                println!("[executor] Bridge closed connection");
                break;
            }
            Ok(_) => {}
            Err(e) => {
                // Timeouts are expected while idle; keep waiting.
                if e.kind() == std::io::ErrorKind::WouldBlock
                    || e.kind() == std::io::ErrorKind::TimedOut
                {
                    continue;
                }
                return Err(e.into());
            }
        }

        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }

        let env: Envelope = match serde_json::from_str(trimmed) {
            Ok(v) => v,
            Err(e) => {
                eprintln!("[executor] bad envelope: {} ({})", e, trimmed);
                continue;
            }
        };

        if env.kind == "ping" {
            let pong = Envelope {
                kind: "pong".into(),
                id: env.id,
                command: None,
                success: None,
                data: None,
                error: None,
                role: None,
                version: None,
            };
            write_line(&mut writer, &pong)?;
            continue;
        }

        if env.kind != "command" {
            continue;
        }

        let id = env.id.clone().unwrap_or_default();
        let cmd_value = env.command.unwrap_or(Value::Null);
        let response = match serde_json::from_value::<IPCCommand>(cmd_value) {
            Ok(cmd) => {
                if let Err(e) = cmd.validate() {
                    crate::ipc_handler::IPCResponse::failure(e.to_string())
                } else {
                    IPCHandler::execute_command(&controller, cmd)
                }
            }
            Err(e) => crate::ipc_handler::IPCResponse::failure(format!("invalid command: {}", e)),
        };

        let result = Envelope {
            kind: "result".into(),
            id: Some(id),
            command: None,
            success: Some(response.success),
            data: response.data,
            error: response.error,
            role: None,
            version: None,
        };
        write_line(&mut writer, &result)?;
    }

    Ok(())
}

fn write_line(writer: &mut TcpStream, env: &Envelope) -> Result<()> {
    let mut bytes = serde_json::to_vec(env)?;
    bytes.push(b'\n');
    writer.write_all(&bytes)?;
    writer.flush()?;
    Ok(())
}
