-- Versioned article metadata and taxonomy snapshots for drafts and revisions.

-- +kxone dialect sqlite
ALTER TABLE cms_entry_drafts ADD COLUMN author_name varchar(255) NOT NULL DEFAULT '';
ALTER TABLE cms_entry_drafts ADD COLUMN published_at datetime;
ALTER TABLE cms_entry_drafts ADD COLUMN source_name varchar(255) NOT NULL DEFAULT '';
ALTER TABLE cms_entry_drafts ADD COLUMN source_url varchar(1024) NOT NULL DEFAULT '';
ALTER TABLE cms_entry_drafts ADD COLUMN featured_weight integer NOT NULL DEFAULT 0;
ALTER TABLE cms_entry_drafts ADD COLUMN metadata_json text NOT NULL DEFAULT '{}';

ALTER TABLE cms_revisions ADD COLUMN author_name varchar(255) NOT NULL DEFAULT '';
ALTER TABLE cms_revisions ADD COLUMN published_at datetime;
ALTER TABLE cms_revisions ADD COLUMN source_name varchar(255) NOT NULL DEFAULT '';
ALTER TABLE cms_revisions ADD COLUMN source_url varchar(1024) NOT NULL DEFAULT '';
ALTER TABLE cms_revisions ADD COLUMN featured_weight integer NOT NULL DEFAULT 0;
ALTER TABLE cms_revisions ADD COLUMN metadata_json text NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_cms_drafts_published_at ON cms_entry_drafts(published_at);
CREATE INDEX IF NOT EXISTS idx_cms_revisions_published_at ON cms_revisions(published_at);

CREATE TABLE IF NOT EXISTS cms_terms (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	taxonomy varchar(32) NOT NULL,
	parent_id integer,
	name varchar(128) NOT NULL,
	slug varchar(128) NOT NULL,
	description text NOT NULL DEFAULT '',
	status varchar(32) NOT NULL DEFAULT 'active',
	sort_order integer NOT NULL DEFAULT 0,
	created_by varchar(128) NOT NULL,
	updated_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	CHECK (taxonomy IN ('category', 'tag')),
	CHECK (status IN ('active', 'archived')),
	FOREIGN KEY (parent_id) REFERENCES cms_terms(id),
	UNIQUE (taxonomy, slug)
);
CREATE INDEX IF NOT EXISTS idx_cms_terms_taxonomy_status ON cms_terms(taxonomy, status);
CREATE INDEX IF NOT EXISTS idx_cms_terms_parent_id ON cms_terms(parent_id);
CREATE INDEX IF NOT EXISTS idx_cms_terms_sort_order ON cms_terms(sort_order, name);

CREATE TABLE IF NOT EXISTS cms_draft_terms (
	entry_id integer NOT NULL,
	term_id integer NOT NULL,
	PRIMARY KEY (entry_id, term_id),
	FOREIGN KEY (entry_id) REFERENCES cms_entry_drafts(entry_id) ON DELETE CASCADE,
	FOREIGN KEY (term_id) REFERENCES cms_terms(id)
);
CREATE INDEX IF NOT EXISTS idx_cms_draft_terms_term_id ON cms_draft_terms(term_id);

CREATE TABLE IF NOT EXISTS cms_revision_terms (
	revision_id integer NOT NULL,
	term_id integer NOT NULL,
	taxonomy varchar(32) NOT NULL,
	parent_id integer,
	name varchar(128) NOT NULL,
	slug varchar(128) NOT NULL,
	description text NOT NULL DEFAULT '',
	sort_order integer NOT NULL DEFAULT 0,
	PRIMARY KEY (revision_id, term_id),
	FOREIGN KEY (revision_id) REFERENCES cms_revisions(id) ON DELETE CASCADE,
	FOREIGN KEY (term_id) REFERENCES cms_terms(id)
);
CREATE INDEX IF NOT EXISTS idx_cms_revision_terms_term_id ON cms_revision_terms(term_id);

-- +kxone dialect mysql
ALTER TABLE cms_entry_drafts ADD COLUMN author_name varchar(255) NOT NULL DEFAULT '';
ALTER TABLE cms_entry_drafts ADD COLUMN published_at datetime NULL;
ALTER TABLE cms_entry_drafts ADD COLUMN source_name varchar(255) NOT NULL DEFAULT '';
ALTER TABLE cms_entry_drafts ADD COLUMN source_url varchar(1024) NOT NULL DEFAULT '';
ALTER TABLE cms_entry_drafts ADD COLUMN featured_weight integer NOT NULL DEFAULT 0;
ALTER TABLE cms_entry_drafts ADD COLUMN metadata_json text NULL;
UPDATE cms_entry_drafts SET metadata_json = '{}' WHERE metadata_json IS NULL OR metadata_json = '';
ALTER TABLE cms_entry_drafts MODIFY COLUMN metadata_json text NOT NULL;

ALTER TABLE cms_revisions ADD COLUMN author_name varchar(255) NOT NULL DEFAULT '';
ALTER TABLE cms_revisions ADD COLUMN published_at datetime NULL;
ALTER TABLE cms_revisions ADD COLUMN source_name varchar(255) NOT NULL DEFAULT '';
ALTER TABLE cms_revisions ADD COLUMN source_url varchar(1024) NOT NULL DEFAULT '';
ALTER TABLE cms_revisions ADD COLUMN featured_weight integer NOT NULL DEFAULT 0;
ALTER TABLE cms_revisions ADD COLUMN metadata_json text NULL;
UPDATE cms_revisions SET metadata_json = '{}' WHERE metadata_json IS NULL OR metadata_json = '';
ALTER TABLE cms_revisions MODIFY COLUMN metadata_json text NOT NULL;

CREATE INDEX idx_cms_drafts_published_at ON cms_entry_drafts(published_at);
CREATE INDEX idx_cms_revisions_published_at ON cms_revisions(published_at);

CREATE TABLE IF NOT EXISTS cms_terms (
	id bigint PRIMARY KEY AUTO_INCREMENT,
	taxonomy varchar(32) NOT NULL,
	parent_id bigint NULL,
	name varchar(128) NOT NULL,
	slug varchar(128) NOT NULL,
	description text NOT NULL,
	status varchar(32) NOT NULL DEFAULT 'active',
	sort_order integer NOT NULL DEFAULT 0,
	created_by varchar(128) NOT NULL,
	updated_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	CONSTRAINT fk_cms_terms_parent FOREIGN KEY (parent_id) REFERENCES cms_terms(id),
	UNIQUE KEY idx_cms_terms_taxonomy_slug (taxonomy, slug),
	KEY idx_cms_terms_taxonomy_status (taxonomy, status),
	KEY idx_cms_terms_parent_id (parent_id),
	KEY idx_cms_terms_sort_order (sort_order, name)
);

CREATE TABLE IF NOT EXISTS cms_draft_terms (
	entry_id bigint NOT NULL,
	term_id bigint NOT NULL,
	PRIMARY KEY (entry_id, term_id),
	KEY idx_cms_draft_terms_term_id (term_id),
	CONSTRAINT fk_cms_draft_terms_entry FOREIGN KEY (entry_id) REFERENCES cms_entry_drafts(entry_id) ON DELETE CASCADE,
	CONSTRAINT fk_cms_draft_terms_term FOREIGN KEY (term_id) REFERENCES cms_terms(id)
);

CREATE TABLE IF NOT EXISTS cms_revision_terms (
	revision_id bigint NOT NULL,
	term_id bigint NOT NULL,
	taxonomy varchar(32) NOT NULL,
	parent_id bigint NULL,
	name varchar(128) NOT NULL,
	slug varchar(128) NOT NULL,
	description text NOT NULL,
	sort_order integer NOT NULL DEFAULT 0,
	PRIMARY KEY (revision_id, term_id),
	KEY idx_cms_revision_terms_term_id (term_id),
	CONSTRAINT fk_cms_revision_terms_revision FOREIGN KEY (revision_id) REFERENCES cms_revisions(id) ON DELETE CASCADE,
	CONSTRAINT fk_cms_revision_terms_term FOREIGN KEY (term_id) REFERENCES cms_terms(id)
);
