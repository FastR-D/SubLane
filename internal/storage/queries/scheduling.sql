-- name: GetSchedulingPolicy :one
SELECT scheduling_policy,scheduling_revision FROM tenants WHERE id=?;

-- name: SaveSchedulingPolicy :execrows
UPDATE tenants SET scheduling_policy=sqlc.arg(policy),scheduling_revision=scheduling_revision+1
WHERE id=sqlc.arg(tenant_id) AND scheduling_revision=sqlc.arg(revision);

-- name: InsertSchedulingRevision :exec
INSERT INTO scheduling_revisions(tenant_id,revision,policy,created_at) VALUES(?,?,?,?);

-- name: ListSchedulingRevisions :many
SELECT revision,policy,created_at FROM scheduling_revisions WHERE tenant_id=? ORDER BY revision DESC LIMIT 50;
