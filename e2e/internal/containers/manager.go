// Package containers provides Docker container management for e2e tests.
package containers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/autobrr/qui/e2e/internal/client"
)

// Port range for random port assignment in parallel tests
const (
	minRandomPort = 10000
	maxRandomPort = 60000
)

// portMutex protects random port generation to avoid collisions in parallel tests
var portMutex sync.Mutex

// usedPorts tracks ports that have been allocated to avoid duplicates
var usedPorts = make(map[int]bool)

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

// QBitInstance holds info about a single qBittorrent instance in a multi-instance setup.
type QBitInstance struct {
	ID        int                      // qui instance ID (assigned after registration)
	Container testcontainers.Container // Docker container
	URL       string                   // Internal URL for qui to connect
	ExtURL    string                   // External URL for direct access
	Password  string                   // WebUI password
}

// MultiInstanceEnv holds containers for multi-instance tests.
type MultiInstanceEnv struct {
	Network   *testcontainers.DockerNetwork
	Qui       testcontainers.Container
	QuiURL    string
	Instances []*QBitInstance

	quiClient *client.Client
}

// Client returns an authenticated client for qui's API.
func (e *MultiInstanceEnv) Client() *client.Client {
	return e.quiClient
}

// Teardown stops and removes all containers.
func (e *MultiInstanceEnv) Teardown(ctx context.Context) {
	if e.Qui != nil {
		_ = e.Qui.Terminate(ctx)
	}
	for _, inst := range e.Instances {
		if inst.Container != nil {
			_ = inst.Container.Terminate(ctx)
		}
	}
	if e.Network != nil {
		_ = e.Network.Remove(ctx)
	}
}

// SetupMultiInstance creates a test environment with multiple qBittorrent instances.
func SetupMultiInstance(ctx context.Context, t *testing.T, cfg Config, instanceCount int) *MultiInstanceEnv {
	t.Helper()

	env := &MultiInstanceEnv{
		Instances: make([]*QBitInstance, instanceCount),
	}

	// Create shared network
	net, err := network.New(ctx)
	if err != nil {
		t.Fatalf("failed to create network: %v", err)
	}
	env.Network = net

	// Prepare qui Dockerfile
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
	defer os.Remove(e2eDockerfilePath)

	// Start qui container first (can run in parallel with qBittorrent startup)
	t.Logf("Starting %d qBittorrent instances + qui...", instanceCount)

	quiReq := quiRequest(absPath, platform, net.Name, cfg.Timeout)
	quiContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: quiReq,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start qui container: %v", err)
	}
	env.Qui = quiContainer

	quiPort, err := env.Qui.MappedPort(ctx, "7476")
	if err != nil {
		t.Fatalf("failed to get qui port: %v", err)
	}
	env.QuiURL = "http://localhost:" + quiPort.Port()

	// Configure qui (create admin user)
	env.quiClient, err = configureQuiShared(ctx, env.QuiURL, "admin", "adminadmin")
	if err != nil {
		t.Fatalf("failed to configure qui: %v", err)
	}

	// Start qBittorrent instances sequentially to avoid port binding races
	// Each container must fully start before the next port is allocated
	for i := range instanceCount {
		inst := &QBitInstance{}
		env.Instances[i] = inst

		// Allocate ports for this instance
		webUIPort := randomPortInRange(minRandomPort, maxRandomPort)
		torrentPort := randomPortInRange(minRandomPort, maxRandomPort)
		webUIPortStr := strconv.Itoa(webUIPort)

		// Start the container
		qbitReq := qbittorrentRequest(net.Name, cfg.Timeout, webUIPort, torrentPort)
		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: qbitReq,
			Started:          true,
		})
		if err != nil {
			t.Fatalf("failed to start qbittorrent container %d: %v", i, err)
		}
		inst.Container = container

		// Get external URL
		qbitPort, err := inst.Container.MappedPort(ctx, nat.Port(webUIPortStr+"/tcp"))
		if err != nil {
			t.Fatalf("failed to get qbittorrent port for instance %d: %v", i, err)
		}
		inst.ExtURL = "http://localhost:" + qbitPort.Port()

		// Extract password
		inst.Password, err = extractQBitPasswordShared(ctx, inst.Container)
		if err != nil {
			t.Fatalf("failed to extract password for instance %d: %v", i, err)
		}

		// Configure qBittorrent settings
		if err := configureQBittorrentShared(ctx, inst.ExtURL, inst.Password); err != nil {
			t.Logf("Warning: failed to configure qBittorrent instance %d: %v", i, err)
		}

		// Get internal network IP
		qbitIP, err := inst.Container.ContainerIP(ctx)
		if err != nil {
			t.Fatalf("failed to get qbittorrent IP for instance %d: %v", i, err)
		}
		inst.URL = fmt.Sprintf("http://%s:%s", qbitIP, webUIPortStr)

		// Register instance with qui
		inst.ID = env.quiClient.CreateInstance(t, client.InstanceConfig{
			Name:     fmt.Sprintf("qbit-%d", i+1),
			Host:     inst.URL,
			Username: "admin",
			Password: inst.Password,
		})
	}

	t.Logf("Multi-instance environment ready: %d qBittorrent instances", instanceCount)
	return env
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

// Setup creates and starts all containers (for individual test use).
func Setup(ctx context.Context, t *testing.T, cfg Config) *TestEnv {
	t.Helper()

	env, err := SetupShared(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to setup test environment: %v", err)
	}

	t.Logf("Created network: %s", env.Network.Name)
	t.Logf("qBittorrent ready at %s (password: %s)", env.QBitExtURL, env.QBitPassword)
	t.Logf("qui ready at %s", env.QuiURL)
	t.Logf("qui configured: admin user created")

	return env
}

// SetupShared creates and starts all containers without requiring testing.T.
// Used by TestMain for shared environment setup.
func SetupShared(ctx context.Context, cfg Config) (*TestEnv, error) {
	env := &TestEnv{}

	// Create shared network
	net, err := network.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}
	env.Network = net

	// Prepare qui Dockerfile (must be done before building request)
	absPath, err := filepath.Abs(cfg.QuiSourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}
	platform := "linux/" + runtime.GOARCH
	dockerfilePath := filepath.Join(absPath, "distrib/docker/Dockerfile")
	e2eDockerfilePath := filepath.Join(absPath, "distrib/docker/Dockerfile.e2e")
	if err := createE2EDockerfile(dockerfilePath, e2eDockerfilePath, platform); err != nil {
		return nil, fmt.Errorf("failed to create e2e Dockerfile: %w", err)
	}
	// Note: caller should clean up e2eDockerfilePath if needed
	defer os.Remove(e2eDockerfilePath)

	// Generate random ports for this test instance to allow parallel execution
	webUIPort := randomPortInRange(minRandomPort, maxRandomPort)
	torrentPort := randomPortInRange(minRandomPort, maxRandomPort)

	// Build container requests
	qbitReq := qbittorrentRequest(net.Name, cfg.Timeout, webUIPort, torrentPort)
	quiReq := quiRequest(absPath, platform, net.Name, cfg.Timeout)

	// Start both containers in parallel
	containers, err := testcontainers.ParallelContainers(ctx, []testcontainers.GenericContainerRequest{
		{ContainerRequest: qbitReq, Started: true},
		{ContainerRequest: quiReq, Started: true},
	}, testcontainers.ParallelContainersOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to start containers: %w", err)
	}

	env.QBittorrent = containers[0]
	env.Qui = containers[1]

	// Setup qBittorrent (extract password, configure settings)
	webUIPortStr := strconv.Itoa(webUIPort)
	qbitPort, err := env.QBittorrent.MappedPort(ctx, nat.Port(webUIPortStr+"/tcp"))
	if err != nil {
		return nil, fmt.Errorf("failed to get qbittorrent port: %w", err)
	}
	env.QBitExtURL = "http://localhost:" + qbitPort.Port()

	env.QBitPassword, err = extractQBitPasswordShared(ctx, env.QBittorrent)
	if err != nil {
		return nil, fmt.Errorf("failed to extract qbittorrent password: %w", err)
	}

	if err := configureQBittorrentShared(ctx, env.QBitExtURL, env.QBitPassword); err != nil {
		// Non-fatal warning - tests may be flaky
		fmt.Printf("Warning: failed to configure qBittorrent settings: %v\n", err)
	}

	// Get qBittorrent internal network IP for qui to connect to
	qbitIP, err := env.QBittorrent.ContainerIP(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get qbittorrent IP: %w", err)
	}
	env.QBitURL = fmt.Sprintf("http://%s:%s", qbitIP, webUIPortStr)

	// Setup qui URL
	quiPort, err := env.Qui.MappedPort(ctx, "7476")
	if err != nil {
		return nil, fmt.Errorf("failed to get qui port: %w", err)
	}
	env.QuiURL = "http://localhost:" + quiPort.Port()

	// Configure qui (create admin user)
	env.quiClient, err = configureQuiShared(ctx, env.QuiURL, "admin", "adminadmin")
	if err != nil {
		return nil, fmt.Errorf("failed to configure qui: %w", err)
	}

	return env, nil
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

// findAvailablePort finds an available TCP port.
func findAvailablePort() (int, error) {
	// Use port 0 to let the OS assign an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// randomPortInRange returns a random port in the given range.
// randomPortInRange returns a random unused port in the given range.
// Thread-safe for parallel test execution.
// isPortAvailable checks if a TCP port is available on the host
func isPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

func randomPortInRange(min, max int) int {
	portMutex.Lock()
	defer portMutex.Unlock()

	for attempts := 0; attempts < 1000; attempts++ {
		port := min + rand.IntN(max-min+1)
		if !usedPorts[port] && isPortAvailable(port) {
			usedPorts[port] = true
			return port
		}
	}
	// Fallback: return a random port anyway (very unlikely to reach here)
	return min + rand.IntN(max-min+1)
}

// qbittorrentRequest returns a container request for qBittorrent.
// webUIPort and torrentPort allow parallel tests to use different ports.
//
// IMPORTANT: qBittorrent requires the external host port to EXACTLY match the
// WEBUI_PORT environment variable. If they don't match, login requests will fail
// with 403 Forbidden. This is a security feature of qBittorrent to prevent
// port-based attacks. Therefore, we cannot use dynamic port mapping (e.g., "8080/tcp")
// and must use fixed mapping (e.g., "12345:12345/tcp") with randomly chosen ports.
func qbittorrentRequest(networkName string, timeout time.Duration, webUIPort, torrentPort int) testcontainers.ContainerRequest {
	webUIPortStr := strconv.Itoa(webUIPort)
	torrentPortStr := strconv.Itoa(torrentPort)

	return testcontainers.ContainerRequest{
		Image: "linuxserver/qbittorrent:4.6.7",
		Env: map[string]string{
			"PUID":            "1000",
			"PGID":            "1000",
			"TZ":              "UTC",
			"WEBUI_PORT":      webUIPortStr,
			"TORRENTING_PORT": torrentPortStr,
		},
		ExposedPorts: []string{
			webUIPortStr + ":" + webUIPortStr + "/tcp",       // Fixed mapping with random port
			torrentPortStr + ":" + torrentPortStr + "/tcp",   // Fixed mapping with random port
			torrentPortStr + ":" + torrentPortStr + "/udp",   // Fixed mapping with random port
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

	password, err := extractQBitPasswordShared(ctx, container)
	if err != nil {
		t.Fatalf("failed to extract qbittorrent password: %v", err)
	}
	return password
}

// extractQBitPasswordShared extracts the temp password without requiring testing.T.
func extractQBitPasswordShared(ctx context.Context, container testcontainers.Container) (string, error) {
	logs, err := container.Logs(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get container logs: %w", err)
	}
	defer logs.Close()

	logBytes, err := io.ReadAll(logs)
	if err != nil {
		return "", fmt.Errorf("failed to read container logs: %w", err)
	}

	// Parse: "A temporary password is provided for this session: ABC123"
	re := regexp.MustCompile(`temporary password is provided for this session: (\S+)`)
	matches := re.FindSubmatch(logBytes)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not find qBittorrent password in logs")
	}

	return string(matches[1]), nil
}

// configureQBittorrent sets up qBittorrent for e2e testing via API.
func configureQBittorrent(ctx context.Context, t *testing.T, baseURL, password string) error {
	t.Helper()
	return configureQBittorrentShared(ctx, baseURL, password)
}

// configureQBittorrentShared sets up qBittorrent for e2e testing via API:
// - Reduces ban duration to 1 second (avoid test failures from rapid retries)
// - Enables UPnP for torrent ports
// Returns error if configuration fails (non-fatal, caller can decide to warn or fail).
func configureQBittorrentShared(ctx context.Context, baseURL, password string) error {
	httpClient := &http.Client{Timeout: 10 * time.Second}

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

		loginResp, err := httpClient.Do(req)
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

	prefsResp, err := httpClient.Do(prefsReq)
	if err != nil {
		return fmt.Errorf("set preferences: %w", err)
	}
	defer prefsResp.Body.Close()

	if prefsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(prefsResp.Body)
		return fmt.Errorf("setPreferences failed (status %d): %s", prefsResp.StatusCode, string(body))
	}

	return nil
}

// configureQui creates the initial admin user and returns an authenticated client.
func configureQui(ctx context.Context, t *testing.T, baseURL, username, password string) *client.Client {
	t.Helper()

	c, err := configureQuiShared(ctx, baseURL, username, password)
	if err != nil {
		t.Fatalf("failed to configure qui: %v", err)
	}
	return c
}

// configureQuiShared creates the initial admin user without requiring testing.T.
func configureQuiShared(ctx context.Context, baseURL, username, password string) (*client.Client, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	body := map[string]string{
		"username": username,
		"password": password,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal setup body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/auth/setup", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create setup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to setup qui: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("qui setup failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Create client and set cookies from response
	c := client.New(baseURL)
	c.SetCookies(resp.Cookies())

	return c, nil
}
