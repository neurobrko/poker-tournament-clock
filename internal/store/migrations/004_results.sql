CREATE TABLE tournament_ends (
    event_id INTEGER PRIMARY KEY REFERENCES audit_events(id),
    completed INTEGER NOT NULL CHECK (completed IN (0, 1)),
    prize_pool_cents INTEGER NOT NULL CHECK (prize_pool_cents >= 0 AND prize_pool_cents % 10 = 0),
    explanation TEXT
);

CREATE TABLE end_prize_groups (
    end_event_id INTEGER NOT NULL REFERENCES tournament_ends(event_id),
    group_order INTEGER NOT NULL CHECK (group_order > 0),
    first_place INTEGER NOT NULL CHECK (first_place > 0),
    last_place INTEGER NOT NULL CHECK (last_place >= first_place),
    total_cents INTEGER NOT NULL CHECK (total_cents >= 0 AND total_cents % 10 = 0),
    pending_remainder_cents INTEGER NOT NULL DEFAULT 0 CHECK (pending_remainder_cents >= 0 AND pending_remainder_cents % 10 = 0),
    PRIMARY KEY (end_event_id, group_order)
);

CREATE TABLE end_prize_players (
    end_event_id INTEGER NOT NULL,
    group_order INTEGER NOT NULL,
    player_id TEXT NOT NULL REFERENCES players(id),
    amount_cents INTEGER NOT NULL CHECK (amount_cents >= 0 AND amount_cents % 10 = 0),
    gets_remainder INTEGER NOT NULL DEFAULT 0 CHECK (gets_remainder IN (0, 1)),
    PRIMARY KEY (end_event_id, group_order, player_id),
    FOREIGN KEY (end_event_id, group_order) REFERENCES end_prize_groups(end_event_id, group_order)
);

PRAGMA user_version = 4;
