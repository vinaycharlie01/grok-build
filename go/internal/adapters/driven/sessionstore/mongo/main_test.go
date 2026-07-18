package mongo

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs goleak.VerifyTestMain after every test in this package —
// see chatservice's main_test.go for why. This package's own unit tests
// (document_test.go) never connect to a real MongoDB — no driver
// background goroutines to worry about here; that's what
// tests/integration/sessionstore_mongo_test.go covers separately.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
