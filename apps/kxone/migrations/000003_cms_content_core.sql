-- CMS content identity, editable drafts, immutable revisions, review history,
-- and CMS-specific roles. Publishing/media tables are introduced separately.

-- +kxone dialect sqlite
CREATE TABLE IF NOT EXISTS cms_entries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	kind varchar(32) NOT NULL,
	path varchar(512) NOT NULL,
	template_key varchar(64) NOT NULL,
	status varchar(32) NOT NULL,
	published_revision_id integer,
	version integer NOT NULL DEFAULT 0,
	created_by varchar(128) NOT NULL,
	updated_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	CHECK (kind IN ('page', 'article', 'global')),
	CHECK (status IN ('active', 'archived'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cms_entries_path ON cms_entries(path);
CREATE INDEX IF NOT EXISTS idx_cms_entries_kind ON cms_entries(kind);
CREATE INDEX IF NOT EXISTS idx_cms_entries_status ON cms_entries(status);
CREATE INDEX IF NOT EXISTS idx_cms_entries_published_revision_id ON cms_entries(published_revision_id);
CREATE INDEX IF NOT EXISTS idx_cms_entries_created_at ON cms_entries(created_at);
CREATE INDEX IF NOT EXISTS idx_cms_entries_updated_at ON cms_entries(updated_at);

CREATE TABLE IF NOT EXISTS cms_entry_drafts (
	entry_id integer PRIMARY KEY,
	title varchar(255) NOT NULL,
	summary text,
	seo_title varchar(255) NOT NULL,
	seo_description text NOT NULL,
	canonical_url varchar(1024),
	robots varchar(64) NOT NULL,
	cover_media_id integer,
	content_json text NOT NULL,
	content_checksum varchar(64) NOT NULL,
	lock_version integer NOT NULL DEFAULT 0,
	last_edited_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	FOREIGN KEY (entry_id) REFERENCES cms_entries(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_cms_entry_drafts_cover_media_id ON cms_entry_drafts(cover_media_id);
CREATE INDEX IF NOT EXISTS idx_cms_entry_drafts_updated_at ON cms_entry_drafts(updated_at);

CREATE TABLE IF NOT EXISTS cms_revisions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	entry_id integer NOT NULL,
	revision_no integer NOT NULL,
	schema_version integer NOT NULL,
	title varchar(255) NOT NULL,
	summary text,
	seo_title varchar(255) NOT NULL,
	seo_description text NOT NULL,
	canonical_url varchar(1024),
	robots varchar(64) NOT NULL,
	cover_media_id integer,
	content_json text NOT NULL,
	content_checksum varchar(64) NOT NULL,
	workflow_status varchar(32) NOT NULL,
	source_lock_version integer NOT NULL,
	created_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	FOREIGN KEY (entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	UNIQUE (entry_id, revision_no),
	CHECK (workflow_status IN ('draft_snapshot', 'submitted', 'approved', 'rejected'))
);
CREATE INDEX IF NOT EXISTS idx_cms_revisions_entry_id ON cms_revisions(entry_id);
CREATE INDEX IF NOT EXISTS idx_cms_revisions_cover_media_id ON cms_revisions(cover_media_id);
CREATE INDEX IF NOT EXISTS idx_cms_revisions_content_checksum ON cms_revisions(content_checksum);
CREATE INDEX IF NOT EXISTS idx_cms_revisions_workflow_status ON cms_revisions(workflow_status);
CREATE INDEX IF NOT EXISTS idx_cms_revisions_created_at ON cms_revisions(created_at);

CREATE TABLE IF NOT EXISTS cms_review_records (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	revision_id integer NOT NULL,
	action varchar(32) NOT NULL,
	comment text,
	actor_id varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	FOREIGN KEY (revision_id) REFERENCES cms_revisions(id) ON DELETE RESTRICT,
	CHECK (action IN ('submit', 'approve', 'reject'))
);
CREATE INDEX IF NOT EXISTS idx_cms_review_records_revision_id ON cms_review_records(revision_id);
CREATE INDEX IF NOT EXISTS idx_cms_review_records_action ON cms_review_records(action);
CREATE INDEX IF NOT EXISTS idx_cms_review_records_actor_id ON cms_review_records(actor_id);
CREATE INDEX IF NOT EXISTS idx_cms_review_records_created_at ON cms_review_records(created_at);

CREATE TABLE IF NOT EXISTS cms_user_roles (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id integer NOT NULL,
	role varchar(32) NOT NULL,
	granted_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	UNIQUE (user_id, role),
	CHECK (role IN ('cms_admin', 'editor', 'reviewer', 'publisher'))
);
CREATE INDEX IF NOT EXISTS idx_cms_user_roles_user_id ON cms_user_roles(user_id);
CREATE INDEX IF NOT EXISTS idx_cms_user_roles_created_at ON cms_user_roles(created_at);

-- +kxone dialect mysql
CREATE TABLE IF NOT EXISTS cms_entries (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	kind varchar(32) NOT NULL,
	path varchar(512) NOT NULL,
	template_key varchar(64) NOT NULL,
	status varchar(32) NOT NULL,
	published_revision_id BIGINT,
	version int NOT NULL DEFAULT 0,
	created_by varchar(128) NOT NULL,
	updated_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	UNIQUE KEY idx_cms_entries_path (path),
	KEY idx_cms_entries_kind (kind),
	KEY idx_cms_entries_status (status),
	KEY idx_cms_entries_published_revision_id (published_revision_id),
	KEY idx_cms_entries_created_at (created_at),
	KEY idx_cms_entries_updated_at (updated_at),
	CONSTRAINT chk_cms_entry_kind CHECK (kind IN ('page', 'article', 'global')),
	CONSTRAINT chk_cms_entry_status CHECK (status IN ('active', 'archived'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cms_entry_drafts (
	entry_id BIGINT PRIMARY KEY,
	title varchar(255) NOT NULL,
	summary text,
	seo_title varchar(255) NOT NULL,
	seo_description text NOT NULL,
	canonical_url varchar(1024),
	robots varchar(64) NOT NULL,
	cover_media_id BIGINT,
	content_json longtext NOT NULL,
	content_checksum varchar(64) NOT NULL,
	lock_version int NOT NULL DEFAULT 0,
	last_edited_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	KEY idx_cms_entry_drafts_cover_media_id (cover_media_id),
	KEY idx_cms_entry_drafts_updated_at (updated_at),
	CONSTRAINT fk_cms_draft_entry FOREIGN KEY (entry_id) REFERENCES cms_entries(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cms_revisions (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	entry_id BIGINT NOT NULL,
	revision_no int NOT NULL,
	schema_version int NOT NULL,
	title varchar(255) NOT NULL,
	summary text,
	seo_title varchar(255) NOT NULL,
	seo_description text NOT NULL,
	canonical_url varchar(1024),
	robots varchar(64) NOT NULL,
	cover_media_id BIGINT,
	content_json longtext NOT NULL,
	content_checksum varchar(64) NOT NULL,
	workflow_status varchar(32) NOT NULL,
	source_lock_version int NOT NULL,
	created_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	UNIQUE KEY idx_cms_revision_entry_no (entry_id, revision_no),
	KEY idx_cms_revisions_entry_id (entry_id),
	KEY idx_cms_revisions_cover_media_id (cover_media_id),
	KEY idx_cms_revisions_content_checksum (content_checksum),
	KEY idx_cms_revisions_workflow_status (workflow_status),
	KEY idx_cms_revisions_created_at (created_at),
	CONSTRAINT fk_cms_revision_entry FOREIGN KEY (entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	CONSTRAINT chk_cms_revision_status CHECK (workflow_status IN ('draft_snapshot', 'submitted', 'approved', 'rejected'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cms_review_records (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	revision_id BIGINT NOT NULL,
	action varchar(32) NOT NULL,
	comment text,
	actor_id varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	KEY idx_cms_review_records_revision_id (revision_id),
	KEY idx_cms_review_records_action (action),
	KEY idx_cms_review_records_actor_id (actor_id),
	KEY idx_cms_review_records_created_at (created_at),
	CONSTRAINT fk_cms_review_revision FOREIGN KEY (revision_id) REFERENCES cms_revisions(id) ON DELETE RESTRICT,
	CONSTRAINT chk_cms_review_action CHECK (action IN ('submit', 'approve', 'reject'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cms_user_roles (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	user_id BIGINT NOT NULL,
	role varchar(32) NOT NULL,
	granted_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	UNIQUE KEY idx_cms_user_role (user_id, role),
	KEY idx_cms_user_roles_user_id (user_id),
	KEY idx_cms_user_roles_created_at (created_at),
	CONSTRAINT chk_cms_user_role CHECK (role IN ('cms_admin', 'editor', 'reviewer', 'publisher'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
