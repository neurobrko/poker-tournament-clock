package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	_ "modernc.org/sqlite"
)

func TestOpenUpgradesVersionOneDatabaseThroughCurrentMigrations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "version-one.db")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	migration, err := migrationFS.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	wall := FormatWallTime(time.Now())
	if _, err := db.ExecContext(ctx, `INSERT INTO tournament (id, name, status, configuration_json, created_at, updated_at, remaining_ns)
		VALUES (1, 'Migrated', 'not_started', ?, ?, ?, ?)`, string(configuration), wall, wall, int64(template.Schedule[0].Duration.Value())); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audit_events (kind, wall_timestamp, payload_json)
		VALUES ('tournament_created', ?, '{}')`, wall); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	record, err := migrated.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Name != "Migrated" || record.ResultCompleted != nil {
		t.Fatalf("migrated tournament record = %+v", record)
	}
	var version int
	if err := migrated.ViewTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	}); err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
	var tableCount int
	if err := migrated.ViewTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
			WHERE type = 'table' AND name IN ('player_events', 'bust_participant_corrections', 'tournament_ends')`).Scan(&tableCount)
	}); err != nil {
		t.Fatal(err)
	}
	if tableCount != 3 {
		t.Fatalf("migrated domain tables = %d, want 3", tableCount)
	}
}
