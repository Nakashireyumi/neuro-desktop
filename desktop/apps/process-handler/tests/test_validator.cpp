// ============================================================
// tests/test_validator.cpp - Validator Tests
// ============================================================

#include <gtest/gtest.h>
#include <gmock/gmock.h>
#include "process_handler.h"

using namespace neuro;

class ValidatorTest : public ::testing::Test {
protected:
    void SetUp() override {}
    void TearDown() override {}
};

TEST_F(ValidatorTest, SafeJsonValidation_Valid) {
    std::string valid_json = R"({"key": "value", "number": 123})";
    EXPECT_TRUE(MessageValidator::is_safe_json(valid_json));
}

TEST_F(ValidatorTest, SafeJsonValidation_TooLarge) {
    std::string large_json(2 * 1024 * 1024, 'x');  // 2MB
    EXPECT_FALSE(MessageValidator::is_safe_json(large_json));
}

TEST_F(ValidatorTest, RateLimiting) {
    // Test that rate limiting works
    EXPECT_TRUE(MessageValidator::check_rate_limit("source1", 100));
}

// ============================================================
// Main test runner
// ============================================================

int main(int argc, char** argv) {
    testing::InitGoogleTest(&argc, argv);
    return RUN_ALL_TESTS();
}