package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const auditContentType = "application/vnd.forgeflow.audit+json"

type S3Options struct {
	Bucket       string
	Region       string
	Endpoint     string
	Prefix       string
	KMSKeyID     string
	UsePathStyle bool
	Retention    time.Duration
	IntegrityKey []byte
}

type s3API interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	GetObjectLockConfiguration(context.Context, *s3.GetObjectLockConfigurationInput, ...func(*s3.Options)) (*s3.GetObjectLockConfigurationOutput, error)
}

type S3Store struct {
	client  s3API
	options S3Options
	prefix  string
}

func NewS3Store(ctx context.Context, options S3Options) (*S3Store, error) {
	if err := validateS3Options(options); err != nil {
		return nil, err
	}
	configuration, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(options.Region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration for audit storage: %w", err)
	}
	client := s3.NewFromConfig(configuration, func(value *s3.Options) {
		value.UsePathStyle = options.UsePathStyle
		if options.Endpoint != "" {
			value.BaseEndpoint = aws.String(options.Endpoint)
		}
	})
	return newS3Store(client, options)
}

func newS3Store(client s3API, options S3Options) (*S3Store, error) {
	if client == nil {
		return nil, fmt.Errorf("S3 audit client is required")
	}
	if err := validateS3Options(options); err != nil {
		return nil, err
	}
	return &S3Store{client: client, options: options, prefix: strings.Trim(path.Clean("/"+options.Prefix), "/")}, nil
}

func validateS3Options(options S3Options) error {
	if strings.TrimSpace(options.Bucket) == "" || strings.TrimSpace(options.Region) == "" || strings.TrimSpace(options.KMSKeyID) == "" {
		return fmt.Errorf("S3 audit bucket, region, and KMS key are required")
	}
	if strings.TrimSpace(options.Prefix) == "" || strings.Contains(options.Prefix, "..") || strings.ContainsAny(options.Prefix, "\x00\r\n") {
		return fmt.Errorf("S3 audit prefix is invalid")
	}
	if options.Retention < 24*time.Hour || options.Retention > 10*365*24*time.Hour {
		return fmt.Errorf("S3 audit retention must be between 24h and 87600h")
	}
	if len(options.IntegrityKey) != 32 {
		return fmt.Errorf("audit integrity key must contain exactly 32 bytes")
	}
	if options.Endpoint != "" {
		parsed, err := url.Parse(options.Endpoint)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return fmt.Errorf("S3 audit endpoint is invalid")
		}
	}
	return nil
}

func (s *S3Store) Append(ctx context.Context, event Event) error {
	if err := Verify(event, s.options.IntegrityKey); err != nil {
		return err
	}
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal audit event: %w", err)
	}
	digest := sha256.Sum256(body)
	key := s.objectKey(event)
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.options.Bucket), Key: aws.String(key), Body: bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))), ContentType: aws.String(auditContentType),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256, ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(digest[:])),
		IfNoneMatch: aws.String("*"), ServerSideEncryption: types.ServerSideEncryptionAwsKms,
		SSEKMSKeyId: aws.String(s.options.KMSKeyID), BucketKeyEnabled: aws.Bool(true),
		ObjectLockMode: types.ObjectLockModeCompliance, ObjectLockRetainUntilDate: aws.Time(event.OccurredAt.Add(s.options.Retention)),
		Metadata: map[string]string{"event-id": event.ID, "sha256": hex.EncodeToString(digest[:]), "schema-version": SchemaVersion},
	}
	if _, err := s.client.PutObject(ctx, input); err != nil {
		if verifyErr := s.verifyObject(ctx, key); verifyErr == nil {
			return nil
		}
		return fmt.Errorf("append immutable audit object: %w", err)
	}
	return s.verifyObject(ctx, key)
}

func (s *S3Store) Check(ctx context.Context) error {
	if _, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.options.Bucket)}); err != nil {
		return fmt.Errorf("check audit bucket: %w", err)
	}
	lock, err := s.client.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{Bucket: aws.String(s.options.Bucket)})
	if err != nil {
		return fmt.Errorf("check audit bucket Object Lock: %w", err)
	}
	if lock == nil || lock.ObjectLockConfiguration == nil || lock.ObjectLockConfiguration.ObjectLockEnabled != types.ObjectLockEnabledEnabled {
		return fmt.Errorf("audit bucket Object Lock must be enabled")
	}
	return nil
}

func (s *S3Store) Recover(ctx context.Context, cursor string, limit int32) (RecoveryPage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	output, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.options.Bucket), Prefix: aws.String(s.prefix + "/events/"), ContinuationToken: emptyPointer(cursor), MaxKeys: aws.Int32(limit),
	})
	if err != nil {
		return RecoveryPage{}, fmt.Errorf("list audit objects: %w", err)
	}
	page := RecoveryPage{Events: []Event{}, NextCursor: aws.ToString(output.NextContinuationToken)}
	for _, object := range output.Contents {
		event, err := s.readObject(ctx, aws.ToString(object.Key))
		if err != nil {
			return RecoveryPage{}, err
		}
		page.Events = append(page.Events, event)
	}
	return page, nil
}

func (s *S3Store) verifyObject(ctx context.Context, key string) error {
	_, err := s.readObject(ctx, key)
	return err
}

func (s *S3Store) readObject(ctx context.Context, key string) (Event, error) {
	if !strings.HasPrefix(key, s.prefix+"/events/") {
		return Event{}, fmt.Errorf("audit object key is outside the configured prefix")
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.options.Bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		return Event{}, fmt.Errorf("read audit object: %w", err)
	}
	if output == nil || output.Body == nil || aws.ToString(output.ContentType) != auditContentType || output.ServerSideEncryption != types.ServerSideEncryptionAwsKms || strings.TrimSpace(aws.ToString(output.SSEKMSKeyId)) != strings.TrimSpace(s.options.KMSKeyID) || output.ObjectLockMode != types.ObjectLockModeCompliance || output.ObjectLockRetainUntilDate == nil {
		if output != nil && output.Body != nil {
			_ = output.Body.Close()
		}
		return Event{}, fmt.Errorf("audit object security metadata is invalid")
	}
	body, readErr := io.ReadAll(io.LimitReader(output.Body, 1024*1024+1))
	closeErr := output.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return Event{}, fmt.Errorf("read audit object body: %w", err)
	}
	if len(body) > 1024*1024 || output.ContentLength == nil || *output.ContentLength != int64(len(body)) {
		return Event{}, fmt.Errorf("audit object size is invalid")
	}
	digest := sha256.Sum256(body)
	digestHex := hex.EncodeToString(digest[:])
	if output.Metadata["sha256"] != digestHex || output.Metadata["event-id"] == "" || output.Metadata["schema-version"] != SchemaVersion {
		return Event{}, fmt.Errorf("audit object metadata does not match content")
	}
	if checksum := aws.ToString(output.ChecksumSHA256); checksum != "" && checksum != base64.StdEncoding.EncodeToString(digest[:]) {
		return Event{}, fmt.Errorf("audit object checksum does not match content")
	}
	var event Event
	if err := json.Unmarshal(body, &event); err != nil {
		return Event{}, fmt.Errorf("decode audit object: %w", err)
	}
	if event.ID != output.Metadata["event-id"] {
		return Event{}, fmt.Errorf("audit event ID does not match object metadata")
	}
	if output.ObjectLockRetainUntilDate.Before(event.OccurredAt.Add(s.options.Retention).Add(-time.Second)) {
		return Event{}, fmt.Errorf("audit object retention is shorter than configured")
	}
	if err := Verify(event, s.options.IntegrityKey); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (s *S3Store) objectKey(event Event) string {
	return path.Join(s.prefix, "events", event.OccurredAt.UTC().Format("2006/01/02/15"), event.OccurredAt.UTC().Format("20060102T150405.000000000Z")+"-"+event.ID+".json")
}

func emptyPointer(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return aws.String(value)
}

var _ Appender = (*S3Store)(nil)
