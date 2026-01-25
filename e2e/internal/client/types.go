package client

import "encoding/json"

// InstanceConfig is the request body for creating/updating instances.
type InstanceConfig struct {
	Name          string  `json:"name"`
	Host          string  `json:"host"`
	Username      string  `json:"username"`
	Password      string  `json:"password,omitempty"`
	BasicUsername *string `json:"basicUsername,omitempty"`
	BasicPassword *string `json:"basicPassword,omitempty"`
	TLSSkipVerify bool    `json:"tlsSkipVerify,omitempty"`
}

// Instance represents an instance response from the API.
type Instance struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Host             string `json:"host"`
	Username         string `json:"username"`
	TLSSkipVerify    bool   `json:"tlsSkipVerify"`
	Connected        bool   `json:"connected"`
	ConnectionStatus string `json:"connectionStatus,omitempty"`
	IsActive         bool   `json:"isActive"`
	DisplayOrder     int    `json:"displayOrder"`
}

// TestConnectionResponse is the response from testing an instance connection.
type TestConnectionResponse struct {
	Connected bool   `json:"connected"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Capabilities represents instance capabilities.
type Capabilities struct {
	WebAPIVersion           string `json:"webApiVersion"`
	SupportsSetTags         bool   `json:"supportsSetTags"`
	SupportsTorrentCreation bool   `json:"supportsTorrentCreation"`
	SupportsTorrentExport   bool   `json:"supportsTorrentExport"`
	SupportsTrackerEditing  bool   `json:"supportsTrackerEditing"`
	SupportsFilePriority    bool   `json:"supportsFilePriority"`
	SupportsSubcategories   bool   `json:"supportsSubcategories"`
	SupportsRenameTorrent   bool   `json:"supportsRenameTorrent"`
	SupportsRenameFile      bool   `json:"supportsRenameFile"`
	SupportsRenameFolder    bool   `json:"supportsRenameFolder"`
}

// AddTorrentOptions for adding torrents.
type AddTorrentOptions struct {
	SavePath     string   `json:"savepath,omitempty"`
	Category     string   `json:"category,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Paused       bool     `json:"paused,omitempty"`
	SkipChecking bool     `json:"skip_checking,omitempty"`
}

// Category represents a qBittorrent category.
type Category struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath"`
}

// Torrent represents a torrent in the list response.
type Torrent struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	State       string  `json:"state"`
	Progress    float64 `json:"progress"`
	Size        int64   `json:"size"`
	Downloaded  int64   `json:"downloaded"`
	Uploaded    int64   `json:"uploaded"`
	DlSpeed     int64   `json:"dlspeed"`
	UpSpeed     int64   `json:"upspeed"`
	Category    string  `json:"category"`
	Tags        string  `json:"tags"` // Comma-separated string from API
	SavePath    string  `json:"save_path"`
	AddedOn     int64   `json:"added_on"`
	Ratio       float64 `json:"ratio"`
	NumSeeds    int     `json:"num_seeds"`
	NumLeechers int     `json:"num_leechs"`
}

// TorrentListResponse is the response from listing torrents.
type TorrentListResponse struct {
	Torrents []Torrent `json:"torrents"`
	Total    int       `json:"total"`
	Stats    Stats     `json:"stats"`
}

// Stats contains aggregate statistics.
type Stats struct {
	Total              int   `json:"total"`
	Downloading        int   `json:"downloading"`
	Seeding            int   `json:"seeding"`
	Paused             int   `json:"paused"`
	Checking           int   `json:"checking"`
	Error              int   `json:"error"`
	TotalDownloadSpeed int64 `json:"totalDownloadSpeed"`
	TotalUploadSpeed   int64 `json:"totalUploadSpeed"`
}

// FilterOptions for filtering torrent lists.
type FilterOptions struct {
	Status     []string `json:"status,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

// TorrentProperties represents detailed properties of a torrent.
// Only includes fields relevant for e2e testing.
type TorrentProperties struct {
	SavePath   string `json:"save_path"`
	TotalSize  int64  `json:"total_size"`
	PiecesNum  int    `json:"pieces_num"`
	PieceSize  int64  `json:"piece_size"`
	AddedOn    int64  `json:"addition_date"`
	Comment    string `json:"comment"`
	CreatedBy  string `json:"created_by"`
	IsPrivate  bool   `json:"isPrivate"`
	HasMetadata bool `json:"has_metadata,omitempty"`
}

// Tracker represents a torrent tracker.
type Tracker struct {
	URL           string `json:"url"`
	Status        int    `json:"status"`
	Tier          int    `json:"tier"`
	NumPeers      int    `json:"num_peers"`
	NumSeeds      int    `json:"num_seeds"`
	NumLeechers   int    `json:"num_leeches"`
	NumDownloaded int    `json:"num_downloaded"`
	Msg           string `json:"msg"`
}

// TorrentFile represents a file within a torrent.
type TorrentFile struct {
	Index        int     `json:"index"`
	Name         string  `json:"name"`
	Size         int64   `json:"size"`
	Progress     float64 `json:"progress"`
	Priority     int     `json:"priority"`
	IsSeed       bool    `json:"is_seed"`
	PieceRange   []int   `json:"piece_range"`
	Availability float64 `json:"availability"`
}

// ---- Automations ----

// Automation represents an automation rule.
type Automation struct {
	ID              int               `json:"id"`
	InstanceID      int               `json:"instanceId"`
	Name            string            `json:"name"`
	TrackerPattern  string            `json:"trackerPattern"`
	TrackerDomains  []string          `json:"trackerDomains,omitempty"`
	Enabled         bool              `json:"enabled"`
	SortOrder       int               `json:"sortOrder"`
	IntervalSeconds *int              `json:"intervalSeconds,omitempty"`
	Conditions      *ActionConditions `json:"conditions"`
	CreatedAt       string            `json:"createdAt,omitempty"`
	UpdatedAt       string            `json:"updatedAt,omitempty"`
}

// AutomationPayload is the request body for creating/updating automations.
type AutomationPayload struct {
	Name            string            `json:"name"`
	TrackerPattern  string            `json:"trackerPattern,omitempty"`
	TrackerDomains  []string          `json:"trackerDomains,omitempty"`
	Enabled         *bool             `json:"enabled,omitempty"`
	SortOrder       *int              `json:"sortOrder,omitempty"`
	IntervalSeconds *int              `json:"intervalSeconds,omitempty"`
	Conditions      *ActionConditions `json:"conditions"`
}

// ActionConditions holds all the action configurations for an automation.
type ActionConditions struct {
	SchemaVersion string             `json:"schemaVersion,omitempty"`
	SpeedLimits   *SpeedLimitAction  `json:"speedLimits,omitempty"`
	ShareLimits   *ShareLimitsAction `json:"shareLimits,omitempty"`
	Pause         *PauseAction       `json:"pause,omitempty"`
	Delete        *DeleteAction      `json:"delete,omitempty"`
	Tag           *TagAction         `json:"tag,omitempty"`
	Category      *CategoryAction    `json:"category,omitempty"`
}

// SpeedLimitAction configures speed limit application.
type SpeedLimitAction struct {
	Enabled     bool           `json:"enabled"`
	UploadKiB   *int64         `json:"uploadKiB,omitempty"`
	DownloadKiB *int64         `json:"downloadKiB,omitempty"`
	Condition   *RuleCondition `json:"condition,omitempty"`
}

// ShareLimitsAction configures share limit application.
type ShareLimitsAction struct {
	Enabled            bool           `json:"enabled"`
	RatioLimit         *float64       `json:"ratioLimit,omitempty"`
	SeedingTimeMinutes *int64         `json:"seedingTimeMinutes,omitempty"`
	Condition          *RuleCondition `json:"condition,omitempty"`
}

// PauseAction configures pause action.
type PauseAction struct {
	Enabled   bool           `json:"enabled"`
	Condition *RuleCondition `json:"condition,omitempty"`
}

// DeleteAction configures delete action.
type DeleteAction struct {
	Enabled          bool           `json:"enabled"`
	Mode             string         `json:"mode,omitempty"` // "delete", "deleteWithFiles", etc.
	IncludeHardlinks bool           `json:"includeHardlinks,omitempty"`
	Condition        *RuleCondition `json:"condition,omitempty"`
}

// TagAction configures tagging action.
type TagAction struct {
	Enabled         bool           `json:"enabled"`
	Tags            []string       `json:"tags,omitempty"`
	Mode            string         `json:"mode,omitempty"` // "full", "add", "remove"
	UseTrackerAsTag bool           `json:"useTrackerAsTag,omitempty"`
	UseDisplayName  bool           `json:"useDisplayName,omitempty"`
	Condition       *RuleCondition `json:"condition,omitempty"`
}

// CategoryAction configures category assignment.
type CategoryAction struct {
	Enabled                      bool           `json:"enabled"`
	Category                     string         `json:"category,omitempty"`
	IncludeCrossSeeds            bool           `json:"includeCrossSeeds,omitempty"`
	BlockIfCrossSeedInCategories []string       `json:"blockIfCrossSeedInCategories,omitempty"`
	Condition                    *RuleCondition `json:"condition,omitempty"`
}

// RuleCondition represents a condition in an automation rule.
// For group conditions (AND/OR), set Operator to "AND" or "OR" and provide child Conditions.
// For leaf conditions, set Field, Operator, and Value.
type RuleCondition struct {
	Field      string           `json:"field,omitempty"`
	Operator   string           `json:"operator,omitempty"` // For leaf: EQUALS, MATCHES, etc. For group: AND, OR
	Value      string           `json:"value,omitempty"`
	MinValue   *float64         `json:"minValue,omitempty"`
	MaxValue   *float64         `json:"maxValue,omitempty"`
	Regex      bool             `json:"regex,omitempty"`
	Negate     bool             `json:"negate,omitempty"`
	Conditions []*RuleCondition `json:"conditions,omitempty"`
}

// AutomationActivity represents a logged automation action.
type AutomationActivity struct {
	ID            int             `json:"id"`
	InstanceID    int             `json:"instanceId"`
	Hash          string          `json:"hash"`
	TorrentName   string          `json:"torrentName,omitempty"`
	TrackerDomain string          `json:"trackerDomain,omitempty"`
	Action        string          `json:"action"`
	RuleID        *int            `json:"ruleId,omitempty"`
	RuleName      string          `json:"ruleName,omitempty"`
	Outcome       string          `json:"outcome"`
	Reason        string          `json:"reason,omitempty"`
	Details       json.RawMessage `json:"details,omitempty"`
	CreatedAt     string          `json:"createdAt"`
}

// RegexValidationResult is the response from ValidateRegex.
type RegexValidationResult struct {
	Valid  bool                   `json:"valid"`
	Errors []RegexValidationError `json:"errors"`
}

// RegexValidationError represents a regex validation error.
type RegexValidationError struct {
	Path     string `json:"path"`
	Message  string `json:"message"`
	Pattern  string `json:"pattern"`
	Field    string `json:"field"`
	Operator string `json:"operator"`
}

// PreviewResult contains torrents that would match an automation rule.
type PreviewResult struct {
	TotalMatches   int              `json:"totalMatches"`
	CrossSeedCount int              `json:"crossSeedCount,omitempty"`
	Examples       []PreviewTorrent `json:"examples"`
}

// PreviewTorrent is a simplified torrent for preview display.
type PreviewTorrent struct {
	Name           string  `json:"name"`
	Hash           string  `json:"hash"`
	Size           int64   `json:"size"`
	Ratio          float64 `json:"ratio"`
	SeedingTime    int64   `json:"seedingTime"`
	Tracker        string  `json:"tracker"`
	Category       string  `json:"category"`
	Tags           string  `json:"tags"`
	State          string  `json:"state"`
	AddedOn        int64   `json:"addedOn"`
	Uploaded       int64   `json:"uploaded"`
	Downloaded     int64   `json:"downloaded"`
	ContentPath    string  `json:"contentPath,omitempty"`
	IsUnregistered bool    `json:"isUnregistered,omitempty"`
	IsCrossSeed    bool    `json:"isCrossSeed,omitempty"`
	IsHardlinkCopy bool    `json:"isHardlinkCopy,omitempty"`
	NumSeeds       int64   `json:"numSeeds"`
	NumComplete    int64   `json:"numComplete"`
	NumLeechs      int64   `json:"numLeechs"`
	NumIncomplete  int64   `json:"numIncomplete"`
	Progress       float64 `json:"progress"`
	Availability   float64 `json:"availability"`
	TimeActive     int64   `json:"timeActive"`
	LastActivity   int64   `json:"lastActivity"`
	CompletionOn   int64   `json:"completionOn"`
	TotalSize      int64   `json:"totalSize"`
	HardlinkScope  string  `json:"hardlinkScope,omitempty"`
}
