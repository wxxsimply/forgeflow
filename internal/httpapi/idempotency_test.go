package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"forgeflow/internal/auth"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/controlplane"
	"forgeflow/internal/domain"
)

func TestAtomicRunCreationRecovery(t *testing.T) {
	for _, fault := range []struct{ name, trigger string }{
		{"before_checkpoint", `CREATE TRIGGER fail_create BEFORE INSERT ON runs FOR EACH ROW EXECUTE FUNCTION fail_test_creation()`},
		{"after_checkpoint", `CREATE TRIGGER fail_create BEFORE UPDATE ON idempotency_keys FOR EACH ROW EXECUTE FUNCTION fail_test_creation()`},
		{"at_commit", `CREATE CONSTRAINT TRIGGER fail_create AFTER UPDATE ON idempotency_keys DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_test_creation()`},
	} {
		t.Run(fault.name, func(t *testing.T) {
			f := newFixture(t, auth.NewMemoryLimiter(20, time.Minute))
			admin := f.login(t, "admin@example.com", "correct horse battery staple", "")
			response := f.request(t, admin, http.MethodPost, "/api/v1/repositories", `{"name":"demo","localPath":"."}`, true, nil)
			if response.StatusCode != http.StatusCreated {
				t.Fatalf("register: %s", read(response))
			}
			var repo controlplane.Repository
			decodeResponse(t, response, &repo)
			body := `{"repositoryId":"` + repo.ID + `","task":"atomic recovery"}`
			headers := map[string]string{"Idempotency-Key": "atomic-retry"}
			if _, err := f.db.Exec(`CREATE FUNCTION fail_test_creation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected creation failure'; END $$; ` + fault.trigger); err != nil {
				t.Fatal(err)
			}
			cleanup := func() {
				if _, err := f.db.Exec(`DROP TRIGGER IF EXISTS fail_create ON runs; DROP TRIGGER IF EXISTS fail_create ON idempotency_keys; DROP FUNCTION IF EXISTS fail_test_creation()`); err != nil {
					t.Error(err)
				}
			}
			t.Cleanup(cleanup)
			response = f.request(t, admin, http.MethodPost, "/api/v1/runs", body, true, headers)
			status := response.StatusCode
			_ = read(response)
			if status < 400 {
				t.Fatalf("injected failure status=%d", status)
			}
			for _, table := range []string{"runs", "checkpoints", "run_events", "outbox", "idempotency_keys"} {
				var count int
				if err := f.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("rollback %s count=%d err=%v", table, count, err)
				}
			}
			cleanup()
			response = f.request(t, admin, http.MethodPost, "/api/v1/runs", body, true, headers)
			if response.StatusCode != http.StatusAccepted {
				t.Fatalf("retry: %s", read(response))
			}
			var created domain.RunState
			decodeResponse(t, response, &created)
			// New store instance models retry after process restart or response loss.
			replay, err := checkpoint.NewPostgresStore(f.db).CreateIdempotent(context.Background(), domain.NewRunState(domain.NewRunInput{OwnerID: admin.userID}), "atomic-retry", []byte(body))
			if err != nil || replay.RunID != created.RunID {
				t.Fatalf("restart replay=%+v err=%v", replay, err)
			}
			response = f.request(t, admin, http.MethodPost, "/api/v1/runs", body, true, headers)
			if response.StatusCode != http.StatusAccepted || response.Header.Get("ETag") == "" {
				t.Fatalf("HTTP replay: %s", read(response))
			}
			_ = read(response)
			response = f.request(t, admin, http.MethodPost, "/api/v1/runs", strings.Replace(body, "atomic recovery", "different task", 1), true, headers)
			if response.StatusCode != http.StatusConflict {
				t.Fatalf("mismatched body: %s", read(response))
			}
			_ = read(response)
			for _, table := range []string{"runs", "checkpoints", "outbox", "idempotency_keys"} {
				var count int
				if err := f.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 1 {
					t.Fatalf("retry %s count=%d err=%v", table, count, err)
				}
			}
			if _, err := f.db.Exec(`UPDATE idempotency_keys SET expires_at=now()-interval '1 second'`); err != nil {
				t.Fatal(err)
			}
			response = f.request(t, admin, http.MethodPost, "/api/v1/runs", body, true, headers)
			if response.StatusCode != http.StatusAccepted {
				t.Fatalf("expired completed key: %s", read(response))
			}
			var next domain.RunState
			decodeResponse(t, response, &next)
			if next.RunID == created.RunID {
				t.Fatal("expired completed key was not reusable")
			}
		})
	}
}

func TestLegacyPendingCreationFailsClosed(t *testing.T) {
	f := newFixture(t, auth.NewMemoryLimiter(20, time.Minute))
	admin := f.login(t, "admin@example.com", "correct horse battery staple", "")
	sum := sha256.Sum256([]byte("legacy request"))
	if _, err := f.db.Exec(`INSERT INTO idempotency_keys(owner_id,key,request_hash,status,expires_at) VALUES($1,'legacy',$2,'pending',now()-interval '1 day')`, admin.userID, sum[:]); err != nil {
		t.Fatal(err)
	}
	_, err := checkpoint.NewPostgresStore(f.db).CreateIdempotent(context.Background(), domain.NewRunState(domain.NewRunInput{OwnerID: admin.userID}), "legacy", []byte("legacy request"))
	if err != checkpoint.ErrIdempotencyLegacyPending {
		t.Fatalf("legacy claim: %v", err)
	}
	var claims, runs int
	if err := f.db.QueryRow(`SELECT (SELECT count(*) FROM idempotency_keys),(SELECT count(*) FROM runs)`).Scan(&claims, &runs); err != nil || claims != 1 || runs != 0 {
		t.Fatalf("legacy claim changed: %d/%d %v", claims, runs, err)
	}
}

func TestRunTaskUTF8Limit(t *testing.T) {
	f := newFixture(t, auth.NewMemoryLimiter(20, time.Minute))
	admin := f.login(t, "admin@example.com", "correct horse battery staple", "")
	response := f.request(t, admin, http.MethodPost, "/api/v1/repositories", `{"name":"demo","localPath":"."}`, true, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register: %s", read(response))
	}
	var repo controlplane.Repository
	decodeResponse(t, response, &repo)
	for _, tc := range []struct {
		name, task string
		status     int
	}{
		{"english_boundary", strings.Repeat("a", 20000), http.StatusAccepted},
		{"english_overflow", strings.Repeat("a", 20001), http.StatusBadRequest},
		{"chinese_boundary", strings.Repeat("中", 6666) + "ab", http.StatusAccepted},
		{"chinese_overflow", strings.Repeat("中", 7000), http.StatusBadRequest},
		{"emoji_boundary", strings.Repeat("😀", 5000), http.StatusAccepted},
		{"emoji_overflow", strings.Repeat("😀", 5001), http.StatusBadRequest},
		{"blank", " \n\t ", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"repositoryId": repo.ID, "task": tc.task})
			response := f.request(t, admin, http.MethodPost, "/api/v1/runs", string(body), true, map[string]string{"Idempotency-Key": tc.name})
			if response.StatusCode != tc.status {
				t.Fatalf("status=%d want=%d body=%s", response.StatusCode, tc.status, read(response))
			}
			_ = read(response)
		})
	}
}
