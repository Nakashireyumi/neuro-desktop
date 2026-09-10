// ============================================================
// tests/test_integration.cpp - Integration Tests
// ============================================================

#include <gtest/gtest.h>
#include <gmock/gmock.h>
#include "process_handler.h"

using namespace neuro;

class IntegrationTest : public ::testing::Test {
protected:
    std::unique_ptr<ProcessManager> manager;
    
    void SetUp() override {
        manager = std::make_unique<ProcessManager>();
    }
    
    void TearDown() override {
        manager->shutdown();
    }
};

TEST_F(IntegrationTest, FullLifecycle) {
    // Register a mock process
    ProcessConfig config;
    config.name = "mock_process";
#ifdef _WIN32
    config.executable_path = "cmd.exe";
    config.args = {"/c", "timeout", "10"};  // Run for 10 seconds
#else
    config.executable_path = "/bin/sleep";
    config.args = {"10"};
#endif
    config.comm_methods = {CommMethod::FILE_IPC};
    config.auto_restart = false;
    config.enable_heartbeat = false;
    
    ASSERT_TRUE(manager->register_process(config));
    
    // Start the process
    ASSERT_TRUE(manager->start_process("mock_process"));
    
    // Give it a moment to start
    std::this_thread::sleep_for(std::chrono::milliseconds(500));
    
    // Check state
    ProcessState state = manager->get_process_state("mock_process");
    EXPECT_EQ(state, ProcessState::RUNNING);
    
    // Stop the process
    ASSERT_TRUE(manager->stop_process("mock_process", false));
    
    // Check state
    state = manager->get_process_state("mock_process");
    EXPECT_EQ(state, ProcessState::STOPPED);
}

TEST_F(IntegrationTest, MessageFlow) {
    // Test end-to-end message flow between processes
    // This would require setting up actual IPC channels
    
    FileIPCChannel channel("integration_test.json");
    ASSERT_TRUE(channel.initialize());
    
    Message msg;
    msg.type = MessageType::COMMAND;
    msg.source_process = "test";
    msg.target_process = "target";
    msg.command = "ping";
    msg.data = R"({"test": true})";
    msg.timestamp = std::chrono::system_clock::now().time_since_epoch().count();
    msg.message_id = "test-msg-001";
    
    EXPECT_TRUE(channel.send(msg));
    
    channel.close();
}

// ============================================================
// Main test runner
// ============================================================

int main(int argc, char** argv) {
    testing::InitGoogleTest(&argc, argv);
    return RUN_ALL_TESTS();
}