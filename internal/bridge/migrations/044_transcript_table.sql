-- Migration 044: Create separate tables for transcript and proxy log events
--
-- This migration creates two new tables to replace O(n²) JSONB append operations
-- on the sessions table. Each table stores events in separate rows with batch indexing.
--
-- The sessions.transcript and sessions.proxy_log columns are retained for backward
-- compatibility but will no longer be written to by new code.

-- Create transcript_events table
CREATE TABLE transcript_events (
    id BIGSERIAL PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    batch_index INT NOT NULL,
    events JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(session_id, batch_index)
);

-- Create proxy_log_events table
CREATE TABLE proxy_log_events (
    id BIGSERIAL PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    batch_index INT NOT NULL,
    events JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(session_id, batch_index)
);