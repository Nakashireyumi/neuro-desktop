// ============================================================
// tests/test_channels.cpp - Communication Channel Tests
// ============================================================

#include <gtest/gtest.h>
#include <gmock/gmock.h>
#include "process_handler.h"

using namespace neuro;

class ChannelTest : public ::testing::Test {
protected:
    void SetUp() override {}
    void TearDown() override {
        // Clean up any test files
    }
};

TEST_F(ChannelTest, FileIPCChannel_Initialize) {
    FileIPCChannel channel("test_ipc.json");
    EXPECT_TRUE(channel.initialize());
    EXPECT_EQ(channel.get_method(), CommMethod::FILE_IPC);
}

TEST_F(ChannelTest, FileIPCChannel_SendReceive) {
    FileIPCChannel channel("test_ipc.json");
    ASSERT_TRUE(channel.initialize());
    
    Message send_msg;
    send_msg.type = MessageType::COMMAND;
    send_msg.source_process = "test";
    send_msg.target_process = "target";
    send_msg.command = "ping";
    send_msg.data = "{}";
    
    EXPECT_TRUE(channel.send(send_msg));
    
    // Simulate response
    // In real scenario, another process would write the response
    
    channel.close();
}

TEST_F(ChannelTest, StdioChannel_Initialize) {
    StdioChannel channel;
    EXPECT_TRUE(channel.initialize());
    EXPECT_EQ(channel.get_method(), CommMethod::STDIO);
    
    EXPECT_NE(channel.get_stdin_handle(), nullptr);
    EXPECT_NE(channel.get_stdout_handle(), nullptr);
    
    channel.close();
}

// ============================================================
// Main test runner
// ============================================================

int main(int argc, char** argv) {
    testing::InitGoogleTest(&argc, argv);
    return RUN_ALL_TESTS();
}