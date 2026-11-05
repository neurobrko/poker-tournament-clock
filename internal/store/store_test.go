package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	_ "modernc.org/sqlite"
)

func TestCreateOpenLockAndExternalReadOnlyReport(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tournament.db")
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	store, err := Create(ctx, path, "Friday game", template)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, path); !errors.Is(err, ErrDatabaseInUse) {
		t.Fatalf("second app open error = %v, want ErrDatabaseInUse", err)
	}
	if _, err := Create(ctx, path, "Overwrite attempt", template); !errors.Is(err, ErrDatabaseExists) {
		t.Fatalf("create existing path error = %v, want ErrDatabaseExists", err)
	}

	// The application lock must not prevent ordinary read-only SQLite reports.
	reader, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := reader.QueryRowContext(ctx, "SELECT name FROM tournament WHERE id = 1").Scan(&name); err != nil {
		t.Fatalf("read tournament through external read-only connection: %v", err)
	}
	if name != "Friday game" {
		t.Fatalf("read-only report name = %q", name)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	record, err := store.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Name != "Friday game" || record.Status != StatusNotStarted {
		t.Fatalf("initial tournament record = %+v", record)
	}
	if record.Checkpoint.SegmentIndex != 0 || record.Checkpoint.Remaining != 20*time.Minute {
		t.Fatalf("initial checkpoint = %+v", record.Checkpoint)
	}
	events, err := store.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "tournament_created" {
		t.Fatalf("initial audit events = %+v", events)
	}
	if events[0].ClockElapsed != nil || events[0].RealElapsed != nil {
		t.Fatal("pre-start tournament creation event should have null relative times")
	}
	if events[0].WallTime.Format(time.RFC3339Nano) == "" {
		t.Fatal("creation event has no wall timestamp")
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("open after lock release: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDatabasePathWithURICharacters(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tournament # 1.db")
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	store, err := Create(ctx, path, "URI path", template)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("open database with URI-special path characters: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTournamentRenameIsAuditedAndLockedAfterStart(t *testing.T) {
	ctx := context.Background()
	database := createTestStore(t)
	defer database.Close()

	if err := database.RenameTournament(ctx, "  Saturday  ", time.Time{}); err != nil {
		t.Fatal(err)
	}
	record, err := database.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Name != "Saturday" {
		t.Fatalf("renamed tournament = %q", record.Name)
	}

	if _, err := database.db.ExecContext(ctx, "UPDATE tournament SET status = ? WHERE id = 1", StatusPaused); err != nil {
		t.Fatal(err)
	}
	before, err := database.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.RenameTournament(ctx, "Sunday", time.Now()); !errors.Is(err, ErrConfigurationLocked) {
		t.Fatalf("rename after start error = %v, want ErrConfigurationLocked", err)
	}
	after, err := database.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("rejected rename left audit event behind: before=%d after=%d", len(before), len(after))
	}
}

func TestCommitEventAtomicallyPersistsCheckpointAndMutation(t *testing.T) {
	ctx := context.Background()
	store := createTestStore(t)
	defer store.Close()

	clockElapsed, realElapsed := 3*time.Second, 9*time.Second
	if _, err := store.CommitEvent(ctx, EventInput{Kind: "missing_checkpoint", ClockElapsed: &clockElapsed, RealElapsed: &realElapsed}, nil, nil); err == nil {
		t.Fatal("CommitEvent accepted elapsed times without an atomic checkpoint")
	}
	checkpoint := Checkpoint{ClockElapsed: clockElapsed, RealElapsed: realElapsed, SegmentIndex: 1, Remaining: 17 * time.Minute}
	id, err := store.CommitEvent(ctx, EventInput{
		Kind: "player_registered", Payload: map[string]string{"player_id": "p1"},
		ClockElapsed: &clockElapsed, RealElapsed: &realElapsed,
	}, &checkpoint, func(tx *sql.Tx, eventID int64) error {
		if eventID <= 1 {
			return fmt.Errorf("unexpected event ID %d", eventID)
		}
		_, err := tx.ExecContext(ctx, "UPDATE tournament SET registration_closed = 1 WHERE id = 1")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if id <= 1 {
		t.Fatalf("new event ID = %d", id)
	}
	record, err := store.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Checkpoint != checkpoint || !record.RegistrationClosed {
		t.Fatalf("checkpoint or domain mutation not committed: checkpoint=%+v closed=%v", record.Checkpoint, record.RegistrationClosed)
	}
	events, err := store.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].ClockElapsed == nil || *events[1].ClockElapsed != clockElapsed {
		t.Fatalf("committed event = %+v", events)
	}

	lower := Checkpoint{ClockElapsed: 2 * time.Second, RealElapsed: 10 * time.Second, SegmentIndex: 1, Remaining: 16 * time.Minute}
	if _, err := store.CommitEvent(ctx, EventInput{Kind: "bad_checkpoint"}, &lower, nil); !errors.Is(err, ErrCheckpointRegression) {
		t.Fatalf("regressing checkpoint error = %v", err)
	}
	unchanged, err := store.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged) != len(events) {
		t.Fatal("failed checkpoint transaction left an audit event")
	}

	badMutation := errors.New("simulated domain write failure")
	later := Checkpoint{ClockElapsed: 4 * time.Second, RealElapsed: 10 * time.Second, SegmentIndex: 1, Remaining: 16 * time.Minute}
	if _, err := store.CommitEvent(ctx, EventInput{Kind: "rolled_back"}, &later, func(*sql.Tx, int64) error { return badMutation }); !errors.Is(err, badMutation) {
		t.Fatalf("mutation failure = %v", err)
	}
	unchanged, err = store.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged) != len(events) {
		t.Fatal("failed domain mutation left an audit event")
	}
	record, err = store.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Checkpoint != checkpoint {
		t.Fatalf("failed transaction changed checkpoint: %+v", record.Checkpoint)
	}
}

func TestCheckpointCannotRegress(t *testing.T) {
	ctx := context.Background()
	store := createTestStore(t)
	defer store.Close()
	if err := store.SaveCheckpoint(ctx, Checkpoint{ClockElapsed: time.Second, RealElapsed: 2 * time.Second, SegmentIndex: 1, Remaining: 19 * time.Minute}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCheckpoint(ctx, Checkpoint{ClockElapsed: time.Second, RealElapsed: time.Second, SegmentIndex: 1, Remaining: 19 * time.Minute}, time.Time{}); !errors.Is(err, ErrCheckpointRegression) {
		t.Fatalf("regressing checkpoint error = %v", err)
	}
}

func TestCrashReleasesApplicationLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crash.db")
	cmd := exec.Command(os.Args[0], "-test.run=^TestApplicationLockCrashHelper$")
	cmd.Env = append(os.Environ(), "POKER_CLOCK_LOCK_HELPER=1", "POKER_CLOCK_LOCK_PATH="+path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lock helper process: %v\n%s", err, output)
	}
	lock, err := acquireLock(path)
	if err != nil {
		t.Fatalf("acquire lock after process exit: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestApplicationLockCrashHelper is run in a subprocess that exits without unlocking.
func TestApplicationLockCrashHelper(t *testing.T) {
	if os.Getenv("POKER_CLOCK_LOCK_HELPER") != "1" {
		return
	}
	lock, err := acquireLock(os.Getenv("POKER_CLOCK_LOCK_PATH"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	_ = lock
	os.Exit(0)
}

func TestOpenRejectsUninitializedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.db")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), path)
	if err == nil || (!errors.Is(err, ErrNotTournamentDatabase) && !strings.Contains(err.Error(), "no such table")) {
		t.Fatalf("open empty database error = %v", err)
	}
}

func createTestStore(t *testing.T) *Store {
	t.Helper()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	store, err := Create(context.Background(), filepath.Join(t.TempDir(), "test.db"), "Test tournament", template)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
