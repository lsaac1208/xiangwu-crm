-- +kxone dialect sqlite
CREATE TABLE IF NOT EXISTS xiangwu_form_submissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    lead_id integer NOT NULL,
    source varchar(32) NOT NULL,
    external_lead_id varchar(36) NOT NULL,
    payload_hash varchar(64) NOT NULL,
    details text NOT NULL,
    source_path varchar(512) NOT NULL,
    compatibility_request boolean NOT NULL DEFAULT false,
    created_at datetime NOT NULL,
    UNIQUE(source, external_lead_id),
    UNIQUE(lead_id)
);
CREATE INDEX IF NOT EXISTS idx_xiangwu_form_submissions_created_at ON xiangwu_form_submissions(created_at);

-- +kxone dialect mysql
CREATE TABLE IF NOT EXISTS xiangwu_form_submissions (
    id bigint PRIMARY KEY AUTO_INCREMENT,
    lead_id bigint NOT NULL,
    source varchar(32) NOT NULL,
    external_lead_id varchar(36) NOT NULL,
    payload_hash varchar(64) NOT NULL,
    details text NOT NULL,
    source_path varchar(512) NOT NULL,
    compatibility_request boolean NOT NULL DEFAULT false,
    created_at datetime NOT NULL,
    UNIQUE KEY idx_xiangwu_form_source_external (source, external_lead_id),
    UNIQUE KEY idx_xiangwu_form_lead (lead_id),
    KEY idx_xiangwu_form_created (created_at)
);
