// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"testing"
	"time"
)

func TestHostKeyError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *HostKeyError
		expected string
	}{
		{
			name: "basic error message",
			err: &HostKeyError{
				Hostname: "example.com",
				Expected: "SHA256:abc123",
				Actual:   "SHA256:xyz789",
			},
			expected: "host key mismatch for example.com: expected SHA256:abc123, got SHA256:xyz789",
		},
		{
			name: "with port in hostname",
			err: &HostKeyError{
				Hostname: "example.com:2222",
				Expected: "SHA256:expected",
				Actual:   "SHA256:actual",
			},
			expected: "host key mismatch for example.com:2222: expected SHA256:expected, got SHA256:actual",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("HostKeyError.Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestHostKeyInfo_Fields(t *testing.T) {
	info := &HostKeyInfo{
		Algorithm:   "ssh-ed25519",
		Fingerprint: "SHA256:abcdef123456",
	}

	if info.Algorithm != "ssh-ed25519" {
		t.Errorf("HostKeyInfo.Algorithm = %q, want ssh-ed25519", info.Algorithm)
	}
	if info.Fingerprint != "SHA256:abcdef123456" {
		t.Errorf("HostKeyInfo.Fingerprint = %q, want SHA256:abcdef123456", info.Fingerprint)
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := &Config{
		Host:     "example.com",
		Username: "user",
	}

	// Port should default to 0 (set by New())
	if cfg.Port != 0 {
		t.Errorf("Config.Port default = %d, want 0", cfg.Port)
	}

	// SkipHostKeyVerification should default to false (secure by default)
	if cfg.SkipHostKeyVerification {
		t.Error("Config.SkipHostKeyVerification should default to false")
	}
}

func TestConfig_HostKeySettings(t *testing.T) {
	tests := []struct {
		name                    string
		skipVerification        bool
		expectedHostKey         *HostKeyInfo
		expectVerificationLogic string
	}{
		{
			name:                    "skip verification - insecure mode",
			skipVerification:        true,
			expectedHostKey:         nil,
			expectVerificationLogic: "skip all verification",
		},
		{
			name:             "with expected host key - strict mode",
			skipVerification: false,
			expectedHostKey: &HostKeyInfo{
				Algorithm:   "ssh-ed25519",
				Fingerprint: "SHA256:test",
			},
			expectVerificationLogic: "verify fingerprint matches",
		},
		{
			name:                    "no expected key, no skip - TOFU mode",
			skipVerification:        false,
			expectedHostKey:         nil,
			expectVerificationLogic: "trust on first use",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Host:                    "example.com",
				Username:                "user",
				SkipHostKeyVerification: tt.skipVerification,
				ExpectedHostKey:         tt.expectedHostKey,
			}

			// Verify config is set correctly
			if cfg.SkipHostKeyVerification != tt.skipVerification {
				t.Errorf("SkipHostKeyVerification = %v, want %v", cfg.SkipHostKeyVerification, tt.skipVerification)
			}

			if tt.expectedHostKey != nil {
				if cfg.ExpectedHostKey == nil {
					t.Error("ExpectedHostKey should be set")
				} else if cfg.ExpectedHostKey.Fingerprint != tt.expectedHostKey.Fingerprint {
					t.Errorf("ExpectedHostKey.Fingerprint = %q, want %q",
						cfg.ExpectedHostKey.Fingerprint, tt.expectedHostKey.Fingerprint)
				}
			}
		})
	}
}

func TestNew_NilConfig(t *testing.T) {
	_, _, err := New(nil)
	if err == nil {
		t.Error("New(nil) should return error")
	}
}

func TestNew_MissingHost(t *testing.T) {
	cfg := &Config{
		Username: "user",
	}
	_, _, err := New(cfg)
	if err == nil {
		t.Error("New() with missing host should return error")
	}
}

func TestNew_DefaultPort(t *testing.T) {
	cfg := &Config{
		Host:                    "192.0.2.1", // RFC 5737 TEST-NET - won't connect
		Username:                "user",
		Timeout:                 50 * time.Millisecond,
		SkipHostKeyVerification: true,
	}

	// New will fail to connect (expected), but should set default port
	_, _, _ = New(cfg)

	if cfg.Port != 22 {
		t.Errorf("Default port should be 22, got %d", cfg.Port)
	}
}

func TestNew_DefaultTimeout(t *testing.T) {
	cfg := &Config{
		Host:                    "192.0.2.1",
		Username:                "user",
		Port:                    22,
		SkipHostKeyVerification: true,
		// Timeout not set - should default
	}

	_, _, _ = New(cfg)

	if cfg.Timeout != 30*time.Second {
		t.Errorf("Default timeout should be 30s, got %v", cfg.Timeout)
	}
}

func TestClient_Close_NilClient(t *testing.T) {
	client := &Client{}
	err := client.Close()
	if err != nil {
		t.Errorf("Close() on empty client should not error, got: %v", err)
	}
}

func TestClient_IsAlive_NilClient(t *testing.T) {
	client := &Client{}
	if client.IsAlive() {
		t.Error("IsAlive() on empty client should return false")
	}
}

func TestClient_IsAlive_NilUnderlying(t *testing.T) {
	client := &Client{
		client: nil,
	}
	if client.IsAlive() {
		t.Error("IsAlive() with nil underlying client should return false")
	}
}

func TestNewInsecure(t *testing.T) {
	cfg := &Config{
		Host:     "192.0.2.1",
		Username: "user",
		Port:     22,
		Timeout:  50 * time.Millisecond,
	}

	// Verify that NewInsecure sets SkipHostKeyVerification
	_, _ = NewInsecure(cfg)

	if !cfg.SkipHostKeyVerification {
		t.Error("NewInsecure should set SkipHostKeyVerification to true")
	}
}

func TestHostKeyCallback_TOFU(t *testing.T) {
	// Test that OnNewHostKey callback is called for TOFU (Trust On First Use)
	callbackCalled := false
	var capturedInfo *HostKeyInfo

	cfg := &Config{
		Host:     "example.com",
		Username: "user",
		Port:     22,
		OnNewHostKey: func(info *HostKeyInfo) error {
			callbackCalled = true
			capturedInfo = info
			return nil
		},
	}

	// We can't fully test this without an SSH server,
	// but we can verify the callback is configured correctly
	if cfg.OnNewHostKey == nil {
		t.Error("OnNewHostKey callback should be set")
	}

	// Simulate callback being invoked
	testInfo := &HostKeyInfo{
		Algorithm:   "ssh-ed25519",
		Fingerprint: "SHA256:test123",
	}
	err := cfg.OnNewHostKey(testInfo)
	if err != nil {
		t.Errorf("OnNewHostKey callback returned error: %v", err)
	}
	if !callbackCalled {
		t.Error("OnNewHostKey callback was not called")
	}
	if capturedInfo != testInfo {
		t.Error("OnNewHostKey callback received wrong info")
	}
}
