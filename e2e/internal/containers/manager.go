// Package containers provides Docker container management for e2e tests.
package containers

import (
	"bytes"
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

	"github.com/autobrr/qui/e2e/internal/client"
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

	// qui credentials (set during setup)
	quiClient *client.Client
}

// Client returns an authenticated client for qui's API.
func (e *TestEnv) Client() *client.Client {
	return e.quiClient
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

	// Prepare qui Dockerfile (must be done before building request)
	absPath, err := filepath.Abs(cfg.QuiSourcePath)
	if err != nil {
		t.Fatalf("failed to get absolute path: %v", err)
	}
	platform := "linux/" + runtime.GOARCH
	dockerfilePath := filepath.Join(absPath, "distrib/docker/Dockerfile")
	e2eDockerfilePath := filepath.Join(absPath, "distrib/docker/Dockerfile.e2e")
	if err := createE2EDockerfile(dockerfilePath, e2eDockerfilePath, platform); err != nil {
		t.Fatalf("failed to create e2e Dockerfile: %v", err)
	}
	t.Cleanup(func() { os.Remove(e2eDockerfilePath) })

	// Build container requests
	qbitReq := qbittorrentRequest(net.Name, cfg.Timeout)
	quiReq := quiRequest(absPath, platform, net.Name, cfg.Timeout)

	// Start both containers in parallel
	t.Log("Starting containers in parallel...")
	containers, err := testcontainers.ParallelContainers(ctx, []testcontainers.GenericContainerRequest{
		{ContainerRequest: qbitReq, Started: true},
		{ContainerRequest: quiReq, Started: true},
	}, testcontainers.ParallelContainersOptions{})
	if err != nil {
		t.Fatalf("failed to start containers: %v", err)
	}

	env.QBittorrent = containers[0]
	env.Qui = containers[1]

	// Setup qBittorrent (extract password, configure settings)
	qbitPort, err := env.QBittorrent.MappedPort(ctx, qbitWebUIPort)
	if err != nil {
		t.Fatalf("failed to get qbittorrent port: %v", err)
	}
	env.QBitExtURL = "http://localhost:" + qbitPort.Port()

	env.QBitPassword = extractQBitPassword(ctx, t, env.QBittorrent)
	t.Logf("qBittorrent ready at %s (password: %s)", env.QBitExtURL, env.QBitPassword)

	if err := configureQBittorrent(ctx, t, env.QBitExtURL, env.QBitPassword); err != nil {
		t.Logf("Warning: failed to configure qBittorrent settings: %v (tests may be flaky with rapid retries)", err)
	}

	// Get qBittorrent internal network IP for qui to connect to
	qbitIP, err := env.QBittorrent.ContainerIP(ctx)
	if err != nil {
		t.Fatalf("failed to get qbittorrent IP: %v", err)
	}
	env.QBitURL = fmt.Sprintf("http://%s:%s", qbitIP, qbitWebUIPort)

	// Setup qui URL
	quiPort, err := env.Qui.MappedPort(ctx, "7476")
	if err != nil {
		t.Fatalf("failed to get qui port: %v", err)
	}
	env.QuiURL = "http://localhost:" + quiPort.Port()
	t.Logf("qui ready at %s", env.QuiURL)

	// Configure qui (create admin user)
	env.quiClient = configureQui(ctx, t, env.QuiURL, "admin", "adminadmin")

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

// quiRequest returns a container request for qui.
// Caller must create the Dockerfile.e2e file before calling this.
func quiRequest(absPath, platform, networkName string, timeout time.Duration) testcontainers.ContainerRequest {
	return testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    absPath,
			Dockerfile: "distrib/docker/Dockerfile.e2e",
			BuildArgs: map[string]*string{
				"VERSION": ptr("e2e-test"),
			},
			PrintBuildLog: true,
			KeepImage:     true, // Reuse built image across test runs
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
		"FROM --platform="+platform,
		1,
	)

	if err := os.WriteFile(dstPath, []byte(modified), 0o600); err != nil {
		return fmt.Errorf("write dockerfile: %w", err)
	}

	return nil
}

func ptr(s string) *string {
	return &s
}

// qbittorrentRequest returns a container request for qBittorrent.
func qbittorrentRequest(networkName string, timeout time.Duration) testcontainers.ContainerRequest {
	// Use fixed port mapping so WEBUI_PORT matches the exposed host port
	// This is required because qBittorrent validates that the request port matches WEBUI_PORT
	return testcontainers.ContainerRequest{
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
		Networks: []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {"qbittorrent"},
		},
		// Wait for the temp password log message which indicates WebUI is ready
		WaitingFor: wait.ForLog("temporary password is provided").WithStartupTimeout(timeout),
	}
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
func configureQBittorrent(ctx context.Context, t *testing.T, baseURL, password string) error {
	t.Helper()

	client := &http.Client{Timeout: 10 * time.Second}

	// Login to get session cookie (with retries - WebUI may not be fully ready)
	loginData := url.Values{}
	loginData.Set("username", "admin")
	loginData.Set("password", password)

	var cookies []*http.Cookie
	var loginErr error

	for range 5 {
		req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/v2/auth/login", strings.NewReader(loginData.Encode()))
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

	prefsReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/v2/app/setPreferences", strings.NewReader(prefsData.Encode()))
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

// configureQui creates the initial admin user and returns an authenticated client.
func configureQui(ctx context.Context, t *testing.T, baseURL, username, password string) *client.Client {
	t.Helper()

	httpClient := &http.Client{Timeout: 10 * time.Second}

	body := map[string]string{
		"username": username,
		"password": password,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal setup body: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/auth/setup", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("failed to create setup request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("failed to setup qui: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("qui setup failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Create client and set cookies from response
	c := client.New(baseURL)
	c.SetCookies(resp.Cookies())

	t.Logf("qui configured: admin user created")
	return c
}
