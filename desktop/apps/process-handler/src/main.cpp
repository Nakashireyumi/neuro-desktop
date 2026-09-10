// ============================================================
// main.cpp - Process Handler Entry Point
// ============================================================

#include "process_handler.h"
#include <iostream>
#include <csignal>
#include <atomic>
#include <filesystem>
#include <string>

using namespace neuro;

static std::string native_binary(const char* base_name) {
#ifdef _WIN32
    return std::string("./") + base_name + ".exe";
#else
    return std::string("./") + base_name;
#endif
}

std::atomic<bool> g_shutdown_requested{false};
ProcessManager* g_manager = nullptr;

void signal_handler(int signal) {
    std::cout << "\nReceived signal " << signal << ", shutting down..." << std::endl;
    g_shutdown_requested = true;
    if (g_manager) {
        g_manager->shutdown();
    }
}

void setup_signal_handlers() {
#ifdef _WIN32
    signal(SIGINT, signal_handler);
    signal(SIGTERM, signal_handler);
    signal(SIGABRT, signal_handler);
#else
    struct sigaction sa;
    sa.sa_handler = signal_handler;
    sigemptyset(&sa.sa_mask);
    sa.sa_flags = 0;
    
    sigaction(SIGINT, &sa, nullptr);
    sigaction(SIGTERM, &sa, nullptr);
    sigaction(SIGHUP, &sa, nullptr);
#endif
}

int main(int argc, char* argv[]) {
    (void)argc;
    (void)argv;
    std::cout << "=======================================================" << std::endl;
    std::cout << "        Neuro Desktop Process Handler" << std::endl;
    std::cout << "=======================================================" << std::endl;
    std::cout << std::endl;
    
    setup_signal_handlers();
    
    ProcessManager manager;
    g_manager = &manager;
    
    const std::string rust_bin = native_binary("neuro-desktop");
    const std::string go_bin = native_binary("neuro-integration");
    const std::string relay_bin = native_binary("neuro-relay");

    // ============================================================
    // Configure Rust Main Process (executor / client)
    // ============================================================
    ProcessConfig rust_config;
    rust_config.type = ProcessType::RUST_MAIN;
    rust_config.name = "rust_main";
    rust_config.executable_path = rust_bin;
    rust_config.args = {"--supervised"};
    rust_config.comm_methods = {CommMethod::FILE_IPC, CommMethod::STDIO};
    rust_config.auto_restart = true;
    rust_config.max_restart_attempts = 3;
    rust_config.enable_heartbeat = true;
    rust_config.heartbeat_interval = std::chrono::seconds(5);
    rust_config.env_vars["NEURO_IPC_FILE"] = "./ipc_rust_main.json";
    rust_config.env_vars["NEURO_SUPERVISED"] = "true";
    
    // ============================================================
    // Configure Go Integration
    // ============================================================
    ProcessConfig go_config;
    go_config.type = ProcessType::GO_INTEGRATION;
    go_config.name = "go_integration";
    go_config.executable_path = go_bin;
    go_config.comm_methods = {CommMethod::FILE_IPC};
    go_config.auto_restart = true;
    go_config.max_restart_attempts = 5;
    go_config.enable_heartbeat = true;
    go_config.heartbeat_interval = std::chrono::seconds(10);
    go_config.args = {
        "--ws-url", "ws://localhost:8000",
        "--ipc-file", "./neuro-integration-code-ipc.json",
        "--permissions-file", "./permissions.json"
    };
    go_config.depends_on = {"rust_main"};  // Start after Rust

    const bool relay_exists = std::filesystem::exists(relay_bin);
    ProcessConfig relay_config;
    if (relay_exists) {
        relay_config.type = ProcessType::CUSTOM;
        relay_config.name = "neuro_relay";
        relay_config.executable_path = relay_bin;
        relay_config.args = {
            "-name", "NeuroDesktopHub",
            "-neuro-url", "ws://localhost:8000",
            "-emulated-addr", "127.0.0.1:8001"
        };
        relay_config.comm_methods = {CommMethod::FILE_IPC};
        relay_config.auto_restart = true;
        relay_config.max_restart_attempts = 5;
        relay_config.enable_heartbeat = false;
        relay_config.depends_on = {"rust_main"};

        go_config.args = {
            "--ws-url", "ws://127.0.0.1:8001",
            "--ipc-file", "./neuro-integration-code-ipc.json",
            "--permissions-file", "./permissions.json"
        };
    }
    
    // ============================================================
    // Register Processes
    // ============================================================
    std::cout << "[1/3] Registering processes..." << std::endl;
    
    if (!manager.register_process(rust_config)) {
        std::cerr << "Failed to register Rust process" << std::endl;
        return 1;
    }
    
    if (!manager.register_process(go_config)) {
        std::cerr << "Failed to register Go process" << std::endl;
        return 1;
    }

    if (relay_exists) {
        if (!manager.register_process(relay_config)) {
            std::cerr << "Failed to register relay process" << std::endl;
            return 1;
        }
    }
    
    std::cout << "      All processes registered" << std::endl;
    std::cout << std::endl;
    
    // ============================================================
    // Setup Message Handlers
    // ============================================================
    std::cout << "[2/3] Setting up message handlers..." << std::endl;
    
    manager.register_message_handler("heartbeat", [](const Message& msg) {
        // Update last heartbeat time
        std::cout << "Heartbeat from " << msg.source_process << std::endl;
    });
    
    manager.register_message_handler("status", [&manager](const Message& msg) {
        (void)msg;
        std::cout << "\n=== Process Status ===" << std::endl;
        for (const auto& info : manager.get_all_processes()) {
            std::cout << info.config.name << ": " 
                     << static_cast<int>(info.state) 
                     << " (PID: " << info.pid << ")" << std::endl;
        }
        std::cout << "======================" << std::endl;
    });
    
    manager.register_message_handler("restart", [&manager](const Message& msg) {
        std::cout << "Restart requested for " << msg.data << std::endl;
        // Parse process name from msg.data and restart
    });
    
    manager.register_message_handler("shutdown", [](const Message& msg) {
        (void)msg;
        std::cout << "Shutdown requested" << std::endl;
        g_shutdown_requested = true;
    });
    
    std::cout << "      Message handlers configured" << std::endl;
    std::cout << std::endl;
    
    // ============================================================
    // Start All Processes
    // ============================================================
    std::cout << "[3/3] Starting all processes..." << std::endl;
    
    std::cout << "=======================================================" << std::endl;
    std::cout << "  All systems ready!" << std::endl;
    std::cout << "  Process Handler is managing all processes" << std::endl;
    std::cout << "=======================================================" << std::endl;
    std::cout << std::endl;
    std::cout << "Press Ctrl+C to stop all processes" << std::endl;
    std::cout << std::endl;
    
    // ============================================================
    // Run Event Loop
    // ============================================================
    manager.run();
    
    std::cout << std::endl;
    std::cout << "Process Handler stopped" << std::endl;
    
    return 0;
}

// Example of how to add a new process:
/*

ProcessConfig custom_config;
custom_config.type = ProcessType::CUSTOM;
custom_config.name = "my_custom_service";
custom_config.executable_path = "./my-service.exe";
custom_config.args = {"--port", "3000", "--verbose"};
custom_config.comm_methods = {CommMethod::FILE_IPC, CommMethod::NAMED_PIPE};
custom_config.auto_restart = true;
custom_config.max_restart_attempts = 5;
custom_config.restart_delay = std::chrono::seconds(10);
custom_config.enable_heartbeat = true;
custom_config.heartbeat_interval = std::chrono::seconds(30);
custom_config.heartbeat_timeout = std::chrono::seconds(90);
custom_config.depends_on = {"rust_main", "go_integration"};
custom_config.env_vars["MY_SERVICE_PORT"] = "3000";
custom_config.env_vars["MY_SERVICE_MODE"] = "production";

manager.register_process(custom_config);

// That's it! The process will be started automatically in dependency order
// and monitored for crashes with automatic restarts.

*/
