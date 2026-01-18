-- Replace boolean flags with action enums for better user control
-- file_exists_action: 'abort' (fail), 'skip' (skip identical), 'overwrite' (force)
-- source_action: 'keep' (keep seeding), 'pause' (pause torrent), 'delete' (remove + files)

ALTER TABLE transfers ADD COLUMN file_exists_action TEXT NOT NULL DEFAULT 'abort';
ALTER TABLE transfers ADD COLUMN source_action TEXT NOT NULL DEFAULT 'keep';

-- Migrate existing data
UPDATE transfers SET file_exists_action = 'overwrite' WHERE force = 1;
UPDATE transfers SET source_action = 'delete' WHERE delete_from_source = 1;
