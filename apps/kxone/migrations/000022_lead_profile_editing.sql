-- Separate customer-profile concurrency from workflow business versions.

-- +kxone dialect sqlite
ALTER TABLE leads ADD COLUMN profile_version integer NOT NULL DEFAULT 1;

-- +kxone dialect mysql
ALTER TABLE leads ADD COLUMN profile_version int NOT NULL DEFAULT 1 AFTER version;
