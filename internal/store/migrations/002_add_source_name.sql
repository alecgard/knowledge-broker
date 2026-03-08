-- Add source_name column to fragments for existing databases.
-- SQLite ignores ALTER TABLE ADD COLUMN if the column already exists (raises error),
-- so we check via pragma first. However, the simplest safe approach is to catch the
-- error in Go. This statement will succeed on old databases and fail harmlessly on new ones.
ALTER TABLE fragments ADD COLUMN source_name TEXT DEFAULT '';
