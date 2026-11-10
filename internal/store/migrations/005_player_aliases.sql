ALTER TABLE players ADD COLUMN alias TEXT NOT NULL DEFAULT '' CHECK (length(alias) <= 80);

PRAGMA user_version = 5;
