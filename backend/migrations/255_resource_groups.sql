-- Administrative resource organization. These groups do not change existing
-- API routing, Drive placement, user quotas or Colab scheduling.
CREATE TABLE resource_groups (
 id BIGSERIAL PRIMARY KEY,
 kind TEXT NOT NULL CHECK (kind IN ('storage','compute')),
 name VARCHAR(100) NOT NULL CHECK (length(btrim(name)) > 0),
 description VARCHAR(1000) NOT NULL DEFAULT '',
 version BIGINT NOT NULL DEFAULT 1,
 archived_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE (id,kind)
);
CREATE UNIQUE INDEX resource_groups_active_name ON resource_groups(kind,lower(name)) WHERE archived_at IS NULL;
CREATE TABLE resource_group_members (
 group_id BIGINT NOT NULL,
 kind TEXT NOT NULL,
 storage_account_id BIGINT REFERENCES drive_storage_accounts(id) ON DELETE CASCADE,
 compute_account_id BIGINT REFERENCES colab_accounts(id) ON DELETE CASCADE,
 FOREIGN KEY (group_id,kind) REFERENCES resource_groups(id,kind) ON DELETE CASCADE,
 CHECK ((kind='storage' AND storage_account_id IS NOT NULL AND compute_account_id IS NULL)
     OR (kind='compute' AND compute_account_id IS NOT NULL AND storage_account_id IS NULL))
);
CREATE UNIQUE INDEX resource_group_storage_member ON resource_group_members(group_id,storage_account_id) WHERE storage_account_id IS NOT NULL;
CREATE UNIQUE INDEX resource_group_compute_member ON resource_group_members(group_id,compute_account_id) WHERE compute_account_id IS NOT NULL;
INSERT INTO resource_groups(kind,name,description) VALUES
 ('storage','默认存储组','现有存储账号'),('compute','默认计算组','现有计算账号');
INSERT INTO resource_group_members(group_id,kind,storage_account_id)
 SELECT g.id,g.kind,a.id FROM resource_groups g CROSS JOIN drive_storage_accounts a WHERE g.kind='storage';
INSERT INTO resource_group_members(group_id,kind,compute_account_id)
 SELECT g.id,g.kind,a.id FROM resource_groups g CROSS JOIN colab_accounts a WHERE g.kind='compute';
