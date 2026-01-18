// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ftpclient

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/jlaffaye/ftp"

	"github.com/autobrr/qui/pkg/pathutil"
)

// validatePath checks for path traversal attempts and forbidden characters.
// FTP supports both absolute and relative paths.
func validatePath(p string) error {
	// Use shared path validation with relative paths allowed
	if err := pathutil.ValidateRelative(p); err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	return nil
}

// Stat returns file info for a remote path.
func (c *Client) Stat(remotePath string) (*ftp.Entry, error) {
	if err := validatePath(remotePath); err != nil {
		return nil, err
	}

	// FTP doesn't have a direct STAT for files, so we list the parent and find the entry
	dir := path.Dir(remotePath)
	name := path.Base(remotePath)

	entries, err := c.conn.List(dir)
	if err != nil {
		return nil, fmt.Errorf("list directory: %w", err)
	}

	for _, entry := range entries {
		if entry.Name == name {
			return entry, nil
		}
	}

	return nil, os.ErrNotExist
}

// Exists checks if a remote path exists.
func (c *Client) Exists(remotePath string) (bool, error) {
	_, err := c.Stat(remotePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsDir checks if a remote path is a directory.
func (c *Client) IsDir(remotePath string) (bool, error) {
	entry, err := c.Stat(remotePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return entry.Type == ftp.EntryTypeFolder, nil
}

// FileSize returns the size of a remote file.
func (c *Client) FileSize(remotePath string) (int64, error) {
	size, err := c.conn.FileSize(remotePath)
	if err != nil {
		return 0, fmt.Errorf("get file size: %w", err)
	}
	return size, nil
}

// MkdirAll creates a directory and all parent directories on the remote host.
func (c *Client) MkdirAll(remotePath string) error {
	if err := validatePath(remotePath); err != nil {
		return err
	}

	// Normalize path
	remotePath = path.Clean(remotePath)
	if remotePath == "." || remotePath == "/" {
		return nil
	}

	// Split into components and create each level
	parts := strings.Split(remotePath, "/")
	current := ""

	for _, part := range parts {
		if part == "" {
			current = "/"
			continue
		}

		if current == "" || current == "/" {
			current = current + part
		} else {
			current = current + "/" + part
		}

		// Try to create, ignore "already exists" errors
		err := c.conn.MakeDir(current)
		if err != nil {
			// Check if it already exists
			isDir, checkErr := c.IsDir(current)
			if checkErr != nil || !isDir {
				return fmt.Errorf("mkdir %s: %w", current, err)
			}
			// Directory exists, continue
		}
	}

	return nil
}

// Remove removes a file on the remote host.
func (c *Client) Remove(remotePath string) error {
	if err := validatePath(remotePath); err != nil {
		return err
	}
	return c.conn.Delete(remotePath)
}

// RemoveDir removes an empty directory on the remote host.
func (c *Client) RemoveDir(remotePath string) error {
	if err := validatePath(remotePath); err != nil {
		return err
	}
	return c.conn.RemoveDir(remotePath)
}

// RemoveAll removes a file or directory recursively on the remote host.
func (c *Client) RemoveAll(remotePath string) error {
	if err := validatePath(remotePath); err != nil {
		return err
	}

	entry, err := c.Stat(remotePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if entry.Type != ftp.EntryTypeFolder {
		return c.conn.Delete(remotePath)
	}

	// List and remove all contents
	entries, err := c.conn.List(remotePath)
	if err != nil {
		return fmt.Errorf("list directory: %w", err)
	}

	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		childPath := path.Join(remotePath, e.Name)
		if err := c.RemoveAll(childPath); err != nil {
			return err
		}
	}

	// Remove the now-empty directory
	return c.conn.RemoveDir(remotePath)
}

// List returns the contents of a remote directory.
func (c *Client) List(remotePath string) ([]*ftp.Entry, error) {
	return c.conn.List(remotePath)
}

// Rename renames a remote file or directory.
func (c *Client) Rename(oldPath, newPath string) error {
	return c.conn.Rename(oldPath, newPath)
}

// CurrentDir returns the current working directory.
func (c *Client) CurrentDir() (string, error) {
	return c.conn.CurrentDir()
}

// ChangeDir changes the current working directory.
func (c *Client) ChangeDir(remotePath string) error {
	return c.conn.ChangeDir(remotePath)
}

// Walk walks a remote directory tree, calling walkFn for each file or directory.
func (c *Client) Walk(root string, walkFn func(path string, entry *ftp.Entry, err error) error) error {
	return c.walk(root, walkFn)
}

func (c *Client) walk(dir string, walkFn func(path string, entry *ftp.Entry, err error) error) error {
	entries, err := c.conn.List(dir)
	if err != nil {
		return walkFn(dir, nil, err)
	}

	for _, entry := range entries {
		if entry.Name == "." || entry.Name == ".." {
			continue
		}

		entryPath := path.Join(dir, entry.Name)

		if err := walkFn(entryPath, entry, nil); err != nil {
			return err
		}

		if entry.Type == ftp.EntryTypeFolder {
			if err := c.walk(entryPath, walkFn); err != nil {
				return err
			}
		}
	}

	return nil
}
