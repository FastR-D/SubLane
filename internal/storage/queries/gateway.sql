-- name: CountAccountAffinity :one
SELECT count(*) FROM account_affinity;

-- name: CountDerivedAffinity :one
SELECT count(*) FROM account_affinity WHERE derived = 1;

-- name: EvictDerivedAffinity :execrows
-- Explicit bindings may displace the derived preference closest to expiry.
DELETE FROM account_affinity WHERE rowid = (
  SELECT rowid FROM account_affinity WHERE derived = 1 ORDER BY expires_at, rowid LIMIT 1
);

-- name: PruneAccountAffinity :exec
DELETE FROM account_affinity WHERE expires_at <= sqlc.arg(now);

-- name: CreateAccountAffinity :exec
INSERT INTO account_affinity(user_id, group_id, session_hash, provider, account_id, expires_at, model, rebound, derived) VALUES (sqlc.arg(user_id), sqlc.arg(group_id), sqlc.arg(session_hash), sqlc.arg(provider), sqlc.arg(account_id), sqlc.arg(expires_at), sqlc.arg(model), sqlc.arg(rebound), sqlc.arg(derived));

-- name: TouchAccountAffinity :exec
UPDATE account_affinity SET expires_at = sqlc.arg(expires_at), model = sqlc.arg(model)
WHERE user_id = sqlc.arg(user_id) AND group_id = sqlc.arg(group_id) AND session_hash = sqlc.arg(session_hash) AND provider = sqlc.arg(provider)
AND (expires_at < sqlc.arg(threshold) OR model != sqlc.arg(model));

-- name: ReplaceAccountAffinity :exec
UPDATE account_affinity SET account_id = sqlc.arg(account_id), model = sqlc.arg(model), expires_at = sqlc.arg(expires_at), rebound = rebound OR account_id != sqlc.arg(account_id)
WHERE user_id = sqlc.arg(user_id) AND group_id = sqlc.arg(group_id) AND session_hash = sqlc.arg(session_hash) AND provider = sqlc.arg(provider);

-- name: ListSessionAffinities :many
SELECT provider, account_id, expires_at, model, rebound FROM account_affinity
WHERE user_id = sqlc.arg(user_id) AND group_id = sqlc.arg(group_id)
AND session_hash = sqlc.arg(session_hash) AND expires_at > sqlc.arg(now)
ORDER BY provider;

-- name: ListRoutingAccounts :many
SELECT a.id,a.provider,a.enabled,a.status,a.models_snapshot,a.models_revision,u.snapshot AS usage_snapshot,u.updated_at AS usage_updated_at
FROM accounts a JOIN group_accounts ga ON ga.account_id=a.id
LEFT JOIN account_usage u ON u.account_id=a.id AND u.revision=a.models_revision+a.usage_revision
WHERE a.tenant_id=sqlc.arg(tenant_id) AND ga.group_id=sqlc.arg(group_id)
ORDER BY a.created_at DESC,a.id DESC;

-- name: ListResourceGroups :many
SELECT ga.group_id FROM group_accounts ga JOIN account_groups g ON g.id=ga.group_id
WHERE ga.account_id=sqlc.arg(account_id) AND g.tenant_id=sqlc.arg(tenant_id);
