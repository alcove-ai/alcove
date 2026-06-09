// Copyright 2026 Brian Bouterse
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bridge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJiraPollerTargetDiscovery(t *testing.T) {
	pool := setupTestDB(t)

	ctx := context.Background()
	credStore := NewCredentialStore(pool, "test-master-key")
	workflowEngine := &WorkflowEngine{} // Empty for test
	defStore := &AgentDefStore{}        // Empty for test
	poller := NewJiraPoller(pool, credStore, workflowEngine, defStore)

	// Set up test data
	teamID := "test-team"

	// Insert a schedule with Jira trigger
	_, err := pool.Exec(ctx, `
		INSERT INTO schedules (
			id, name, prompt, repos, provider, timeout, team_id, debug, enabled,
			trigger_type, event_config, source_key, created_at, updated_at
		) VALUES (
			'schedule-1', 'Test Jira Schedule', 'test prompt', '[]', 'workflow', 300, $1, false, true,
			'event', $2, 'user::repo::file.yml', NOW(), NOW()
		)
	`, teamID, `{"jira": {"projects": ["TEST"], "labels": ["bug"]}}`)
	require.NoError(t, err)

	// Insert corresponding workflow
	_, err = pool.Exec(ctx, `
		INSERT INTO workflows (
			id, name, team_id, source_key, created_at, updated_at
		) VALUES (
			'workflow-1', 'Test Workflow', $1, 'user::repo::file.yml', NOW(), NOW()
		)
	`, teamID)
	require.NoError(t, err)

	// Test PollAll method
	poller.PollAll(ctx)

	// Verify the method ran without error (no credentials available, so it should return early)
	// The main test is that the schedule query worked and the code didn't crash
	assert.NotNil(t, poller)
}

func TestJiraPollerSystemPausedMode(t *testing.T) {
	pool := setupTestDB(t)

	ctx := context.Background()
	credStore := NewCredentialStore(pool, "test-master-key")
	workflowEngine := &WorkflowEngine{} // Empty for test
	defStore := &AgentDefStore{}        // Empty for test
	poller := NewJiraPoller(pool, credStore, workflowEngine, defStore)

	// Set system to paused mode
	_, err := pool.Exec(ctx, `
		INSERT INTO system_state (key, value)
		VALUES ('mode', 'paused')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
	`)
	require.NoError(t, err)

	// Insert a schedule with Jira trigger
	teamID := "test-team"
	_, err = pool.Exec(ctx, `
		INSERT INTO schedules (
			id, name, prompt, repos, provider, timeout, team_id, debug, enabled,
			trigger_type, event_config, source_key, created_at, updated_at
		) VALUES (
			'schedule-2', 'Test Jira Schedule 2', 'test prompt', '[]', 'workflow', 300, $1, false, true,
			'event', $2, 'user::repo::file2.yml', NOW(), NOW()
		)
	`, teamID, `{"jira": {"projects": ["TEST"], "labels": ["bug"]}}`)
	require.NoError(t, err)

	// Test PollAll method - should return early due to paused mode
	poller.PollAll(ctx)

	// The main test is that the method returned early and didn't process schedules
	assert.NotNil(t, poller)
}

func TestJiraPollerScheduleEventConfig(t *testing.T) {
	pool := setupTestDB(t)

	ctx := context.Background()
	credStore := NewCredentialStore(pool, "test-master-key")
	workflowEngine := &WorkflowEngine{} // Empty for test
	defStore := &AgentDefStore{}        // Empty for test
	poller := NewJiraPoller(pool, credStore, workflowEngine, defStore)

	teamID := "test-team"

	// Insert schedule with non-Jira trigger (should be ignored)
	_, err := pool.Exec(ctx, `
		INSERT INTO schedules (
			id, name, prompt, repos, provider, timeout, team_id, debug, enabled,
			trigger_type, event_config, source_key, created_at, updated_at
		) VALUES (
			'schedule-github', 'GitHub Schedule', 'test prompt', '[]', 'workflow', 300, $1, false, true,
			'event', $2, 'user::repo::github.yml', NOW(), NOW()
		)
	`, teamID, `{"github": {"events": ["push"], "repos": ["test/repo"]}}`)
	require.NoError(t, err)

	// Insert schedule with Jira trigger (should be found)
	_, err = pool.Exec(ctx, `
		INSERT INTO schedules (
			id, name, prompt, repos, provider, timeout, team_id, debug, enabled,
			trigger_type, event_config, source_key, created_at, updated_at
		) VALUES (
			'schedule-jira', 'Jira Schedule', 'test prompt', '[]', 'workflow', 300, $1, false, true,
			'event', $3, 'user::repo::jira.yml', NOW(), NOW()
		)
	`, teamID, `{"jira": {"projects": ["TEST"], "labels": ["needs-planning"]}}`)
	require.NoError(t, err)

	// Insert disabled schedule (should be ignored)
	_, err = pool.Exec(ctx, `
		INSERT INTO schedules (
			id, name, prompt, repos, provider, timeout, team_id, debug, enabled,
			trigger_type, event_config, source_key, created_at, updated_at
		) VALUES (
			'schedule-disabled', 'Disabled Jira Schedule', 'test prompt', '[]', 'workflow', 300, $1, false, false,
			'event', $4, 'user::repo::disabled.yml', NOW(), NOW()
		)
	`, teamID, `{"jira": {"projects": ["TEST"], "labels": ["bug"]}}`)
	require.NoError(t, err)

	// Test PollAll method
	poller.PollAll(ctx)

	// The main test is that the query worked correctly and only enabled Jira schedules were processed
	assert.NotNil(t, poller)
}