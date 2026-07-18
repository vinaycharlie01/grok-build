package chatservice_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs goleak.VerifyTestMain after every test in this package,
// failing the run if any goroutine started during the tests is still
// running once they've finished — automating what "no goroutine that can
// outlive its context — verified, not assumed" (ROADMAP.md's Definition
// of Done) has meant only manual review until now.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
