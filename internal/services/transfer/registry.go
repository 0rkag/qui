// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"github.com/autobrr/qui/internal/models"
)

// ExecutorRegistry manages available executors and selects the appropriate one
// based on instance configuration. Executors are checked in order, and the first
// one that can handle the transfer is used.
type ExecutorRegistry struct {
	executors []TransferExecutor
}

// NewExecutorRegistry creates a registry with the default set of executors.
// LocalExecutor is checked first (most efficient for local transfers),
// then SSHExecutor for remote transfers.
func NewExecutorRegistry(local *LocalExecutor, ssh *SSHExecutor) *ExecutorRegistry {
	executors := []TransferExecutor{local}
	if ssh != nil {
		executors = append(executors, ssh)
	}
	return &ExecutorRegistry{
		executors: executors,
	}
}

// SelectExecutor finds an executor that can handle the transfer between
// the given source and target instances.
func (r *ExecutorRegistry) SelectExecutor(source, target *models.Instance) (TransferExecutor, error) {
	for _, exec := range r.executors {
		if exec.CanHandle(source, target) {
			return exec, nil
		}
	}
	return nil, ErrNoExecutorAvailable
}

// RegisterExecutor adds a new executor to the registry.
// Executors are checked in the order they were registered.
func (r *ExecutorRegistry) RegisterExecutor(exec TransferExecutor) {
	r.executors = append(r.executors, exec)
}
