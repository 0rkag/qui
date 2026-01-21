package client

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
	HasMetdata bool   `json:"has_metadata,omitempty"`
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
