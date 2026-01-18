//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// TestEnv holds all resources for the E2E test environment.
type TestEnv struct {
	// Paths
	TestDir    string
	ComposeDir string
	QuiBinary  string
	QuiDataDir string
	SSHKeyPath string

	// QUI process
	QuiCmd *exec.Cmd
	QuiPID int

	// Instance info
	Instances []*QBitInstance

	// HTTP client with cookies for QUI API
	HTTPClient *http.Client
	QUIURL     string
}

// QBitInstance represents a qBittorrent test instance.
type QBitInstance struct {
	ID            int
	Name          string
	Host          string
	Port          int
	SSHPort       int
	Password      string // Retrieved from container logs
	ContainerName string
}

// SetupTestEnvironment creates and initializes the complete test environment.
func SetupTestEnvironment(ctx context.Context) (*TestEnv, error) {
	log.Info().Msg("Setting up E2E test environment...")

	// Find test directory (where this code lives)
	testDir, err := findTestDir()
	if err != nil {
		return nil, fmt.Errorf("find test dir: %w", err)
	}

	env := &TestEnv{
		TestDir:    testDir,
		ComposeDir: testDir,
		QuiDataDir: filepath.Join(testDir, ".testdata"),
		SSHKeyPath: filepath.Join(testDir, ".testdata", "ssh_key"),
		QUIURL:     "http://localhost:17476",
		Instances: []*QBitInstance{
			{Name: "qbit1", Port: 18081, SSHPort: 12221, ContainerName: "qui-test-qbit1"},
			{Name: "qbit2", Port: 18082, SSHPort: 12222, ContainerName: "qui-test-qbit2"},
			{Name: "qbit3", Port: 18083, SSHPort: 12223, ContainerName: "qui-test-qbit3"},
		},
	}

	// Create cookie jar for HTTP client
	jar, _ := cookiejar.New(nil)
	env.HTTPClient = &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}

	// Cleanup any previous run
	log.Info().Msg("Cleaning up previous test artifacts...")
	_ = env.Teardown(ctx)

	// Create data directory
	if err := os.MkdirAll(env.QuiDataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	// Generate SSH keys
	if err := env.generateSSHKeys(ctx); err != nil {
		return nil, fmt.Errorf("generate SSH keys: %w", err)
	}

	// Build QUI
	if err := env.buildQUI(ctx); err != nil {
		return nil, fmt.Errorf("build qui: %w", err)
	}

	// Start containers
	if err := env.startContainers(ctx); err != nil {
		return nil, fmt.Errorf("start containers: %w", err)
	}

	// Wait for containers and get passwords
	if err := env.waitForContainers(ctx); err != nil {
		return nil, fmt.Errorf("wait for containers: %w", err)
	}

	// Setup SSH in containers
	if err := env.setupSSH(ctx); err != nil {
		return nil, fmt.Errorf("setup SSH: %w", err)
	}

	// Start QUI
	if err := env.startQUI(ctx); err != nil {
		return nil, fmt.Errorf("start qui: %w", err)
	}

	// Configure QUI with instances
	if err := env.configureQUI(ctx); err != nil {
		return nil, fmt.Errorf("configure qui: %w", err)
	}

	log.Info().Msg("Test environment ready!")
	return env, nil
}

// Teardown cleans up all test resources.
func (e *TestEnv) Teardown(ctx context.Context) error {
	log.Info().Msg("Tearing down test environment...")

	// Stop QUI
	if e.QuiCmd != nil && e.QuiCmd.Process != nil {
		_ = e.QuiCmd.Process.Kill()
		_ = e.QuiCmd.Wait()
	}

	// Stop containers (pass PUID/PGID for docker-compose variable substitution)
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", filepath.Join(e.ComposeDir, "docker-compose.yml"), "down", "-v")
	cmd.Dir = e.ComposeDir
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PUID=%d", os.Getuid()),
		fmt.Sprintf("PGID=%d", os.Getgid()),
	)
	_ = cmd.Run()

	// Remove data directory
	if e.QuiDataDir != "" {
		_ = os.RemoveAll(e.QuiDataDir)
	}

	return nil
}

// PrintInfo prints connection info for debugging.
func (e *TestEnv) PrintInfo() {
	fmt.Println("\n========================================")
	fmt.Println("Test Environment Info")
	fmt.Println("========================================")
	fmt.Printf("qui: %s (user: test, pass: secret123)\n", e.QUIURL)
	fmt.Println("\nqBittorrent instances:")
	for _, inst := range e.Instances {
		fmt.Printf("  - %s: http://127.0.0.1:%d (admin / %s)\n", inst.Name, inst.Port, inst.Password)
	}
	fmt.Println("\nTo cleanup: go test -tags=e2e ./tests/e2e/... -run TestCleanup")
	fmt.Println("========================================")
}

func findTestDir() (string, error) {
	// Get the directory of this source file
	_, filename, _, ok := runtimeCaller(0)
	if !ok {
		// Fallback: look for tests/e2e relative to working directory
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		// Walk up to find project root
		for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
			testDir := filepath.Join(dir, "tests", "e2e")
			if _, err := os.Stat(filepath.Join(testDir, "docker-compose.yml")); err == nil {
				return testDir, nil
			}
		}
		return "", fmt.Errorf("could not find tests/e2e directory")
	}
	return filepath.Dir(filename), nil
}

// runtimeCaller is a variable so it can be replaced in tests
var runtimeCaller = func(skip int) (pc uintptr, file string, line int, ok bool) {
	// Use runtime.Caller but import it dynamically to avoid import cycle issues
	// For now, return false to use fallback
	return 0, "", 0, false
}

func (e *TestEnv) generateSSHKeys(ctx context.Context) error {
	log.Info().Msg("Generating SSH keys...")

	keyPath := e.SSHKeyPath
	if _, err := os.Stat(keyPath); err == nil {
		return nil // Already exists
	}

	cmd := exec.CommandContext(ctx, "ssh-keygen", "-t", "ed25519", "-f", keyPath, "-N", "", "-C", "qui-e2e-test", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ssh-keygen failed: %w: %s", err, out)
	}
	return nil
}

func (e *TestEnv) buildQUI(ctx context.Context) error {
	log.Info().Msg("Building qui...")

	// Find project root (parent of tests/e2e)
	projectRoot := filepath.Dir(filepath.Dir(e.TestDir))
	e.QuiBinary = filepath.Join(e.QuiDataDir, "qui-test")

	cmd := exec.CommandContext(ctx, "go", "build", "-o", e.QuiBinary, "./cmd/qui")
	cmd.Dir = projectRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %w: %s", err, out)
	}
	return nil
}

func (e *TestEnv) startContainers(ctx context.Context) error {
	log.Info().Msg("Creating bind mount directories...")

	// Create bind mount directories
	volumeDirs := []string{
		filepath.Join(e.TestDir, ".testdata", "volumes", "qbit1", "downloads"),
		filepath.Join(e.TestDir, ".testdata", "volumes", "qbit1", "config"),
		filepath.Join(e.TestDir, ".testdata", "volumes", "qbit2", "downloads"),
		filepath.Join(e.TestDir, ".testdata", "volumes", "qbit2", "config"),
		filepath.Join(e.TestDir, ".testdata", "volumes", "qbit3", "downloads"),
		filepath.Join(e.TestDir, ".testdata", "volumes", "qbit3", "config"),
		filepath.Join(e.TestDir, ".testdata", "volumes", "shared"),
	}
	for _, dir := range volumeDirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create volume dir %s: %w", dir, err)
		}
	}

	log.Info().Msg("Starting containers...")

	// Get current user's UID/GID for container permissions
	uid := os.Getuid()
	gid := os.Getgid()

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", filepath.Join(e.ComposeDir, "docker-compose.yml"), "up", "-d")
	cmd.Dir = e.ComposeDir
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PUID=%d", uid),
		fmt.Sprintf("PGID=%d", gid),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose up failed: %w: %s", err, out)
	}

	// Wait for containers to initialize
	time.Sleep(15 * time.Second)

	return nil
}

func (e *TestEnv) waitForContainers(ctx context.Context) error {
	log.Info().Msg("Waiting for containers and retrieving passwords...")

	pwRegex := regexp.MustCompile(`temporary password.*: (\S+)`)

	for _, inst := range e.Instances {
		// Get password from logs
		for i := 0; i < 30; i++ {
			cmd := exec.CommandContext(ctx, "docker", "logs", inst.ContainerName)
			out, err := cmd.Output()
			if err != nil {
				time.Sleep(1 * time.Second)
				continue
			}

			matches := pwRegex.FindStringSubmatch(string(out))
			if len(matches) >= 2 {
				inst.Password = matches[1]
				log.Info().Str("instance", inst.Name).Str("password", inst.Password).Msg("Got password")
				break
			}
			time.Sleep(1 * time.Second)
		}

		if inst.Password == "" {
			return fmt.Errorf("could not get password for %s", inst.Name)
		}

		// Verify login works
		if err := e.verifyQBitLogin(ctx, inst); err != nil {
			return fmt.Errorf("verify login for %s: %w", inst.Name, err)
		}
	}

	return nil
}

func (e *TestEnv) verifyQBitLogin(ctx context.Context, inst *QBitInstance) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/v2/auth/login", inst.Port)
	body := fmt.Sprintf("username=admin&password=%s", inst.Password)

	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if string(respBody) != "Ok." {
		return fmt.Errorf("login failed: %s", respBody)
	}
	return nil
}

func (e *TestEnv) setupSSH(ctx context.Context) error {
	log.Info().Msg("Setting up SSH in containers...")

	pubKey, err := os.ReadFile(e.SSHKeyPath + ".pub")
	if err != nil {
		return fmt.Errorf("read public key: %w", err)
	}

	for _, inst := range e.Instances {
		// Install packages
		if err := e.dockerExec(ctx, inst.ContainerName, "sh", "-c", "apk update && apk add openssh rsync"); err != nil {
			return fmt.Errorf("install packages in %s: %w", inst.Name, err)
		}

		// Generate host keys
		if err := e.dockerExec(ctx, inst.ContainerName, "ssh-keygen", "-A"); err != nil {
			return fmt.Errorf("generate host keys in %s: %w", inst.Name, err)
		}

		// Setup authorized_keys
		cmds := []string{
			"mkdir -p /root/.ssh && chmod 700 /root/.ssh",
			fmt.Sprintf("echo '%s' > /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys", strings.TrimSpace(string(pubKey))),
			"echo -e 'Host *\\n  StrictHostKeyChecking no\\n  UserKnownHostsFile /dev/null' > /root/.ssh/config && chmod 600 /root/.ssh/config",
			fmt.Sprintf("sed -i 's/#Port 22/Port %d/' /etc/ssh/sshd_config", inst.SSHPort),
			"sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config",
			"sed -i 's/#PubkeyAuthentication.*/PubkeyAuthentication yes/' /etc/ssh/sshd_config",
			"echo 'root:testpass' | chpasswd",
		}

		for _, c := range cmds {
			if err := e.dockerExec(ctx, inst.ContainerName, "sh", "-c", c); err != nil {
				return fmt.Errorf("setup SSH in %s: %w", inst.Name, err)
			}
		}

		// Copy private key for inter-container rsync
		if err := e.dockerCp(ctx, e.SSHKeyPath, inst.ContainerName+":/root/.ssh/id_ed25519"); err != nil {
			return fmt.Errorf("copy SSH key to %s: %w", inst.Name, err)
		}
		if err := e.dockerExec(ctx, inst.ContainerName, "chmod", "600", "/root/.ssh/id_ed25519"); err != nil {
			return fmt.Errorf("chmod SSH key in %s: %w", inst.Name, err)
		}

		// Start sshd
		if err := e.dockerExec(ctx, inst.ContainerName, "/usr/sbin/sshd"); err != nil {
			return fmt.Errorf("start sshd in %s: %w", inst.Name, err)
		}

		log.Info().Str("instance", inst.Name).Int("sshPort", inst.SSHPort).Msg("SSH configured")
	}

	// Verify SSH works
	for _, inst := range e.Instances {
		cmd := exec.CommandContext(ctx, "ssh",
			"-i", e.SSHKeyPath,
			"-p", fmt.Sprintf("%d", inst.SSHPort),
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "ConnectTimeout=5",
			"root@127.0.0.1", "echo ok")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("SSH verification failed for %s: %w: %s", inst.Name, err, out)
		}
	}

	return nil
}

func (e *TestEnv) dockerExec(ctx context.Context, container string, args ...string) error {
	cmdArgs := append([]string{"exec", container}, args...)
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func (e *TestEnv) dockerCp(ctx context.Context, src, dst string) error {
	cmd := exec.CommandContext(ctx, "docker", "cp", src, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func (e *TestEnv) startQUI(ctx context.Context) error {
	log.Info().Msg("Starting qui...")

	configDir := filepath.Join(e.QuiDataDir, "config")
	dataDir := filepath.Join(e.QuiDataDir, "data")

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	// Generate config
	cmd := exec.CommandContext(ctx, e.QuiBinary, "--config-dir", configDir, "generate-config")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("generate-config failed: %w: %s", err, out)
	}

	// Create user
	cmd = exec.CommandContext(ctx, e.QuiBinary, "--config-dir", configDir, "--data-dir", dataDir,
		"create-user", "--username", "test", "--password", "secret123")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create-user failed: %w: %s", err, out)
	}

	// Update config to use test port
	configFile := filepath.Join(configDir, "config.toml")
	configData, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	configData = bytes.Replace(configData, []byte("port = 7476"), []byte("port = 17476"), 1)
	if err := os.WriteFile(configFile, configData, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// Start server
	e.QuiCmd = exec.Command(e.QuiBinary, "--config-dir", configDir, "--data-dir", dataDir, "serve")
	e.QuiCmd.Stdout = os.Stdout
	e.QuiCmd.Stderr = os.Stderr

	if err := e.QuiCmd.Start(); err != nil {
		return fmt.Errorf("start qui: %w", err)
	}
	e.QuiPID = e.QuiCmd.Process.Pid

	// Wait for QUI to be ready
	for i := 0; i < 30; i++ {
		resp, err := http.Get(e.QUIURL + "/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			log.Info().Int("pid", e.QuiPID).Msg("qui started")
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("qui did not become ready")
}

func (e *TestEnv) configureQUI(ctx context.Context) error {
	log.Info().Msg("Configuring qui with instances...")

	// Login
	if err := e.quiLogin(ctx); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	// Create instances
	for _, inst := range e.Instances {
		id, err := e.quiCreateInstance(ctx, inst)
		if err != nil {
			return fmt.Errorf("create instance %s: %w", inst.Name, err)
		}
		inst.ID = id

		// Create SSH connection
		if err := e.quiCreateSSHConnection(ctx, inst); err != nil {
			return fmt.Errorf("create SSH connection for %s: %w", inst.Name, err)
		}

		// Create path mappings
		for _, path := range []string{"/downloads", "/shared"} {
			if err := e.quiCreatePathMapping(ctx, inst.ID, path); err != nil {
				return fmt.Errorf("create path mapping for %s: %w", inst.Name, err)
			}
		}

		log.Info().Str("instance", inst.Name).Int("id", inst.ID).Msg("Instance configured")
	}

	return nil
}

func (e *TestEnv) quiLogin(ctx context.Context) error {
	body := `{"username": "test", "password": "secret123"}`
	req, _ := http.NewRequestWithContext(ctx, "POST", e.QUIURL+"/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed: %s", body)
	}
	return nil
}

func (e *TestEnv) quiCreateInstance(ctx context.Context, inst *QBitInstance) (int, error) {
	body := fmt.Sprintf(`{
		"name": "%s",
		"host": "http://127.0.0.1:%d",
		"username": "admin",
		"password": "%s",
		"hasLocalFilesystemAccess": false
	}`, inst.Name, inst.Port, inst.Password)

	req, _ := http.NewRequestWithContext(ctx, "POST", e.QUIURL+"/api/instances", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("create instance failed (status %d): %s", resp.StatusCode, respBody)
	}

	var result struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, fmt.Errorf("decode response: %w (body: %s)", err, respBody)
	}
	return result.ID, nil
}

func (e *TestEnv) quiCreateSSHConnection(ctx context.Context, inst *QBitInstance) error {
	body := fmt.Sprintf(`{
		"protocol": "ssh",
		"host": "127.0.0.1",
		"port": %d,
		"username": "root",
		"privateKeyPath": "%s",
		"enabled": true
	}`, inst.SSHPort, e.SSHKeyPath)

	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/instances/%d/connections", e.QUIURL, inst.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create SSH connection failed: %s", body)
	}
	return nil
}

func (e *TestEnv) quiCreatePathMapping(ctx context.Context, instanceID int, path string) error {
	body := fmt.Sprintf(`{
		"instancePath": "%s",
		"canonicalPath": "%s",
		"enabled": true
	}`, path, path)

	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/instances/%d/path-mappings", e.QUIURL, instanceID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create path mapping failed: %s", body)
	}
	return nil
}
