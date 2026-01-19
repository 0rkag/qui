// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/pkg/pathutil"
)

// Validation patterns for SSH configuration
var (
	// validUsername allows alphanumeric, underscore, hyphen, and dot (common SSH username chars)
	validUsername = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$`)
	// validHostname allows alphanumeric, hyphen, dot, colon for IPv6
	// Must start with alphanumeric or colon (for raw IPv6 like ::1)
	validHostname = regexp.MustCompile(`^[a-zA-Z0-9:][a-zA-Z0-9.\-:]*$`)
)

// Local rsync version detection (cached)
var (
	localRsyncOnce            sync.Once
	localRsyncSupportsInfo    bool // true if local rsync supports --info=progress2 (3.1.0+)
	localRsyncVersionDetected bool
)

// detectLocalRsyncVersion checks if the local rsync supports --info=progress2 (requires 3.1.0+).
// The result is cached for the lifetime of the process.
func detectLocalRsyncVersion() {
	localRsyncOnce.Do(func() {
		cmd := exec.Command("rsync", "--version")
		output, err := cmd.Output()
		if err != nil {
			return
		}

		// Parse version from output like "rsync  version 3.2.7  protocol version 31"
		// or older format "rsync  version 2.6.9  protocol version 29"
		versionStr := string(output)
		if idx := strings.Index(versionStr, "version "); idx != -1 {
			versionStr = versionStr[idx+8:]
			if spaceIdx := strings.IndexAny(versionStr, " \t\n"); spaceIdx != -1 {
				versionStr = versionStr[:spaceIdx]
			}

			// Parse major.minor version
			parts := strings.Split(versionStr, ".")
			if len(parts) >= 2 {
				major, errMajor := strconv.Atoi(parts[0])
				minor, errMinor := strconv.Atoi(parts[1])
				if errMajor == nil && errMinor == nil {
					// --info=progress2 was added in rsync 3.1.0
					localRsyncSupportsInfo = major > 3 || (major == 3 && minor >= 1)
					localRsyncVersionDetected = true
				}
			}
		}
	})
}

// ValidatePath checks if a path is safe for use in shell commands.
// It ensures the path is absolute, contains no traversal attempts, and has reasonable length.
func ValidatePath(p string) error {
	if len(p) > 4096 {
		return fmt.Errorf("path too long (max 4096 chars)")
	}
	if err := pathutil.ValidateAbsolute(p); err != nil {
		return err
	}
	return nil
}

// ValidatePathWithBase validates that a path is safe and stays within the given base directory.
func ValidatePathWithBase(path, baseDir string) error {
	if err := ValidatePath(path); err != nil {
		return err
	}
	if err := ValidatePath(baseDir); err != nil {
		return fmt.Errorf("invalid base directory: %w", err)
	}
	cleaned := filepath.Clean(path)
	cleanedBase := filepath.Clean(baseDir)
	// Path must be under or equal to the base directory
	if cleaned != cleanedBase && !strings.HasPrefix(cleaned, cleanedBase+"/") {
		return fmt.Errorf("path escapes base directory")
	}
	return nil
}

// ValidateUsername checks if a username is safe for use in shell commands.
func ValidateUsername(username string) error {
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}
	if len(username) > 64 {
		return fmt.Errorf("username too long (max 64 chars)")
	}
	if !validUsername.MatchString(username) {
		return fmt.Errorf("username contains invalid characters")
	}
	return nil
}

// ValidateHostname checks if a hostname is safe for use in shell commands.
func ValidateHostname(host string) error {
	if host == "" {
		return fmt.Errorf("hostname cannot be empty")
	}
	if len(host) > 253 {
		return fmt.Errorf("hostname too long (max 253 chars)")
	}
	if !validHostname.MatchString(host) {
		return fmt.Errorf("hostname contains invalid characters")
	}
	return nil
}

// validateConfig validates SSH configuration for safe use in commands.
func validateConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config cannot be nil")
	}
	if err := ValidateUsername(cfg.Username); err != nil {
		return err
	}
	if err := ValidateHostname(cfg.Host); err != nil {
		return err
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

// verifyHostKey performs a quick connection to verify the host key matches the expected fingerprint.
// This should be called before running rsync/scp commands to ensure we're connecting to the right host.
// If no expected key is set in config, this function returns nil (TOFU behavior).
// Note: ctx parameter is reserved for future context-aware dialing implementation.
func verifyHostKey(_ context.Context, cfg *Config) error {
	if cfg.ExpectedHostKey == nil || cfg.ExpectedHostKey.Fingerprint == "" {
		// No expected key - TOFU behavior, accept any key
		return nil
	}
	if cfg.SkipHostKeyVerification {
		// Explicitly skipping verification
		return nil
	}

	// Create a minimal connection to verify the host key
	// We use a short timeout since this is just for verification
	verifyCfg := &Config{
		Host:            cfg.Host,
		Port:            cfg.Port,
		Username:        cfg.Username,
		Password:        cfg.Password,
		PrivateKeyPath:  cfg.PrivateKeyPath,
		Timeout:         cfg.Timeout,
		ExpectedHostKey: cfg.ExpectedHostKey,
	}

	client, _, err := New(verifyCfg)
	if err != nil {
		return fmt.Errorf("host key verification failed: %w", err)
	}
	client.Close()
	return nil
}

// captureHostKey connects to a host and captures its public key for known_hosts generation.
// Returns the host key in OpenSSH known_hosts format.
// Note: ctx parameter is reserved for future context-aware dialing implementation.
func captureHostKey(_ context.Context, cfg *Config) (string, error) {
	var capturedKey ssh.PublicKey

	// Build auth methods
	var authMethods []ssh.AuthMethod
	if cfg.PrivateKeyPath != "" {
		signer, err := loadPrivateKey(cfg.PrivateKeyPath)
		if err != nil {
			return "", fmt.Errorf("load private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}
	if len(authMethods) == 0 {
		return "", fmt.Errorf("no authentication method available")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * 1000000000 // 10 seconds
	}

	sshConfig := &ssh.ClientConfig{
		User: cfg.Username,
		Auth: authMethods,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			capturedKey = key
			// If we have an expected key, verify it
			if cfg.ExpectedHostKey != nil && cfg.ExpectedHostKey.Fingerprint != "" {
				fingerprint := ssh.FingerprintSHA256(key)
				if fingerprint != cfg.ExpectedHostKey.Fingerprint {
					return &HostKeyError{
						Expected: cfg.ExpectedHostKey.Fingerprint,
						Actual:   fingerprint,
						Hostname: hostname,
					}
				}
			}
			return nil
		},
		Timeout: timeout,
	}

	port := cfg.Port
	if port == 0 {
		port = 22
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return "", err
	}
	client.Close()

	if capturedKey == nil {
		return "", fmt.Errorf("failed to capture host key")
	}

	// Format as known_hosts entry: [host]:port keytype base64key
	hostEntry := cfg.Host
	if port != 22 {
		hostEntry = fmt.Sprintf("[%s]:%d", cfg.Host, port)
	}
	keyType := capturedKey.Type()
	keyData := base64.StdEncoding.EncodeToString(capturedKey.Marshal())

	return fmt.Sprintf("%s %s %s", hostEntry, keyType, keyData), nil
}

// createTempKnownHosts creates a temporary known_hosts file with the given entries.
// Returns the path to the temporary file. Caller is responsible for removing it.
func createTempKnownHosts(entries ...string) (string, error) {
	if len(entries) == 0 {
		return "", fmt.Errorf("no known_hosts entries provided")
	}

	tmpFile, err := os.CreateTemp("", "qui-known-hosts-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	// Set restrictive permissions (0600) for security
	if err := os.Chmod(tmpFile.Name(), 0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("set known_hosts permissions: %w", err)
	}

	content := strings.Join(entries, "\n") + "\n"
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("write known_hosts: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("close known_hosts: %w", err)
	}

	return tmpFile.Name(), nil
}


// ProgressInfo contains transfer progress information.
type ProgressInfo struct {
	BytesTransferred int64
	BytesTotal       int64
	Percentage       int
	Speed            string // Human-readable speed like "12.34MB/s"
	SpeedBytesPerSec int64  // Speed in bytes per second
	ETA              string // Estimated time remaining
}

// ProgressCallback is called periodically with transfer progress.
type ProgressCallback func(info ProgressInfo)

// TransferOptions configures file transfer behavior.
type TransferOptions struct {
	// PreservePermissions preserves file permissions and timestamps.
	PreservePermissions bool
	// Delete removes files at destination that don't exist at source.
	Delete bool
	// DryRun simulates the transfer without making changes.
	DryRun bool
	// OnProgress is called periodically with progress updates.
	OnProgress ProgressCallback
}

// TransferResult holds the result of a file transfer.
type TransferResult struct {
	// FilesTransferred is the number of files transferred.
	FilesTransferred int
	// BytesTransferred is the total bytes transferred.
	BytesTransferred int64
	// Output is the raw rsync output.
	Output string
}

// RsyncPush transfers files from local to remote using rsync.
func RsyncPush(ctx context.Context, cfg *Config, localPath, remotePath string, opts *TransferOptions) (*TransferResult, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if opts == nil {
		opts = &TransferOptions{PreservePermissions: true}
	}

	// Capture host key and create temporary known_hosts file for rsync
	knownHostsEntry, err := captureHostKey(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("verify host key: %w", err)
	}

	knownHostsFile, err := createTempKnownHosts(knownHostsEntry)
	if err != nil {
		return nil, fmt.Errorf("create known_hosts: %w", err)
	}
	defer os.Remove(knownHostsFile)

	args := buildRsyncArgs(opts)
	args = append(args, "-e", buildSSHCommandSecure(cfg, knownHostsFile))
	args = append(args, localPath)
	args = append(args, fmt.Sprintf("%s@%s:%s", cfg.Username, cfg.Host, remotePath))

	return runRsyncWithProgress(ctx, args, opts.OnProgress)
}

// RsyncPull transfers files from remote to local using rsync.
func RsyncPull(ctx context.Context, cfg *Config, remotePath, localPath string, opts *TransferOptions) (*TransferResult, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if opts == nil {
		opts = &TransferOptions{PreservePermissions: true}
	}

	// Capture host key and create temporary known_hosts file for rsync
	knownHostsEntry, err := captureHostKey(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("verify host key: %w", err)
	}

	knownHostsFile, err := createTempKnownHosts(knownHostsEntry)
	if err != nil {
		return nil, fmt.Errorf("create known_hosts: %w", err)
	}
	defer os.Remove(knownHostsFile)

	args := buildRsyncArgs(opts)
	args = append(args, "-e", buildSSHCommandSecure(cfg, knownHostsFile))
	args = append(args, fmt.Sprintf("%s@%s:%s", cfg.Username, cfg.Host, remotePath))
	args = append(args, localPath)

	return runRsyncWithProgress(ctx, args, opts.OnProgress)
}

// RsyncRemoteToRemote transfers files between two remote hosts.
// Requires the source host to have SSH access to the destination host.
//
// Host key verification:
//   - Source host: Verified using srcCfg.ExpectedHostKey if set
//   - Destination host (from source): Uses StrictHostKeyChecking=accept-new
//     which accepts new keys but rejects changed keys (TOFU behavior)
func RsyncRemoteToRemote(ctx context.Context, srcCfg, dstCfg *Config, srcPath, dstPath string, opts *TransferOptions) (*TransferResult, error) {
	// Validate both configs to prevent command injection
	if err := validateConfig(srcCfg); err != nil {
		return nil, fmt.Errorf("invalid source config: %w", err)
	}
	if err := validateConfig(dstCfg); err != nil {
		return nil, fmt.Errorf("invalid destination config: %w", err)
	}
	if opts == nil {
		opts = &TransferOptions{PreservePermissions: true}
	}

	// Build rsync command to run on source host
	// All arguments must be properly quoted to prevent shell injection
	rsyncArgs := buildRsyncArgs(opts)

	// Build the SSH command for the destination connection
	// Use accept-new which accepts new keys but rejects changed keys (safer than no)
	sshCmd := fmt.Sprintf("ssh -p %d -o StrictHostKeyChecking=accept-new", dstCfg.Port)
	rsyncArgs = append(rsyncArgs, "-e", sshCmd)

	// Quote the source path
	rsyncArgs = append(rsyncArgs, ShellQuote(srcPath))

	// Build and quote the destination (user@host validated above, path quoted)
	dstSpec := fmt.Sprintf("%s@%s:%s", dstCfg.Username, dstCfg.Host, ShellQuote(dstPath))
	rsyncArgs = append(rsyncArgs, dstSpec)

	// Connect to source using proper host key verification
	client, _, err := New(srcCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to source: %w", err)
	}
	defer client.Close()

	// Build command with each argument properly quoted
	var quotedArgs []string
	for _, arg := range rsyncArgs {
		// Args like -a, -v, --progress don't need quoting
		// But we already quoted paths above, and -e value needs special handling
		if strings.HasPrefix(arg, "-") && !strings.Contains(arg, " ") {
			quotedArgs = append(quotedArgs, arg)
		} else if strings.HasPrefix(arg, "'") {
			// Already quoted
			quotedArgs = append(quotedArgs, arg)
		} else if strings.Contains(arg, "@") && strings.Contains(arg, ":") {
			// Destination spec - already has quoted path component
			quotedArgs = append(quotedArgs, arg)
		} else {
			quotedArgs = append(quotedArgs, ShellQuote(arg))
		}
	}

	cmd := "rsync " + strings.Join(quotedArgs, " ")
	result, err := client.Exec(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("rsync: %w", err)
	}

	if result.ExitCode != 0 {
		return nil, fmt.Errorf("rsync failed (exit %d): %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}

	return &TransferResult{
		Output: result.Stdout,
	}, nil
}

// ScpPush copies a single file from local to remote using scp.
func ScpPush(ctx context.Context, cfg *Config, localPath, remotePath string) error {
	if err := validateConfig(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Capture host key and create temporary known_hosts file for scp
	knownHostsEntry, err := captureHostKey(ctx, cfg)
	if err != nil {
		return fmt.Errorf("verify host key: %w", err)
	}

	knownHostsFile, err := createTempKnownHosts(knownHostsEntry)
	if err != nil {
		return fmt.Errorf("create known_hosts: %w", err)
	}
	defer os.Remove(knownHostsFile)

	args := []string{
		"-P", fmt.Sprintf("%d", cfg.Port),
		"-o", "StrictHostKeyChecking=yes",
		"-o", fmt.Sprintf("UserKnownHostsFile=%s", knownHostsFile),
		"-i", cfg.PrivateKeyPath,
		localPath,
		fmt.Sprintf("%s@%s:%s", cfg.Username, cfg.Host, remotePath),
	}

	cmd := exec.CommandContext(ctx, "scp", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp failed: %w", err)
	}
	return nil
}

// ScpPull copies a single file from remote to local using scp.
func ScpPull(ctx context.Context, cfg *Config, remotePath, localPath string) error {
	if err := validateConfig(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Capture host key and create temporary known_hosts file for scp
	knownHostsEntry, err := captureHostKey(ctx, cfg)
	if err != nil {
		return fmt.Errorf("verify host key: %w", err)
	}

	knownHostsFile, err := createTempKnownHosts(knownHostsEntry)
	if err != nil {
		return fmt.Errorf("create known_hosts: %w", err)
	}
	defer os.Remove(knownHostsFile)

	args := []string{
		"-P", fmt.Sprintf("%d", cfg.Port),
		"-o", "StrictHostKeyChecking=yes",
		"-o", fmt.Sprintf("UserKnownHostsFile=%s", knownHostsFile),
		"-i", cfg.PrivateKeyPath,
		fmt.Sprintf("%s@%s:%s", cfg.Username, cfg.Host, remotePath),
		localPath,
	}

	cmd := exec.CommandContext(ctx, "scp", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp failed: %w", err)
	}
	return nil
}

func buildRsyncArgs(opts *TransferOptions) []string {
	// Detect local rsync version to determine progress flag support
	detectLocalRsyncVersion()

	// Use --info=progress2 for rsync 3.1+ (easier to parse overall progress)
	// Fall back to --progress for older versions (like macOS default rsync 2.6.9)
	var progressFlag string
	if localRsyncSupportsInfo {
		progressFlag = "--info=progress2"
	} else {
		progressFlag = "--progress"
	}

	var args []string
	if opts.PreservePermissions {
		args = []string{"-a", progressFlag}
	} else {
		args = []string{"-r", progressFlag}
	}

	if opts.Delete {
		args = append(args, "--delete")
	}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}

	return args
}

// buildSSHCommandSecure builds an SSH command with proper host key verification using a known_hosts file.
func buildSSHCommandSecure(cfg *Config, knownHostsFile string) string {
	return fmt.Sprintf("ssh -p %d -o StrictHostKeyChecking=yes -o UserKnownHostsFile=%s -i %s",
		cfg.Port, ShellQuote(knownHostsFile), ShellQuote(cfg.PrivateKeyPath))
}

// buildSSHCommandInsecure builds an SSH command that skips host key verification.
// Deprecated: Use buildSSHCommandSecure with a proper known_hosts file instead.
func buildSSHCommandInsecure(cfg *Config) string {
	return fmt.Sprintf("ssh -p %d -o StrictHostKeyChecking=no -i %s",
		cfg.Port, ShellQuote(cfg.PrivateKeyPath))
}

func runRsync(ctx context.Context, args []string) (*TransferResult, error) {
	return runRsyncWithProgress(ctx, args, nil)
}

func runRsyncWithProgress(ctx context.Context, args []string, onProgress ProgressCallback) (*TransferResult, error) {
	cmd := exec.CommandContext(ctx, "rsync", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// If no progress callback, just buffer stdout
	if onProgress == nil {
		var stdout bytes.Buffer
		cmd.Stdout = &stdout

		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return nil, fmt.Errorf("rsync failed (exit %d): %s", exitErr.ExitCode(), stderr.String())
			}
			return nil, fmt.Errorf("rsync: %w", err)
		}

		return &TransferResult{
			Output: stdout.String(),
		}, nil
	}

	// Stream stdout for progress parsing
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start rsync: %w", err)
	}

	// Ensure version detection has run (called in buildRsyncArgs, but be safe)
	detectLocalRsyncVersion()

	// Create tracker for legacy mode (only used if rsync < 3.1)
	var tracker *rsyncProgressTracker
	if !localRsyncSupportsInfo {
		tracker = &rsyncProgressTracker{}
	}

	// Parse progress in a separate goroutine
	var outputBuf bytes.Buffer
	go func() {
		buf := make([]byte, 1024)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				outputBuf.Write(buf[:n])
				chunk := string(buf[:n])

				// Use appropriate parser based on rsync version
				var info *ProgressInfo
				if tracker != nil {
					// Legacy mode: use cumulative tracker
					info = tracker.parseProgressLegacy(chunk)
				} else {
					// Modern mode: parse --info=progress2 output
					info = parseRsyncProgress(chunk)
				}

				if info != nil {
					onProgress(*info)
				}
			}
			if readErr != nil {
				break
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("rsync failed (exit %d): %s", exitErr.ExitCode(), stderr.String())
		}
		return nil, fmt.Errorf("rsync: %w", err)
	}

	return &TransferResult{
		Output: outputBuf.String(),
	}, nil
}

// parseRsyncProgress parses rsync --info=progress2 output.
// Format:     12,345,678  23%   45.67MB/s    0:01:23
func parseRsyncProgress(output string) *ProgressInfo {
	// Look for progress line pattern
	// rsync outputs: "     bytes  pct%  speed  eta"
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Progress lines contain % and time format
		if !strings.Contains(line, "%") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		// Parse bytes (remove commas)
		bytesStr := strings.ReplaceAll(fields[0], ",", "")
		bytes, err := parseInt64(bytesStr)
		if err != nil {
			continue
		}

		// Parse percentage
		pctStr := strings.TrimSuffix(fields[1], "%")
		pct, err := parseInt(pctStr)
		if err != nil {
			continue
		}

		// Speed is field 2
		speed := fields[2]
		speedBytes := parseSpeed(speed)

		// ETA is field 3 (may be "xfr#" info instead)
		eta := ""
		if len(fields) >= 4 && strings.Contains(fields[3], ":") {
			eta = fields[3]
		}

		// Calculate total from percentage
		var total int64
		if pct > 0 {
			total = bytes * 100 / int64(pct)
		}

		return &ProgressInfo{
			BytesTransferred: bytes,
			BytesTotal:       total,
			Percentage:       pct,
			Speed:            speed,
			SpeedBytesPerSec: speedBytes,
			ETA:              eta,
		}
	}
	return nil
}

func parseInt64(s string) (int64, error) {
	var result int64
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

func parseInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

// rsyncProgressTracker tracks cumulative progress for legacy rsync (pre-3.1).
// Since --progress shows per-file progress, we need to aggregate across files
// to calculate overall transfer progress.
type rsyncProgressTracker struct {
	mu sync.Mutex

	// totalFiles is the total number of files to transfer (from to-chk denominator)
	totalFiles int
	// completedFiles is the number of files fully transferred (from xfr#)
	completedFiles int
	// completedBytes is cumulative bytes from completed files
	completedBytes int64
	// currentFileBytes is bytes transferred for the current file
	currentFileBytes int64
	// currentFileTotal is the total size of the current file (if known)
	currentFileTotal int64
	// lastSpeed is the most recent speed reading
	lastSpeed string
	// lastSpeedBytes is the most recent speed in bytes/sec
	lastSpeedBytes int64
}

// parseProgressLegacy parses rsync --progress output (legacy format, pre-3.1).
// Format:
//
//	filename.txt
//	      1234567 100%    1.23MB/s    0:00:01 (xfr#1, to-chk=5/10)
//
// Returns a ProgressInfo with cumulative progress across all files.
func (t *rsyncProgressTracker) parseProgressLegacy(output string) *ProgressInfo {
	t.mu.Lock()
	defer t.mu.Unlock()

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check for xfr# pattern which indicates a file transfer completion or progress
		// Format: (xfr#N, to-chk=M/T) or (xfr#N, ir-chk=M/T)
		if xfrIdx := strings.Index(line, "(xfr#"); xfrIdx != -1 {
			// Parse the xfr and to-chk info
			xfrPart := line[xfrIdx:]

			// Extract xfr number: (xfr#N,
			var xfrNum int
			if _, err := fmt.Sscanf(xfrPart, "(xfr#%d,", &xfrNum); err == nil {
				t.completedFiles = xfrNum
			}

			// Extract to-chk or ir-chk: to-chk=M/T) or ir-chk=M/T)
			var remaining, total int
			if strings.Contains(xfrPart, "to-chk=") {
				idx := strings.Index(xfrPart, "to-chk=")
				if _, err := fmt.Sscanf(xfrPart[idx:], "to-chk=%d/%d)", &remaining, &total); err == nil && total > 0 {
					t.totalFiles = total
				}
			} else if strings.Contains(xfrPart, "ir-chk=") {
				idx := strings.Index(xfrPart, "ir-chk=")
				if _, err := fmt.Sscanf(xfrPart[idx:], "ir-chk=%d/%d)", &remaining, &total); err == nil && total > 0 {
					t.totalFiles = total
				}
			}

			// Parse the bytes/speed from the line before the (xfr#
			progressPart := strings.TrimSpace(line[:xfrIdx])
			fields := strings.Fields(progressPart)

			if len(fields) >= 3 {
				// First field is bytes (may have commas)
				bytesStr := strings.ReplaceAll(fields[0], ",", "")
				if bytes, err := parseInt64(bytesStr); err == nil {
					// When we see 100%, add to completed bytes
					if len(fields) >= 2 && fields[1] == "100%" {
						t.completedBytes += bytes
						t.currentFileBytes = 0
						t.currentFileTotal = 0
					} else {
						t.currentFileBytes = bytes
					}
				}

				// Speed is typically field 2
				if len(fields) >= 3 {
					t.lastSpeed = fields[2]
					t.lastSpeedBytes = parseSpeed(fields[2])
				}
			}
		} else if strings.Contains(line, "%") {
			// Intermediate progress line (no xfr# yet) - just bytes and percentage
			// Format:      1234567  50%    1.23MB/s    0:00:01
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				bytesStr := strings.ReplaceAll(fields[0], ",", "")
				if bytes, err := parseInt64(bytesStr); err == nil {
					t.currentFileBytes = bytes
				}
				t.lastSpeed = fields[2]
				t.lastSpeedBytes = parseSpeed(fields[2])
			}
		}
	}

	// Calculate overall progress
	// Use file count as the primary progress indicator since we don't know total bytes
	var pct int
	if t.totalFiles > 0 {
		// Weight: completed files + partial progress on current file
		completedPct := float64(t.completedFiles) / float64(t.totalFiles) * 100
		// Add small amount for current file if in progress
		if t.currentFileBytes > 0 && t.completedFiles < t.totalFiles {
			// Estimate current file as 1/totalFiles contribution
			filePct := 100.0 / float64(t.totalFiles)
			// Assume current file is 50% done if we have any bytes
			completedPct += filePct * 0.5
		}
		pct = int(completedPct)
		if pct > 100 {
			pct = 100
		}
	}

	totalBytes := t.completedBytes + t.currentFileBytes

	return &ProgressInfo{
		BytesTransferred: totalBytes,
		BytesTotal:       0, // Unknown in legacy mode
		Percentage:       pct,
		Speed:            t.lastSpeed,
		SpeedBytesPerSec: t.lastSpeedBytes,
		ETA:              "", // Not easily calculable in legacy mode
	}
}

// parseSpeed converts speed string like "45.67MB/s" to bytes per second
func parseSpeed(s string) int64 {
	s = strings.TrimSuffix(s, "/s")

	multiplier := int64(1)
	if strings.HasSuffix(s, "KB") {
		multiplier = 1024
		s = strings.TrimSuffix(s, "KB")
	} else if strings.HasSuffix(s, "MB") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	} else if strings.HasSuffix(s, "GB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	} else if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}

	var value float64
	if _, err := fmt.Sscanf(s, "%f", &value); err != nil {
		return 0
	}

	return int64(value * float64(multiplier))
}
