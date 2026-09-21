package fixtures

import (
	"os"
	"testing"
)

// TestRegenerateFixture rebuilds internal/fixtures/fixtures.json only when
// P BENCH_REGEN=1.  The committed file is the fixed acceptance artifact.
func TestRegenerateFixture(t *testing.T) {
	if os.Getenv("PBENCH_REGEN") != "1" {
		t.Skip("set PBENCH_REGEN=1 to regenerate fixtures.json")
	}
	if err := os.WriteFile("fixtures.json", Generate(), 0o644); err != nil {
		t.Fatal(err)
	}
}
