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
	"os/exec"
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

// Coverage collection support.
// Set QUI_E2E_COVERAGE=1 to build and run the coverage-instrumented binary.
// Coverage data is collected from each qui container via docker cp after graceful shutdown.
var (
	coverageEnabled = os.Getenv("QUI_E2E_COVERAGE") == "1"
	coverageDir     string
	coverageMutex   sync.Mutex // serializes docker cp to avoid filesystem races
	projectRoot     string     // absolute path to the project root, set during Warmup
)

// QBitInstance holds info about a single qBittorrent instance.
type QBitInstance struct {
	ID        int                      // qui instance ID (assigned after registration)
	Container testcontainers.Container // Docker container
	URL       string                   // Internal URL for qui to connect
	ExtURL    string                   // External URL for direct access
	Password  string                   // WebUI password
}

// Env holds all containers for a test run.
type Env struct {
	Network   *testcontainers.DockerNetwork
	Qui       testcontainers.Container
	QuiURL    string
	Instances []*QBitInstance

	quiClient *client.Client
}

// Client returns an authenticated client for qui's API.
func (e *Env) Client() *client.Client {
	return e.quiClient
}

// Teardown stops and removes all containers.
// When coverage is enabled, the qui container is stopped gracefully first
// so coverage data flushes, then copied out via docker cp.
func (e *Env) Teardown(ctx context.Context) {
	var quiStopped bool
	if coverageEnabled && e.Qui != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "coverage: panic during collection: %v\n", r)
				}
			}()
			quiStopped = collectCoverage(ctx, e.Qui)
		}()
	}
	if e.Qui != nil {
		if quiStopped {
			// Already stopped by coverage collection, just remove the container
			_ = exec.CommandContext(ctx, "docker", "rm", e.Qui.GetContainerID()).Run()
		} else {
			_ = e.Qui.Terminate(ctx)
		}
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

// collectCoverage stops a qui container gracefully (flushing coverage data)
// and copies the coverage files to the host via docker cp.
// Returns true if the container was stopped successfully.
func collectCoverage(ctx context.Context, c testcontainers.Container) bool {
	containerID := c.GetContainerID()
	if len(containerID) < 12 {
		fmt.Fprintf(os.Stderr, "coverage: container has no valid ID, skipping\n")
		return false
	}
	short := containerID[:12]

	// Stop gracefully — clean shutdown writes coverage data via Go's atexit hook
	timeout := 10 * time.Second
	stopped := true
	if err := c.Stop(ctx, &timeout); err != nil {
		fmt.Fprintf(os.Stderr, "coverage: failed to stop container %s: %v\n", short, err)
		stopped = false
		// Continue — partial coverage data may still exist
	}

	// Serialize docker cp to avoid filesystem races in the shared coverage dir
	coverageMutex.Lock()
	defer coverageMutex.Unlock()

	cmd := exec.CommandContext(ctx, "docker", "cp", containerID+":/tmp/covdata/.", coverageDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "coverage: failed to copy from container %s: %v: %s\n", short, err, out)
	}
	return stopped
}

// CoverageEnabled reports whether e2e coverage collection is active.
func CoverageEnabled() bool { return coverageEnabled }

// CoverageDir returns the host directory where coverage data is collected.
func CoverageDir() string { return coverageDir }

// ProjectRoot returns the absolute path to the project root, set during Warmup.
func ProjectRoot() string { return projectRoot }

// RegisterInstances registers all qBittorrent instances with qui.
// Call this after Setup if you need instances to be registered.
func (e *Env) RegisterInstances(t *testing.T) {
	t.Helper()
	for i, inst := range e.Instances {
		inst.ID = e.quiClient.CreateInstance(t, client.InstanceConfig{
			Name:     fmt.Sprintf("qbit-%d", i+1),
			Host:     inst.URL,
			Username: "admin",
			Password: inst.Password,
		})
	}
}

// Config for test environment.
type Config struct {
	Timeout      time.Duration // Container startup timeout
	SkipQuiSetup bool          // Skip initial admin setup (auth test drives setup itself)
}

// DefaultConfig returns sensible defaults for local development.
func DefaultConfig() Config {
	return Config{
		Timeout: 3 * time.Minute,
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
	projectRoot = absPath

	// Set up coverage directory if coverage is enabled
	if coverageEnabled {
		coverageDir = filepath.Join(absPath, "e2e", "covdata")
		// Clean and recreate to avoid stale data from previous runs
		if err := os.RemoveAll(coverageDir); err != nil {
			return fmt.Errorf("failed to clean coverage dir: %w", err)
		}
		if err := os.MkdirAll(coverageDir, 0o755); err != nil {
			return fmt.Errorf("failed to create coverage dir: %w", err)
		}
		fmt.Printf("Coverage collection enabled: %s\n", coverageDir)
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

// Setup creates a test environment with qui and N qBittorrent instances.
// Instances are NOT auto-registered with qui - call env.RegisterInstances(t) if needed.
func Setup(ctx context.Context, t *testing.T, cfg Config, instanceCount int) *Env {
	t.Helper()

	env := &Env{
		Instances: make([]*QBitInstance, instanceCount),
	}

	// Create shared network
	net, err := network.New(ctx)
	if err != nil {
		t.Fatalf("failed to create network: %v", err)
	}
	env.Network = net

	t.Logf("Starting %d qBittorrent instance(s) + qui...", instanceCount)

	// Pre-allocate ports for all instances (thread-safe)
	type instancePorts struct {
		webUIPort   int
		torrentPort int
	}
	ports := make([]instancePorts, instanceCount)
	for i := range instanceCount {
		ports[i] = instancePorts{
			webUIPort:   randomPortInRange(minRandomPort, maxRandomPort),
			torrentPort: randomPortInRange(minRandomPort, maxRandomPort),
		}
	}

	// Start all containers in parallel with rate limiting using worker pool
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)         // Limit concurrent container startups
	errors := make([]error, instanceCount+1) // qui + N qbittorrent

	// Start qui container
	wg.Add(1)
	go func() {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		var err error
		env.Qui, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: quiRequest(net.Name, cfg.Timeout),
			Started:          true,
		})
		errors[0] = err
	}()

	// Start qBittorrent instances
	for i := range instanceCount {
		env.Instances[i] = &QBitInstance{}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
				ContainerRequest: qbittorrentRequest(net.Name, cfg.Timeout, ports[idx].webUIPort, ports[idx].torrentPort),
				Started:          true,
			})
			env.Instances[idx].Container = container
			env.Instances[idx].ExtURL = "http://localhost:" + strconv.Itoa(ports[idx].webUIPort)
			errors[idx+1] = err
		}(i)
	}

	wg.Wait()

	// Check for startup errors - clean up any started containers on failure
	for i, err := range errors {
		if err != nil {
			// Clean up containers that did start before failing
			for _, inst := range env.Instances {
				if inst != nil && inst.Container != nil {
					_ = inst.Container.Terminate(ctx)
				}
			}
			if env.Qui != nil {
				_ = env.Qui.Terminate(ctx)
			}
			if env.Network != nil {
				_ = env.Network.Remove(ctx)
			}
			if i == 0 {
				t.Fatalf("failed to start qui container: %v", err)
			}
			t.Fatalf("failed to start qbittorrent container %d: %v", i-1, err)
		}
	}

	// Configure qui
	quiPort, err := getMappedPortWithRetry(ctx, env.Qui, "7476", 10)
	if err != nil {
		t.Fatalf("failed to get qui port: %v", err)
	}
	env.QuiURL = "http://localhost:" + quiPort.Port()

	if !cfg.SkipQuiSetup {
		env.quiClient, err = configureQuiShared(ctx, env.QuiURL, "admin", "adminadmin")
		if err != nil {
			t.Fatalf("failed to configure qui: %v", err)
		}
	}

	// Configure qBittorrent instances (but don't register with qui)
	for i := range instanceCount {
		inst := env.Instances[i]

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
		inst.URL = fmt.Sprintf("http://%s:%d", qbitIP, ports[i].webUIPort)
	}

	t.Logf("Environment ready: %d qBittorrent instance(s)", instanceCount)
	return env
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

	req := testcontainers.ContainerRequest{
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

	if coverageEnabled {
		req.Entrypoint = []string{"/usr/local/bin/qui-cover"}
		req.Env = map[string]string{"GOCOVERDIR": "/tmp/covdata"}
	}

	return req
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

// isPortAvailable checks if a TCP port is available on the host.
func isPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

// randomPortInRange returns a random unused port in the given range.
// Thread-safe for parallel test execution.
// Panics if no available port can be found after 1000 attempts.
func randomPortInRange(min, max int) int {
	portMutex.Lock()
	defer portMutex.Unlock()

	for range 1000 {
		port := min + rand.IntN(max-min+1)
		if !usedPorts[port] && isPortAvailable(port) {
			usedPorts[port] = true
			return port
		}
	}
	// No available port found - this indicates port exhaustion
	panic(fmt.Sprintf("failed to find available port in range %d-%d after 1000 attempts", min, max))
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
		// and add tmpfs mount for downloads directory
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.PortBindings = nat.PortMap{
				webUIContainerPort: []nat.PortBinding{{HostIP: "", HostPort: webUIPortStr}},
				torrentTCPPort:     []nat.PortBinding{{HostIP: "", HostPort: torrentPortStr}},
				torrentUDPPort:     []nat.PortBinding{{HostIP: "", HostPort: torrentPortStr}},
			}
			// Add tmpfs mount for downloads directory to avoid permission issues
			hc.Tmpfs = map[string]string{
				"/downloads": "rw,size=1g",
			}
		},
		// Wait for linuxserver init to complete - works across all versions
		// After this log, the WebUI port is open and ready
		WaitingFor: wait.ForLog("[ls.io-init] done.").WithStartupTimeout(timeout),
	}
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
