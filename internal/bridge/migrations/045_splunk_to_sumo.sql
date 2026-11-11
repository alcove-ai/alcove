-- Splunk support has been replaced by Sumo Logic.
--
-- Existing "splunk" credentials hold Splunk bearer tokens, which cannot be
-- used as Sumo Logic credentials (Sumo uses accessId:accessKey Basic auth).
-- Rather than deleting them, convert them to generic secrets so the values
-- remain available to agents that reference them via the credentials: map,
-- and so they are not mistaken for LLM credentials (queries that pick the
-- team's LLM provider exclude known non-LLM providers by name).
UPDATE provider_credentials
SET provider = 'generic', updated_at = NOW()
WHERE provider = 'splunk';
