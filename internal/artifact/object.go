package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"forgeflow/internal/config"
	"forgeflow/internal/domain"
)

type S3Options struct {
	Bucket       string
	Region       string
	Endpoint     string
	Prefix       string
	SpoolDir     string
	SSE          string
	KMSKeyID     string
	UsePathStyle bool
	MaxBytes     int64
}

type s3API interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
}

type S3Store struct {
	client   s3API
	metadata MetadataRepository
	options  S3Options
	prefix   string
	sse      types.ServerSideEncryption
}

type MigrationResult struct {
	Migrated        int      `json:"migrated"`
	AlreadyMigrated int      `json:"alreadyMigrated"`
	StorageKeys     []string `json:"storageKeys"`
}

func NewConfiguredStore(ctx context.Context, configuration config.Config, metadata MetadataRepository) (Store, error) {
	switch configuration.ArtifactBackend {
	case "file":
		return NewFileStore(configuration.ArtifactRoot, metadata, int64(configuration.ArtifactMaxBytes))
	case "s3":
		return NewS3Store(ctx, metadata, S3Options{
			Bucket: configuration.ArtifactS3Bucket, Region: configuration.ArtifactS3Region,
			Endpoint: configuration.ArtifactS3Endpoint, Prefix: configuration.ArtifactS3Prefix,
			SpoolDir: configuration.ArtifactS3SpoolDir, SSE: configuration.ArtifactS3SSE,
			KMSKeyID: configuration.ArtifactS3KMSKeyID, UsePathStyle: configuration.ArtifactS3UsePathStyle,
			MaxBytes: int64(configuration.ArtifactMaxBytes),
		})
	default:
		return nil, fmt.Errorf("unsupported Artifact backend %q", configuration.ArtifactBackend)
	}
}

func NewS3Store(ctx context.Context, metadata MetadataRepository, options S3Options) (*S3Store, error) {
	if err := validateS3Options(metadata, options); err != nil {
		return nil, err
	}
	awsConfiguration, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(options.Region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	client := s3.NewFromConfig(awsConfiguration, func(value *s3.Options) {
		value.UsePathStyle = options.UsePathStyle
		if options.Endpoint != "" {
			value.BaseEndpoint = aws.String(options.Endpoint)
		}
	})
	return newS3Store(client, metadata, options)
}

func newS3Store(client s3API, metadata MetadataRepository, options S3Options) (*S3Store, error) {
	if client == nil {
		return nil, fmt.Errorf("S3 client is required")
	}
	if err := validateS3Options(metadata, options); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(options.SpoolDir, 0o700); err != nil {
		return nil, fmt.Errorf("create Artifact spool directory: %w", err)
	}
	prefix := strings.Trim(path.Clean("/"+options.Prefix), "/")
	sse := types.ServerSideEncryptionAes256
	if options.SSE == "aws:kms" {
		sse = types.ServerSideEncryptionAwsKms
	}
	return &S3Store{client: client, metadata: metadata, options: options, prefix: prefix, sse: sse}, nil
}

func validateS3Options(metadata MetadataRepository, options S3Options) error {
	if metadata == nil || strings.TrimSpace(options.Bucket) == "" || strings.TrimSpace(options.Region) == "" {
		return fmt.Errorf("S3 bucket, region, and metadata repository are required")
	}
	if strings.TrimSpace(options.Prefix) == "" || strings.Contains(options.Prefix, "..") || strings.ContainsAny(options.Prefix, "\x00\r\n") {
		return fmt.Errorf("S3 Artifact prefix is invalid")
	}
	if strings.TrimSpace(options.SpoolDir) == "" {
		return fmt.Errorf("S3 Artifact spool directory is required")
	}
	if options.MaxBytes <= 0 {
		return fmt.Errorf("S3 Artifact byte limit must be positive")
	}
	if options.SSE != "AES256" && options.SSE != "aws:kms" {
		return fmt.Errorf("S3 Artifact encryption must be AES256 or aws:kms")
	}
	if options.SSE == "aws:kms" && strings.TrimSpace(options.KMSKeyID) == "" {
		return fmt.Errorf("S3 Artifact KMS key is required")
	}
	if options.Endpoint != "" {
		parsed, err := url.Parse(options.Endpoint)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return fmt.Errorf("S3 Artifact endpoint is invalid")
		}
	}
	return nil
}

func (s *S3Store) Put(ctx context.Context, request PutRequest, body io.Reader) (Meta, error) {
	if err := validatePutRequest(request, body); err != nil {
		return Meta{}, err
	}
	if !safeKeySegment(request.OwnerID) || !safeKeySegment(request.RunID) {
		return Meta{}, fmt.Errorf("artifact owner and run identifiers are required for S3 isolation")
	}
	file, size, digest, cleanup, err := s.spool(body)
	if err != nil {
		return Meta{}, err
	}
	defer cleanup()
	meta := Meta{
		ID: domain.NewID(), RunID: request.RunID, Kind: request.Kind,
		SHA256: digest, Size: size, ContentType: request.ContentType,
		Attributes: cloneAttributes(request.Attributes), CreatedAt: time.Now().UTC(),
	}
	meta.StorageKey = s.objectKey(request.OwnerID, meta)
	if err := s.upload(ctx, file, meta, request.OwnerID); err != nil {
		return Meta{}, err
	}
	if err := s.metadata.Insert(ctx, meta); err != nil {
		cleanupErr := s.deleteObject(ctx, meta.StorageKey)
		return Meta{}, errors.Join(fmt.Errorf("insert Artifact metadata: %w", err), cleanupErr)
	}
	return meta, nil
}

func (s *S3Store) Open(ctx context.Context, artifactID string) (io.ReadCloser, Meta, error) {
	meta, err := s.metadata.Get(ctx, artifactID)
	if err != nil {
		return nil, Meta{}, err
	}
	if meta.Size < 0 || meta.Size > s.options.MaxBytes {
		return nil, Meta{}, fmt.Errorf("artifact size is outside the configured limit")
	}
	if !s.ownsStorageKey(meta) {
		return nil, Meta{}, fmt.Errorf("artifact storage key is outside the configured tenant prefix")
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.options.Bucket), Key: aws.String(meta.StorageKey), ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return nil, Meta{}, fmt.Errorf("get Artifact object: %w", err)
	}
	if err := s.validateObject(output, meta); err != nil {
		_ = output.Body.Close()
		return nil, Meta{}, err
	}
	return newVerifyingReadCloser(output.Body, meta), meta, nil
}

func (s *S3Store) Delete(ctx context.Context, artifactID string) error {
	meta, err := s.metadata.Get(ctx, artifactID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !s.ownsStorageKey(meta) {
		return fmt.Errorf("artifact storage key is outside the configured tenant prefix")
	}
	if err := s.deleteObject(ctx, meta.StorageKey); err != nil {
		return err
	}
	if err := s.metadata.Delete(ctx, artifactID); err != nil {
		return fmt.Errorf("delete Artifact metadata: %w", err)
	}
	return nil
}

func (s *S3Store) Check(ctx context.Context) error {
	if _, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.options.Bucket)}); err != nil {
		return fmt.Errorf("check Artifact bucket: %w", err)
	}
	return nil
}

func (s *S3Store) MigrateRun(ctx context.Context, source Store, ownerID, runID string) (MigrationResult, error) {
	result := MigrationResult{StorageKeys: []string{}}
	if source == nil || !safeKeySegment(ownerID) || !safeKeySegment(runID) {
		return result, fmt.Errorf("migration source, owner, and run are required")
	}
	items, err := s.metadata.List(ctx, runID)
	if err != nil {
		return result, fmt.Errorf("list Artifact metadata for migration: %w", err)
	}
	for _, meta := range items {
		if storedOwner, migrated := s.storageKeyOwner(meta); migrated {
			if storedOwner != ownerID {
				return result, fmt.Errorf("migrated Artifact %s belongs to a different owner prefix", meta.ID)
			}
			body, _, err := s.Open(ctx, meta.ID)
			if err != nil {
				return result, fmt.Errorf("verify migrated Artifact %s: %w", meta.ID, err)
			}
			_, readErr := io.Copy(io.Discard, body)
			closeErr := body.Close()
			if err := errors.Join(readErr, closeErr); err != nil {
				return result, fmt.Errorf("verify migrated Artifact %s: %w", meta.ID, err)
			}
			result.AlreadyMigrated++
			continue
		}
		body, loaded, err := source.Open(ctx, meta.ID)
		if err != nil {
			return result, fmt.Errorf("open source Artifact %s: %w", meta.ID, err)
		}
		file, size, digest, cleanup, spoolErr := s.spool(body)
		closeErr := body.Close()
		if err := errors.Join(spoolErr, closeErr); err != nil {
			cleanup()
			return result, fmt.Errorf("spool source Artifact %s: %w", meta.ID, err)
		}
		if loaded.ID != meta.ID || size != meta.Size || digest != meta.SHA256 {
			cleanup()
			return result, fmt.Errorf("source Artifact %s does not match metadata", meta.ID)
		}
		migrated := meta
		migrated.StorageKey = s.objectKey(ownerID, meta)
		if err := s.upload(ctx, file, migrated, ownerID); err != nil {
			cleanup()
			return result, fmt.Errorf("upload source Artifact %s: %w", meta.ID, err)
		}
		cleanup()
		if err := s.metadata.UpdateStorageKey(ctx, meta.ID, meta.StorageKey, migrated.StorageKey); err != nil {
			current, currentErr := s.metadata.Get(ctx, meta.ID)
			if currentErr == nil && current.StorageKey == migrated.StorageKey {
				result.AlreadyMigrated++
				continue
			}
			return result, errors.Join(
				fmt.Errorf("switch Artifact %s storage key; verified target %s retained for retry: %w", meta.ID, migrated.StorageKey, err), currentErr,
			)
		}
		result.Migrated++
		result.StorageKeys = append(result.StorageKeys, migrated.StorageKey)
	}
	return result, nil
}

func (s *S3Store) upload(ctx context.Context, file *os.File, meta Meta, ownerID string) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind Artifact spool: %w", err)
	}
	digestBytes, err := hex.DecodeString(meta.SHA256)
	if err != nil {
		return fmt.Errorf("decode Artifact checksum: %w", err)
	}
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.options.Bucket), Key: aws.String(meta.StorageKey), Body: file,
		ContentLength: aws.Int64(meta.Size), ContentType: aws.String(meta.ContentType),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256, ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(digestBytes)),
		IfNoneMatch: aws.String("*"), ServerSideEncryption: s.sse,
		Metadata: map[string]string{"artifact-id": meta.ID, "run-id": meta.RunID, "owner-id": ownerID, "sha256": meta.SHA256},
	}
	if s.sse == types.ServerSideEncryptionAwsKms {
		input.SSEKMSKeyId = aws.String(s.options.KMSKeyID)
		input.BucketKeyEnabled = aws.Bool(true)
	}
	if _, err := s.client.PutObject(ctx, input); err != nil {
		if verifyErr := s.verifyObject(ctx, meta); verifyErr == nil {
			return nil
		}
		return fmt.Errorf("put Artifact object: %w", err)
	}
	return s.verifyObject(ctx, meta)
}

func (s *S3Store) verifyObject(ctx context.Context, meta Meta) error {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.options.Bucket), Key: aws.String(meta.StorageKey), ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return fmt.Errorf("verify Artifact object: %w", err)
	}
	if err := s.validateObject(output, meta); err != nil {
		_ = output.Body.Close()
		return err
	}
	reader := newVerifyingReadCloser(output.Body, meta)
	_, readErr := io.Copy(io.Discard, reader)
	return errors.Join(readErr, reader.Close())
}

func (s *S3Store) validateObject(output *s3.GetObjectOutput, meta Meta) error {
	if output == nil || output.Body == nil || output.ContentLength == nil || *output.ContentLength != meta.Size {
		return fmt.Errorf("artifact object size does not match metadata")
	}
	if aws.ToString(output.ContentType) != meta.ContentType {
		return fmt.Errorf("artifact object content type does not match metadata")
	}
	ownerID, ok := s.storageKeyOwner(meta)
	if !ok {
		return fmt.Errorf("artifact storage key is outside the configured tenant prefix")
	}
	if output.Metadata["artifact-id"] != meta.ID || output.Metadata["run-id"] != meta.RunID ||
		output.Metadata["owner-id"] != ownerID || output.Metadata["sha256"] != meta.SHA256 {
		return fmt.Errorf("artifact object metadata does not match PostgreSQL")
	}
	if output.ServerSideEncryption != s.sse {
		return fmt.Errorf("artifact object encryption does not match configuration")
	}
	if s.sse == types.ServerSideEncryptionAwsKms &&
		strings.TrimSpace(aws.ToString(output.SSEKMSKeyId)) != strings.TrimSpace(s.options.KMSKeyID) {
		return fmt.Errorf("artifact object KMS key identity does not match configuration")
	}
	if output.ChecksumSHA256 != nil {
		digestBytes, err := hex.DecodeString(meta.SHA256)
		if err != nil || aws.ToString(output.ChecksumSHA256) != base64.StdEncoding.EncodeToString(digestBytes) {
			return fmt.Errorf("artifact object checksum header does not match metadata")
		}
	}
	return nil
}

func (s *S3Store) spool(body io.Reader) (*os.File, int64, string, func(), error) {
	file, err := os.CreateTemp(s.options.SpoolDir, ".artifact-*.spool")
	if err != nil {
		return nil, 0, "", func() {}, fmt.Errorf("create Artifact spool: %w", err)
	}
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(body, s.options.MaxBytes+1))
	if copyErr != nil {
		cleanup()
		return nil, 0, "", func() {}, fmt.Errorf("spool Artifact: %w", copyErr)
	}
	if written > s.options.MaxBytes {
		cleanup()
		return nil, 0, "", func() {}, fmt.Errorf("artifact exceeds byte limit")
	}
	if err := file.Sync(); err != nil {
		cleanup()
		return nil, 0, "", func() {}, fmt.Errorf("sync Artifact spool: %w", err)
	}
	return file, written, hex.EncodeToString(digest.Sum(nil)), cleanup, nil
}

func (s *S3Store) deleteObject(ctx context.Context, key string) error {
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.options.Bucket), Key: aws.String(key)}); err != nil {
		return fmt.Errorf("delete Artifact object: %w", err)
	}
	return nil
}

func (s *S3Store) objectKey(ownerID string, meta Meta) string {
	return path.Join(s.prefix, "tenants", ownerID, "runs", meta.RunID, meta.ID, meta.SHA256)
}

func (s *S3Store) ownsStorageKey(meta Meta) bool {
	_, ok := s.storageKeyOwner(meta)
	return ok
}

func (s *S3Store) storageKeyOwner(meta Meta) (string, bool) {
	prefix := s.prefix + "/"
	if !strings.HasPrefix(meta.StorageKey, prefix) {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(meta.StorageKey, prefix), "/")
	valid := len(parts) == 6 && parts[0] == "tenants" && safeKeySegment(parts[1]) &&
		parts[2] == "runs" && parts[3] == meta.RunID && parts[4] == meta.ID && parts[5] == meta.SHA256
	if !valid {
		return "", false
	}
	return parts[1], true
}

func safeKeySegment(value string) bool {
	return value != "" && len(value) <= 128 && value != "." && value != ".." &&
		!strings.ContainsAny(value, "/\\\x00\r\n") && strings.TrimSpace(value) == value
}

var _ Store = (*S3Store)(nil)
