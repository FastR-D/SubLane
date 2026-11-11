-- Derived session-stickiness preferences are tracked separately so they can be capped and evicted
-- before they ever block an explicit conversation binding.
ALTER TABLE account_affinity ADD COLUMN derived INTEGER NOT NULL DEFAULT 0 CHECK(derived IN (0,1));
