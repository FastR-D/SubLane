-- name: SaveSchedulingSample :exec
INSERT INTO scheduling_samples(tenant_id,account_id,group_id,bucket,kind,scope,revision,observed_at,payload)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(tenant_id,account_id,group_id,bucket,kind,scope,revision) DO UPDATE SET
 observed_at=excluded.observed_at,payload=excluded.payload;

-- name: GetSchedulingSample :one
SELECT payload FROM scheduling_samples WHERE tenant_id=? AND account_id=? AND group_id=? AND bucket=? AND kind=? AND scope=? AND revision=?;

-- name: MarkSchedulingCollection :exec
UPDATE tenants SET scheduling_started_at=CASE WHEN scheduling_started_at=0 THEN sqlc.arg(now) ELSE scheduling_started_at END,
scheduling_last_flush=sqlc.arg(now),scheduling_dropped=scheduling_dropped+sqlc.arg(dropped) WHERE id=sqlc.arg(tenant_id);

-- name: GetSchedulingCoverage :one
SELECT scheduling_started_at,scheduling_last_flush,scheduling_dropped FROM tenants WHERE id=?;

-- name: ListSchedulingSamples :many
SELECT s.payload FROM scheduling_samples s WHERE s.tenant_id=sqlc.arg(tenant_id) AND s.bucket>=sqlc.arg(since)
AND (s.group_id=sqlc.arg(group_id) OR (s.group_id=0 AND EXISTS(SELECT 1 FROM group_accounts ga WHERE ga.group_id=sqlc.arg(group_id) AND ga.account_id=s.account_id)))
AND (sqlc.arg(account_id)='' OR s.account_id=sqlc.arg(account_id))
ORDER BY s.bucket DESC,s.account_id,s.kind LIMIT 2001;

-- name: RecordSchedulingUsage :exec
INSERT INTO scheduling_usage(tenant_id,group_id,account_id,hour,requests,completed,errors,limited,duration_ms,wait_ms,input_tokens,output_tokens,input_reported,output_reported,first_output_ms,first_output_reported)
VALUES(?,?,?,?,1,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(tenant_id,group_id,account_id,hour) DO UPDATE SET
requests=requests+1,completed=completed+excluded.completed,errors=errors+excluded.errors,limited=limited+excluded.limited,
duration_ms=duration_ms+excluded.duration_ms,wait_ms=wait_ms+excluded.wait_ms,
input_tokens=input_tokens+excluded.input_tokens,output_tokens=output_tokens+excluded.output_tokens,
input_reported=input_reported+excluded.input_reported,output_reported=output_reported+excluded.output_reported,
first_output_ms=first_output_ms+excluded.first_output_ms,first_output_reported=first_output_reported+excluded.first_output_reported;

-- name: ListSchedulingUsage :many
SELECT account_id,hour,requests,completed,errors,limited,duration_ms,wait_ms,input_tokens,output_tokens,input_reported,output_reported,first_output_ms,first_output_reported
FROM scheduling_usage WHERE tenant_id=sqlc.arg(tenant_id) AND group_id=sqlc.arg(group_id) AND hour>=sqlc.arg(since)
AND (sqlc.arg(account_id)='' OR account_id=sqlc.arg(account_id)) ORDER BY hour DESC,account_id LIMIT 2001;

-- name: PruneSchedulingSamples :execrows
DELETE FROM scheduling_samples WHERE rowid IN (SELECT old.rowid FROM scheduling_samples old WHERE old.bucket<sqlc.arg(short_before) AND (old.kind!='placement' OR old.bucket<sqlc.arg(long_before)) LIMIT 500);

-- name: PruneSchedulingUsage :execrows
DELETE FROM scheduling_usage WHERE rowid IN (SELECT old.rowid FROM scheduling_usage old WHERE old.hour < sqlc.arg(before_hour) LIMIT 500);

-- name: PreviousSchedulingQuota :one
SELECT payload FROM scheduling_samples WHERE tenant_id=? AND account_id=? AND kind='quota' AND revision=? AND observed_at<? AND json_extract(payload,'$.scope')=? ORDER BY observed_at DESC LIMIT 1;

-- name: SchedulingWindowCoverage :one
SELECT count(*) AS observations,CAST(COALESCE(MIN(observed_at),0) AS INTEGER) AS first_at FROM scheduling_samples
WHERE tenant_id=? AND account_id=? AND kind='quota' AND revision=? AND scope=?;

-- name: SchedulingWindowPolicy :one
SELECT CAST(COALESCE(MAX(revision),0) AS INTEGER) AS revision,
CAST(COALESCE(SUM(CASE WHEN created_at>=sqlc.arg(start_at) THEN 1 ELSE 0 END),0) AS INTEGER) AS changes
FROM scheduling_revisions WHERE tenant_id=sqlc.arg(tenant_id) AND created_at<=sqlc.arg(reset_at);

-- name: InsertSchedulingCycle :exec
INSERT OR IGNORE INTO scheduling_cycles(tenant_id,account_id,scope,lifecycle,reset_at,window_seconds,remaining,observation_age,observations,complete,policy_revision) VALUES(?,?,?,?,?,?,?,?,?,?,?);

-- name: ListSchedulingCycles :many
SELECT c.account_id,c.lifecycle,c.scope,c.reset_at,c.window_seconds,c.remaining,c.observation_age,c.observations,c.complete,c.policy_revision
FROM scheduling_cycles c WHERE c.tenant_id=sqlc.arg(tenant_id) AND c.reset_at>=sqlc.arg(since)
AND EXISTS(SELECT 1 FROM group_accounts ga JOIN accounts a ON a.id=ga.account_id WHERE ga.group_id=sqlc.arg(group_id) AND ga.account_id=c.account_id AND c.lifecycle=a.models_revision+a.usage_revision)
AND (sqlc.arg(account_id)='' OR c.account_id=sqlc.arg(account_id)) ORDER BY c.reset_at DESC LIMIT 500;

-- name: PruneSchedulingCycles :execrows
DELETE FROM scheduling_cycles WHERE rowid IN (SELECT old.rowid FROM scheduling_cycles old WHERE old.reset_at<sqlc.arg(before_time) LIMIT 500);
