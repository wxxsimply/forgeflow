package audit

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var testIntegrityKey = []byte("0123456789abcdef0123456789abcdef")

func TestNewEventRedactsSensitiveDetailsAndSignsEnvelope(t *testing.T) {
	now := time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC)
	event, err := NewEvent("actor-1", "approval.approve", "approval", "approval-1", "request-1", "203.0.113.10", map[string]any{
		"runId": "run-1", "password": "never-store", "authorization": "Bearer secret", "nested": map[string]any{"apiKey": "secret", "version": 4},
	}, testIntegrityKey, now)
	if err != nil {
		t.Fatal(err)
	}
	if event.SourceIPHash == "" || strings.Contains(event.SourceIPHash, "203.0.113.10") {
		t.Fatalf("source IP was not pseudonymized: %q", event.SourceIPHash)
	}
	if _, exists := event.Details["password"]; exists {
		t.Fatal("password survived audit redaction")
	}
	nested, ok := event.Details["nested"].(map[string]any)
	if !ok || nested["version"] != 4 || nested["apiKey"] != nil {
		t.Fatalf("nested redaction=%#v", event.Details["nested"])
	}
	if err := Verify(event, testIntegrityKey); err != nil {
		t.Fatal(err)
	}
	event.ResourceID = "tampered"
	if err := Verify(event, testIntegrityKey); err == nil {
		t.Fatal("tampered event passed integrity verification")
	}
}

type auditObject struct {
	body          []byte
	contentType   *string
	contentLength int64
	checksum      *string
	metadata      map[string]string
	encryption    types.ServerSideEncryption
	kmsKeyID      *string
	lockMode      types.ObjectLockMode
	retainUntil   *time.Time
}

type fakeAuditS3 struct {
	objects     map[string]auditObject
	lastPut     *s3.PutObjectInput
	putErr      error
	getErr      error
	listErr     error
	headErr     error
	lockErr     error
	lockEnabled bool
}

func newFakeAuditS3() *fakeAuditS3 {
	return &fakeAuditS3{objects: map[string]auditObject{}, lockEnabled: true}
}

func (f *fakeAuditS3) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.lastPut = input
	if f.putErr != nil {
		return nil, f.putErr
	}
	key := aws.ToString(input.Key)
	if _, exists := f.objects[key]; exists && aws.ToString(input.IfNoneMatch) == "*" {
		return nil, errors.New("precondition failed")
	}
	body, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	f.objects[key] = auditObject{
		body: append([]byte(nil), body...), contentType: input.ContentType, contentLength: int64(len(body)), checksum: input.ChecksumSHA256,
		metadata: input.Metadata, encryption: input.ServerSideEncryption, kmsKeyID: input.SSEKMSKeyId, lockMode: input.ObjectLockMode, retainUntil: input.ObjectLockRetainUntilDate,
	}
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeAuditS3) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	object, exists := f.objects[aws.ToString(input.Key)]
	if !exists {
		return nil, errors.New("not found")
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(strings.NewReader(string(object.body))), ContentType: object.contentType, ContentLength: aws.Int64(object.contentLength),
		ChecksumSHA256: object.checksum, Metadata: object.metadata, ServerSideEncryption: object.encryption, SSEKMSKeyId: object.kmsKeyID,
		ObjectLockMode: object.lockMode, ObjectLockRetainUntilDate: object.retainUntil,
	}, nil
}

func (f *fakeAuditS3) ListObjectsV2(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	output := &s3.ListObjectsV2Output{Contents: make([]types.Object, 0, len(keys))}
	for _, key := range keys {
		output.Contents = append(output.Contents, types.Object{Key: aws.String(key)})
	}
	return output, nil
}

func (f *fakeAuditS3) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, f.headErr
}

func (f *fakeAuditS3) GetObjectLockConfiguration(context.Context, *s3.GetObjectLockConfigurationInput, ...func(*s3.Options)) (*s3.GetObjectLockConfigurationOutput, error) {
	if f.lockErr != nil {
		return nil, f.lockErr
	}
	status := types.ObjectLockEnabled("")
	if f.lockEnabled {
		status = types.ObjectLockEnabledEnabled
	}
	return &s3.GetObjectLockConfigurationOutput{ObjectLockConfiguration: &types.ObjectLockConfiguration{ObjectLockEnabled: status}}, nil
}

func testAuditStore(t *testing.T, client *fakeAuditS3) *S3Store {
	t.Helper()
	store, err := newS3Store(client, S3Options{
		Bucket: "audit-test", Region: "test-1", Prefix: "forgeflow/audit", KMSKeyID: "alias/forgeflow-audit",
		Retention: 30 * 24 * time.Hour, IntegrityKey: testIntegrityKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestS3StoreAppendAndRecoverImmutableEvent(t *testing.T) {
	client := newFakeAuditS3()
	store := testAuditStore(t, client)
	event, err := NewEvent("actor-1", "repository.create", "repository", "repository-1", "request-1", "127.0.0.1", map[string]any{"version": 1}, testIntegrityKey, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if client.lastPut.ObjectLockMode != types.ObjectLockModeCompliance || client.lastPut.ObjectLockRetainUntilDate == nil || client.lastPut.ServerSideEncryption != types.ServerSideEncryptionAwsKms || aws.ToString(client.lastPut.IfNoneMatch) != "*" {
		t.Fatal("audit object was not conditionally created with KMS and compliance retention")
	}
	page, err := store.Recover(context.Background(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].ID != event.ID {
		t.Fatalf("recovery=%+v", page)
	}
	for key, object := range client.objects {
		originalRetention := object.retainUntil
		object.retainUntil = aws.Time(event.OccurredAt.Add(time.Hour))
		client.objects[key] = object
		if _, err := store.Recover(context.Background(), "", 100); err == nil {
			t.Fatal("recovery accepted shortened Object Lock retention")
		}
		object.retainUntil = originalRetention
		object.body[len(object.body)-2] ^= 1
		client.objects[key] = object
	}
	if _, err := store.Recover(context.Background(), "", 100); err == nil {
		t.Fatal("recovery accepted tampered content")
	}
}

func TestS3StoreFailsClosedWithoutObjectLockOrAppend(t *testing.T) {
	client := newFakeAuditS3()
	store := testAuditStore(t, client)
	client.lockEnabled = false
	if err := store.Check(context.Background()); err == nil {
		t.Fatal("readiness accepted a bucket without Object Lock")
	}
	event, err := NewEvent("actor-1", "run.create", "run", "run-1", "request-1", "", nil, testIntegrityKey, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	client.putErr = errors.New("backend unavailable")
	if err := store.Append(context.Background(), event); err == nil {
		t.Fatal("append accepted an unavailable backend")
	}
}
