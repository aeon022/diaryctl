package suite

import (
	"os"
	"strings"
	"testing"
)

// The developer's shell exports <TOOL>_DATA_DIR for the real databases; tests must never read them.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasSuffix(k, "_DATA_DIR") {
			os.Unsetenv(k)
		}
	}
	os.Exit(m.Run())
}
