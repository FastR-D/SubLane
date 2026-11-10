ALTER TABLE group_accounts ADD COLUMN preference_boost INTEGER NOT NULL DEFAULT 0
CHECK(preference_boost BETWEEN 0 AND 300);
