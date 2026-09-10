#pragma once
#include <string>
#include <memory>
#include <vector>
#include <map>
#include <functional>
#include <mutex>
#include <chrono>

// ============================================================
// process_handler.h - Core Process Management System
// ============================================================

namespace neuro {

// Process types
enum class ProcessType {
    RUST_MAIN,
    GO_INTEGRATION,
    PYTHON_CONTROLLER,
    FRONTEND_SERVER,
    CUSTOM
};

// Communication methods
enum class CommMethod {
    STDIO,          // Standard input/output pipes
    FILE_IPC,       // JSON file-based IPC
    NAMED_PIPE,     // Windows named pipes / Unix domain sockets
    SHARED_MEMORY,  // Shared memory segment
    TCP_SOCKET      // TCP localhost socket
};

// Process state
enum class ProcessState {
    CREATED,
    STARTING,
    RUNNING,
    STOPPING,
    STOPPED,
    CRASHED,
    ZOMBIE
};

// Message types
enum class MessageType {
    COMMAND,
    RESPONSE,
    EVENT,
    HEARTBEAT,
    SHUTDOWN,
    ERROR
};

// Message structure
struct Message {
    MessageType type;
    std::string source_process;
    std::string target_process;
    std::string command;
    std::string data;  // JSON payload
    uint64_t timestamp;
    std::string message_id;
    
    std::string to_json() const;
    static Message from_json(const std::string& json);
};

// Process configuration
struct ProcessConfig {
    ProcessType type;
    std::string name;
    std::string executable_path;
    std::vector<std::string> args;
    std::map<std::string, std::string> env_vars;
    std::vector<CommMethod> comm_methods;
    
    // Restart policy
    bool auto_restart = true;
    int max_restart_attempts = 3;
    std::chrono::seconds restart_delay{5};
    
    // Health check
    bool enable_heartbeat = true;
    std::chrono::seconds heartbeat_interval{5};
    std::chrono::seconds heartbeat_timeout{15};
    
    // Dependencies (must start before this process)
    std::vector<std::string> depends_on;
};

// Process information
struct ProcessInfo {
    ProcessConfig config;
    ProcessState state;
    void* platform_handle;  // HANDLE on Windows, pid_t on Unix
    uint32_t pid;
    std::chrono::system_clock::time_point start_time;
    std::chrono::system_clock::time_point last_heartbeat;
    int restart_count = 0;
    std::string last_error;
};

// Communication channel interface
class ICommChannel {
public:
    virtual ~ICommChannel() = default;
    virtual bool initialize() = 0;
    virtual bool send(const Message& msg) = 0;
    virtual bool receive(Message& msg, int timeout_ms) = 0;
    virtual void close() = 0;
    virtual CommMethod get_method() const = 0;
};

// File IPC channel (your current implementation)
class FileIPCChannel : public ICommChannel {
private:
    std::string ipc_file_path;
    std::string response_file_path;
    std::mutex file_mutex;
    
public:
    explicit FileIPCChannel(const std::string& ipc_path);
    bool initialize() override;
    bool send(const Message& msg) override;
    bool receive(Message& msg, int timeout_ms) override;
    void close() override;
    CommMethod get_method() const override { return CommMethod::FILE_IPC; }
};

// STDIO channel (pipes)
class StdioChannel : public ICommChannel {
private:
    void* stdin_pipe;
    void* stdout_pipe;
    void* stderr_pipe;
    
public:
    StdioChannel();
    bool initialize() override;
    bool send(const Message& msg) override;
    bool receive(Message& msg, int timeout_ms) override;
    void close() override;
    CommMethod get_method() const override { return CommMethod::STDIO; }
    
    void* get_stdin_handle() { return stdin_pipe; }
    void* get_stdout_handle() { return stdout_pipe; }
    void* get_stderr_handle() { return stderr_pipe; }
};

// Named pipe channel
class NamedPipeChannel : public ICommChannel {
private:
    std::string pipe_name;
    void* pipe_handle;
    
public:
    explicit NamedPipeChannel(const std::string& name);
    bool initialize() override;
    bool send(const Message& msg) override;
    bool receive(Message& msg, int timeout_ms) override;
    void close() override;
    CommMethod get_method() const override { return CommMethod::NAMED_PIPE; }
};

// Message validator
class MessageValidator {
public:
    static bool validate_message(const Message& msg, std::string& error);
    static bool is_safe_json(const std::string& json);
    static bool check_rate_limit(const std::string& source, int max_per_second);
};

// Message router
class MessageRouter {
private:
    std::map<std::string, std::vector<std::function<void(const Message&)>>> handlers;
    std::mutex router_mutex;
    
public:
    void register_handler(const std::string& command, 
                         std::function<void(const Message&)> handler);
    void route_message(const Message& msg);
    void unregister_all();
};

// Process Manager
class ProcessManager {
private:
    std::map<std::string, ProcessInfo> processes;
    std::map<std::string, std::unique_ptr<ICommChannel>> channels;
    std::unique_ptr<MessageRouter> router;
    std::mutex manager_mutex;
    bool running = false;
    
    // Internal methods
    bool spawn_process(ProcessInfo& info);
    void monitor_process(const std::string& name);
    void handle_process_crash(const std::string& name);
    void send_heartbeat_check(const std::string& name);
    bool check_dependencies_ready(const ProcessConfig& config);
    
public:
    ProcessManager();
    ~ProcessManager();
    
    // Process lifecycle
    bool register_process(const ProcessConfig& config);
    bool start_process(const std::string& name);
    bool stop_process(const std::string& name, bool force = false);
    bool restart_process(const std::string& name);
    
    // Communication
    bool send_message(const std::string& target, const Message& msg);
    bool broadcast_message(const Message& msg);
    void register_message_handler(const std::string& command,
                                  std::function<void(const Message&)> handler);
    
    // Status and control
    ProcessState get_process_state(const std::string& name);
    std::vector<ProcessInfo> get_all_processes();
    void start_all();
    void stop_all();
    void run();  // Main event loop
    void shutdown();
    
    // Health monitoring
    void enable_health_monitoring(bool enable);
    std::string get_health_report();
};

} // namespace neuro