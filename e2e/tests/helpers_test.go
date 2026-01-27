package tests

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/autobrr/qui/e2e/internal/client"
)

// testdataPath returns the absolute path to a testdata file.
func testdataPath(relativePath string) string {
	_, filename, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(filename)
	return filepath.Join(testDir, "..", "testdata", relativePath)
}

// testMagnet is a well-known public domain torrent (Big Buck Bunny) used across tests.
const testMagnet = "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c&dn=Big+Buck+Bunny&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337"

// waitForInstance waits for an instance to be connected and ready.
// Polls the capabilities endpoint until it succeeds or timeout is reached.
func waitForInstance(t *testing.T, c *client.Client, instanceID int) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		_, err := c.TryGetCapabilities(t, instanceID)
		if err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("instance %d did not become ready within timeout", instanceID)
}

// validAutomationPayload creates a minimal valid automation payload for testing.
func validAutomationPayload(name string) client.AutomationPayload {
	enabled := true
	return client.AutomationPayload{
		Name:           name,
		TrackerPattern: "*", // Apply to all trackers
		Enabled:        &enabled,
		Conditions: &client.ActionConditions{
			Pause: &client.PauseAction{
				Enabled: true,
				Condition: &client.RuleCondition{
					Field:    "STATE",
					Operator: "EQUALS",
					Value:    "downloading",
				},
			},
		},
	}
}

