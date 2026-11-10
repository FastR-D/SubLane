CREATE INDEX scheduling_samples_observations ON scheduling_samples(tenant_id,account_id,kind,revision,observed_at DESC);
CREATE INDEX scheduling_samples_retention ON scheduling_samples(bucket);
CREATE INDEX scheduling_usage_retention ON scheduling_usage(hour);
CREATE TABLE scheduling_cycles (
 tenant_id INTEGER NOT NULL REFERENCES tenants(id),
 account_id TEXT NOT NULL,
 scope TEXT NOT NULL CHECK(length(scope)<=256),
 lifecycle INTEGER NOT NULL,
 reset_at INTEGER NOT NULL,
 window_seconds INTEGER NOT NULL,
 remaining REAL NOT NULL CHECK(remaining BETWEEN 0 AND 100),
 observation_age INTEGER NOT NULL,
 observations INTEGER NOT NULL,
 complete INTEGER NOT NULL CHECK(complete IN(0,1)),
 policy_revision INTEGER NOT NULL,
 PRIMARY KEY(tenant_id,account_id,scope,lifecycle,reset_at)
);
CREATE INDEX scheduling_cycles_retention ON scheduling_cycles(reset_at);
