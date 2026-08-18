package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"forgeflow/internal/domain"
)

type Kind string

const (
	KindPatch        Kind = "patch"
	KindTestEvidence Kind = "test_evidence"
	KindRunReport    Kind = "run_report"
	KindLog          Kind = "log"
)

type Meta struct {
	ID          string            `json:"id"`
	RunID       string            `json:"runId"`
	Kind        Kind              `json:"kind"`
	StorageKey  string            `json:"storageKey"`
	SHA256      string            `json:"sha256"`
	Size        int64             `json:"size"`
	ContentType string            `json:"contentType"`
	Attributes  map[string]string `json:"attributes"`
	CreatedAt   time.Time         `json:"createdAt"`
}

type PutRequest struct {
	RunID       string
	Kind        Kind
	ContentType string
	Attributes  map[string]string
}

type MetadataRepository interface {
	Insert(context.Context, Meta) error
	Get(context.Context, string) (Meta, error)
	List(context.Context, string) ([]Meta, error)
}

type Store interface {
	Put(context.Context, PutRequest, io.Reader) (Meta, error)
	Open(context.Context, string) (io.ReadCloser, Meta, error)
}

type FileStore struct {
	root     string
	metadata MetadataRepository
	maxBytes int64
}

func NewFileStore(root string, metadata MetadataRepository, maxBytes int64) (*FileStore, error) {
	if strings.TrimSpace(root) == "" || metadata == nil {
		return nil, fmt.Errorf("artifact root and metadata repository are required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return nil, fmt.Errorf("create artifact root: %w", err)
	}
	if maxBytes <= 0 {
		maxBytes = 64 * 1024 * 1024
	}
	return &FileStore{root: filepath.Clean(absolute), metadata: metadata, maxBytes: maxBytes}, nil
}

func (s *FileStore) Put(ctx context.Context, request PutRequest, body io.Reader) (Meta, error) {
	if body == nil || strings.TrimSpace(request.RunID) == "" || !validKind(request.Kind) {
		return Meta{}, fmt.Errorf("artifact run, kind, and body are required")
	}
	if request.ContentType == "" || len(request.ContentType) > 255 || len(request.Attributes) > 64 {
		return Meta{}, fmt.Errorf("artifact content type or attributes are invalid")
	}
	for key, value := range request.Attributes {
		if strings.TrimSpace(key) == "" || len(key) > 128 || len(value) > 2_000 {
			return Meta{}, fmt.Errorf("artifact attribute is invalid")
		}
	}
	id := domain.NewID()
	temporary, err := os.CreateTemp(s.root, ".artifact-*.tmp")
	if err != nil {
		return Meta{}, fmt.Errorf("create artifact temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(body, s.maxBytes+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		return Meta{}, fmt.Errorf("write artifact: %w", copyErr)
	}
	if closeErr != nil {
		return Meta{}, fmt.Errorf("close artifact: %w", closeErr)
	}
	if written > s.maxBytes {
		return Meta{}, fmt.Errorf("artifact exceeds byte limit")
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	storageKey := filepath.ToSlash(filepath.Join(request.RunID, id, digest))
	target, err := s.resolveStorageKey(storageKey)
	if err != nil {
		return Meta{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return Meta{}, fmt.Errorf("create artifact directory: %w", err)
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil || !withinRoot(s.root, resolvedParent) {
		return Meta{}, fmt.Errorf("artifact directory escapes storage root")
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return Meta{}, fmt.Errorf("commit artifact body: %w", err)
	}
	meta := Meta{
		ID: id, RunID: request.RunID, Kind: request.Kind, StorageKey: storageKey,
		SHA256: digest, Size: written, ContentType: request.ContentType,
		Attributes: cloneAttributes(request.Attributes), CreatedAt: time.Now().UTC(),
	}
	if err := s.metadata.Insert(ctx, meta); err != nil {
		_ = os.Remove(target)
		return Meta{}, fmt.Errorf("insert artifact metadata: %w", err)
	}
	return meta, nil
}

func (s *FileStore) Open(ctx context.Context, artifactID string) (io.ReadCloser, Meta, error) {
	meta, err := s.metadata.Get(ctx, artifactID)
	if err != nil {
		return nil, Meta{}, err
	}
	path, err := s.resolveStorageKey(meta.StorageKey)
	if err != nil {
		return nil, Meta{}, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != meta.Size {
		return nil, Meta{}, fmt.Errorf("artifact body is missing or does not match metadata")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !withinRoot(s.root, resolved) {
		return nil, Meta{}, fmt.Errorf("artifact body escapes storage root")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, Meta{}, fmt.Errorf("open artifact body: %w", err)
	}
	return &verifyingReadCloser{file: file, digest: sha256.New(), expected: meta.SHA256}, meta, nil
}

type verifyingReadCloser struct {
	file     *os.File
	digest   hash.Hash
	expected string
	verified bool
}

func (r *verifyingReadCloser) Read(buffer []byte) (int, error) {
	n, err := r.file.Read(buffer)
	if n > 0 {
		_, _ = r.digest.Write(buffer[:n])
	}
	if err == io.EOF && !r.verified {
		r.verified = true
		actual := hex.EncodeToString(r.digest.Sum(nil))
		if actual != r.expected {
			return n, fmt.Errorf("artifact checksum mismatch")
		}
	}
	return n, err
}

func (r *verifyingReadCloser) Close() error { return r.file.Close() }

func withinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func (s *FileStore) resolveStorageKey(key string) (string, error) {
	if key == "" || filepath.IsAbs(key) || strings.ContainsRune(key, 0) {
		return "", fmt.Errorf("artifact storage key is invalid")
	}
	cleaned := filepath.Clean(filepath.FromSlash(key))
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact storage key escapes root")
	}
	target := filepath.Join(s.root, cleaned)
	relative, err := filepath.Rel(s.root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact storage key escapes root")
	}
	return target, nil
}

func validKind(kind Kind) bool {
	return kind == KindPatch || kind == KindTestEvidence || kind == KindRunReport || kind == KindLog
}

func cloneAttributes(attributes map[string]string) map[string]string {
	if attributes == nil {
		return map[string]string{}
	}
	clone := make(map[string]string, len(attributes))
	for key, value := range attributes {
		clone[key] = value
	}
	return clone
}

func encodeAttributes(attributes map[string]string) []byte {
	encoded, _ := json.Marshal(cloneAttributes(attributes))
	return encoded
}
