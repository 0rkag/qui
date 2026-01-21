package containers

import (
	"context"
	"sync"
	"testing"
)

var (
	sharedEnv  *TestEnv
	sharedOnce sync.Once
	sharedErr  error
)

// SharedEnv returns a shared test environment for faster test runs.
// Use this for tests that don't need a completely clean state.
// The environment is created once and reused across all tests.
func SharedEnv(t *testing.T) *TestEnv {
	t.Helper()

	sharedOnce.Do(func() {
		ctx := context.Background()
		cfg := DefaultConfig()
		sharedEnv = Setup(ctx, t, cfg)
	})

	if sharedEnv == nil {
		t.Fatal("shared environment not initialized")
	}

	return sharedEnv
}

// IsolatedEnv creates a fresh environment for tests that need complete isolation.
// The caller must call env.Teardown() when done, typically via t.Cleanup().
func IsolatedEnv(t *testing.T, cfg Config) *TestEnv {
	t.Helper()

	ctx := context.Background()
	env := Setup(ctx, t, cfg)

	t.Cleanup(func() {
		env.Teardown(context.Background())
	})

	return env
}

// CleanupSharedEnv tears down the shared environment.
// Call this once at the end of all tests via TestMain.
func CleanupSharedEnv() {
	if sharedEnv != nil {
		sharedEnv.Teardown(context.Background())
		sharedEnv = nil
	}
}
