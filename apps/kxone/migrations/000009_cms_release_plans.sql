-- Editable release plans, immutable plan items, scheduling metadata, and
-- linkage from the resulting immutable release snapshot.

-- +kxone dialect sqlite
CREATE TABLE IF NOT EXISTS cms_release_plans (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name varchar(255) NOT NULL,
	description text NOT NULL DEFAULT '',
	status varchar(32) NOT NULL,
	scheduled_at datetime,
	release_id integer,
	queue_task_id varchar(255) NOT NULL DEFAULT '',
	error_message text NOT NULL DEFAULT '',
	created_by varchar(128) NOT NULL,
	updated_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	ready_at datetime,
	completed_at datetime,
	CHECK (status IN ('draft', 'ready', 'scheduled', 'queued', 'building', 'active', 'failed', 'cancelled'))
);
CREATE INDEX IF NOT EXISTS idx_cms_release_plans_status ON cms_release_plans(status);
CREATE INDEX IF NOT EXISTS idx_cms_release_plans_scheduled_at ON cms_release_plans(scheduled_at);
CREATE INDEX IF NOT EXISTS idx_cms_release_plans_release_id ON cms_release_plans(release_id);
CREATE INDEX IF NOT EXISTS idx_cms_release_plans_created_at ON cms_release_plans(created_at);

CREATE TABLE IF NOT EXISTS cms_release_plan_items (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	plan_id integer NOT NULL,
	entry_id integer NOT NULL,
	revision_id integer,
	action varchar(16) NOT NULL,
	position integer NOT NULL DEFAULT 0,
	FOREIGN KEY (plan_id) REFERENCES cms_release_plans(id) ON DELETE CASCADE,
	FOREIGN KEY (entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	FOREIGN KEY (revision_id) REFERENCES cms_revisions(id) ON DELETE RESTRICT,
	CHECK (action IN ('publish', 'unpublish')),
	CHECK ((action = 'publish' AND revision_id IS NOT NULL) OR (action = 'unpublish' AND revision_id IS NULL)),
	UNIQUE (plan_id, entry_id)
);
CREATE INDEX IF NOT EXISTS idx_cms_release_plan_items_plan ON cms_release_plan_items(plan_id, position);
CREATE INDEX IF NOT EXISTS idx_cms_release_plan_items_entry ON cms_release_plan_items(entry_id);
CREATE INDEX IF NOT EXISTS idx_cms_release_plan_items_revision ON cms_release_plan_items(revision_id);

ALTER TABLE cms_releases ADD COLUMN plan_id integer;
CREATE INDEX IF NOT EXISTS idx_cms_releases_plan_id ON cms_releases(plan_id);

-- +kxone dialect mysql
CREATE TABLE IF NOT EXISTS cms_release_plans (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	name varchar(255) NOT NULL,
	description text NOT NULL,
	status varchar(32) NOT NULL,
	scheduled_at datetime NULL,
	release_id BIGINT NULL,
	queue_task_id varchar(255) NOT NULL DEFAULT '',
	error_message text NOT NULL,
	created_by varchar(128) NOT NULL,
	updated_by varchar(128) NOT NULL,
	created_at datetime NOT NULL,
	updated_at datetime NOT NULL,
	ready_at datetime NULL,
	completed_at datetime NULL,
	KEY idx_cms_release_plans_status (status),
	KEY idx_cms_release_plans_scheduled_at (scheduled_at),
	KEY idx_cms_release_plans_release_id (release_id),
	KEY idx_cms_release_plans_created_at (created_at),
	CONSTRAINT chk_cms_release_plan_status CHECK (status IN ('draft', 'ready', 'scheduled', 'queued', 'building', 'active', 'failed', 'cancelled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cms_release_plan_items (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	plan_id BIGINT NOT NULL,
	entry_id BIGINT NOT NULL,
	revision_id BIGINT NULL,
	action varchar(16) NOT NULL,
	position integer NOT NULL DEFAULT 0,
	UNIQUE KEY idx_cms_release_plan_entry (plan_id, entry_id),
	KEY idx_cms_release_plan_items_plan (plan_id, position),
	KEY idx_cms_release_plan_items_entry (entry_id),
	KEY idx_cms_release_plan_items_revision (revision_id),
	CONSTRAINT fk_cms_release_plan_item_plan FOREIGN KEY (plan_id) REFERENCES cms_release_plans(id) ON DELETE CASCADE,
	CONSTRAINT fk_cms_release_plan_item_entry FOREIGN KEY (entry_id) REFERENCES cms_entries(id) ON DELETE RESTRICT,
	CONSTRAINT fk_cms_release_plan_item_revision FOREIGN KEY (revision_id) REFERENCES cms_revisions(id) ON DELETE RESTRICT,
	CONSTRAINT chk_cms_release_plan_action CHECK (action IN ('publish', 'unpublish')),
	CONSTRAINT chk_cms_release_plan_revision CHECK ((action = 'publish' AND revision_id IS NOT NULL) OR (action = 'unpublish' AND revision_id IS NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE cms_releases ADD COLUMN plan_id BIGINT NULL;
CREATE INDEX idx_cms_releases_plan_id ON cms_releases(plan_id);
