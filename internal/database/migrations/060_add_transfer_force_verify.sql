-- Add force and verify_transfer options to transfers table
-- force: Allow overwriting existing files at the target
-- verify_transfer: Enable post-transfer checksum verification

ALTER TABLE transfers ADD COLUMN force INTEGER NOT NULL DEFAULT 0;
ALTER TABLE transfers ADD COLUMN verify_transfer INTEGER NOT NULL DEFAULT 0;
