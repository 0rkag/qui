// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

var (
	ErrPathMappingNotFound   = errors.New("path mapping not found")
	ErrDuplicateInstancePath = errors.New("instance path already exists for this instance")
)

// InstancePathMapping represents a path mapping between an instance's view
// and the QUI server's canonical view of the filesystem.
type InstancePathMapping struct {
	ID            int64     `json:"id"`
	InstanceID    int       `json:"instanceId"`
	InstancePath  string    `json:"instancePath"`  // Path as seen by the qBittorrent instance
	CanonicalPath string    `json:"canonicalPath"` // Path as seen by QUI server
	Enabled       bool      `json:"enabled"`
	Description   string    `json:"description,omitempty"`
	SortOrder     int       `json:"sortOrder"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Validate checks that the path mapping has valid data.
func (m *InstancePathMapping) Validate() error {
	if m.InstanceID <= 0 {
		return errors.New("instance ID is required")
	}
	if strings.TrimSpace(m.InstancePath) == "" {
		return errors.New("instance path is required")
	}
	if strings.TrimSpace(m.CanonicalPath) == "" {
		return errors.New("canonical path is required")
	}
	// Normalize paths (remove trailing slashes for consistency)
	m.InstancePath = normalizePath(m.InstancePath)
	m.CanonicalPath = normalizePath(m.CanonicalPath)
	return nil
}

// normalizePath cleans a path and removes trailing slashes.
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	p = filepath.Clean(p)
	// Remove trailing slash unless it's the root
	if len(p) > 1 && (strings.HasSuffix(p, "/") || strings.HasSuffix(p, "\\")) {
		p = p[:len(p)-1]
	}
	return p
}

// InstancePathMappingStore handles database operations for instance path mappings.
type InstancePathMappingStore struct {
	db dbinterface.Querier
}

// NewInstancePathMappingStore creates a new InstancePathMappingStore.
func NewInstancePathMappingStore(db dbinterface.Querier) *InstancePathMappingStore {
	return &InstancePathMappingStore{db: db}
}

// Create inserts a new path mapping.
func (s *InstancePathMappingStore) Create(ctx context.Context, m *InstancePathMapping) (*InstancePathMapping, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO instance_path_mappings (
			instance_id, instance_path, canonical_path, enabled, description, sort_order, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.InstanceID, m.InstancePath, m.CanonicalPath, m.Enabled, m.Description, m.SortOrder, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrDuplicateInstancePath
		}
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	m.ID = id
	m.CreatedAt = now
	m.UpdatedAt = now
	return m, nil
}

// Get retrieves a path mapping by ID.
func (s *InstancePathMappingStore) Get(ctx context.Context, id int64) (*InstancePathMapping, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, instance_path, canonical_path, enabled, description, sort_order, created_at, updated_at
		FROM instance_path_mappings
		WHERE id = ?`, id)

	return s.scanMapping(row)
}

// Update modifies an existing path mapping.
func (s *InstancePathMappingStore) Update(ctx context.Context, m *InstancePathMapping) error {
	if err := m.Validate(); err != nil {
		return err
	}

	m.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE instance_path_mappings
		SET instance_path = ?, canonical_path = ?, enabled = ?, description = ?, sort_order = ?, updated_at = ?
		WHERE id = ?`,
		m.InstancePath, m.CanonicalPath, m.Enabled, m.Description, m.SortOrder, m.UpdatedAt, m.ID,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrDuplicateInstancePath
		}
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrPathMappingNotFound
	}
	return nil
}

// Delete removes a path mapping by ID.
func (s *InstancePathMappingStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM instance_path_mappings WHERE id = ?`, id)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrPathMappingNotFound
	}
	return nil
}

// ListByInstance retrieves all path mappings for an instance.
func (s *InstancePathMappingStore) ListByInstance(ctx context.Context, instanceID int) ([]*InstancePathMapping, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, instance_path, canonical_path, enabled, description, sort_order, created_at, updated_at
		FROM instance_path_mappings
		WHERE instance_id = ?
		ORDER BY sort_order, id`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanMappings(rows)
}

// ListEnabledByInstance retrieves all enabled path mappings for an instance.
func (s *InstancePathMappingStore) ListEnabledByInstance(ctx context.Context, instanceID int) ([]*InstancePathMapping, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, instance_path, canonical_path, enabled, description, sort_order, created_at, updated_at
		FROM instance_path_mappings
		WHERE instance_id = ? AND enabled = 1
		ORDER BY sort_order, id`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanMappings(rows)
}

// DeleteByInstance removes all path mappings for an instance.
func (s *InstancePathMappingStore) DeleteByInstance(ctx context.Context, instanceID int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM instance_path_mappings WHERE instance_id = ?`, instanceID)
	return err
}

// UpdateSortOrder updates the sort order for multiple mappings.
func (s *InstancePathMappingStore) UpdateSortOrder(ctx context.Context, orders map[int64]int) error {
	for id, order := range orders {
		_, err := s.db.ExecContext(ctx, `
			UPDATE instance_path_mappings SET sort_order = ?, updated_at = ? WHERE id = ?`,
			order, time.Now().UTC(), id)
		if err != nil {
			return err
		}
	}
	return nil
}

// scanMapping scans a single row into an InstancePathMapping.
func (s *InstancePathMappingStore) scanMapping(row *sql.Row) (*InstancePathMapping, error) {
	var m InstancePathMapping
	var description sql.NullString

	err := row.Scan(
		&m.ID, &m.InstanceID, &m.InstancePath, &m.CanonicalPath,
		&m.Enabled, &description, &m.SortOrder, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPathMappingNotFound
		}
		return nil, err
	}

	if description.Valid {
		m.Description = description.String
	}
	return &m, nil
}

// scanMappings scans multiple rows into InstancePathMapping slice.
func (s *InstancePathMappingStore) scanMappings(rows *sql.Rows) ([]*InstancePathMapping, error) {
	var mappings []*InstancePathMapping
	for rows.Next() {
		var m InstancePathMapping
		var description sql.NullString

		err := rows.Scan(
			&m.ID, &m.InstanceID, &m.InstancePath, &m.CanonicalPath,
			&m.Enabled, &description, &m.SortOrder, &m.CreatedAt, &m.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if description.Valid {
			m.Description = description.String
		}
		mappings = append(mappings, &m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return mappings, nil
}

// PathResolver provides methods to translate paths between instance and canonical forms.
type PathResolver struct {
	store *InstancePathMappingStore
}

// NewPathResolver creates a new PathResolver.
func NewPathResolver(store *InstancePathMappingStore) *PathResolver {
	return &PathResolver{store: store}
}

// ToCanonicalPath converts an instance path to the canonical (QUI server) path.
// Uses longest prefix matching.
func (r *PathResolver) ToCanonicalPath(ctx context.Context, instanceID int, instancePath string) (string, error) {
	mappings, err := r.store.ListEnabledByInstance(ctx, instanceID)
	if err != nil {
		return "", err
	}

	return toCanonicalPath(instancePath, mappings)
}

// FromCanonicalPath converts a canonical (QUI server) path to an instance path.
// Uses longest prefix matching.
func (r *PathResolver) FromCanonicalPath(ctx context.Context, instanceID int, canonicalPath string) (string, error) {
	mappings, err := r.store.ListEnabledByInstance(ctx, instanceID)
	if err != nil {
		return "", err
	}

	return fromCanonicalPath(canonicalPath, mappings)
}

// ResolveTargetPath resolves the target path for a transfer from source to target instance.
// If transferMappings is provided, it's used directly (legacy per-transfer override).
// Otherwise, uses instance mappings via canonical path translation.
func (r *PathResolver) ResolveTargetPath(
	ctx context.Context,
	sourcePath string,
	sourceInstanceID int,
	targetInstanceID int,
	transferMappings map[string]string,
) (string, error) {
	// If per-transfer mappings provided, use them directly (existing behavior)
	if len(transferMappings) > 0 {
		return ApplyDirectMappings(sourcePath, transferMappings), nil
	}

	// Two-step translation via canonical path
	// Step 1: Source instance path → Canonical path
	canonicalPath, err := r.ToCanonicalPath(ctx, sourceInstanceID, sourcePath)
	if err != nil {
		return "", err
	}

	// Step 2: Canonical path → Target instance path
	targetPath, err := r.FromCanonicalPath(ctx, targetInstanceID, canonicalPath)
	if err != nil {
		return "", err
	}

	return targetPath, nil
}

// toCanonicalPath converts an instance path to canonical path using longest prefix match.
func toCanonicalPath(instancePath string, mappings []*InstancePathMapping) (string, error) {
	instancePath = normalizePath(instancePath)

	var bestMatch *InstancePathMapping
	var bestLen int

	for _, m := range mappings {
		if matchesPrefix(instancePath, m.InstancePath) {
			if len(m.InstancePath) > bestLen {
				bestMatch = m
				bestLen = len(m.InstancePath)
			}
		}
	}

	if bestMatch == nil {
		// No mapping found - return path as-is (assumes same path on both sides)
		return instancePath, nil
	}

	// Replace the matched prefix
	return bestMatch.CanonicalPath + instancePath[len(bestMatch.InstancePath):], nil
}

// fromCanonicalPath converts a canonical path to instance path using longest prefix match.
func fromCanonicalPath(canonicalPath string, mappings []*InstancePathMapping) (string, error) {
	canonicalPath = normalizePath(canonicalPath)

	var bestMatch *InstancePathMapping
	var bestLen int

	for _, m := range mappings {
		if matchesPrefix(canonicalPath, m.CanonicalPath) {
			if len(m.CanonicalPath) > bestLen {
				bestMatch = m
				bestLen = len(m.CanonicalPath)
			}
		}
	}

	if bestMatch == nil {
		// No mapping found - return path as-is (assumes same path on both sides)
		return canonicalPath, nil
	}

	// Replace the matched prefix
	return bestMatch.InstancePath + canonicalPath[len(bestMatch.CanonicalPath):], nil
}

// matchesPrefix checks if path starts with prefix, ensuring we match on path boundaries.
func matchesPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	// Ensure we're matching on a path boundary (not partial directory name)
	nextChar := path[len(prefix)]
	return nextChar == '/' || nextChar == '\\'
}

// ApplyDirectMappings applies direct path mappings using longest prefix match.
// This is used for per-transfer path overrides.
func ApplyDirectMappings(sourcePath string, mappings map[string]string) string {
	sourcePath = normalizePath(sourcePath)

	var bestMatch string
	var bestReplacement string

	for oldPrefix, newPrefix := range mappings {
		oldPrefix = normalizePath(oldPrefix)
		if matchesPrefix(sourcePath, oldPrefix) {
			if len(oldPrefix) > len(bestMatch) {
				bestMatch = oldPrefix
				bestReplacement = normalizePath(newPrefix)
			}
		}
	}

	if bestMatch == "" {
		return sourcePath
	}

	return bestReplacement + sourcePath[len(bestMatch):]
}
