-- Migration 043: Add transcript_event_count and proxy_event_count columns for observability
--
-- This migration adds two new columns to the sessions table:
-- 1. transcript_event_count: Number of transcript events in the session (for fast querying)
-- 2. proxy_event_count: Number of proxy log entries in the session
--
-- Both columns are incremented atomically when appending events via API.
-- A backfill is included to populate existing sessions.

-- Add the columns
ALTER TABLE sessions ADD COLUMN transcript_event_count INTEGER DEFAULT 0;
ALTER TABLE sessions ADD COLUMN proxy_event_count INTEGER DEFAULT 0;

-- Backfill existing sessions
UPDATE sessions
SET transcript_event_count = COALESCE(jsonb_array_length(transcript), 0),
    proxy_event_count = COALESCE(jsonb_array_length(proxy_log), 0);

-- Add indexes for fast filtering
CREATE INDEX idx_sessions_transcript_event_count ON sessions (transcript_event_count);
CREATE INDEX idx_sessions_proxy_event_count ON sessions (proxy_event_count);