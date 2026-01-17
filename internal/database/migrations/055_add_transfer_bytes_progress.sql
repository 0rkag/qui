-- Migration: Add bytes progress tracking to transfers
-- This enables transfer speed and ETA calculation

ALTER TABLE transfers ADD COLUMN bytes_total INTEGER NOT NULL DEFAULT 0;
ALTER TABLE transfers ADD COLUMN bytes_transferred INTEGER NOT NULL DEFAULT 0;
