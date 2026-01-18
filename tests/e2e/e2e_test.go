//go:build e2e

// Package e2e provides end-to-end tests for qui's transfer functionality.
// These tests require Docker and spin up real qBittorrent containers.
//
// Run with: go test -tags=e2e -v ./tests/e2e/...
package e2e

import (
	"context"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	// env is the shared test environment, initialized in TestMain
	env *TestEnv

	// keepRunning controls whether containers are kept after tests
	keepRunning = flag.Bool("keep", false, "keep containers running after tests")
)

func TestMain(m *testing.M) {
	flag.Parse()

	// Check for E2E_KEEP_RUNNING env var
	if os.Getenv("E2E_KEEP_RUNNING") == "true" {
		*keepRunning = true
	}

	// Setup logging
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Setup test environment
	var err error
	env, err = SetupTestEnvironment(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to setup test environment")
	}

	// Run tests
	code := m.Run()

	// Teardown unless --keep flag is set
	if !*keepRunning {
		if err := env.Teardown(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to teardown test environment")
		}
	} else {
		log.Info().Msg("Keeping test environment running (--keep flag set)")
		env.PrintInfo()
	}

	os.Exit(code)
}
