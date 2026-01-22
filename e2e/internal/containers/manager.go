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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
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

// Default qBittorrent image to use if not specified
const defaultQBitImage = "linuxserver/qbittorrent:5.1.4"

// Supported qBittorrent images for version matrix testing.
// Run with QUI_E2E_QBIT_IMAGE=<short-name> to test different versions.
// Test matrix: 5.1.4 (default), 5.1.4-libtorrentv1, 5.0.2, 4.6.7
var supportedQBitImages = map[string]string{
	// 4.6.x series (last major 4.x with nested categories)
	"4.6.7": "linuxserver/qbittorrent:4.6.7",
	// 5.0.x series (first 5.x, torrent creation feature)
	"5.0.2": "linuxserver/qbittorrent:5.0.2",
	// 5.1.x series (latest)
	"5.1.4":              "linuxserver/qbittorrent:5.1.4",
	"5.1.4-libtorrentv1": "linuxserver/qbittorrent:5.1.4-libtorrentv1",
}

// getQBitImage returns the qBittorrent image to use for tests.
// Checks QUI_E2E_QBIT_IMAGE env var, falls back to default.
func getQBitImage() string {
	if img := os.Getenv("QUI_E2E_QBIT_IMAGE"); img != "" {
		// Check if it's a short name (e.g., "4.6.7")
		if fullImg, ok := supportedQBitImages[img]; ok {
			return fullImg
		}
		// Otherwise use as-is (full image name)
		return img
	}
	return defaultQBitImage
}

// portMutex protects random port generation to avoid collisions in parallel tests
var portMutex sync.Mutex

// usedPorts tracks ports that have been allocated to avoid duplicates
var usedPorts = make(map[int]bool)

// quiImageName stores the pre-built qui image name from warmup
var quiImageName string
var quiImageMutex sync.RWMutex

// containerSem limits concurrent container startups to avoid overwhelming Docker
var containerSem chan struct{}
var containerSemOnce sync.Once

// getContainerSem returns a semaphore that limits concurrent container startups.
// Default is 4 concurrent startups, configurable via QUI_E2E_MAX_PARALLEL env var.
func getContainerSem() chan struct{} {
	containerSemOnce.Do(func() {
		maxParallel := 4
		if env := os.Getenv("QUI_E2E_MAX_PARALLEL"); env != "" {
			if n, err := strconv.Atoi(env); err == nil && n > 0 {
				maxParallel = n
			}
		}
		containerSem = make(chan struct{}, maxParallel)
	})
	return containerSem
}

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

// Warmup pre-builds the qui Docker image and pulls the qBittorrent image
// to avoid parallel build/pull races. Call this from TestMain before running parallel tests.
func Warmup(ctx context.Context, quiSourcePath string) error {
	qbitImage := getQBitImage()
	fmt.Printf("Warming up: building qui image and pulling %s...\n", qbitImage)

	absPath, err := filepath.Abs(quiSourcePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Create a temporary network for the warmup containers
	net, err := network.New(ctx)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}
	defer net.Remove(ctx)

	// Build qui image with a fixed tag so tests can reuse it
	// Uses the permanent Dockerfile.e2e which doesn't require BuildKit
	imageRepo := "qui-e2e"
	imageTag := "test"
	quiReq := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:       absPath,
			Dockerfile:    "distrib/docker/Dockerfile.e2e",
			PrintBuildLog: true,
			KeepImage:     true,
			Repo:          imageRepo,
			Tag:           imageTag,
		},
		ExposedPorts: []string{"7476/tcp"},
		Networks:     []string{net.Name},
		Cmd:          []string{"serve"},
		WaitingFor: wait.ForHTTP("/health").
			WithPort("7476").
			WithStartupTimeout(3 * time.Minute),
	}
	qbitReq := testcontainers.ContainerRequest{
		Image:      qbitImage,
		WaitingFor: wait.ForLog("[ls.io-init] done.").WithStartupTimeout(3 * time.Minute),
	}

	containers, err := testcontainers.ParallelContainers(ctx, []testcontainers.GenericContainerRequest{
		{ContainerRequest: quiReq, Started: true},
		{ContainerRequest: qbitReq, Started: true},
	}, testcontainers.ParallelContainersOptions{})
	if err != nil {
		return fmt.Errorf("failed to warmup containers: %w", err)
	}

	// Store the built image name for tests to use
	quiImageMutex.Lock()
	quiImageName = imageRepo + ":" + imageTag
	quiImageMutex.Unlock()

	// Terminate both - we just needed to build/pull the images
	for _, c := range containers {
		if err := c.Terminate(ctx); err != nil {
			return fmt.Errorf("failed to terminate warmup container: %w", err)
		}
	}

	fmt.Println("Warmup complete: images ready")
	return nil
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

	// Start qui container first (can run in parallel with qBittorrent startup)
	t.Logf("Starting %d qBittorrent instances + qui...", instanceCount)

	quiReq := quiRequest(net.Name, cfg.Timeout)
	quiContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: quiReq,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start qui container: %v", err)
	}
	env.Qui = quiContainer

	quiPort, err := getMappedPortWithRetry(ctx, env.Qui, "7476", 10)
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

		// Get external URL - we use fixed port binding, so we know the host port
		inst.ExtURL = "http://localhost:" + webUIPortStr

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

	// Acquire semaphore to limit concurrent container startups
	sem := getContainerSem()
	sem <- struct{}{}
	defer func() { <-sem }()

	// Log which qBittorrent image we're using (useful for version matrix testing)
	fmt.Printf("Using qBittorrent image: %s\n", getQBitImage())

	// Create shared network
	net, err := network.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}
	env.Network = net

	// Generate random ports for this test instance to allow parallel execution
	webUIPort := randomPortInRange(minRandomPort, maxRandomPort)
	torrentPort := randomPortInRange(minRandomPort, maxRandomPort)

	// Build container requests
	qbitReq := qbittorrentRequest(net.Name, cfg.Timeout, webUIPort, torrentPort)
	quiReq := quiRequest(net.Name, cfg.Timeout)

	// Start containers sequentially to avoid overwhelming Docker
	qbitContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: qbitReq,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start qbittorrent container: %w", err)
	}
	env.QBittorrent = qbitContainer

	quiContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: quiReq,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start qui container: %w", err)
	}
	env.Qui = quiContainer

	// Setup qBittorrent (extract password, configure settings)
	// We use fixed port binding, so we already know the host port
	webUIPortStr := strconv.Itoa(webUIPort)
	env.QBitExtURL = "http://localhost:" + webUIPortStr

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
	quiPort, err := getMappedPortWithRetry(ctx, env.Qui, "7476", 10)
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
// Uses the pre-built image from warmup if available, otherwise builds from source.
func quiRequest(networkName string, timeout time.Duration) testcontainers.ContainerRequest {
	quiImageMutex.RLock()
	imageName := quiImageName
	quiImageMutex.RUnlock()

	if imageName == "" {
		panic("quiRequest called before Warmup - no pre-built image available")
	}

	return testcontainers.ContainerRequest{
		Image:        imageName,
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

// getMappedPortWithRetry retries getting a mapped port up to maxRetries times.
// This handles race conditions where port mappings aren't immediately available
// after ParallelContainers returns.
func getMappedPortWithRetry(ctx context.Context, container testcontainers.Container, port nat.Port, maxRetries int) (nat.Port, error) {
	var lastErr error
	for i := range maxRetries {
		mappedPort, err := container.MappedPort(ctx, port)
		if err == nil {
			return mappedPort, nil
		}
		lastErr = err
		if i < maxRetries-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return "", fmt.Errorf("failed to get mapped port after %d retries: %w", maxRetries, lastErr)
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

	webUIContainerPort := nat.Port(webUIPortStr + "/tcp")
	torrentTCPPort := nat.Port(torrentPortStr + "/tcp")
	torrentUDPPort := nat.Port(torrentPortStr + "/udp")

	return testcontainers.ContainerRequest{
		Image: getQBitImage(),
		Env: map[string]string{
			"PUID":            "1000",
			"PGID":            "1000",
			"TZ":              "UTC",
			"WEBUI_PORT":      webUIPortStr,
			"TORRENTING_PORT": torrentPortStr,
		},
		ExposedPorts: []string{
			webUIPortStr + "/tcp",
			torrentPortStr + "/tcp",
			torrentPortStr + "/udp",
		},
		Networks: []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {"qbittorrent"},
		},
		// Use HostConfigModifier to bind container ports to specific host ports
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.PortBindings = nat.PortMap{
				webUIContainerPort: []nat.PortBinding{{HostIP: "", HostPort: webUIPortStr}},
				torrentTCPPort:     []nat.PortBinding{{HostIP: "", HostPort: torrentPortStr}},
				torrentUDPPort:     []nat.PortBinding{{HostIP: "", HostPort: torrentPortStr}},
			}
		},
		// Wait for linuxserver init to complete - works across all versions
		// After this log, the WebUI port is open and ready
		WaitingFor: wait.ForLog("[ls.io-init] done.").WithStartupTimeout(timeout),
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
// For qBittorrent 4.6+, extracts from logs. For older versions, returns default "adminadmin".
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
	// This is only present in qBittorrent 4.6+
	re := regexp.MustCompile(`temporary password is provided for this session: (\S+)`)
	matches := re.FindSubmatch(logBytes)
	if len(matches) < 2 {
		// qBittorrent < 4.6 uses default password "adminadmin"
		return "adminadmin", nil
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
