package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lib/pq"
)

func migrationDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run Postgres integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	admin, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	schema := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("clean up test schema: %v", err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	pool, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	return ctx, pool
}

func TestMigrateRepeatAndHistory(t *testing.T) {
	ctx, pool := migrationDB(t)
	for range 2 {
		if err := Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.ExecContext(ctx, "INSERT INTO short_url (code, url) VALUES ($1, $2)", "abc", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, "INSERT INTO short_url (code, url) VALUES ($1, $2)", "abc", "https://example.org"); err == nil {
		t.Fatal("expected duplicate short code to be rejected")
	}
	var count int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration history: count=%d, err=%v", count, err)
	}
	files := fstest.MapFS{
		"migrations/0001_create_short_url.sql": {Data: []byte("SELECT 1;")},
	}
	if err := migrate(ctx, pool, files); err == nil || !strings.Contains(err.Error(), "history differs") {
		t.Fatalf("expected changed-history error, got %v", err)
	}
	files = fstest.MapFS{"migrations/0002_other.sql": {Data: []byte("SELECT 1;")}}
	if err := migrate(ctx, pool, files); err == nil {
		t.Fatal("expected missing/reordered history to be rejected")
	}
}

func TestMigrateRollbackAndRetry(t *testing.T) {
	ctx, pool := migrationDB(t)
	files := fstest.MapFS{
		"migrations/0001_initial.sql": {Data: []byte("CREATE TABLE entries (id INT PRIMARY KEY);")},
	}
	if err := migrate(ctx, pool, files); err != nil {
		t.Fatal(err)
	}
	files["migrations/0002_insert.sql"] = &fstest.MapFile{Data: []byte("INSERT INTO entries VALUES (1);")}
	files["migrations/0003_broken.sql"] = &fstest.MapFile{Data: []byte("SELECT * FROM missing_table;")}
	if err := migrate(ctx, pool, files); err == nil {
		t.Fatal("expected migration failure")
	}
	var count int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM entries").Scan(&count); err != nil || count != 0 {
		t.Fatalf("pending SQL was not rolled back: count=%d, err=%v", count, err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 1 {
		t.Fatalf("pending history was not rolled back: count=%d, err=%v", count, err)
	}
	files["migrations/0003_broken.sql"] = &fstest.MapFile{Data: []byte("INSERT INTO entries VALUES (2);")}
	if err := migrate(ctx, pool, files); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM entries").Scan(&count); err != nil || count != 2 {
		t.Fatalf("retry failed: count=%d, err=%v", count, err)
	}
}

func TestMigrateConcurrent(t *testing.T) {
	ctx, pool := migrationDB(t)
	files := fstest.MapFS{
		"migrations/0001_initial.sql": {Data: []byte("SELECT pg_sleep(0.2); CREATE TABLE entries (id INT PRIMARY KEY); INSERT INTO entries VALUES (1);")},
	}
	results := make(chan error, 3)
	for range 3 {
		go func() { results <- migrate(ctx, pool, files) }()
	}
	for range 3 {
		if err := <-results; err != nil {
			t.Errorf("concurrent migration failed: %v", err)
		}
	}
	var count int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM entries").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration did not run exactly once: count=%d, err=%v", count, err)
	}
}
