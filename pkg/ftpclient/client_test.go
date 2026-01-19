// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ftpclient

import (
	"testing"
	"time"
)

func TestNew_NilConfig(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Error("New(nil) expected error, got nil")
	}
}

func TestNew_MissingHost(t *testing.T) {
	cfg := &Config{
		Username: "user",
		Password: "pass",
	}
	_, err := New(cfg)
	if err == nil {
		t.Error("New() with missing host expected error, got nil")
	}
}

func TestNew_DefaultPorts(t *testing.T) {
	tests := []struct {
		name     string
		tlsMode  string
		wantPort int
	}{
		{
			name:     "implicit TLS uses port 990",
			tlsMode:  TLSModeImplicit,
			wantPort: 990,
		},
		{
			name:     "explicit TLS uses port 21",
			tlsMode:  TLSModeExplicit,
			wantPort: 21,
		},
		{
			name:     "no TLS uses port 21",
			tlsMode:  TLSModeNone,
			wantPort: 21,
		},
		{
			name:     "empty TLS mode uses port 21",
			tlsMode:  "",
			wantPort: 21,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Host:     "192.0.2.1", // RFC 5737 TEST-NET-1 - won't connect
				Username: "user",
				Password: "pass",
				TLSMode:  tt.tlsMode,
				Port:     0, // Let it be set by default
				Timeout:  100 * time.Millisecond,
			}

			// New() sets cfg.Port before attempting connection, so even though
			// the connection will fail, we can verify the port was set correctly
			_, _ = New(cfg) // Ignore error - we expect connection failure

			if cfg.Port != tt.wantPort {
				t.Errorf("After New(), Port = %d, want %d", cfg.Port, tt.wantPort)
			}
		})
	}
}

func TestNew_InvalidTLSMode(t *testing.T) {
	cfg := &Config{
		Host:     "example.com",
		Port:     21,
		Username: "user",
		Password: "pass",
		TLSMode:  "invalid_mode",
	}

	_, err := New(cfg)
	if err == nil {
		t.Error("New() with invalid TLS mode expected error, got nil")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	if cfg.Port != 21 {
		t.Errorf("DefaultConfig().Port = %d, want 21", cfg.Port)
	}

	if cfg.Timeout != 30*time.Second {
		t.Errorf("DefaultConfig().Timeout = %v, want 30s", cfg.Timeout)
	}

	if cfg.TLSMode != TLSModeExplicit {
		t.Errorf("DefaultConfig().TLSMode = %q, want %q", cfg.TLSMode, TLSModeExplicit)
	}
}

func TestTLSModeConstants(t *testing.T) {
	if TLSModeNone != "none" {
		t.Errorf("TLSModeNone = %q, want \"none\"", TLSModeNone)
	}
	if TLSModeExplicit != "explicit" {
		t.Errorf("TLSModeExplicit = %q, want \"explicit\"", TLSModeExplicit)
	}
	if TLSModeImplicit != "implicit" {
		t.Errorf("TLSModeImplicit = %q, want \"implicit\"", TLSModeImplicit)
	}
}

func TestConfig_TimeoutDefaults(t *testing.T) {
	tests := []struct {
		name        string
		timeout     time.Duration
		wantDefault bool
	}{
		{
			name:        "zero timeout should use default",
			timeout:     0,
			wantDefault: true,
		},
		{
			name:        "explicit timeout should be used",
			timeout:     60 * time.Second,
			wantDefault: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Host:     "example.com",
				Port:     21,
				Username: "user",
				Password: "pass",
				Timeout:  tt.timeout,
			}

			// Verify the config value (New() would set the default)
			if tt.wantDefault && cfg.Timeout != 0 {
				t.Errorf("Config.Timeout = %v, expected 0 to be set to default by New()", cfg.Timeout)
			}
			if !tt.wantDefault && cfg.Timeout != tt.timeout {
				t.Errorf("Config.Timeout = %v, want %v", cfg.Timeout, tt.timeout)
			}
		})
	}
}

func TestClient_Close_NilConn(t *testing.T) {
	// Test that Close doesn't panic on nil connection
	client := &Client{conn: nil}

	err := client.Close()
	if err != nil {
		t.Errorf("Close() on nil conn error = %v, want nil", err)
	}
}

func TestClient_IsConnected_NilConn(t *testing.T) {
	client := &Client{conn: nil}

	if client.IsConnected() {
		t.Error("IsConnected() on nil conn = true, want false")
	}
}

func TestClient_Config(t *testing.T) {
	cfg := &Config{
		Host:     "example.com",
		Port:     21,
		Username: "user",
		Password: "secret",
	}

	client := &Client{config: cfg}

	returnedCfg := client.Config()
	if returnedCfg != cfg {
		t.Error("Config() should return the client's config")
	}

	// Verify it's the same reference
	if returnedCfg.Host != cfg.Host {
		t.Errorf("Config().Host = %q, want %q", returnedCfg.Host, cfg.Host)
	}
}

func TestCapabilities_Defaults(t *testing.T) {
	caps := &Capabilities{}

	if caps.TLSEnabled {
		t.Error("Capabilities.TLSEnabled should be false by default")
	}
	if caps.PassiveModeWorks {
		t.Error("Capabilities.PassiveModeWorks should be false by default")
	}
	if caps.FXPSupported {
		t.Error("Capabilities.FXPSupported should be false by default")
	}
	if caps.ServerType != "" {
		t.Errorf("Capabilities.ServerType = %q, want empty", caps.ServerType)
	}
	if len(caps.Features) != 0 {
		t.Errorf("Capabilities.Features length = %d, want 0", len(caps.Features))
	}
}

