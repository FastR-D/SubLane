-- name: CountAccounts :one
SELECT count(*) FROM accounts WHERE tenant_id = sqlc.arg(tenant_id);

-- name: CountAllAccounts :one
SELECT count(*) FROM accounts;

-- name: ListAccounts :many
SELECT id, provider, name, email, plan, enabled, status, expires_at, created_at, updated_at, max_concurrency, proxy_id,
 (SELECT count(*) FROM group_accounts ga WHERE ga.account_id=accounts.id) AS group_count
FROM accounts WHERE tenant_id = sqlc.arg(tenant_id) ORDER BY created_at DESC, id DESC LIMIT 100;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id);

-- name: CreateAccount :execrows
INSERT INTO accounts(id, tenant_id, provider, name, account_id, email, plan, enabled, status, credential, expires_at, created_at, updated_at, proxy_id)
VALUES (sqlc.arg(id), sqlc.arg(tenant_id), sqlc.arg(provider), sqlc.arg(name), sqlc.arg(account_id), sqlc.arg(email), sqlc.arg(plan), 1, sqlc.arg(status), sqlc.arg(credential), sqlc.arg(expires_at), sqlc.arg(created_at), sqlc.arg(updated_at), sqlc.narg(proxy_id))
ON CONFLICT(tenant_id, provider, account_id) DO NOTHING;

-- name: UpdateAccountCredential :exec
UPDATE accounts SET account_id = sqlc.arg(account_id), credential = sqlc.arg(credential), email = sqlc.arg(email), plan = sqlc.arg(plan), status = sqlc.arg(status), expires_at = sqlc.arg(expires_at), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: SetAccountEnabled :exec
UPDATE accounts SET enabled = sqlc.arg(enabled), updated_at = sqlc.arg(updated_at),
models_revision = models_revision + CASE WHEN enabled != sqlc.arg(enabled) THEN 1 ELSE 0 END
WHERE id = sqlc.arg(id);

-- name: SetAccountStatus :exec
UPDATE accounts SET status = sqlc.arg(status), updated_at = sqlc.arg(updated_at) WHERE id = sqlc.arg(id);

-- name: DeleteAccount :execrows
DELETE FROM accounts WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id);

-- name: RenameAccount :exec
UPDATE accounts SET name = sqlc.arg(name), updated_at = sqlc.arg(updated_at) WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id);
