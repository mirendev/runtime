-- Write your migrate up statements here

CREATE TABLE IF NOT EXISTS organizations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    name TEXT NOT NULL,
    external_id TEXT NOT NULL,

    UNIQUE(name),
    UNIQUE(external_id)
);

CREATE TABLE applications (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    xid TEXT NOT NULL,
    name TEXT NOT NULL,

    UNIQUE(xid),
    UNIQUE(organization_id, name)
);

CREATE TABLE application_versions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    application_id BIGINT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    xid TEXT NOT NULL,
    version TEXT NOT NULL,
    static_dir TEXT,
    configuration JSONB,

    UNIQUE(xid),
    UNIQUE(application_id, version)
);

INSERT INTO organizations (name, external_id) VALUES ('local', 'local');

---- create above / drop below ----

DROP TABLE application_versions;
DROP TABLE applications;
DROP TABLE organizations;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
