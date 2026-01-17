// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import "time"

// TimeoutConfig holds timeout settings for various operations.
type TimeoutConfig struct {
	// APIRequest is the default timeout for API handler operations (seconds)
	APIRequest int `toml:"apiRequest" mapstructure:"apiRequest"`
	// ClientConnection is the timeout for establishing qBittorrent client connections (seconds)
	ClientConnection int `toml:"clientConnection" mapstructure:"clientConnection"`
	// HealthCheck is the timeout for health check operations (seconds)
	HealthCheck int `toml:"healthCheck" mapstructure:"healthCheck"`
	// BackgroundTask is the timeout for background async tasks (seconds)
	BackgroundTask int `toml:"backgroundTask" mapstructure:"backgroundTask"`
}

// DefaultTimeoutConfig returns the default timeout configuration.
func DefaultTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		APIRequest:       15,
		ClientConnection: 15,
		HealthCheck:      10,
		BackgroundTask:   30,
	}
}

// APIRequestTimeout returns the API request timeout as a duration.
func (t TimeoutConfig) APIRequestTimeout() time.Duration {
	if t.APIRequest <= 0 {
		return 15 * time.Second
	}
	return time.Duration(t.APIRequest) * time.Second
}

// ClientConnectionTimeout returns the client connection timeout as a duration.
func (t TimeoutConfig) ClientConnectionTimeout() time.Duration {
	if t.ClientConnection <= 0 {
		return 15 * time.Second
	}
	return time.Duration(t.ClientConnection) * time.Second
}

// HealthCheckTimeout returns the health check timeout as a duration.
func (t TimeoutConfig) HealthCheckTimeout() time.Duration {
	if t.HealthCheck <= 0 {
		return 10 * time.Second
	}
	return time.Duration(t.HealthCheck) * time.Second
}

// BackgroundTaskTimeout returns the background task timeout as a duration.
func (t TimeoutConfig) BackgroundTaskTimeout() time.Duration {
	if t.BackgroundTask <= 0 {
		return 30 * time.Second
	}
	return time.Duration(t.BackgroundTask) * time.Second
}

// Config represents the application configuration
type Config struct {
	Version                  string
	Host                     string `toml:"host" mapstructure:"host"`
	Port                     int    `toml:"port" mapstructure:"port"`
	BaseURL                  string `toml:"baseUrl" mapstructure:"baseUrl"`
	SessionSecret            string `toml:"sessionSecret" mapstructure:"sessionSecret"`
	LogLevel                 string `toml:"logLevel" mapstructure:"logLevel"`
	LogPath                  string `toml:"logPath" mapstructure:"logPath"`
	LogMaxSize               int    `toml:"logMaxSize" mapstructure:"logMaxSize"`
	LogMaxBackups            int    `toml:"logMaxBackups" mapstructure:"logMaxBackups"`
	DataDir                  string `toml:"dataDir" mapstructure:"dataDir"`
	CheckForUpdates          bool   `toml:"checkForUpdates" mapstructure:"checkForUpdates"`
	PprofEnabled             bool   `toml:"pprofEnabled" mapstructure:"pprofEnabled"`
	MetricsEnabled           bool   `toml:"metricsEnabled" mapstructure:"metricsEnabled"`
	MetricsHost              string `toml:"metricsHost" mapstructure:"metricsHost"`
	MetricsPort              int    `toml:"metricsPort" mapstructure:"metricsPort"`
	MetricsBasicAuthUsers    string `toml:"metricsBasicAuthUsers" mapstructure:"metricsBasicAuthUsers"`
	TrackerIconsFetchEnabled bool   `toml:"trackerIconsFetchEnabled" mapstructure:"trackerIconsFetchEnabled"`

	// CORSAllowedOrigins specifies which origins are allowed for CORS requests.
	// If empty, same-origin requests are allowed (recommended for production).
	// Use ["*"] to allow all origins (development only, not recommended for production).
	CORSAllowedOrigins []string `toml:"corsAllowedOrigins" mapstructure:"corsAllowedOrigins"`

	// Timeouts configuration (in seconds)
	// These control various operation timeouts throughout the application.
	Timeouts TimeoutConfig `toml:"timeouts" mapstructure:"timeouts"`

	ExternalProgramAllowList []string `toml:"externalProgramAllowList" mapstructure:"externalProgramAllowList"`

	// CrossSeedRecoverErroredTorrents enables recovery attempts for errored/missingFiles torrents
	// in cross-seed automation. When enabled, qui will pause, recheck, and resume errored torrents
	// before candidate selection. This can cause automation runs to take 25+ minutes per torrent.
	// When disabled (default), errored torrents are simply excluded from candidate selection.
	CrossSeedRecoverErroredTorrents bool `toml:"crossSeedRecoverErroredTorrents" mapstructure:"crossSeedRecoverErroredTorrents"`

	// OIDC Configuration
	OIDCEnabled             bool   `toml:"oidcEnabled" mapstructure:"oidcEnabled"`
	OIDCIssuer              string `toml:"oidcIssuer" mapstructure:"oidcIssuer"`
	OIDCClientID            string `toml:"oidcClientId" mapstructure:"oidcClientId"`
	OIDCClientSecret        string `toml:"oidcClientSecret" mapstructure:"oidcClientSecret"`
	OIDCRedirectURL         string `toml:"oidcRedirectUrl" mapstructure:"oidcRedirectUrl"`
	OIDCDisableBuiltInLogin bool   `toml:"oidcDisableBuiltInLogin" mapstructure:"oidcDisableBuiltInLogin"`
}
