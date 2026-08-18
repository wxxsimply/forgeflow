package repository

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"forgeflow/internal/apperror"
)

var errListLimit = errors.New("file list limit reached")

type SafeFileReader struct {
	root   string
	limits Limits
}

func NewSafeFileReader(root string, limits Limits) (*SafeFileReader, error) {
	if limits.MaxReadBytes <= 0 || limits.MaxListEntries <= 0 || limits.MaxSearchMatches <= 0 || limits.MaxSearchFiles <= 0 {
		return nil, apperror.New(apperror.CodeValidation, "repository file limits must be positive")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, apperror.Wrap(err, apperror.CodeValidation, "repository.files.root", "repository root is invalid")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, mapPathError(err, "repository root does not exist")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, mapPathError(err, "repository root is not accessible")
	}
	if !info.IsDir() {
		return nil, apperror.New(apperror.CodeValidation, "repository root must be a directory")
	}
	return &SafeFileReader{root: filepath.Clean(resolved), limits: limits}, nil
}

func (r *SafeFileReader) ListFiles(ctx context.Context, relativeDirectory string, recursive bool) (FileList, error) {
	directory, err := r.resolveExisting(relativeDirectory)
	if err != nil {
		return FileList{}, err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return FileList{}, mapPathError(err, "directory is not accessible")
	}
	if !info.IsDir() {
		return FileList{}, apperror.New(apperror.CodeValidation, "list path must be a directory")
	}

	result := FileList{Entries: []FileEntry{}}
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == directory {
			return nil
		}
		if entry.IsDir() && ignoredDirectory(entry.Name()) {
			return filepath.SkipDir
		}
		if !recursive && filepath.Dir(path) != directory {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(result.Entries) >= r.limits.MaxListEntries {
			result.Truncated = true
			return errListLimit
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(r.root, path)
		if err != nil {
			return err
		}
		result.Entries = append(result.Entries, FileEntry{
			Path: filepath.ToSlash(relative), Size: fileInfo.Size(), IsDir: entry.IsDir(),
			IsSymlink: entry.Type()&os.ModeSymlink != 0,
		})
		return nil
	})
	if err != nil && !errors.Is(err, errListLimit) {
		return FileList{}, apperror.Wrap(err, apperror.CodeInternal, "repository.files.list", "could not list repository files")
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Path < result.Entries[j].Path })
	return result, nil
}

func (r *SafeFileReader) ReadFile(ctx context.Context, relativePath string) (FileContent, error) {
	path, err := r.resolveExisting(relativePath)
	if err != nil {
		return FileContent{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return FileContent{}, mapPathError(err, "file is not accessible")
	}
	if !info.Mode().IsRegular() {
		return FileContent{}, apperror.New(apperror.CodeValidation, "read path must be a regular file")
	}
	if info.Size() > r.limits.MaxReadBytes {
		return FileContent{}, apperror.New(apperror.CodePolicyDenied, "file exceeds the configured read limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return FileContent{}, mapPathError(err, "file cannot be opened")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, r.limits.MaxReadBytes+1))
	if err != nil {
		return FileContent{}, apperror.Wrap(err, apperror.CodeInternal, "repository.files.read", "file could not be read")
	}
	if int64(len(data)) > r.limits.MaxReadBytes {
		return FileContent{}, apperror.New(apperror.CodePolicyDenied, "file grew beyond the configured read limit")
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return FileContent{}, apperror.New(apperror.CodePolicyDenied, "binary or non-UTF-8 files cannot be read as text")
	}
	if err := ctx.Err(); err != nil {
		return FileContent{}, err
	}
	digest := sha256.Sum256(data)
	relative, _ := filepath.Rel(r.root, path)
	return FileContent{
		Path: filepath.ToSlash(relative), Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), Text: string(data),
	}, nil
}

func (r *SafeFileReader) SearchCode(ctx context.Context, query string, options SearchOptions) ([]SearchMatch, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 256 {
		return nil, apperror.New(apperror.CodeValidation, "search query must contain 1 to 256 characters")
	}
	listed, err := r.ListFiles(ctx, ".", true)
	if err != nil {
		return nil, err
	}
	matches := make([]SearchMatch, 0)
	searchedFiles := 0
	needle := query
	if !options.CaseSensitive {
		needle = strings.ToLower(query)
	}
	for _, entry := range listed.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir || entry.IsSymlink || !extensionAllowed(entry.Path, options.Extensions) {
			continue
		}
		if searchedFiles >= r.limits.MaxSearchFiles {
			break
		}
		searchedFiles++
		content, err := r.ReadFile(ctx, entry.Path)
		if err != nil {
			if apperror.IsCode(err, apperror.CodePolicyDenied) {
				continue
			}
			return nil, err
		}
		scanner := bufio.NewScanner(strings.NewReader(content.Text))
		scanner.Buffer(make([]byte, 64*1024), int(r.limits.MaxReadBytes))
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := scanner.Text()
			haystack := line
			if !options.CaseSensitive {
				haystack = strings.ToLower(line)
			}
			if strings.Contains(haystack, needle) {
				matches = append(matches, SearchMatch{Path: entry.Path, Line: lineNumber, Preview: truncateLine(line, 300)})
				if len(matches) >= r.limits.MaxSearchMatches {
					return matches, nil
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, apperror.Wrap(err, apperror.CodeInternal, "repository.files.search", "repository file could not be searched")
		}
	}
	return matches, nil
}

func (r *SafeFileReader) resolveExisting(relativePath string) (string, error) {
	if strings.ContainsRune(relativePath, 0) || filepath.IsAbs(relativePath) {
		return "", apperror.New(apperror.CodePolicyDenied, "absolute or invalid paths are not allowed")
	}
	cleaned := filepath.Clean(relativePath)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", apperror.New(apperror.CodePolicyDenied, "path traversal outside the repository is not allowed")
	}
	candidate := filepath.Join(r.root, cleaned)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", mapPathError(err, "repository path does not exist")
	}
	if !withinRoot(r.root, resolved) {
		return "", apperror.New(apperror.CodePolicyDenied, "symbolic link escapes the repository root")
	}
	return filepath.Clean(resolved), nil
}

func withinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func mapPathError(err error, message string) error {
	if errors.Is(err, os.ErrNotExist) {
		return apperror.Wrap(err, apperror.CodeNotFound, "repository.path", message)
	}
	if errors.Is(err, os.ErrPermission) {
		return apperror.Wrap(err, apperror.CodeForbidden, "repository.path", message)
	}
	return apperror.Wrap(err, apperror.CodeInternal, "repository.path", message)
}

func ignoredDirectory(name string) bool {
	switch name {
	case ".git", ".forgeflow", ".cache", "node_modules", "vendor", "dist", "bin":
		return true
	default:
		return false
	}
}

func extensionAllowed(path string, extensions []string) bool {
	if len(extensions) == 0 {
		return true
	}
	extension := strings.ToLower(filepath.Ext(path))
	for _, allowed := range extensions {
		if extension == strings.ToLower(allowed) {
			return true
		}
	}
	return false
}

func truncateLine(line string, limit int) string {
	line = strings.TrimSpace(line)
	if len(line) <= limit {
		return line
	}
	return line[:limit] + "…"
}

var _ FileReader = (*SafeFileReader)(nil)
