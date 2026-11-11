-- name: ListGroups :many
SELECT g.*, (SELECT count(*) FROM group_accounts a WHERE a.group_id=g.id) AS account_count,
(SELECT count(*) FROM group_accounts ga JOIN accounts a ON a.id=ga.account_id WHERE ga.group_id=g.id AND a.provider!='openai') AS subscription_count,
(SELECT count(*) FROM group_accounts ga JOIN accounts a ON a.id=ga.account_id WHERE ga.group_id=g.id AND a.provider='openai') AS channel_count,
(SELECT count(*) FROM effective_group_access access WHERE access.group_id=g.id) AS member_count
FROM account_groups g WHERE g.tenant_id=sqlc.arg(tenant_id) AND g.deleted_at IS NULL ORDER BY g.id;

-- name: GetGroup :one
SELECT * FROM account_groups WHERE id=sqlc.arg(id);

-- name: GetTenantGroup :one
SELECT * FROM account_groups WHERE id=sqlc.arg(id) AND tenant_id=sqlc.arg(tenant_id) AND deleted_at IS NULL;

-- name: CountGroups :one
SELECT count(*) FROM account_groups WHERE tenant_id=sqlc.arg(tenant_id) AND deleted_at IS NULL;

-- name: CreateGroup :execlastid
INSERT INTO account_groups(tenant_id,name,enabled,created_at,updated_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(name),sqlc.arg(enabled),sqlc.arg(now),sqlc.arg(now));

-- name: FindGroupName :one
SELECT id FROM account_groups WHERE tenant_id=sqlc.arg(tenant_id) AND name=sqlc.arg(name) COLLATE NOCASE AND deleted_at IS NULL;

-- name: UpdateGroup :exec
UPDATE account_groups SET name=sqlc.arg(name),enabled=sqlc.arg(enabled),updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND tenant_id=sqlc.arg(tenant_id);

-- name: ListGroupAccounts :many
SELECT account_id FROM group_accounts WHERE group_id=sqlc.arg(group_id) ORDER BY account_id;

-- name: ClearGroupAccounts :exec
DELETE FROM group_accounts WHERE group_id=sqlc.arg(group_id);

-- name: AddGroupAccount :exec
INSERT INTO group_accounts(group_id,account_id) VALUES(sqlc.arg(group_id),sqlc.arg(account_id));

-- name: GroupAccountExists :one
SELECT EXISTS(SELECT 1 FROM accounts WHERE id=sqlc.arg(account_id) AND tenant_id=sqlc.arg(tenant_id));

-- name: ListMemberGroups :many
SELECT m.group_id FROM group_members m JOIN account_groups g ON g.id=m.group_id
WHERE m.user_id=sqlc.arg(user_id) AND g.tenant_id=sqlc.arg(tenant_id) AND g.deleted_at IS NULL ORDER BY m.group_id;

-- name: ClearMemberGroups :exec
DELETE FROM group_members WHERE user_id=sqlc.arg(user_id)
AND group_id IN (SELECT id FROM account_groups WHERE tenant_id=sqlc.arg(tenant_id));

-- name: MemberExistsInTenant :one
SELECT EXISTS(SELECT 1 FROM memberships m
WHERE m.tenant_id=sqlc.arg(tenant_id) AND m.user_id=sqlc.arg(user_id) AND m.removed_at IS NULL);

-- name: GrantMemberGroup :exec
INSERT INTO group_members(group_id,user_id) VALUES(sqlc.arg(group_id),sqlc.arg(user_id));

-- name: CanUseGroup :one
SELECT EXISTS(SELECT 1 FROM account_groups g JOIN users u ON u.id=sqlc.arg(user_id)
WHERE g.id=sqlc.arg(group_id) AND g.enabled=1 AND u.enabled=1
AND EXISTS(SELECT 1 FROM effective_group_access access WHERE access.group_id=g.id AND access.user_id=u.id));

-- name: ListAvailableGroups :many
SELECT g.id,g.name,(SELECT count(*) FROM group_accounts ga JOIN accounts a ON a.id=ga.account_id WHERE ga.group_id=g.id AND a.provider IN ('codex','claude','antigravity','xai','openai')) AS account_count
FROM account_groups g JOIN users u ON u.id=sqlc.arg(user_id)
WHERE g.tenant_id=sqlc.arg(tenant_id) AND g.enabled=1 AND u.enabled=1
AND EXISTS(SELECT 1 FROM effective_group_access access WHERE access.group_id=g.id AND access.user_id=u.id) ORDER BY g.id;

-- name: GroupConnectionStatus :one
SELECT CASE WHEN EXISTS(
 SELECT 1 FROM routable_group_resources ga JOIN accounts a ON a.id=ga.account_id JOIN account_groups g ON g.id=ga.group_id JOIN users u ON u.id=sqlc.arg(user_id)
 WHERE g.tenant_id=sqlc.arg(tenant_id) AND g.enabled=1 AND a.enabled=1 AND a.provider IN ('codex','claude','antigravity','xai','openai') AND a.status='ready' AND u.enabled=1 AND EXISTS(SELECT 1 FROM effective_group_access access WHERE access.group_id=g.id AND access.user_id=u.id)
) THEN 'ready' WHEN EXISTS(
 SELECT 1 FROM routable_group_resources ga JOIN accounts a ON a.id=ga.account_id JOIN account_groups g ON g.id=ga.group_id JOIN users u ON u.id=sqlc.arg(user_id)
 WHERE g.tenant_id=sqlc.arg(tenant_id) AND g.enabled=1 AND a.enabled=1 AND a.provider IN ('codex','claude','antigravity','xai','openai') AND u.enabled=1 AND EXISTS(SELECT 1 FROM effective_group_access access WHERE access.group_id=g.id AND access.user_id=u.id)
) THEN 'needs_attention' ELSE 'not_configured' END AS status;

-- name: CountGroupMembers :one
SELECT count(*) FROM effective_group_access access
WHERE access.group_id=sqlc.arg(group_id);

-- name: ListPoolMembers :many
-- Owners and administrators use enabled pools through their role, without a direct grant.
SELECT u.id,u.username FROM effective_group_access access
JOIN users u ON u.id=access.user_id
JOIN account_groups g ON g.id=access.group_id
JOIN memberships m ON m.tenant_id=g.tenant_id AND m.user_id=access.user_id
WHERE access.group_id=sqlc.arg(group_id)
ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END,u.username,u.id LIMIT 100;

-- name: SetGroupModelPolicy :exec
UPDATE account_groups SET restricted_models=sqlc.arg(restricted) WHERE id=sqlc.arg(id);

-- name: ClearGroupModels :exec
DELETE FROM group_models WHERE group_id=sqlc.arg(group_id);

-- name: AddGroupModel :exec
INSERT INTO group_models(group_id,model) VALUES(sqlc.arg(group_id),sqlc.arg(model));

-- name: ListGroupModels :many
SELECT model FROM group_models WHERE group_id=sqlc.arg(group_id) ORDER BY model;

-- name: SetGroupRouting :exec
UPDATE account_groups SET routing_preference=sqlc.arg(preference),allow_api_fallback=sqlc.arg(allow_api_fallback),session_sticky=sqlc.arg(session_sticky)
WHERE id=sqlc.arg(id);

-- name: GroupHasSubscriptions :one
SELECT EXISTS(SELECT 1 FROM group_accounts ga JOIN accounts a ON a.id=ga.account_id
WHERE ga.group_id=sqlc.arg(group_id) AND a.provider!='openai');

-- name: GroupActiveKeyCount :one
SELECT count(*) FROM api_keys WHERE group_id=sqlc.arg(group_id) AND revoked_at IS NULL;

-- name: GroupHasAllocation :one
SELECT EXISTS(SELECT 1 FROM allocation_schemes WHERE group_id=sqlc.arg(group_id));

-- name: ClearGroupMembers :exec
DELETE FROM group_members WHERE group_id=sqlc.arg(group_id);

-- name: ClearGroupAffinity :exec
DELETE FROM account_affinity WHERE group_id=sqlc.arg(group_id);

-- name: RetireGroup :execrows
-- Retired groups keep their names; uniqueness only applies to active groups.
UPDATE account_groups SET enabled=0,deleted_at=sqlc.arg(now),updated_at=sqlc.arg(now)
WHERE id=sqlc.arg(id) AND tenant_id=sqlc.arg(tenant_id) AND deleted_at IS NULL;
