package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenRecoversRunningClockAtLastCheckpointPaused(t *testing.T) {
	ctx := context.Background()
	store := createTestStore(t)
	path := store.Path()
	startedAt := time.Now().Add(-45 * time.Second)
	if _, err := store.db.ExecContext(ctx, `UPDATE tournament SET status = ?, started_at = ?,
		clock_running = 1, clock_elapsed_ns = ?, real_elapsed_ns = ?, segment_index = 1, remaining_ns = ? WHERE id = 1`,
		StatusRunning, FormatWallTime(startedAt), int64(7*time.Second), int64(20*time.Second), int64(13*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	record, err := reopened.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusPaused || record.Checkpoint.Running {
		t.Fatalf("recovered state = status %q, checkpoint %+v; want paused", record.Status, record.Checkpoint)
	}
	if record.Checkpoint.ClockElapsed != 7*time.Second || record.Checkpoint.Remaining != 13*time.Minute {
		t.Fatalf("recovery did not retain the last saved clock checkpoint: %+v", record.Checkpoint)
	}
	if record.Checkpoint.RealElapsed < 45*time.Second {
		t.Fatalf("real elapsed = %s, want at least time since tournament start", record.Checkpoint.RealElapsed)
	}
	events, err := reopened.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Kind != "clock_recovered_paused" {
		t.Fatalf("recovery audit history = %+v", events)
	}
	if events[1].ClockElapsed == nil || *events[1].ClockElapsed != 7*time.Second {
		t.Fatalf("recovery audit clock elapsed = %v", events[1].ClockElapsed)
	}
}

func TestOpenDoesNotInitializeUnrelatedSQLiteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.db")
	// The SQLite application ID remains zero for a database not owned by this app.
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path); err != ErrNotTournamentDatabase {
		t.Fatalf("open unrelated database error = %v, want ErrNotTournamentDatabase", err)
	}
	reader, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var version int
	if err := reader.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Fatalf("opening unrelated database changed user_version to %d", version)
	}
}

func TestOpenEndedTournamentLeavesItReadOnly(t *testing.T) {
	ctx := context.Background()
	store := createTestStore(t)
	path := store.Path()
	endedAt := time.Now().Add(-time.Hour)
	startedAt := endedAt.Add(-time.Hour)
	if _, err := store.db.ExecContext(ctx, `UPDATE tournament SET status = ?, started_at = ?, ended_at = ?,
		result_completed = 0, real_elapsed_ns = 0 WHERE id = 1`, StatusEnded, FormatWallTime(startedAt), FormatWallTime(endedAt)); err != nil {
		t.Fatal(err)
	}
	before, err := store.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := store.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eventsAfter, err := reopened.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != StatusEnded || after.Checkpoint.RealElapsed != before.Checkpoint.RealElapsed || !after.UpdatedAt.Equal(before.UpdatedAt) || len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("opening ended tournament mutated it: before=%+v after=%+v events=%d/%d", before, after, len(eventsBefore), len(eventsAfter))
	}
}
