-- CMS media metadata, generated variants and immutable content references.

-- +kxone dialect sqlite
CREATE TABLE IF NOT EXISTS cms_media (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	kind varchar(32) NOT NULL,
	status varchar(32) NOT NULL,
	original_name varchar(255) NOT NULL,
	storage_key varchar(512) NOT NULL UNIQUE,
	mime_type varchar(128) NOT NULL,
	size_bytes integer NOT NULL,
	width integer NOT NULL DEFAULT 0,
	height integer NOT NULL DEFAULT 0,
	checksum varchar(64) NOT NULL,
	alt_text varchar(500),
	created_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	CHECK (kind IN ('image', 'document')),
	CHECK (status IN ('active', 'archived'))
);
CREATE INDEX IF NOT EXISTS idx_cms_media_kind ON cms_media(kind);
CREATE INDEX IF NOT EXISTS idx_cms_media_status ON cms_media(status);
CREATE INDEX IF NOT EXISTS idx_cms_media_checksum ON cms_media(checksum);
CREATE INDEX IF NOT EXISTS idx_cms_media_created_at ON cms_media(created_at);

CREATE TABLE IF NOT EXISTS cms_media_variants (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	media_id integer NOT NULL,
	variant_key varchar(64) NOT NULL,
	storage_key varchar(512) NOT NULL UNIQUE,
	mime_type varchar(128) NOT NULL,
	size_bytes integer NOT NULL,
	width integer NOT NULL,
	height integer NOT NULL,
	created_at datetime NOT NULL,
	FOREIGN KEY (media_id) REFERENCES cms_media(id) ON DELETE CASCADE,
	UNIQUE (media_id, variant_key)
);
CREATE INDEX IF NOT EXISTS idx_cms_media_variants_media_id ON cms_media_variants(media_id);

CREATE TABLE IF NOT EXISTS cms_media_references (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	media_id integer NOT NULL,
	object_type varchar(32) NOT NULL,
	object_id integer NOT NULL,
	field_path varchar(512) NOT NULL,
	created_at datetime NOT NULL,
	FOREIGN KEY (media_id) REFERENCES cms_media(id) ON DELETE RESTRICT,
	UNIQUE (media_id, object_type, object_id, field_path)
);
CREATE INDEX IF NOT EXISTS idx_cms_media_references_media_id ON cms_media_references(media_id);
CREATE INDEX IF NOT EXISTS idx_cms_media_references_object ON cms_media_references(object_type, object_id);

-- +kxone dialect mysql
CREATE TABLE IF NOT EXISTS cms_media (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	kind varchar(32) NOT NULL,
	status varchar(32) NOT NULL,
	original_name varchar(255) NOT NULL,
	storage_key varchar(512) NOT NULL,
	mime_type varchar(128) NOT NULL,
	size_bytes BIGINT NOT NULL,
	width int NOT NULL DEFAULT 0,
	height int NOT NULL DEFAULT 0,
	checksum varchar(64) NOT NULL,
	alt_text varchar(500),
	created_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	UNIQUE KEY idx_cms_media_storage_key (storage_key),
	KEY idx_cms_media_kind (kind),
	KEY idx_cms_media_status (status),
	KEY idx_cms_media_checksum (checksum),
	KEY idx_cms_media_created_at (created_at),
	CONSTRAINT chk_cms_media_kind CHECK (kind IN ('image', 'document')),
	CONSTRAINT chk_cms_media_status CHECK (status IN ('active', 'archived'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cms_media_variants (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	media_id BIGINT NOT NULL,
	variant_key varchar(64) NOT NULL,
	storage_key varchar(512) NOT NULL,
	mime_type varchar(128) NOT NULL,
	size_bytes BIGINT NOT NULL,
	width int NOT NULL,
	height int NOT NULL,
	created_at datetime NOT NULL,
	UNIQUE KEY idx_cms_media_variant (media_id, variant_key),
	UNIQUE KEY idx_cms_media_variant_storage_key (storage_key),
	KEY idx_cms_media_variants_media_id (media_id),
	CONSTRAINT fk_cms_media_variant_media FOREIGN KEY (media_id) REFERENCES cms_media(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cms_media_references (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	media_id BIGINT NOT NULL,
	object_type varchar(32) NOT NULL,
	object_id BIGINT NOT NULL,
	field_path varchar(512) NOT NULL,
	created_at datetime NOT NULL,
	UNIQUE KEY idx_cms_media_reference (media_id, object_type, object_id, field_path),
	KEY idx_cms_media_references_media_id (media_id),
	KEY idx_cms_media_references_object (object_type, object_id),
	CONSTRAINT fk_cms_media_reference_media FOREIGN KEY (media_id) REFERENCES cms_media(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
