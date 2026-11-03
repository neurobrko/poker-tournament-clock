CREATE TABLE bust_participant_corrections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bust_event_id INTEGER NOT NULL REFERENCES busts(event_id),
    correction_event_id INTEGER NOT NULL REFERENCES audit_events(id),
    supersedes_player_id TEXT NOT NULL REFERENCES players(id),
    supersedes_role TEXT NOT NULL CHECK (supersedes_role IN ('buster', 'bustee')),
    player_id TEXT REFERENCES players(id),
    role TEXT CHECK (role IN ('buster', 'bustee')),
    finishing_order INTEGER,
    tied INTEGER CHECK (tied IN (0, 1)),
    voided_by_event_id INTEGER REFERENCES audit_events(id),
    CHECK (
        (player_id IS NULL AND role IS NULL AND finishing_order IS NULL AND tied IS NULL)
        OR
        (player_id IS NOT NULL AND role IS NOT NULL AND finishing_order > 0 AND tied IS NOT NULL)
    )
);
CREATE UNIQUE INDEX one_active_participant_correction
    ON bust_participant_corrections(bust_event_id, supersedes_player_id, supersedes_role);
CREATE INDEX bust_corrections_by_player ON bust_participant_corrections(player_id, bust_event_id);

PRAGMA user_version = 3;
