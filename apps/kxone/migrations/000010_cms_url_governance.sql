-- Controlled URL changes and immutable redirect snapshots.

-- +kxone dialect sqlite
CREATE TABLE IF NOT EXISTS cms_redirects (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	source_path varchar(512) NOT NULL UNIQUE,
	target_entry_id integer NOT NULL,
	status_code integer NOT NULL DEFAULT 301,
	status varchar(32) NOT NULL,
	reason varchar(255) NOT NULL DEFAULT '',
	created_by varchar(128) NOT NULL,
	updated_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	CHECK (status_code IN (301, 302)),
	CHECK (status IN ('active', 'archived'))
);
CREATE INDEX IF NOT EXISTS idx_cms_redirects_target_entry_id ON cms_redirects(target_entry_id);
CREATE INDEX IF NOT EXISTS idx_cms_redirects_status ON cms_redirects(status);

CREATE TABLE IF NOT EXISTS cms_release_redirect_items (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	release_id integer NOT NULL,
	redirect_id integer NOT NULL,
	source_path varchar(512) NOT NULL,
	target_entry_id integer NOT NULL,
	target_path varchar(512) NOT NULL,
	status_code integer NOT NULL,
	FOREIGN KEY (release_id) REFERENCES cms_releases(id) ON DELETE CASCADE,
	FOREIGN KEY (redirect_id) REFERENCES cms_redirects(id) ON DELETE RESTRICT,
	FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	UNIQUE (release_id, source_path),
	CHECK (status_code IN (301, 302))
);
CREATE INDEX IF NOT EXISTS idx_cms_release_redirect_release ON cms_release_redirect_items(release_id);

-- +kxone dialect mysql
CREATE TABLE IF NOT EXISTS cms_redirects (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	source_path varchar(512) NOT NULL,
	target_entry_id BIGINT NOT NULL,
	status_code integer NOT NULL DEFAULT 301,
	status varchar(32) NOT NULL,
	reason varchar(255) NOT NULL DEFAULT '',
	created_by varchar(128) NOT NULL,
	updated_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	UNIQUE KEY idx_cms_redirects_source_path (source_path),
	KEY idx_cms_redirects_target_entry_id (target_entry_id),
	KEY idx_cms_redirects_status (status),
	CONSTRAINT fk_cms_redirect_target_entry FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	CONSTRAINT chk_cms_redirect_code CHECK (status_code IN (301, 302)),
	CONSTRAINT chk_cms_redirect_status CHECK (status IN ('active', 'archived'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cms_release_redirect_items (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	release_id BIGINT NOT NULL,
	redirect_id BIGINT NOT NULL,
	source_path varchar(512) NOT NULL,
	target_entry_id BIGINT NOT NULL,
	target_path varchar(512) NOT NULL,
	status_code integer NOT NULL,
	UNIQUE KEY idx_cms_release_redirect_source (release_id, source_path),
	KEY idx_cms_release_redirect_release (release_id),
	KEY idx_cms_release_redirect_id (redirect_id),
	KEY idx_cms_release_redirect_target_entry (target_entry_id),
	CONSTRAINT fk_cms_release_redirect_release FOREIGN KEY (release_id) REFERENCES cms_releases(id) ON DELETE CASCADE,
	CONSTRAINT fk_cms_release_redirect_redirect FOREIGN KEY (redirect_id) REFERENCES cms_redirects(id) ON DELETE RESTRICT,
	CONSTRAINT fk_cms_release_redirect_target FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	CONSTRAINT chk_cms_release_redirect_code CHECK (status_code IN (301, 302))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
