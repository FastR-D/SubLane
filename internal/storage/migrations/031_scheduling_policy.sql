ALTER TABLE tenants ADD COLUMN scheduling_policy TEXT NOT NULL DEFAULT '{}' CHECK(length(scheduling_policy)<=2048 AND json_valid(scheduling_policy));
ALTER TABLE tenants ADD COLUMN scheduling_revision INTEGER NOT NULL DEFAULT 0 CHECK(scheduling_revision>=0);
CREATE TABLE scheduling_revisions (
 tenant_id INTEGER NOT NULL REFERENCES tenants(id),
 revision INTEGER NOT NULL,
 policy TEXT NOT NULL CHECK(length(policy)<=2048 AND json_valid(policy)),
 created_at INTEGER NOT NULL,
 PRIMARY KEY(tenant_id,revision)
);
