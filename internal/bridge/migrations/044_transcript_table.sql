-- Move transcript storage to separate table (Phase 4 of #751)
-- This migration creates separate tables for transcript_events and proxy_log_events
-- to replace O(n²) JSONB append operations on the sessions table.
-- Each append becomes an O(1) INSERT instead of rewriting the entire JSON array.

-- Create transcript_events table
CREATE TABLE transcript_events (
    id BIGSERIAL PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    batch_index INT NOT NULL,
    events JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(session_id, batch_index)
);

-- Create proxy_log_events table with identical structure
CREATE TABLE proxy_log_events (
    id BIGSERIAL PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    batch_index INT NOT NULL,
    events JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(session_id, batch_index)
);