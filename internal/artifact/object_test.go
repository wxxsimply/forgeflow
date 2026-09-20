package artifact

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type storedObject struct {
	body        []byte
	metadata    map[string]string
	length      int64
	contentType *string
	checksum    *string
	encryption  types.ServerSideEncryption
	kmsKeyID    *string
}

type fakeS3 struct {
	objects          map[string]storedObject
	lastPut          *s3.PutObjectInput
	putErr           error
	putAfterWriteErr error
	getErr           error
	deleteErr        error
	headErr          error
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string]storedObject{}}
}

func (f *fakeS3) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.lastPut = input
	if f.putErr != nil {
		return nil, f.putErr
	}
	key := aws.ToString(input.Key)
	if aws.ToString(input.IfNoneMatch) == "*" {
		if _, exists := f.objects[key]; exists {
			return nil, errors.New("precondition failed")
		}
	}
	body, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	f.objects[key] = storedObject{
		body: append([]byte(nil), body...), metadata: cloneAttributes(input.Metadata),
		length: int64(len(body)), contentType: input.ContentType, checksum: input.ChecksumSHA256,
		encryption: input.ServerSideEncryption, kmsKeyID: input.SSEKMSKeyId,
	}
	if f.putAfterWriteErr != nil {
		return nil, f.putAfterWriteErr
	}
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	item, exists := f.objects[aws.ToString(input.Key)]
	if !exists {
		return nil, errors.New("object not found")
	}
	length := item.length
	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(item.body)), ContentLength: &length,
		ContentType: item.contentType, Metadata: cloneAttributes(item.metadata), ChecksumSHA256: item.checksum,
		ServerSideEncryption: item.encryption, SSEKMSKeyId: item.kmsKeyID,
	}, nil
}

func (f *fakeS3) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	delete(f.objects, aws.ToString(input.Key))
	return &s3.DeleteObjectOutput{}, nil
}

func (f *fakeS3) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if f.headErr != nil {
		return nil, f.headErr
	}
	return &s3.HeadBucketOutput{}, nil
}

type failingMetadata struct {
	*MemoryMetadata
	insertErr        error
	updateErr        error
	updateAfterApply bool
	deleteErr        error
}

func (m *failingMetadata) Insert(ctx context.Context, meta Meta) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	return m.MemoryMetadata.Insert(ctx, meta)
}

func (m *failingMetadata) UpdateStorageKey(ctx context.Context, id, previous, next string) error {
	if m.updateErr != nil {
		if m.updateAfterApply {
			if err := m.MemoryMetadata.UpdateStorageKey(ctx, id, previous, next); err != nil {
				return err
			}
		}
		return m.updateErr
	}
	return m.MemoryMetadata.UpdateStorageKey(ctx, id, previous, next)
}

func (m *failingMetadata) Delete(ctx context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	return m.MemoryMetadata.Delete(ctx, id)
}

func testS3Store(t *testing.T, client *fakeS3, metadata MetadataRepository) *S3Store {
	t.Helper()
	store, err := newS3Store(client, metadata, S3Options{
		Bucket: "forgeflow-test", Region: "test-1", Prefix: "forgeflow/artifacts",
		SpoolDir: t.TempDir(), SSE: "aws:kms", KMSKeyID: "alias/forgeflow-test",
		MaxBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestS3StorePutOpenDeleteWithKMSAndIsolation(t *testing.T) {
	ctx := context.Background()
	metadata := NewMemoryMetadata()
	client := newFakeS3()
	store := testS3Store(t, client, metadata)
	created, err := store.Put(ctx, PutRequest{
		OwnerID: "owner-1", RunID: "run-1", Kind: KindPatch,
		ContentType: "text/x-diff", Attributes: map[string]string{"source": "judge"},
	}, strings.NewReader("diff body"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(created.StorageKey, "/tenants/owner-1/runs/run-1/") {
		t.Fatalf("storage key does not isolate owner and run: %s", created.StorageKey)
	}
	if client.lastPut.ServerSideEncryption != types.ServerSideEncryptionAwsKms || aws.ToString(client.lastPut.SSEKMSKeyId) == "" || !aws.ToBool(client.lastPut.BucketKeyEnabled) {
		t.Fatal("PutObject did not require KMS and a bucket key")
	}
	if aws.ToString(client.lastPut.IfNoneMatch) != "*" || aws.ToString(client.lastPut.ChecksumSHA256) == "" {
		t.Fatal("PutObject did not use conditional creation and SHA-256")
	}
	body, loaded, err := store.Open(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if string(content) != "diff body" || loaded.ID != created.ID {
		t.Fatalf("content=%q loaded=%+v", content, loaded)
	}

	item := client.objects[created.StorageKey]
	item.body = []byte("evil body")
	item.metadata["owner-id"] = "owner-2"
	client.objects[created.StorageKey] = item
	if _, _, err := store.Open(ctx, created.ID); err == nil {
		t.Fatal("object owner metadata tampering was accepted")
	}
	item.metadata["owner-id"] = "owner-1"
	client.objects[created.StorageKey] = item
	item.kmsKeyID = aws.String("alias/wrong-key")
	client.objects[created.StorageKey] = item
	if _, _, err := store.Open(ctx, created.ID); err == nil {
		t.Fatal("object KMS key identity tampering was accepted")
	}
	item.kmsKeyID = aws.String("alias/forgeflow-test")
	client.objects[created.StorageKey] = item
	item.contentType = aws.String("application/octet-stream")
	client.objects[created.StorageKey] = item
	if _, _, err := store.Open(ctx, created.ID); err == nil {
		t.Fatal("object content type tampering was accepted")
	}
	item.contentType = aws.String("text/x-diff")
	client.objects[created.StorageKey] = item

	body, _, err = store.Open(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(body); err == nil {
		t.Fatal("same-size object tampering was accepted")
	}
	_ = body.Close()
	item.body = []byte("diff body")
	client.objects[created.StorageKey] = item

	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, exists := client.objects[created.StorageKey]; exists {
		t.Fatal("Delete left the object behind")
	}
	if _, err := metadata.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("metadata was not deleted: %v", err)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("retrying Delete was not idempotent: %v", err)
	}
}

func TestS3StoreRejectsMissingTenantAndOversizedBody(t *testing.T) {
	store := testS3Store(t, newFakeS3(), NewMemoryMetadata())
	if _, err := store.Put(context.Background(), PutRequest{RunID: "run-1", Kind: KindLog, ContentType: "text/plain"}, strings.NewReader("body")); err == nil {
		t.Fatal("S3 store accepted an Artifact without an owner")
	}
	if _, err := store.Put(context.Background(), PutRequest{OwnerID: "owner-1", RunID: "run-1", Kind: KindLog, ContentType: "text/plain\r\nX-Injected: yes"}, strings.NewReader("body")); err == nil {
		t.Fatal("S3 store accepted an invalid Artifact content type")
	}
	store.options.MaxBytes = 4
	if _, err := store.Put(context.Background(), PutRequest{OwnerID: "owner-1", RunID: "run-1", Kind: KindLog, ContentType: "text/plain"}, strings.NewReader("12345")); err == nil {
		t.Fatal("S3 store accepted an oversized Artifact")
	}
}
func TestS3StoreDeleteRecoversAfterMetadataFailure(t *testing.T) {
	ctx := context.Background()
	metadata := &failingMetadata{MemoryMetadata: NewMemoryMetadata()}
	client := newFakeS3()
	store := testS3Store(t, client, metadata)
	created, err := store.Put(ctx, PutRequest{
		OwnerID: "owner-1", RunID: "run-1", Kind: KindLog, ContentType: "text/plain",
	}, strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	metadata.deleteErr = errors.New("database unavailable")
	if err := store.Delete(ctx, created.ID); err == nil {
		t.Fatal("Delete accepted a metadata failure")
	}
	if _, exists := client.objects[created.StorageKey]; exists {
		t.Fatal("Delete left the object after metadata failure")
	}
	metadata.deleteErr = nil
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete retry failed: %v", err)
	}
	if _, err := metadata.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete retry left metadata: %v", err)
	}
}

func TestS3StoreCompensatesMetadataFailureAndRecoversAmbiguousPut(t *testing.T) {
	ctx := context.Background()
	metadata := &failingMetadata{MemoryMetadata: NewMemoryMetadata(), insertErr: errors.New("database unavailable")}
	client := newFakeS3()
	store := testS3Store(t, client, metadata)
	_, err := store.Put(ctx, PutRequest{OwnerID: "owner-1", RunID: "run-1", Kind: KindLog, ContentType: "text/plain"}, strings.NewReader("body"))
	if err == nil || len(client.objects) != 0 {
		t.Fatalf("metadata failure was not compensated: err=%v objects=%d", err, len(client.objects))
	}

	metadata.insertErr = nil
	client.putAfterWriteErr = errors.New("response lost")
	created, err := store.Put(ctx, PutRequest{OwnerID: "owner-1", RunID: "run-1", Kind: KindLog, ContentType: "text/plain"}, strings.NewReader("body"))
	if err != nil {
		t.Fatalf("ambiguous successful Put was not recovered: %v", err)
	}
	if _, err := metadata.Get(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
}

func TestS3StoreMigrationPreservesSourceAndResumes(t *testing.T) {
	ctx := context.Background()
	metadata := NewMemoryMetadata()
	sourceRoot := t.TempDir()
	source, err := NewFileStore(sourceRoot, metadata, 1024)
	if err != nil {
		t.Fatal(err)
	}
	created, err := source.Put(ctx, PutRequest{RunID: "run-1", Kind: KindRunReport, ContentType: "application/json"}, strings.NewReader("{\"ok\":true}"))
	if err != nil {
		t.Fatal(err)
	}
	sourcePath, err := source.resolveStorageKey(created.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	destination := testS3Store(t, newFakeS3(), metadata)
	result, err := destination.MigrateRun(ctx, source, "owner-1", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Migrated != 1 || result.AlreadyMigrated != 0 || len(result.StorageKeys) != 1 {
		t.Fatalf("migration result=%+v", result)
	}
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("migration removed rollback source: %v", err)
	}
	if _, err := destination.MigrateRun(ctx, source, "owner-2", "run-1"); err == nil {
		t.Fatal("migration accepted an already-migrated object under another owner prefix")
	}
	result, err = destination.MigrateRun(ctx, source, "owner-1", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Migrated != 0 || result.AlreadyMigrated != 1 {
		t.Fatalf("resumed migration result=%+v", result)
	}
}

func TestS3StoreMigrationRetainsVerifiedTargetAfterStorageKeyCASFailure(t *testing.T) {
	ctx := context.Background()
	metadata := &failingMetadata{MemoryMetadata: NewMemoryMetadata(), updateErr: errors.New("concurrent metadata change")}
	source, err := NewFileStore(t.TempDir(), metadata, 1024)
	if err != nil {
		t.Fatal(err)
	}
	created, err := source.Put(ctx, PutRequest{RunID: "run-1", Kind: KindLog, ContentType: "text/plain"}, strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	client := newFakeS3()
	destination := testS3Store(t, client, metadata)
	if _, err := destination.MigrateRun(ctx, source, "owner-1", "run-1"); err == nil {
		t.Fatal("migration accepted a failed metadata CAS")
	}
	if len(client.objects) != 1 {
		t.Fatal("failed migration removed the verified retry target")
	}
	current, err := metadata.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.StorageKey != created.StorageKey {
		t.Fatal("failed migration changed source metadata")
	}
}

func TestS3StoreMigrationAcceptsConcurrentWinner(t *testing.T) {
	ctx := context.Background()
	metadata := &failingMetadata{
		MemoryMetadata: NewMemoryMetadata(), updateErr: errors.New("concurrent metadata change"),
		updateAfterApply: true,
	}
	source, err := NewFileStore(t.TempDir(), metadata, 1024)
	if err != nil {
		t.Fatal(err)
	}
	created, err := source.Put(ctx, PutRequest{RunID: "run-1", Kind: KindLog, ContentType: "text/plain"}, strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	client := newFakeS3()
	destination := testS3Store(t, client, metadata)
	result, err := destination.MigrateRun(ctx, source, "owner-1", "run-1")
	if err != nil {
		t.Fatalf("concurrent winner was treated as a failed migration: %v", err)
	}
	current, err := metadata.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.AlreadyMigrated != 1 || !destination.ownsStorageKey(current) || len(client.objects) != 1 {
		t.Fatalf("result=%+v current=%+v objects=%d", result, current, len(client.objects))
	}
}

func TestS3StoreFailsClosedOnKeyOrBucketMismatch(t *testing.T) {
	ctx := context.Background()
	metadata := NewMemoryMetadata()
	client := newFakeS3()
	store := testS3Store(t, client, metadata)
	created, err := store.Put(ctx, PutRequest{OwnerID: "owner-1", RunID: "run-1", Kind: KindLog, ContentType: "text/plain"}, strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	if err := metadata.UpdateStorageKey(ctx, created.ID, created.StorageKey, "other/tenants/owner-1/runs/run-1/"+created.ID+"/"+created.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Open(ctx, created.ID); err == nil {
		t.Fatal("S3 store accepted a key outside its configured prefix")
	}
	client.headErr = errors.New("forbidden")
	if err := store.Check(ctx); err == nil {
		t.Fatal("S3 store readiness accepted an unavailable bucket")
	}
}
