// ============================================================
// process_handler.cpp - Implementation
// ============================================================

#include "process_handler.h"
#include <fstream>
#include <sstream>
#include <thread>
#include <chrono>
#include <iostream>
#include <atomic>

#ifdef _WIN32
#include <windows.h>
#include <process.h>
#else
#include <unistd.h>
#include <sys/wait.h>
#include <signal.h>
#endif

namespace neuro {

// ============================================================
// Message Implementation
// ============================================================

std::string Message::to_json() const {
    std::ostringstream oss;
    oss << "{";
    oss << "\"type\":\"" << static_cast<int>(type) << "\",";
    oss << "\"source\":\"" << source_process << "\",";
    oss << "\"target\":\"" << target_process << "\",";
    oss << "\"command\":\"" << command << "\",";
    oss << "\"data\":" << data << ",";
    oss << "\"timestamp\":" << timestamp << ",";
    oss << "\"message_id\":\"" << message_id << "\"";
    oss << "}";
    return oss.str();
}

Message Message::from_json(const std::string& json) {
    (void)json;
    // Simple JSON parsing (use a proper library in production)
    Message msg;
    // TODO: Implement proper JSON parsing
    return msg;
}

// ============================================================
// FileIPCChannel Implementation
// ============================================================

FileIPCChannel::FileIPCChannel(const std::string& ipc_path) 
    : ipc_file_path(ipc_path),
      response_file_path(ipc_path + ".response") {
}

bool FileIPCChannel::initialize() {
    // Ensure directory exists
    return true;
}

bool FileIPCChannel::send(const Message& msg) {
    std::lock_guard<std::mutex> lock(file_mutex);
    
    std::ofstream file(ipc_file_path);
    if (!file.is_open()) {
        return false;
    }
    
    file << msg.to_json();
    file.close();
    return true;
}

bool FileIPCChannel::receive(Message& msg, int timeout_ms) {
    auto start = std::chrono::steady_clock::now();
    
    while (true) {
        std::ifstream file(response_file_path);
        if (file.is_open()) {
            std::string json((std::istreambuf_iterator<char>(file)),
                           std::istreambuf_iterator<char>());
            file.close();
            
            if (!json.empty()) {
                msg = Message::from_json(json);
                // Delete response file
                std::remove(response_file_path.c_str());
                return true;
            }
        }
        
        auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::steady_clock::now() - start
        ).count();
        
        if (elapsed >= timeout_ms) {
            return false;
        }
        
        std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }
}

void FileIPCChannel::close() {
    std::remove(ipc_file_path.c_str());
    std::remove(response_file_path.c_str());
}

// ============================================================
// StdioChannel Implementation
// ============================================================

StdioChannel::StdioChannel() 
    : stdin_pipe(nullptr), stdout_pipe(nullptr), stderr_pipe(nullptr) {
}

bool StdioChannel::initialize() {
#ifdef _WIN32
    SECURITY_ATTRIBUTES sa;
    sa.nLength = sizeof(SECURITY_ATTRIBUTES);
    sa.bInheritHandle = TRUE;
    sa.lpSecurityDescriptor = NULL;
    
    HANDLE stdin_read, stdin_write;
    HANDLE stdout_read, stdout_write;
    HANDLE stderr_read, stderr_write;
    
    if (!CreatePipe(&stdin_read, &stdin_write, &sa, 0)) return false;
    if (!CreatePipe(&stdout_read, &stdout_write, &sa, 0)) return false;
    if (!CreatePipe(&stderr_read, &stderr_write, &sa, 0)) return false;
    
    // Ensure the write handle to stdin is not inherited
    SetHandleInformation(stdin_write, HANDLE_FLAG_INHERIT, 0);
    SetHandleInformation(stdout_read, HANDLE_FLAG_INHERIT, 0);
    SetHandleInformation(stderr_read, HANDLE_FLAG_INHERIT, 0);
    
    stdin_pipe = stdin_write;
    stdout_pipe = stdout_read;
    stderr_pipe = stderr_read;
    
    return true;
#else
    int stdin_fds[2], stdout_fds[2], stderr_fds[2];
    
    if (pipe(stdin_fds) != 0) return false;
    if (pipe(stdout_fds) != 0) return false;
    if (pipe(stderr_fds) != 0) return false;
    
    stdin_pipe = (void*)(intptr_t)stdin_fds[1];
    stdout_pipe = (void*)(intptr_t)stdout_fds[0];
    stderr_pipe = (void*)(intptr_t)stderr_fds[0];
    
    return true;
#endif
}

bool StdioChannel::send(const Message& msg) {
    std::string json = msg.to_json() + "\n";
    
#ifdef _WIN32
    DWORD written;
    return WriteFile(stdin_pipe, json.c_str(), static_cast<DWORD>(json.length()), &written, NULL);
#else
    int fd = (int)(intptr_t)stdin_pipe;
    return write(fd, json.c_str(), json.length()) > 0;
#endif
}

bool StdioChannel::receive(Message& msg, int timeout_ms) {
    (void)timeout_ms;
    // Non-blocking read with timeout
    char buffer[4096];
    
#ifdef _WIN32
    DWORD available;
    if (!PeekNamedPipe(stdout_pipe, NULL, 0, NULL, &available, NULL)) {
        return false;
    }
    
    if (available > 0) {
        DWORD read;
        if (ReadFile(stdout_pipe, buffer, sizeof(buffer) - 1, &read, NULL)) {
            buffer[read] = '\0';
            msg = Message::from_json(buffer);
            return true;
        }
    }
#else
    // Use select for timeout
    fd_set readfds;
    struct timeval tv;
    int fd = (int)(intptr_t)stdout_pipe;
    
    FD_ZERO(&readfds);
    FD_SET(fd, &readfds);
    
    tv.tv_sec = timeout_ms / 1000;
    tv.tv_usec = (timeout_ms % 1000) * 1000;
    
    int ret = select(fd + 1, &readfds, NULL, NULL, &tv);
    if (ret > 0) {
        ssize_t n = read(fd, buffer, sizeof(buffer) - 1);
        if (n > 0) {
            buffer[n] = '\0';
            msg = Message::from_json(buffer);
            return true;
        }
    }
#endif
    
    return false;
}

void StdioChannel::close() {
#ifdef _WIN32
    if (stdin_pipe) CloseHandle(stdin_pipe);
    if (stdout_pipe) CloseHandle(stdout_pipe);
    if (stderr_pipe) CloseHandle(stderr_pipe);
#else
    if (stdin_pipe) ::close((int)(intptr_t)stdin_pipe);
    if (stdout_pipe) ::close((int)(intptr_t)stdout_pipe);
    if (stderr_pipe) ::close((int)(intptr_t)stderr_pipe);
#endif
}

// ============================================================
// MessageValidator Implementation
// ============================================================

bool MessageValidator::validate_message(const Message& msg, std::string& error) {
    if (msg.source_process.empty()) {
        error = "Source process is empty";
        return false;
    }
    
    if (msg.target_process.empty()) {
        error = "Target process is empty";
        return false;
    }
    
    if (msg.command.empty()) {
        error = "Command is empty";
        return false;
    }
    
    if (!is_safe_json(msg.data)) {
        error = "Invalid JSON data";
        return false;
    }
    
    return true;
}

bool MessageValidator::is_safe_json(const std::string& json) {
    // Basic validation - ensure it's valid JSON and not too large
    if (json.length() > 1024 * 1024) { // 1MB limit
        return false;
    }
    
    // TODO: Add proper JSON validation
    return true;
}

bool MessageValidator::check_rate_limit(const std::string& source, int max_per_second) {
    (void)source;
    (void)max_per_second;
    // TODO: Implement rate limiting per source
    return true;
}

// ============================================================
// MessageRouter Implementation
// ============================================================

void MessageRouter::register_handler(const std::string& command, 
                                     std::function<void(const Message&)> handler) {
    std::lock_guard<std::mutex> lock(router_mutex);
    handlers[command].push_back(handler);
}

void MessageRouter::route_message(const Message& msg) {
    std::lock_guard<std::mutex> lock(router_mutex);
    
    auto it = handlers.find(msg.command);
    if (it != handlers.end()) {
        for (auto& handler : it->second) {
            handler(msg);
        }
    }
}

void MessageRouter::unregister_all() {
    std::lock_guard<std::mutex> lock(router_mutex);
    handlers.clear();
}

// ============================================================
// ProcessManager Implementation
// ============================================================

ProcessManager::ProcessManager() {
    router = std::make_unique<MessageRouter>();
}

ProcessManager::~ProcessManager() {
    shutdown();
}

bool ProcessManager::register_process(const ProcessConfig& config) {
    std::lock_guard<std::mutex> lock(manager_mutex);
    
    if (processes.find(config.name) != processes.end()) {
        std::cerr << "Process " << config.name << " already registered" << std::endl;
        return false;
    }
    
    ProcessInfo info;
    info.config = config;
    info.state = ProcessState::CREATED;
    info.platform_handle = nullptr;
    info.pid = 0;
    
    processes[config.name] = info;
    
    // Create communication channels
    for (auto method : config.comm_methods) {
        std::unique_ptr<ICommChannel> channel;
        
        switch (method) {
            case CommMethod::FILE_IPC:
                channel = std::make_unique<FileIPCChannel>(
                    "ipc_" + config.name + ".json"
                );
                break;
            case CommMethod::STDIO:
                channel = std::make_unique<StdioChannel>();
                break;
            // Add other channel types...
        }
        
        if (channel && channel->initialize()) {
            channels[config.name + "_" + std::to_string(static_cast<int>(method))] 
                = std::move(channel);
        }
    }
    
    std::cout << "Registered process: " << config.name << std::endl;
    return true;
}

bool ProcessManager::spawn_process(ProcessInfo& info) {
#ifdef _WIN32
    STARTUPINFOA si;
    PROCESS_INFORMATION pi;
    
    ZeroMemory(&si, sizeof(si));
    si.cb = sizeof(si);
    ZeroMemory(&pi, sizeof(pi));
    
    // Build command line
    std::string cmdline = info.config.executable_path;
    for (const auto& arg : info.config.args) {
        cmdline += " " + arg;
    }
    
    // Set environment variables
    // TODO: Build environment block
    
    if (!CreateProcessA(
        NULL,
        const_cast<char*>(cmdline.c_str()),
        NULL,
        NULL,
        FALSE,
        0,
        NULL,
        NULL,
        &si,
        &pi
    )) {
        info.last_error = "Failed to create process";
        return false;
    }
    
    info.platform_handle = pi.hProcess;
    info.pid = pi.dwProcessId;
    CloseHandle(pi.hThread);
    
#else
    pid_t pid = fork();
    
    if (pid < 0) {
        info.last_error = "Fork failed";
        return false;
    }
    
    if (pid == 0) {
        // Child process
        std::vector<char*> args;
        args.push_back(const_cast<char*>(info.config.executable_path.c_str()));
        for (auto& arg : info.config.args) {
            args.push_back(const_cast<char*>(arg.c_str()));
        }
        args.push_back(nullptr);
        
        execvp(args[0], args.data());
        exit(1); // If exec fails
    }
    
    // Parent process
    info.platform_handle = (void*)(intptr_t)pid;
    info.pid = pid;
#endif
    
    info.state = ProcessState::RUNNING;
    info.start_time = std::chrono::system_clock::now();
    info.last_heartbeat = std::chrono::system_clock::now();
    
    return true;
}

bool ProcessManager::start_process(const std::string& name) {
    std::lock_guard<std::mutex> lock(manager_mutex);
    
    auto it = processes.find(name);
    if (it == processes.end()) {
        std::cerr << "Process " << name << " not found" << std::endl;
        return false;
    }
    
    auto& info = it->second;
    
    // Check dependencies
    if (!check_dependencies_ready(info.config)) {
        std::cerr << "Dependencies not ready for " << name << std::endl;
        return false;
    }
    
    info.state = ProcessState::STARTING;
    
    if (!spawn_process(info)) {
        info.state = ProcessState::CRASHED;
        return false;
    }
    
    std::cout << "Started process: " << name << " (PID: " << info.pid << ")" << std::endl;
    
    // Start monitoring thread
    std::thread([this, name]() {
        monitor_process(name);
    }).detach();
    
    return true;
}

bool ProcessManager::check_dependencies_ready(const ProcessConfig& config) {
    for (const auto& dep : config.depends_on) {
        auto it = processes.find(dep);
        if (it == processes.end() || it->second.state != ProcessState::RUNNING) {
            return false;
        }
    }
    return true;
}

void ProcessManager::monitor_process(const std::string& name) {
    while (running) {
        std::this_thread::sleep_for(std::chrono::seconds(1));
        
        std::lock_guard<std::mutex> lock(manager_mutex);
        auto it = processes.find(name);
        if (it == processes.end()) break;
        
        auto& info = it->second;
        
        // Check if process is still alive
#ifdef _WIN32
        DWORD exit_code;
        if (GetExitCodeProcess(info.platform_handle, &exit_code)) {
            if (exit_code != STILL_ACTIVE) {
                handle_process_crash(name);
                break;
            }
        }
#else
        int status;
        pid_t result = waitpid(info.pid, &status, WNOHANG);
        if (result != 0) {
            handle_process_crash(name);
            break;
        }
#endif
        
        // Check heartbeat
        if (info.config.enable_heartbeat) {
            auto now = std::chrono::system_clock::now();
            auto elapsed = std::chrono::duration_cast<std::chrono::seconds>(
                now - info.last_heartbeat
            );
            
            if (elapsed > info.config.heartbeat_timeout) {
                std::cerr << "Process " << name << " heartbeat timeout" << std::endl;
                handle_process_crash(name);
                break;
            }
        }
    }
}

void ProcessManager::handle_process_crash(const std::string& name) {
    auto& info = processes[name];
    info.state = ProcessState::CRASHED;
    
    std::cerr << "Process " << name << " crashed!" << std::endl;
    
    if (info.config.auto_restart && info.restart_count < info.config.max_restart_attempts) {
        std::cout << "Attempting to restart " << name << "..." << std::endl;
        std::this_thread::sleep_for(info.config.restart_delay);
        restart_process(name);
    }
}

bool ProcessManager::stop_process(const std::string& name, bool force) {
    std::lock_guard<std::mutex> lock(manager_mutex);
    
    auto it = processes.find(name);
    if (it == processes.end()) return false;
    
    auto& info = it->second;
    info.state = ProcessState::STOPPING;
    
#ifdef _WIN32
    if (force) {
        TerminateProcess(info.platform_handle, 1);
    } else {
        // Try graceful shutdown first
        // TODO: Send shutdown message
        WaitForSingleObject(info.platform_handle, 5000);
        TerminateProcess(info.platform_handle, 0);
    }
    CloseHandle(info.platform_handle);
#else
    if (force) {
        kill(info.pid, SIGKILL);
    } else {
        kill(info.pid, SIGTERM);
        // Wait a bit
        sleep(2);
        kill(info.pid, SIGKILL);
    }
#endif
    
    info.state = ProcessState::STOPPED;
    info.platform_handle = nullptr;
    info.pid = 0;
    
    return true;
}

bool ProcessManager::restart_process(const std::string& name) {
    stop_process(name, false);
    std::this_thread::sleep_for(std::chrono::milliseconds(500));
    return start_process(name);
}

bool ProcessManager::send_message(const std::string& target, const Message& msg) {
    std::lock_guard<std::mutex> lock(manager_mutex);

    auto proc_it = processes.find(target);
    if (proc_it == processes.end()) {
        return false;
    }

    const auto& info = proc_it->second;
    for (auto method : info.config.comm_methods) {
        const std::string channel_key = target + "_" + std::to_string(static_cast<int>(method));
        auto channel_it = channels.find(channel_key);
        if (channel_it != channels.end() && channel_it->second->send(msg)) {
            return true;
        }
    }

    return false;
}

bool ProcessManager::broadcast_message(const Message& msg) {
    std::lock_guard<std::mutex> lock(manager_mutex);

    bool sent_any = false;
    for (const auto& [name, info] : processes) {
        if (info.state != ProcessState::RUNNING) {
            continue;
        }

        Message target_msg = msg;
        target_msg.target_process = name;

        for (auto method : info.config.comm_methods) {
            const std::string channel_key = name + "_" + std::to_string(static_cast<int>(method));
            auto channel_it = channels.find(channel_key);
            if (channel_it != channels.end() && channel_it->second->send(target_msg)) {
                sent_any = true;
                break;
            }
        }
    }

    return sent_any;
}

void ProcessManager::register_message_handler(
    const std::string& command,
    std::function<void(const Message&)> handler
) {
    if (router) {
        router->register_handler(command, std::move(handler));
    }
}

void ProcessManager::send_heartbeat_check(const std::string& name) {
    Message heartbeat;
    heartbeat.type = MessageType::HEARTBEAT;
    heartbeat.source_process = "process_handler";
    heartbeat.target_process = name;
    heartbeat.command = "heartbeat";
    heartbeat.data = "{}";
    heartbeat.timestamp = static_cast<uint64_t>(
        std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::system_clock::now().time_since_epoch()
        ).count()
    );
    heartbeat.message_id = "hb_" + name;

    send_message(name, heartbeat);
}

void ProcessManager::enable_health_monitoring(bool enable) {
    (void)enable;
    // TODO: add dedicated health monitor thread/toggle when implemented.
}

std::string ProcessManager::get_health_report() {
    std::lock_guard<std::mutex> lock(manager_mutex);

    std::ostringstream oss;
    oss << "Process Health Report\n";
    for (const auto& [name, info] : processes) {
        oss << "- " << name
            << " state=" << static_cast<int>(info.state)
            << " pid=" << info.pid
            << " restarts=" << info.restart_count
            << "\n";
    }
    return oss.str();
}

void ProcessManager::start_all() {
    // Start processes in dependency order
    bool progress = true;
    while (progress) {
        progress = false;
        for (auto& [name, info] : processes) {
            if (info.state == ProcessState::CREATED) {
                if (check_dependencies_ready(info.config)) {
                    start_process(name);
                    progress = true;
                }
            }
        }
    }
}

void ProcessManager::stop_all() {
    for (auto& [name, info] : processes) {
        if (info.state == ProcessState::RUNNING) {
            stop_process(name);
        }
    }
}

void ProcessManager::run() {
    running = true;
    start_all();
    
    // Main event loop
    while (running) {
        // Process messages from all channels
        for (auto& [name, channel] : channels) {
            Message msg;
            if (channel->receive(msg, 100)) {
                std::string error;
                if (MessageValidator::validate_message(msg, error)) {
                    router->route_message(msg);
                } else {
                    std::cerr << "Invalid message: " << error << std::endl;
                }
            }
        }
        
        std::this_thread::sleep_for(std::chrono::milliseconds(10));
    }
}

void ProcessManager::shutdown() {
    running = false;
    stop_all();
    
    for (auto& [name, channel] : channels) {
        channel->close();
    }
    
    channels.clear();
    processes.clear();
}

ProcessState ProcessManager::get_process_state(const std::string& name) {
    std::lock_guard<std::mutex> lock(manager_mutex);
    auto it = processes.find(name);
    return it != processes.end() ? it->second.state : ProcessState::STOPPED;
}

std::vector<ProcessInfo> ProcessManager::get_all_processes() {
    std::lock_guard<std::mutex> lock(manager_mutex);
    std::vector<ProcessInfo> result;
    for (const auto& [name, info] : processes) {
        result.push_back(info);
    }
    return result;
}

} // namespace neuro
