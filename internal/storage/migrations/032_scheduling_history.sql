ALTER TABLE tenants ADD COLUMN scheduling_started_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tenants ADD COLUMN scheduling_last_flush INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tenants ADD COLUMN scheduling_dropped INTEGER NOT NULL DEFAULT 0;
CREATE TABLE scheduling_samples (
 tenant_id INTEGER NOT NULL REFERENCES tenants(id),
 account_id TEXT NOT NULL,
 group_id INTEGER NOT NULL,
 bucket INTEGER NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('pressure','quota','placement')),
 scope TEXT NOT NULL CHECK(length(scope)<=256),
 revision INTEGER NOT NULL,
 observed_at INTEGER NOT NULL,
 payload TEXT NOT NULL CHECK(length(payload)<=2048 AND json_valid(payload)),
 PRIMARY KEY(tenant_id,account_id,group_id,bucket,kind,scope,revision)
);
CREATE INDEX scheduling_samples_range ON scheduling_samples(tenant_id,bucket,group_id);
CREATE TABLE scheduling_usage (
 tenant_id INTEGER NOT NULL REFERENCES tenants(id),
 group_id INTEGER NOT NULL,
 account_id TEXT NOT NULL,
 hour INTEGER NOT NULL,
 requests INTEGER NOT NULL,
 completed INTEGER NOT NULL,
 errors INTEGER NOT NULL,
 limited INTEGER NOT NULL,
 duration_ms INTEGER NOT NULL,
 wait_ms INTEGER NOT NULL,
 input_tokens INTEGER NOT NULL,
 output_tokens INTEGER NOT NULL,
 input_reported INTEGER NOT NULL,
 output_reported INTEGER NOT NULL,
 first_output_ms INTEGER NOT NULL,
 first_output_reported INTEGER NOT NULL,
 PRIMARY KEY(tenant_id,group_id,account_id,hour)
);
CREATE INDEX scheduling_usage_range ON scheduling_usage(tenant_id,hour,group_id);
