CREATE TABLE tournament (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    status TEXT NOT NULL CHECK (status IN ('not_started', 'running', 'paused', 'ended')),
    configuration_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    started_at TEXT,
    ended_at TEXT,
    clock_elapsed_ns INTEGER NOT NULL DEFAULT 0 CHECK (clock_elapsed_ns >= 0),
    real_elapsed_ns INTEGER NOT NULL DEFAULT 0 CHECK (real_elapsed_ns >= 0),
    segment_index INTEGER NOT NULL DEFAULT 0 CHECK (segment_index >= 0),
    remaining_ns INTEGER NOT NULL DEFAULT 0 CHECK (remaining_ns >= 0),
    clock_running INTEGER NOT NULL DEFAULT 0 CHECK (clock_running IN (0, 1)),
    registration_closed INTEGER NOT NULL DEFAULT 0 CHECK (registration_closed IN (0, 1)),
    result_completed INTEGER CHECK (result_completed IN (0, 1))
);

CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL CHECK (length(trim(kind)) > 0),
    wall_timestamp TEXT NOT NULL,
    clock_elapsed_ns INTEGER CHECK (clock_elapsed_ns IS NULL OR clock_elapsed_ns >= 0),
    real_elapsed_ns INTEGER CHECK (real_elapsed_ns IS NULL OR real_elapsed_ns >= 0),
    payload_json TEXT NOT NULL,
    explanation TEXT,
    parent_event_id INTEGER REFERENCES audit_events(id)
);
CREATE INDEX audit_events_by_kind ON audit_events(kind, id);

CREATE TABLE players (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    name_key TEXT NOT NULL UNIQUE,
    active INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
    sitting_out INTEGER NOT NULL DEFAULT 0 CHECK (sitting_out IN (0, 1)),
    removed INTEGER NOT NULL DEFAULT 0 CHECK (removed IN (0, 1)),
    created_event_id INTEGER NOT NULL REFERENCES audit_events(id),
    removed_event_id INTEGER REFERENCES audit_events(id)
);

CREATE TABLE purchases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    player_id TEXT NOT NULL REFERENCES players(id),
    kind TEXT NOT NULL CHECK (kind IN ('initial_buy_in', 'rebuy', 'add_on')),
    price_cents INTEGER NOT NULL CHECK (price_cents > 0 AND price_cents % 10 = 0),
    chips INTEGER NOT NULL CHECK (chips > 0),
    event_id INTEGER NOT NULL REFERENCES audit_events(id),
    voided_by_event_id INTEGER REFERENCES audit_events(id)
);
CREATE UNIQUE INDEX one_valid_initial_buy_in_per_player
    ON purchases(player_id)
    WHERE kind = 'initial_buy_in' AND voided_by_event_id IS NULL;
CREATE INDEX purchases_by_player ON purchases(player_id, id);

CREATE TABLE busts (
    event_id INTEGER PRIMARY KEY REFERENCES audit_events(id),
    voided_by_event_id INTEGER REFERENCES audit_events(id)
);
CREATE TABLE bust_participants (
    event_id INTEGER NOT NULL REFERENCES busts(event_id),
    player_id TEXT NOT NULL REFERENCES players(id),
    role TEXT NOT NULL CHECK (role IN ('buster', 'bustee')),
    finishing_order INTEGER NOT NULL CHECK (finishing_order > 0),
    tied INTEGER NOT NULL DEFAULT 0 CHECK (tied IN (0, 1)),
    PRIMARY KEY (event_id, player_id, role),
    UNIQUE (event_id, role, finishing_order, player_id)
);

PRAGMA application_id = 1346587723;
PRAGMA user_version = 1;
