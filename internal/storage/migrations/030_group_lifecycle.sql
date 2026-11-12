-- Optional session stickiness keeps new conversations and subagents on one upstream resource.
ALTER TABLE account_groups ADD COLUMN session_sticky INTEGER NOT NULL DEFAULT 0 CHECK(session_sticky IN (0,1));

-- Deleted groups are retired rather than removed so request history and usage aggregates keep their workspace scope.
ALTER TABLE account_groups ADD COLUMN deleted_at INTEGER;
