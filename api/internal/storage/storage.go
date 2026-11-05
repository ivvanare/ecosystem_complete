// Package storage keeps uploaded files behind a narrow interface, so the local volume used
// today can be replaced by S3 without touching the domain (the decision recorded for photos).
package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Storage stores bytes and returns the URL a client can fetch them from.
type Storage interface {
	// Save writes the object under key (a relative path) and returns its public URL.
	Save(ctx context.Context, key string, body io.Reader) (string, error)
	// Delete removes the object. A missing object is not an error: deletion is idempotent.
	Delete(ctx context.Context, key string) error
}

// Local writes to a directory and serves it from urlPrefix.
//
// It is the development choice: zero dependencies and honest bytes on disk. The interface is the
// part that matters — swapping in S3 means one more implementation, not a change to the domain.
type Local struct {
	root      string
	urlPrefix string
}

// NewLocal wires a directory-backed store.
func NewLocal(root, urlPrefix string) (*Local, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("storage: a root directory is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("storage: creating the media root: %w", err)
	}
	return &Local{root: root, urlPrefix: strings.TrimRight(urlPrefix, "/")}, nil
}

// Save writes the object, creating its directory.
func (l *Local) Save(_ context.Context, key string, body io.Reader) (string, error) {
	clean := filepath.Clean(key)
	// Containment: a key is a path we build, and it must never escape the root even if a caller
	// passes something creative.
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("storage: refusing an unsafe key %q", key)
	}
	path := filepath.Join(l.root, clean)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("storage: creating the object directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("storage: creating the object: %w", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := io.Copy(file, body); err != nil {
		return "", fmt.Errorf("storage: writing the object: %w", err)
	}
	return l.urlPrefix + "/" + filepath.ToSlash(clean), nil
}

// Delete removes the object; a missing one is already what the caller wanted.
func (l *Local) Delete(_ context.Context, key string) error {
	clean := filepath.Clean(key)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return fmt.Errorf("storage: refusing an unsafe key %q", key)
	}
	if err := os.Remove(filepath.Join(l.root, clean)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: removing the object: %w", err)
	}
	return nil
}

// Root is the directory to serve, for the HTTP layer that publishes the files.
func (l *Local) Root() string { return l.root }
