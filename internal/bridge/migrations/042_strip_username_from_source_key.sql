-- Remove the username prefix from source_key values.
-- Old format: "username::repo_url::filename"
-- New format: "repo_url::filename"
--
-- The username prefix caused cross-user cleanup deletion: when user A synced,
-- user B's records (with a different source_key prefix) were treated as stale
-- and deleted, silently breaking workflow polling.
--
-- See: https://github.com/alcove-ai/alcove/issues/721

-- Prevent concurrent syncs from inserting new prefixed keys during migration.
SELECT pg_advisory_xact_lock(721721);

-- agent_definitions: UNIQUE(source_key, team_id)
-- Delete older duplicates that collapse to the same key after stripping.
DELETE FROM agent_definitions ad1
WHERE ad1.source_key ~ '^[^:]+::[^:]+::'
AND EXISTS (
    SELECT 1 FROM agent_definitions ad2
    WHERE ad2.team_id = ad1.team_id
    AND regexp_replace(ad2.source_key, '^[^:]+::', '') = regexp_replace(ad1.source_key, '^[^:]+::', '')
    AND ad2.id != ad1.id
    AND (ad2.updated_at > ad1.updated_at OR (ad2.updated_at = ad1.updated_at AND ad2.id > ad1.id))
);
UPDATE agent_definitions SET source_key = regexp_replace(source_key, '^[^:]+::', '')
WHERE source_key ~ '^[^:]+::[^:]+::';

-- workflows: UNIQUE(source_key, team_id)
DELETE FROM workflows w1
WHERE w1.source_key ~ '^[^:]+::[^:]+::'
AND EXISTS (
    SELECT 1 FROM workflows w2
    WHERE w2.team_id = w1.team_id
    AND regexp_replace(w2.source_key, '^[^:]+::', '') = regexp_replace(w1.source_key, '^[^:]+::', '')
    AND w2.id != w1.id
    AND (w2.updated_at > w1.updated_at OR (w2.updated_at = w1.updated_at AND w2.id > w1.id))
);
UPDATE workflows SET source_key = regexp_replace(source_key, '^[^:]+::', '')
WHERE source_key ~ '^[^:]+::[^:]+::';

-- repo_groups: UNIQUE(source_key, team_id)
DELETE FROM repo_groups rg1
WHERE rg1.source_key ~ '^[^:]+::[^:]+::'
AND EXISTS (
    SELECT 1 FROM repo_groups rg2
    WHERE rg2.team_id = rg1.team_id
    AND regexp_replace(rg2.source_key, '^[^:]+::', '') = regexp_replace(rg1.source_key, '^[^:]+::', '')
    AND rg2.id != rg1.id
    AND (rg2.updated_at > rg1.updated_at OR (rg2.updated_at = rg1.updated_at AND rg2.id > rg1.id))
);
UPDATE repo_groups SET source_key = regexp_replace(source_key, '^[^:]+::', '')
WHERE source_key ~ '^[^:]+::[^:]+::';

-- security_profiles: UNIQUE(source_key) WHERE source_key != '' (single-column, global)
-- Dedup globally, not per team_id.
DELETE FROM security_profiles sp1
WHERE sp1.source_key ~ '^[^:]+::[^:]+::'
AND EXISTS (
    SELECT 1 FROM security_profiles sp2
    WHERE regexp_replace(sp2.source_key, '^[^:]+::', '') = regexp_replace(sp1.source_key, '^[^:]+::', '')
    AND sp2.id != sp1.id
    AND (sp2.updated_at > sp1.updated_at OR (sp2.updated_at = sp1.updated_at AND sp2.id > sp1.id))
);
UPDATE security_profiles SET source_key = regexp_replace(source_key, '^[^:]+::', '')
WHERE source_key ~ '^[^:]+::[^:]+::';

-- policy_rule_sets: UNIQUE(source_key) WHERE source_key IS NOT NULL (single-column, global)
-- Dedup globally, not per team_id.
DELETE FROM policy_rule_sets prs1
WHERE prs1.source_key ~ '^[^:]+::[^:]+::'
AND EXISTS (
    SELECT 1 FROM policy_rule_sets prs2
    WHERE regexp_replace(prs2.source_key, '^[^:]+::', '') = regexp_replace(prs1.source_key, '^[^:]+::', '')
    AND prs2.id != prs1.id
    AND (prs2.updated_at > prs1.updated_at OR (prs2.updated_at = prs1.updated_at AND prs2.id > prs1.id))
);
UPDATE policy_rule_sets SET source_key = regexp_replace(source_key, '^[^:]+::', '')
WHERE source_key ~ '^[^:]+::[^:]+::';

-- schedules: no UNIQUE constraint, dedup by team_id + stripped key
DELETE FROM schedules s1
WHERE s1.source = 'yaml'
AND s1.source_key ~ '^[^:]+::[^:]+::'
AND EXISTS (
    SELECT 1 FROM schedules s2
    WHERE s2.source = 'yaml'
    AND s2.team_id = s1.team_id
    AND regexp_replace(s2.source_key, '^[^:]+::', '') = regexp_replace(s1.source_key, '^[^:]+::', '')
    AND s2.id != s1.id
    AND (s2.created_at > s1.created_at OR (s2.created_at = s1.created_at AND s2.id > s1.id))
);
UPDATE schedules SET source_key = regexp_replace(source_key, '^[^:]+::', '')
WHERE source_key ~ '^[^:]+::[^:]+::' AND source = 'yaml';
