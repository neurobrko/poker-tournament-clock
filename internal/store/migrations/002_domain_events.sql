ALTER TABLE audit_events ADD COLUMN voids_event_id INTEGER REFERENCES audit_events(id);
CREATE UNIQUE INDEX audit_event_void_once
    ON audit_events(voids_event_id)
    WHERE voids_event_id IS NOT NULL;

ALTER TABLE tournament ADD COLUMN segment_finished INTEGER NOT NULL DEFAULT 0 CHECK (segment_finished IN (0, 1));
ALTER TABLE tournament ADD COLUMN hold_reason TEXT;
ALTER TABLE tournament ADD COLUMN pending_segment_index INTEGER NOT NULL DEFAULT 0 CHECK (pending_segment_index >= 0);

ALTER TABLE bust_participants ADD COLUMN voided_by_event_id INTEGER REFERENCES audit_events(id);
CREATE INDEX bust_participants_by_player ON bust_participants(player_id, event_id);

CREATE TABLE player_events (
    event_id INTEGER PRIMARY KEY REFERENCES audit_events(id),
    player_id TEXT NOT NULL REFERENCES players(id),
    kind TEXT NOT NULL CHECK (length(trim(kind)) > 0)
);
CREATE INDEX player_events_by_player ON player_events(player_id, event_id);

PRAGMA user_version = 2;
