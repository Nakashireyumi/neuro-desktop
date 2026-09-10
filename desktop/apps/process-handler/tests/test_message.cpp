// ============================================================
// tests/test_message.cpp - Message System Tests
// ============================================================

#include <gtest/gtest.h>
#include <gmock/gmock.h>
#include "process_handler.h"

using namespace neuro;

class MessageTest : public ::testing::Test {
protected:
    void SetUp() override {
        // Setup code
    }
    
    void TearDown() override {
        // Cleanup code
    }
};

TEST_F(MessageTest, MessageCreation) {
    Message msg;
    msg.type = MessageType::COMMAND;
    msg.source_process = "test_process";
    msg.target_process = "rust_main";
    msg.command = "execute";
    msg.data = R"({"action": "test"})";
    msg.timestamp = 123456789;
    msg.message_id = "msg-001";
    
    EXPECT_EQ(msg.type, MessageType::COMMAND);
    EXPECT_EQ(msg.source_process, "test_process");
    EXPECT_EQ(msg.command, "execute");
}

TEST_F(MessageTest, MessageToJson) {
    Message msg;
    msg.type = MessageType::COMMAND;
    msg.source_process = "test";
    msg.target_process = "target";
    msg.command = "action";
    msg.data = "{}";
    msg.timestamp = 1000;
    msg.message_id = "id1";
    
    std::string json = msg.to_json();
    
    EXPECT_TRUE(json.find("\"command\":\"action\"") != std::string::npos);
    EXPECT_TRUE(json.find("\"source\":\"test\"") != std::string::npos);
}

TEST_F(MessageTest, MessageValidation) {
    Message valid_msg;
    valid_msg.source_process = "source";
    valid_msg.target_process = "target";
    valid_msg.command = "test";
    valid_msg.data = "{}";
    
    std::string error;
    EXPECT_TRUE(MessageValidator::validate_message(valid_msg, error));
    EXPECT_TRUE(error.empty());
}

TEST_F(MessageTest, MessageValidation_EmptySource) {
    Message invalid_msg;
    invalid_msg.source_process = "";
    invalid_msg.target_process = "target";
    invalid_msg.command = "test";
    invalid_msg.data = "{}";
    
    std::string error;
    EXPECT_FALSE(MessageValidator::validate_message(invalid_msg, error));
    EXPECT_FALSE(error.empty());
}

TEST_F(MessageTest, MessageValidation_EmptyCommand) {
    Message invalid_msg;
    invalid_msg.source_process = "source";
    invalid_msg.target_process = "target";
    invalid_msg.command = "";
    invalid_msg.data = "{}";
    
    std::string error;
    EXPECT_FALSE(MessageValidator::validate_message(invalid_msg, error));
    EXPECT_EQ(error, "Command is empty");
}

// ============================================================
// Main test runner
// ============================================================

int main(int argc, char** argv) {
    testing::InitGoogleTest(&argc, argv);
    return RUN_ALL_TESTS();
}