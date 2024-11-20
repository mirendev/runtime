-- Write your migrate up statements here

CREATE TABLE applications (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    name TEXT NOT NULL,

    UNIQUE(name)
);

CREATE TABLE application_versions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    application_id BIGINT NOT NULL REFERENCES applications(id),
    version TEXT NOT NULL,
    static_dir TEXT,

    UNIQUE(application_id, version)
);

---- create above / drop below ----

DROP TABLE applications;
DROP TABLE application_versions;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
