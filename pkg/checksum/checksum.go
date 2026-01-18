// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package checksum provides file integrity verification utilities.
package checksum

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
)

// Algorithm represents a checksum algorithm type.
type Algorithm string

const (
	// AlgorithmMD5 uses MD5 hashing (faster, less secure).
	AlgorithmMD5 Algorithm = "md5"
	// AlgorithmSHA256 uses SHA-256 hashing (slower, more secure).
	AlgorithmSHA256 Algorithm = "sha256"
)

// ErrUnsupportedAlgorithm is returned when an unknown algorithm is specified.
var ErrUnsupportedAlgorithm = fmt.Errorf("unsupported checksum algorithm")

// newHash creates a new hash.Hash for the given algorithm.
func newHash(alg Algorithm) (hash.Hash, error) {
	switch alg {
	case AlgorithmMD5:
		return md5.New(), nil
	case AlgorithmSHA256:
		return sha256.New(), nil
	default:
		return nil, ErrUnsupportedAlgorithm
	}
}

// FileChecksum calculates the checksum of a local file.
func FileChecksum(path string, alg Algorithm) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	return ReaderChecksum(f, alg)
}

// ReaderChecksum calculates the checksum of data from an io.Reader.
func ReaderChecksum(r io.Reader, alg Algorithm) (string, error) {
	h, err := newHash(alg)
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("read data: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// CompareFiles checks if two local files have identical content using checksums.
func CompareFiles(path1, path2 string, alg Algorithm) (bool, error) {
	sum1, err := FileChecksum(path1, alg)
	if err != nil {
		return false, fmt.Errorf("checksum file1: %w", err)
	}

	sum2, err := FileChecksum(path2, alg)
	if err != nil {
		return false, fmt.Errorf("checksum file2: %w", err)
	}

	return sum1 == sum2, nil
}

// FileSizeMatch checks if two files have the same size (quick pre-check before checksumming).
func FileSizeMatch(path1, path2 string) (bool, error) {
	info1, err := os.Stat(path1)
	if err != nil {
		return false, err
	}

	info2, err := os.Stat(path2)
	if err != nil {
		return false, err
	}

	return info1.Size() == info2.Size(), nil
}
