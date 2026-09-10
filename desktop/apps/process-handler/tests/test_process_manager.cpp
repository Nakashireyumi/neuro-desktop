// ============================================================
// tests/test_process_manager.cpp - Process Manager Tests
// ============================================================

#include <gtest/gtest.h>
#include <gmock/gmock.h>
#include "process_handler.h"

using namespace neuro;

class ProcessManagerTest : public ::testing::Test {
protected:
    std::unique_ptr<ProcessManager> manager;
    
    void SetUp() override {
        manager = std::make_unique<ProcessManager>();
    }
    
    void TearDown() override {
        manager->shutdown();
        manager.reset();
    }
};

TEST_F(ProcessManagerTest, RegisterProcess) {
    ProcessConfig config;
    config.type = ProcessType::CUSTOM;
    config.name = "test_process";
    config.executable_path = "./test.exe";
    config.comm_methods = {CommMethod::FILE_IPC};
    
    EXPECT_TRUE(manager->register_process(config));
}

TEST_F(ProcessManagerTest, RegisterProcess_Duplicate) {
    ProcessConfig config;
    config.name = "test_process";
    config.executable_path = "./test.exe";
    
    EXPECT_TRUE(manager->register_process(config));
    EXPECT_FALSE(manager->register_process(config));  // Should fail
}

TEST_F(ProcessManagerTest, ProcessState) {
    ProcessConfig config;
    config.name = "test_process";
    config.executable_path = "./test.exe";
    
    manager->register_process(config);
    
    ProcessState state = manager->get_process_state("test_process");
    EXPECT_EQ(state, ProcessState::CREATED);
}

TEST_F(ProcessManagerTest, ProcessState_NotFound) {
    ProcessState state = manager->get_process_state("nonexistent");
    EXPECT_EQ(state, ProcessState::STOPPED);
}

TEST_F(ProcessManagerTest, GetAllProcesses) {
    ProcessConfig config1;
    config1.name = "process1";
    config1.executable_path = "./test1.exe";
    
    ProcessConfig config2;
    config2.name = "process2";
    config2.executable_path = "./test2.exe";
    
    manager->register_process(config1);
    manager->register_process(config2);
    
    auto processes = manager->get_all_processes();
    EXPECT_EQ(processes.size(), 2);
}

TEST_F(ProcessManagerTest, MessageRouting) {
    bool handler_called = false;
    std::string received_command;
    
    manager->register_message_handler("test_command", 
        [&](const Message& msg) {
            handler_called = true;
            received_command = msg.command;
        }
    );
    
    // This would normally be called when a message arrives
    // For testing, we'd need to trigger it manually or use a mock
}

TEST_F(ProcessManagerTest, DependencyResolution) {
    ProcessConfig config1;
    config1.name = "base_process";
    config1.executable_path = "./base.exe";
    
    ProcessConfig config2;
    config2.name = "dependent_process";
    config2.executable_path = "./dependent.exe";
    config2.depends_on = {"base_process"};
    
    manager->register_process(config1);
    manager->register_process(config2);
    
    // Dependent process should not start before base process
    ProcessState base_state = manager->get_process_state("base_process");
    ProcessState dep_state = manager->get_process_state("dependent_process");
    
    EXPECT_EQ(base_state, ProcessState::CREATED);
    EXPECT_EQ(dep_state, ProcessState::CREATED);
}

// ============================================================
// Main test runner
// ============================================================

int main(int argc, char** argv) {
    testing::InitGoogleTest(&argc, argv);
    return RUN_ALL_TESTS();
}