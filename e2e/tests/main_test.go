package tests

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestMain(m *testing.M) {
	// Warmup: build qui Docker image before running parallel tests
	// This prevents parallel build races that cause timeouts
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := containers.Warmup(ctx, "../.."); err != nil {
		fmt.Fprintf(os.Stderr, "Warmup failed: %v\n", err)
		os.Exit(1)
	}

	// Run tests
	os.Exit(m.Run())
}
