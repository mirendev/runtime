-- Write your migrate up statements here

CREATE TABLE IF NOT EXISTS disks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    name TEXT NOT NULL,
    external_id TEXT NOT NULL,
    capacity BIGINT NOT NULL,

    UNIQUE(organization_id, name),
    UNIQUE(external_id)
);

CREATE TABLE IF NOT EXISTS addon_instances (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    addon_name TEXT NOT NULL,
    addon_plan TEXT NOT NULL,
    container_id TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',

    UNIQUE(external_id),
    UNIQUE(container_id)
);

CREATE TABLE IF NOT EXISTS addon_instance_disks (
    addon_instance_id BIGINT NOT NULL REFERENCES addon_instances(id) ON DELETE CASCADE,
    disk_id BIGINT NOT NULL REFERENCES disks(id) ON DELETE CASCADE,

    UNIQUE(addon_instance_id, disk_id)
);

CREATE TABLE IF NOT EXISTS addon_instance_attachment (
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    name TEXT NOT NULL,
    addon_instance_id BIGINT NOT NULL REFERENCES addon_instances(id) ON DELETE CASCADE,
    application_id BIGINT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,

    UNIQUE(name, application_id),
    UNIQUE(addon_instance_id, application_id)
);

---- create above / drop below ----

DROP TABLE addon_instance_attachment;
DROP TABLE addon_instance_disks;
DROP TABLE addon_instances;
DROP TABLE disks;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
