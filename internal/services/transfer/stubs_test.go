// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"sync"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

// mockSyncManager implements SyncManager for tests
type mockSyncManager struct {
	mu sync.Mutex

	getTorrentsFunc      func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error)
	getTorrentFilesFunc  func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error)
	getTorrentPropsFunc  func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error)
	exportTorrentFunc    func(ctx context.Context, instanceID int, hash string) ([]byte, string, string, error)
	addTorrentFunc       func(ctx context.Context, instanceID int, data []byte, opts map[string]string) error
	bulkActionFunc       func(ctx context.Context, instanceID int, hashes []string, action string) error
	deleteTorrentsFunc   func(ctx context.Context, instanceID int, hashes []string, deleteFiles bool) error
	getCategoriesFunc    func(ctx context.Context, instanceID int) (map[string]qbt.Category, error)
	createCategoryFunc   func(ctx context.Context, instanceID int, name, path string) error
	hasTorrentByHashFunc func(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)

	// Call tracking
	addTorrentCalls    []addTorrentCall
	deleteTorrentCalls []deleteTorrentCall
	bulkActionCalls    []bulkActionCall
}

type addTorrentCall struct {
	InstanceID int
	Options    map[string]string
}

type deleteTorrentCall struct {
	InstanceID  int
	Hashes      []string
	DeleteFiles bool
}

type bulkActionCall struct {
	InstanceID int
	Hashes     []string
	Action     string
}

func newMockSyncManager() *mockSyncManager {
	return &mockSyncManager{
		addTorrentCalls:    make([]addTorrentCall, 0),
		deleteTorrentCalls: make([]deleteTorrentCall, 0),
		bulkActionCalls:    make([]bulkActionCall, 0),
	}
}

func (m *mockSyncManager) GetTorrents(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if m.getTorrentsFunc != nil {
		return m.getTorrentsFunc(ctx, instanceID, filter)
	}
	return nil, nil
}

func (m *mockSyncManager) GetTorrentFiles(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
	if m.getTorrentFilesFunc != nil {
		return m.getTorrentFilesFunc(ctx, instanceID, hash)
	}
	return nil, nil
}

func (m *mockSyncManager) GetTorrentProperties(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
	if m.getTorrentPropsFunc != nil {
		return m.getTorrentPropsFunc(ctx, instanceID, hash)
	}
	return nil, nil
}

func (m *mockSyncManager) ExportTorrent(ctx context.Context, instanceID int, hash string) ([]byte, string, string, error) {
	if m.exportTorrentFunc != nil {
		return m.exportTorrentFunc(ctx, instanceID, hash)
	}
	return []byte("mock torrent data"), "test.torrent", "application/x-bittorrent", nil
}

func (m *mockSyncManager) AddTorrent(ctx context.Context, instanceID int, data []byte, opts map[string]string) error {
	m.mu.Lock()
	m.addTorrentCalls = append(m.addTorrentCalls, addTorrentCall{
		InstanceID: instanceID,
		Options:    opts,
	})
	m.mu.Unlock()

	if m.addTorrentFunc != nil {
		return m.addTorrentFunc(ctx, instanceID, data, opts)
	}
	return nil
}

func (m *mockSyncManager) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	m.mu.Lock()
	m.bulkActionCalls = append(m.bulkActionCalls, bulkActionCall{
		InstanceID: instanceID,
		Hashes:     hashes,
		Action:     action,
	})
	m.mu.Unlock()

	if m.bulkActionFunc != nil {
		return m.bulkActionFunc(ctx, instanceID, hashes, action)
	}
	return nil
}

func (m *mockSyncManager) DeleteTorrents(ctx context.Context, instanceID int, hashes []string, deleteFiles bool) error {
	m.mu.Lock()
	m.deleteTorrentCalls = append(m.deleteTorrentCalls, deleteTorrentCall{
		InstanceID:  instanceID,
		Hashes:      hashes,
		DeleteFiles: deleteFiles,
	})
	m.mu.Unlock()

	if m.deleteTorrentsFunc != nil {
		return m.deleteTorrentsFunc(ctx, instanceID, hashes, deleteFiles)
	}
	return nil
}

func (m *mockSyncManager) GetCategories(ctx context.Context, instanceID int) (map[string]qbt.Category, error) {
	if m.getCategoriesFunc != nil {
		return m.getCategoriesFunc(ctx, instanceID)
	}
	return map[string]qbt.Category{}, nil
}

func (m *mockSyncManager) CreateCategory(ctx context.Context, instanceID int, name, path string) error {
	if m.createCategoryFunc != nil {
		return m.createCategoryFunc(ctx, instanceID, name, path)
	}
	return nil
}

func (m *mockSyncManager) HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
	if m.hasTorrentByHashFunc != nil {
		return m.hasTorrentByHashFunc(ctx, instanceID, hashes)
	}
	return nil, false, nil
}

// mockInstanceProvider implements InstanceProvider for tests
type mockInstanceProvider struct {
	instances map[int]*models.Instance
}

func newMockInstanceProvider() *mockInstanceProvider {
	return &mockInstanceProvider{
		instances: make(map[int]*models.Instance),
	}
}

func (m *mockInstanceProvider) Get(ctx context.Context, id int) (*models.Instance, error) {
	if inst, ok := m.instances[id]; ok {
		return inst, nil
	}
	return nil, ErrTransferNotFound
}

func (m *mockInstanceProvider) AddInstance(inst *models.Instance) {
	m.instances[inst.ID] = inst
}

// mockExecutor implements TransferExecutor for worker tests
type mockExecutor struct {
	canHandleResult bool
	canHandleFunc   func(source, target *models.Instance) bool
	prepareFunc     func(ctx context.Context, t *models.Transfer) (*PrepareResult, error)
	createLinksFunc func(ctx context.Context, t *models.Transfer, prep *PrepareResult) (int, error)
	addTorrentFunc  func(ctx context.Context, t *models.Transfer, prep *PrepareResult) error
	deleteSourceFunc func(ctx context.Context, t *models.Transfer) error
	rollbackFunc    func(ctx context.Context, t *models.Transfer, prep *PrepareResult) error

	// Call tracking
	mu             sync.Mutex
	prepareCalls   int
	linksCalls     int
	addCalls       int
	deleteCalls    int
	rollbackCalls  int
}

func newMockExecutor() *mockExecutor {
	return &mockExecutor{
		canHandleResult: true,
	}
}

func (m *mockExecutor) CanHandle(source, target *models.Instance) bool {
	if m.canHandleFunc != nil {
		return m.canHandleFunc(source, target)
	}
	return m.canHandleResult
}

func (m *mockExecutor) Prepare(ctx context.Context, t *models.Transfer) (*PrepareResult, error) {
	m.mu.Lock()
	m.prepareCalls++
	m.mu.Unlock()

	if m.prepareFunc != nil {
		return m.prepareFunc(ctx, t)
	}
	return newTestPrepareResult(), nil
}

func (m *mockExecutor) CreateLinks(ctx context.Context, t *models.Transfer, prep *PrepareResult) (int, error) {
	m.mu.Lock()
	m.linksCalls++
	m.mu.Unlock()

	if m.createLinksFunc != nil {
		return m.createLinksFunc(ctx, t, prep)
	}
	return 10, nil
}

func (m *mockExecutor) AddTorrent(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	m.mu.Lock()
	m.addCalls++
	m.mu.Unlock()

	if m.addTorrentFunc != nil {
		return m.addTorrentFunc(ctx, t, prep)
	}
	return nil
}

func (m *mockExecutor) DeleteSource(ctx context.Context, t *models.Transfer) error {
	m.mu.Lock()
	m.deleteCalls++
	m.mu.Unlock()

	if m.deleteSourceFunc != nil {
		return m.deleteSourceFunc(ctx, t)
	}
	return nil
}

func (m *mockExecutor) Rollback(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	m.mu.Lock()
	m.rollbackCalls++
	m.mu.Unlock()

	if m.rollbackFunc != nil {
		return m.rollbackFunc(ctx, t, prep)
	}
	return nil
}

// mockTransferStore wraps models.TransferStore behavior for tests
type mockTransferStore struct {
	mu        sync.Mutex
	transfers map[int64]*models.Transfer
	nextID    int64
}

func newMockTransferStore() *mockTransferStore {
	return &mockTransferStore{
		transfers: make(map[int64]*models.Transfer),
		nextID:    1,
	}
}

func (m *mockTransferStore) Create(ctx context.Context, t *models.Transfer) (*models.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t.ID = m.nextID
	m.nextID++
	m.transfers[t.ID] = t
	return t, nil
}

func (m *mockTransferStore) Get(ctx context.Context, id int64) (*models.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, ok := m.transfers[id]; ok {
		return t, nil
	}
	return nil, ErrTransferNotFound
}

func (m *mockTransferStore) Update(ctx context.Context, t *models.Transfer) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.transfers[t.ID] = t
	return nil
}

func (m *mockTransferStore) UpdateState(ctx context.Context, id int64, state models.TransferState, errorMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, ok := m.transfers[id]; ok {
		t.State = state
		t.Error = errorMsg
		return nil
	}
	return ErrTransferNotFound
}

func (m *mockTransferStore) ListByStates(ctx context.Context, states []models.TransferState, limit, offset int) ([]*models.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*models.Transfer
	stateSet := make(map[models.TransferState]bool)
	for _, s := range states {
		stateSet[s] = true
	}

	for _, t := range m.transfers {
		if stateSet[t.State] {
			result = append(result, t)
		}
	}
	return result, nil
}

// Test helper functions

// newTestTransfer creates a transfer with sensible defaults
func newTestTransfer(opts ...func(*models.Transfer)) *models.Transfer {
	t := &models.Transfer{
		ID:               1,
		SourceInstanceID: 1,
		TargetInstanceID: 2,
		TorrentHash:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TorrentName:      "Test Torrent",
		State:            models.TransferStatePending,
		SourceSavePath:   "/downloads/source",
		TargetSavePath:   "/downloads/target",
		LinkMode:         "hardlink",
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// newTestInstance creates an instance with sensible defaults
func newTestInstance(id int, opts ...func(*models.Instance)) *models.Instance {
	inst := &models.Instance{
		ID:                        id,
		Name:                      "Test Instance",
		Host:                      "http://localhost:8080",
		HasLocalFilesystemAccess:  true,
		UseHardlinks:              true,
	}
	for _, opt := range opts {
		opt(inst)
	}
	return inst
}

// newTestPrepareResult creates a PrepareResult with sensible defaults
func newTestPrepareResult(opts ...func(*PrepareResult)) *PrepareResult {
	prep := &PrepareResult{
		TorrentName:    "Test Torrent",
		SourceSavePath: "/downloads/source",
		TargetSavePath: "/downloads/target",
		LinkMode:       "hardlink",
		Files: []TorrentFile{
			{RelPath: "file1.mkv", AbsPath: "/downloads/source/file1.mkv", Size: 1000},
			{RelPath: "file2.mkv", AbsPath: "/downloads/source/file2.mkv", Size: 2000},
		},
		TorrentData:    []byte("mock torrent data"),
		SourceInstance: newTestInstance(1),
		TargetInstance: newTestInstance(2),
	}
	for _, opt := range opts {
		opt(prep)
	}
	return prep
}

// withState sets the transfer state
func withState(state models.TransferState) func(*models.Transfer) {
	return func(t *models.Transfer) {
		t.State = state
	}
}

// withDeleteFromSource sets the delete from source flag
func withDeleteFromSource(delete bool) func(*models.Transfer) {
	return func(t *models.Transfer) {
		t.DeleteFromSource = delete
	}
}

// withLinkMode sets the link mode for the transfer
func withLinkMode(mode string) func(*models.Transfer) {
	return func(t *models.Transfer) {
		t.LinkMode = mode
	}
}

// withLocalAccess sets the local filesystem access flag
func withLocalAccess(local bool) func(*models.Instance) {
	return func(i *models.Instance) {
		i.HasLocalFilesystemAccess = local
	}
}

// withHardlinks sets the use hardlinks flag
func withHardlinks(use bool) func(*models.Instance) {
	return func(i *models.Instance) {
		i.UseHardlinks = use
	}
}

// withHardlinkBaseDir sets the hardlink base directory
func withHardlinkBaseDir(dir string) func(*models.Instance) {
	return func(i *models.Instance) {
		i.HardlinkBaseDir = dir
	}
}
