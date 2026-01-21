package tests

import (
	"os"
	"testing"

	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()

	// Cleanup shared environment
	containers.CleanupSharedEnv()

	os.Exit(code)
}
