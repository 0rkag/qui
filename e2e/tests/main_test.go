package tests

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestMain(m *testing.M) {
	// Warmup: build qui Docker image before running parallel tests.
	// Default 5m is enough with cached Docker layers; increase via
	// QUI_E2E_WARMUP_TIMEOUT for cold builds (e.g. first run).
	warmupTimeout := 5 * time.Minute
	if v, err := strconv.Atoi(os.Getenv("QUI_E2E_WARMUP_TIMEOUT")); err == nil && v > 0 {
		warmupTimeout = time.Duration(v) * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), warmupTimeout)
	defer cancel()

	if err := containers.Warmup(ctx, "../.."); err != nil {
		fmt.Fprintf(os.Stderr, "Warmup failed: %v\n", err)
		os.Exit(1)
	}

	exitCode := m.Run()

	// After all tests, merge coverage data into a standard coverage.out
	if containers.CoverageEnabled() {
		mergeCoverage()
	}

	os.Exit(exitCode)
}

// mergeCoverage converts raw coverage data from all qui containers into
// a single coverage.out file compatible with go tool cover.
func mergeCoverage() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	covDir := containers.CoverageDir()
	outFile := filepath.Join(containers.ProjectRoot(), "e2e", "coverage.out")

	cmd := exec.CommandContext(ctx, "go", "tool", "covdata", "textfmt", "-i="+covDir, "-o="+outFile)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "coverage: failed to generate coverage.out: %v\n", err)
		return
	}

	// Print total coverage line.
	// Must run from the main module root since the profile references its packages.
	funcCmd := exec.CommandContext(ctx, "go", "tool", "cover", "-func="+outFile)
	funcCmd.Dir = containers.ProjectRoot()
	out, err := funcCmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage: failed to compute summary: %v\n", err)
		return
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 0 {
		fmt.Printf("\n%s\n", lines[len(lines)-1])
	}
	fmt.Printf("Coverage report: %s\n", outFile)
}
