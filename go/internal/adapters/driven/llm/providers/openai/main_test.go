package openai_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs goleak.VerifyTestMain after every test in this package —
// see chatservice's main_test.go for why.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
