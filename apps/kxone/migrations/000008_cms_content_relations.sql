-- Versioned content relation rules and immutable Release resolution snapshots.

-- +kxone dialect sqlite
CREATE TABLE IF NOT EXISTS cms_draft_relations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	entry_id integer NOT NULL,
	block_id varchar(128) NOT NULL,
	mode varchar(16) NOT NULL,
	target_entry_id integer,
	query_json text NOT NULL DEFAULT '',
	position integer NOT NULL DEFAULT 0,
	required integer NOT NULL DEFAULT 0,
	CHECK (mode IN ('manual', 'query')),
	FOREIGN KEY (entry_id) REFERENCES cms_entry_drafts(entry_id) ON DELETE CASCADE,
	FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id),
	UNIQUE (entry_id, block_id, position)
);
CREATE INDEX IF NOT EXISTS idx_cms_draft_relations_target ON cms_draft_relations(target_entry_id);
CREATE INDEX IF NOT EXISTS idx_cms_draft_relations_owner ON cms_draft_relations(entry_id, block_id);

CREATE TABLE IF NOT EXISTS cms_revision_relations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	revision_id integer NOT NULL,
	block_id varchar(128) NOT NULL,
	mode varchar(16) NOT NULL,
	target_entry_id integer,
	query_json text NOT NULL DEFAULT '',
	position integer NOT NULL DEFAULT 0,
	required integer NOT NULL DEFAULT 0,
	CHECK (mode IN ('manual', 'query')),
	FOREIGN KEY (revision_id) REFERENCES cms_revisions(id) ON DELETE CASCADE,
	FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id),
	UNIQUE (revision_id, block_id, position)
);
CREATE INDEX IF NOT EXISTS idx_cms_revision_relations_target ON cms_revision_relations(target_entry_id);
CREATE INDEX IF NOT EXISTS idx_cms_revision_relations_owner ON cms_revision_relations(revision_id, block_id);

CREATE TABLE IF NOT EXISTS cms_release_relation_items (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	release_id integer NOT NULL,
	source_entry_id integer NOT NULL,
	source_revision_id integer NOT NULL,
	block_id varchar(128) NOT NULL,
	mode varchar(16) NOT NULL,
	target_entry_id integer NOT NULL,
	target_revision_id integer NOT NULL,
	position integer NOT NULL,
	CHECK (mode IN ('manual', 'query')),
	FOREIGN KEY (release_id) REFERENCES cms_releases(id) ON DELETE CASCADE,
	FOREIGN KEY (source_entry_id) REFERENCES cms_entries(id),
	FOREIGN KEY (source_revision_id) REFERENCES cms_revisions(id),
	FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id),
	FOREIGN KEY (target_revision_id) REFERENCES cms_revisions(id),
	UNIQUE (release_id, source_entry_id, block_id, position)
);
CREATE INDEX IF NOT EXISTS idx_cms_release_relations_source ON cms_release_relation_items(release_id, source_entry_id);
CREATE INDEX IF NOT EXISTS idx_cms_release_relations_target ON cms_release_relation_items(target_entry_id, release_id);

-- +kxone dialect mysql
CREATE TABLE IF NOT EXISTS cms_draft_relations (
	id bigint PRIMARY KEY AUTO_INCREMENT,
	entry_id bigint NOT NULL,
	block_id varchar(128) NOT NULL,
	mode varchar(16) NOT NULL,
	target_entry_id bigint NULL,
	query_json text NOT NULL,
	position integer NOT NULL DEFAULT 0,
	required boolean NOT NULL DEFAULT false,
	UNIQUE KEY idx_cms_draft_relation_position (entry_id, block_id, position),
	KEY idx_cms_draft_relations_target (target_entry_id),
	KEY idx_cms_draft_relations_owner (entry_id, block_id),
	CONSTRAINT fk_cms_draft_relations_entry FOREIGN KEY (entry_id) REFERENCES cms_entry_drafts(entry_id) ON DELETE CASCADE,
	CONSTRAINT fk_cms_draft_relations_target FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id)
);
CREATE TABLE IF NOT EXISTS cms_revision_relations (
	id bigint PRIMARY KEY AUTO_INCREMENT,
	revision_id bigint NOT NULL,
	block_id varchar(128) NOT NULL,
	mode varchar(16) NOT NULL,
	target_entry_id bigint NULL,
	query_json text NOT NULL,
	position integer NOT NULL DEFAULT 0,
	required boolean NOT NULL DEFAULT false,
	UNIQUE KEY idx_cms_revision_relation_position (revision_id, block_id, position),
	KEY idx_cms_revision_relations_target (target_entry_id),
	KEY idx_cms_revision_relations_owner (revision_id, block_id),
	CONSTRAINT fk_cms_revision_relations_revision FOREIGN KEY (revision_id) REFERENCES cms_revisions(id) ON DELETE CASCADE,
	CONSTRAINT fk_cms_revision_relations_target FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id)
);
CREATE TABLE IF NOT EXISTS cms_release_relation_items (
	id bigint PRIMARY KEY AUTO_INCREMENT,
	release_id bigint NOT NULL,
	source_entry_id bigint NOT NULL,
	source_revision_id bigint NOT NULL,
	block_id varchar(128) NOT NULL,
	mode varchar(16) NOT NULL,
	target_entry_id bigint NOT NULL,
	target_revision_id bigint NOT NULL,
	position integer NOT NULL,
	UNIQUE KEY idx_cms_release_relation_position (release_id, source_entry_id, block_id, position),
	KEY idx_cms_release_relations_source (release_id, source_entry_id),
	KEY idx_cms_release_relations_target (target_entry_id, release_id),
	CONSTRAINT fk_cms_release_relations_release FOREIGN KEY (release_id) REFERENCES cms_releases(id) ON DELETE CASCADE,
	CONSTRAINT fk_cms_release_relations_source_entry FOREIGN KEY (source_entry_id) REFERENCES cms_entries(id),
	CONSTRAINT fk_cms_release_relations_source_revision FOREIGN KEY (source_revision_id) REFERENCES cms_revisions(id),
	CONSTRAINT fk_cms_release_relations_target_entry FOREIGN KEY (target_entry_id) REFERENCES cms_entries(id),
	CONSTRAINT fk_cms_release_relations_target_revision FOREIGN KEY (target_revision_id) REFERENCES cms_revisions(id)
);
