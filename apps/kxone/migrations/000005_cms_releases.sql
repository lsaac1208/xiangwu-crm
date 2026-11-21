-- Immutable website release snapshots and build lifecycle.

-- +kxone dialect sqlite
CREATE TABLE IF NOT EXISTS cms_releases (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	status varchar(32) NOT NULL,
	builder_version varchar(128) NOT NULL,
	target_entry_id integer NOT NULL,
	target_revision_id integer NOT NULL,
	output_path varchar(1024),
	error_message text,
	created_by varchar(128) NOT NULL,
	activated_by varchar(128),
	created_at datetime NOT NULL,
	build_started_at datetime,
	build_ended_at datetime,
	activated_at datetime,
	FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	FOREIGN KEY (target_revision_id) REFERENCES cms_revisions(id) ON DELETE RESTRICT,
	CHECK (status IN ('queued', 'building', 'succeeded', 'active', 'failed', 'cancelled', 'rollback_queued', 'rolled_back'))
);
CREATE INDEX IF NOT EXISTS idx_cms_releases_status ON cms_releases(status);
CREATE INDEX IF NOT EXISTS idx_cms_releases_created_at ON cms_releases(created_at);
CREATE INDEX IF NOT EXISTS idx_cms_releases_activated_at ON cms_releases(activated_at);
CREATE INDEX IF NOT EXISTS idx_cms_releases_target_entry_id ON cms_releases(target_entry_id);
CREATE INDEX IF NOT EXISTS idx_cms_releases_target_revision_id ON cms_releases(target_revision_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cms_single_pending_release ON cms_releases((1)) WHERE status IN ('queued', 'building', 'succeeded', 'rollback_queued');

CREATE TABLE IF NOT EXISTS cms_release_items (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	release_id integer NOT NULL,
	entry_id integer NOT NULL,
	revision_id integer NOT NULL,
	path varchar(512) NOT NULL,
	FOREIGN KEY (release_id) REFERENCES cms_releases(id) ON DELETE CASCADE,
	FOREIGN KEY (entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	FOREIGN KEY (revision_id) REFERENCES cms_revisions(id) ON DELETE RESTRICT,
	UNIQUE (release_id, entry_id)
);
CREATE INDEX IF NOT EXISTS idx_cms_release_items_release_id ON cms_release_items(release_id);
CREATE INDEX IF NOT EXISTS idx_cms_release_items_entry_id ON cms_release_items(entry_id);
CREATE INDEX IF NOT EXISTS idx_cms_release_items_revision_id ON cms_release_items(revision_id);

-- +kxone dialect mysql
CREATE TABLE IF NOT EXISTS cms_releases (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	status varchar(32) NOT NULL,
	builder_version varchar(128) NOT NULL,
	target_entry_id BIGINT NOT NULL,
	target_revision_id BIGINT NOT NULL,
	output_path varchar(1024),
	error_message text,
	created_by varchar(128) NOT NULL,
	activated_by varchar(128),
	created_at datetime NOT NULL,
	build_started_at datetime,
	build_ended_at datetime,
	activated_at datetime,
	pending_slot tinyint GENERATED ALWAYS AS (CASE WHEN status IN ('queued', 'building', 'succeeded', 'rollback_queued') THEN 1 ELSE NULL END) STORED,
	KEY idx_cms_releases_status (status),
	KEY idx_cms_releases_created_at (created_at),
	KEY idx_cms_releases_activated_at (activated_at),
	KEY idx_cms_releases_target_entry_id (target_entry_id),
	KEY idx_cms_releases_target_revision_id (target_revision_id),
	UNIQUE KEY idx_cms_single_pending_release (pending_slot),
	CONSTRAINT fk_cms_release_target_entry FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	CONSTRAINT fk_cms_release_target_revision FOREIGN KEY (target_revision_id) REFERENCES cms_revisions(id) ON DELETE RESTRICT,
	CONSTRAINT chk_cms_release_status CHECK (status IN ('queued', 'building', 'succeeded', 'active', 'failed', 'cancelled', 'rollback_queued', 'rolled_back'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cms_release_items (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	release_id BIGINT NOT NULL,
	entry_id BIGINT NOT NULL,
	revision_id BIGINT NOT NULL,
	path varchar(512) NOT NULL,
	UNIQUE KEY idx_cms_release_entry (release_id, entry_id),
	KEY idx_cms_release_items_release_id (release_id),
	KEY idx_cms_release_items_entry_id (entry_id),
	KEY idx_cms_release_items_revision_id (revision_id),
	CONSTRAINT fk_cms_release_item_release FOREIGN KEY (release_id) REFERENCES cms_releases(id) ON DELETE CASCADE,
	CONSTRAINT fk_cms_release_item_entry FOREIGN KEY (entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	CONSTRAINT fk_cms_release_item_revision FOREIGN KEY (revision_id) REFERENCES cms_revisions(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
