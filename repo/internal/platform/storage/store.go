package storage

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// allowedMIMEMagic maps MIME type to expected magic byte prefixes.
var allowedMIMEMagic = map[string][]byte{
	"image/jpeg":      {0xFF, 0xD8, 0xFF},
	"image/png":       {0x89, 0x50, 0x4E, 0x47},
	"application/pdf": {0x25, 0x50, 0x44, 0x46}, // %PDF
}

var mimeToExt = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"application/pdf": ".pdf",
}

// StoredFile holds metadata about a file written to disk.
type StoredFile struct {
	ID          string // UUID, used as the filename stem
	Path        string // absolute path on disk
	Checksum    string // SHA-256 hex
	SizeBytes   int64
	ContentType string
}

// Store manages file persistence under a base directory.
type Store struct {
	baseDir string
}

// NewStore creates a Store backed by baseDir, creating it if necessary.
func NewStore(baseDir string) (*Store, error) {
	if err := os.MkdirAll(baseDir, 0750); err != nil {
		return nil, fmt.Errorf("storage: mkdir %s: %w", baseDir, err)
	}
	return &Store{baseDir: baseDir}, nil
}

// Save decodes base64 data, validates magic bytes against declaredContentType,
// writes to disk under baseDir/subdir, and returns the stored file metadata.
// allowedTypes limits which MIME types are accepted.
func (s *Store) Save(subdir, filename, declaredContentType, base64Data string, allowedTypes []string) (*StoredFile, error) {
	// Decode base64 — try standard then URL-safe
	raw, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(base64Data)
		if err != nil {
			return nil, errors.New("storage: invalid base64 encoding")
		}
	}

	// Validate declared content type is in the allowed list
	allowed := false
	for _, t := range allowedTypes {
		if t == declaredContentType {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("storage: content type %q not allowed", declaredContentType)
	}

	// Validate magic bytes before touching the filesystem
	magic, ok := allowedMIMEMagic[declaredContentType]
	if !ok {
		return nil, fmt.Errorf("storage: unrecognised content type %q", declaredContentType)
	}
	if len(raw) < len(magic) {
		return nil, errors.New("storage: file too small to validate")
	}
	for i, b := range magic {
		if raw[i] != b {
			return nil, fmt.Errorf("storage: file content does not match declared type %q", declaredContentType)
		}
	}

	// Compute SHA-256 checksum
	h := sha256.Sum256(raw)
	checksum := hex.EncodeToString(h[:])

	// Determine extension from content type; fall back to declared filename extension
	ext, ok := mimeToExt[declaredContentType]
	if !ok {
		ext = filepath.Ext(filename)
		if ext == "" {
			ext = ".bin"
		}
	}

	// Generate a unique path
	id := uuid.New().String()
	dir := filepath.Join(s.baseDir, subdir)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("storage: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, id+ext)

	if err := os.WriteFile(path, raw, 0640); err != nil {
		return nil, fmt.Errorf("storage: write file: %w", err)
	}

	return &StoredFile{
		ID:          id,
		Path:        path,
		Checksum:    checksum,
		SizeBytes:   int64(len(raw)),
		ContentType: declaredContentType,
	}, nil
}

// Delete removes a stored file. Path traversal outside baseDir is rejected.
// A missing file is treated as a no-op (returns nil) so callers can safely use
// it in cleanup paths without having to distinguish "never written" from "already gone".
func (s *Store) Delete(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	base, err := filepath.Abs(s.baseDir)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return errors.New("storage: path outside base directory")
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: delete %s: %w", abs, err)
	}
	return nil
}

// Open opens a stored file for reading. Caller must close the returned ReadCloser.
// Path traversal outside baseDir is rejected.
func (s *Store) Open(path string) (io.ReadCloser, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	base, err := filepath.Abs(s.baseDir)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return nil, errors.New("storage: path outside base directory")
	}
	return os.Open(abs)
}
