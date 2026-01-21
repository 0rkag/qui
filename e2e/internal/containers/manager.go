// Package containers provides Docker container management for e2e tests.
package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/build"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// qBittorrent WebUI port - must match WEBUI_PORT env var and exposed port
	qbitWebUIPort = "8080"
	// qBittorrent torrenting port - must match TORRENTING_PORT env var and exposed ports
	qbitTorrentPort = "6881"
)

// TestEnv holds all containers for a test run.
type TestEnv struct {
	Network     *testcontainers.DockerNetwork
	Qui         testcontainers.Container // qui runs in Docker container
	QBittorrent testcontainers.Container

	QuiURL       string // External URL for test client (http://localhost:<port>)
	QBitURL      string // URL for qui to connect to qBittorrent (internal network)
	QBitExtURL   string // External URL for direct access if needed
	QBitPassword string // qBittorrent WebUI password (extracted from logs)
}

// Config for test environment.
type Config struct {
	QuiImage      string        // Image name if not building from source
	BuildQui      bool          // Build qui from source instead of using image
	QuiSourcePath string        // Path to qui source (if BuildQui=true)
	Timeout       time.Duration // Container startup timeout
}

// DefaultConfig returns sensible defaults for local development.
func DefaultConfig() Config {
	return Config{
		BuildQui:      true,
		QuiSourcePath: "../..", // Relative to e2e directory
		Timeout:       3 * time.Minute,
	}
}

// Setup creates and starts all containers.
func Setup(ctx context.Context, t *testing.T, cfg Config) *TestEnv {
	t.Helper()

	env := &TestEnv{}

	// Create shared network
	net, err := network.New(ctx)
	if err != nil {
		t.Fatalf("failed to create network: %v", err)
	}
	env.Network = net
	t.Logf("Created network: %s", net.Name)

	// Start qBittorrent
	env.QBittorrent = startQBittorrent(ctx, t, net.Name, cfg.Timeout)

	qbitPort, err := env.QBittorrent.MappedPort(ctx, qbitWebUIPort)
	if err != nil {
		t.Fatalf("failed to get qbittorrent port: %v", err)
	}
	env.QBitExtURL = fmt.Sprintf("http://localhost:%s", qbitPort.Port())
	// qui runs on host, so it connects to qBittorrent via localhost mapped port
	env.QBitURL = env.QBitExtURL

	// Extract password from container logs
	env.QBitPassword = extractQBitPassword(ctx, t, env.QBittorrent)
	t.Logf("qBittorrent ready at %s (password: %s)", env.QBitExtURL, env.QBitPassword)

	// Configure qBittorrent settings to reduce ban duration and enable UPnP
	// Uses fixed port mapping so host port matches WEBUI_PORT (required for auth)
	if err := configureQBittorrent(t, env.QBitExtURL, env.QBitPassword); err != nil {
		t.Logf("Warning: failed to configure qBittorrent settings: %v (tests may be flaky with rapid retries)", err)
	}

	// Get qBittorrent internal network IP for qui to connect to
	qbitIP, err := env.QBittorrent.ContainerIP(ctx)
	if err != nil {
		t.Fatalf("failed to get qbittorrent IP: %v", err)
	}
	env.QBitURL = fmt.Sprintf("http://%s:%s", qbitIP, qbitWebUIPort)

	// Start qui in Docker container
	env.Qui = startQuiFromDocker(ctx, t, net.Name, cfg.QuiSourcePath, cfg.Timeout)

	quiPort, err := env.Qui.MappedPort(ctx, "7476")
	if err != nil {
		t.Fatalf("failed to get qui port: %v", err)
	}
	env.QuiURL = fmt.Sprintf("http://localhost:%s", quiPort.Port())
	t.Logf("qui ready at %s", env.QuiURL)

	return env
}

// Teardown stops and removes all containers.
func (e *TestEnv) Teardown(ctx context.Context) {
	if e.Qui != nil {
		_ = e.Qui.Terminate(ctx)
	}
	if e.QBittorrent != nil {
		_ = e.QBittorrent.Terminate(ctx)
	}
	if e.Network != nil {
		_ = e.Network.Remove(ctx)
	}
}

// startQuiFromDocker builds and runs qui in a Docker container.
// It creates a modified Dockerfile on-the-fly to handle BUILDPLATFORM
// since testcontainers doesn't use BuildKit which auto-sets that variable.
func startQuiFromDocker(ctx context.Context, t *testing.T, networkName, sourcePath string, timeout time.Duration) testcontainers.Container {
	t.Helper()

	absPath, err := filepath.Abs(sourcePath)
	if err != nil {
		t.Fatalf("failed to get absolute path: %v", err)
	}

	// Detect current platform for BUILDPLATFORM
	platform := fmt.Sprintf("linux/%s", runtime.GOARCH)

	// Create modified Dockerfile that works without BuildKit's auto BUILDPLATFORM
	dockerfilePath := filepath.Join(absPath, "distrib/docker/Dockerfile")
	e2eDockerfilePath := filepath.Join(absPath, "distrib/docker/Dockerfile.e2e")

	if err := createE2EDockerfile(dockerfilePath, e2eDockerfilePath, platform); err != nil {
		t.Fatalf("failed to create e2e Dockerfile: %v", err)
	}
	t.Cleanup(func() { os.Remove(e2eDockerfilePath) })

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    absPath,
			Dockerfile: "distrib/docker/Dockerfile.e2e",
			BuildArgs: map[string]*string{
				"VERSION": ptr("e2e-test"),
			},
			PrintBuildLog: true,
			BuildOptionsModifier: func(opts *build.ImageBuildOptions) {
				opts.Platform = platform
			},
		},
		ExposedPorts: []string{"7476/tcp"},
		Networks:     []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {"qui"},
		},
		Cmd: []string{"serve"},
		WaitingFor: wait.ForHTTP("/health").
			WithPort("7476").
			WithStartupTimeout(timeout),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start qui from docker: %v", err)
	}

	return container
}

// createE2EDockerfile creates a modified Dockerfile for e2e tests.
// It replaces $BUILDPLATFORM with an explicit platform value since
// testcontainers doesn't use BuildKit which auto-sets that variable.
func createE2EDockerfile(srcPath, dstPath, platform string) error {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read dockerfile: %w", err)
	}

	// Replace FROM --platform=$BUILDPLATFORM with explicit platform
	modified := strings.Replace(
		string(content),
		"FROM --platform=$BUILDPLATFORM",
		fmt.Sprintf("FROM --platform=%s", platform),
		1,
	)

	if err := os.WriteFile(dstPath, []byte(modified), 0644); err != nil {
		return fmt.Errorf("write dockerfile: %w", err)
	}

	return nil
}

func ptr(s string) *string {
	return &s
}

func startQBittorrent(ctx context.Context, t *testing.T, networkName string, timeout time.Duration) testcontainers.Container {
	t.Helper()

	// Use fixed port mapping so WEBUI_PORT matches the exposed host port
	// This is required because qBittorrent validates that the request port matches WEBUI_PORT
	req := testcontainers.ContainerRequest{
		Image: "linuxserver/qbittorrent:4.6.7",
		Env: map[string]string{
			"PUID":            "1000",
			"PGID":            "1000",
			"TZ":              "UTC",
			"WEBUI_PORT":      qbitWebUIPort,
			"TORRENTING_PORT": qbitTorrentPort,
		},
		ExposedPorts: []string{
			qbitWebUIPort + ":" + qbitWebUIPort + "/tcp", // Fixed mapping: host:container
			qbitTorrentPort + ":" + qbitTorrentPort + "/tcp",
			qbitTorrentPort + ":" + qbitTorrentPort + "/udp",
		},
		Networks:     []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {"qbittorrent"},
		},
		// Wait for the temp password log message which indicates WebUI is ready
		WaitingFor: wait.ForLog("temporary password is provided").WithStartupTimeout(timeout),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start qbittorrent: %v", err)
	}

	return container
}

// extractQBitPassword extracts the temp password from qBittorrent container logs.
func extractQBitPassword(ctx context.Context, t *testing.T, container testcontainers.Container) string {
	t.Helper()

	logs, err := container.Logs(ctx)
	if err != nil {
		t.Fatalf("failed to get container logs: %v", err)
	}
	defer logs.Close()

	logBytes, err := io.ReadAll(logs)
	if err != nil {
		t.Fatalf("failed to read container logs: %v", err)
	}

	// Parse: "A temporary password is provided for this session: ABC123"
	re := regexp.MustCompile(`temporary password is provided for this session: (\S+)`)
	matches := re.FindSubmatch(logBytes)
	if len(matches) < 2 {
		t.Fatalf("could not find qBittorrent password in logs")
	}

	return string(matches[1])
}

// configureQBittorrent sets up qBittorrent for e2e testing via API:
// - Reduces ban duration to 1 second (avoid test failures from rapid retries)
// - Enables UPnP for torrent ports
// Returns error if configuration fails (non-fatal, caller can decide to warn or fail).
func configureQBittorrent(t *testing.T, baseURL, password string) error {
	t.Helper()

	client := &http.Client{Timeout: 10 * time.Second}

	// Login to get session cookie (with retries - WebUI may not be fully ready)
	loginData := url.Values{}
	loginData.Set("username", "admin")
	loginData.Set("password", password)

	var cookies []*http.Cookie
	var loginErr error

	for i := 0; i < 5; i++ {
		req, err := http.NewRequest("POST", baseURL+"/api/v2/auth/login", strings.NewReader(loginData.Encode()))
		if err != nil {
			loginErr = fmt.Errorf("create request failed: %w", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", baseURL)
		req.Header.Set("Origin", baseURL)

		loginResp, err := client.Do(req)
		if err != nil {
			loginErr = fmt.Errorf("request failed: %w", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		body, _ := io.ReadAll(loginResp.Body)
		loginResp.Body.Close()

		if loginResp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "Ok." {
			loginErr = fmt.Errorf("login failed (status %d): %s", loginResp.StatusCode, string(body))
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Extract session cookie
		for _, cookie := range loginResp.Cookies() {
			if cookie.Name == "SID" {
				cookies = append(cookies, cookie)
				break
			}
		}
		loginErr = nil
		break
	}
	if loginErr != nil {
		return fmt.Errorf("login failed: %w", loginErr)
	}

	// Configure settings
	settings := map[string]any{
		"web_ui_ban_duration": 1,    // 1 second ban duration
		"upnp":                true, // Enable UPnP
	}

	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	prefsData := url.Values{}
	prefsData.Set("json", string(settingsJSON))

	prefsReq, err := http.NewRequest("POST", baseURL+"/api/v2/app/setPreferences", strings.NewReader(prefsData.Encode()))
	if err != nil {
		return fmt.Errorf("create preferences request: %w", err)
	}
	prefsReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	prefsReq.Header.Set("Referer", baseURL)
	for _, cookie := range cookies {
		prefsReq.AddCookie(cookie)
	}

	prefsResp, err := client.Do(prefsReq)
	if err != nil {
		return fmt.Errorf("set preferences: %w", err)
	}
	defer prefsResp.Body.Close()

	if prefsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(prefsResp.Body)
		return fmt.Errorf("setPreferences failed (status %d): %s", prefsResp.StatusCode, string(body))
	}

	t.Log("qBittorrent configured: ban_duration=1s, upnp=true")
	return nil
}

