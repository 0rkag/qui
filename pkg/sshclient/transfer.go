// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Validation patterns for SSH configuration
var (
	// validUsername allows alphanumeric, underscore, hyphen, and dot (common SSH username chars)
	validUsername = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$`)
	// validHostname allows alphanumeric, hyphen, dot, colon for IPv6
	// Must start with alphanumeric or colon (for raw IPv6 like ::1)
	validHostname = regexp.MustCompile(`^[a-zA-Z0-9:][a-zA-Z0-9.\-:]*$`)
)

// ValidatePath checks if a path is safe for use in shell commands.
// It ensures the path is absolute, contains no traversal attempts, and has reasonable length.
func ValidatePath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if len(path) > 4096 {
		return fmt.Errorf("path too long (max 4096 chars)")
	}
	// Must be absolute (starts with /)
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must be absolute")
	}
	// Check for path traversal BEFORE cleaning to catch attempts like /foo/../bar
	// which would be normalized to /bar by filepath.Clean
	if strings.Contains(path, "..") {
		return fmt.Errorf("path contains traversal elements")
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

	args := buildRsyncArgs(opts)
	args = append(args, "-e", buildSSHCommand(cfg))
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

	args := buildRsyncArgs(opts)
	args = append(args, "-e", buildSSHCommand(cfg))
	args = append(args, fmt.Sprintf("%s@%s:%s", cfg.Username, cfg.Host, remotePath))
	args = append(args, localPath)

	return runRsyncWithProgress(ctx, args, opts.OnProgress)
}

// RsyncRemoteToRemote transfers files between two remote hosts.
// Requires the source host to have SSH access to the destination host.
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

	// Build the SSH command with quoted key path
	sshCmd := fmt.Sprintf("ssh -p %d -o StrictHostKeyChecking=no", dstCfg.Port)
	rsyncArgs = append(rsyncArgs, "-e", sshCmd)

	// Quote the source path
	rsyncArgs = append(rsyncArgs, ShellQuote(srcPath))

	// Build and quote the destination (user@host validated above, path quoted)
	dstSpec := fmt.Sprintf("%s@%s:%s", dstCfg.Username, dstCfg.Host, ShellQuote(dstPath))
	rsyncArgs = append(rsyncArgs, dstSpec)

	// Connect to source and run rsync there
	client, err := New(srcCfg)
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

	args := []string{
		"-P", fmt.Sprintf("%d", cfg.Port),
		"-o", "StrictHostKeyChecking=no",
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

	args := []string{
		"-P", fmt.Sprintf("%d", cfg.Port),
		"-o", "StrictHostKeyChecking=no",
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
	// Use --info=progress2 for overall progress (easier to parse)
	args := []string{"-a", "--info=progress2"}

	if !opts.PreservePermissions {
		args = []string{"-r", "--info=progress2"}
	}
	if opts.Delete {
		args = append(args, "--delete")
	}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}

	return args
}

func buildSSHCommand(cfg *Config) string {
	// Note: StrictHostKeyChecking=no is acceptable here as connections are made
	// to user-configured internal/trusted hosts. See comment in client.go.
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

	// Parse progress in a separate goroutine
	var outputBuf bytes.Buffer
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				outputBuf.Write(buf[:n])
				// Try to parse progress from the output
				if info := parseRsyncProgress(string(buf[:n])); info != nil {
					onProgress(*info)
				}
			}
			if err != nil {
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
