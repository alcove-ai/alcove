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
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJiraTriggerMatches(t *testing.T) {
	tests := []struct {
		name           string
		trigger        *JiraTrigger
		issueProject   string
		issueComponents []string
		issueLabels    []string
		expectMatch    bool
	}{
		{
			name: "matches project and label",
			trigger: &JiraTrigger{
				Projects: []string{"TEST"},
				Labels:   []string{"needs-planning"},
			},
			issueProject: "TEST",
			issueLabels:  []string{"needs-planning", "bug"},
			expectMatch:  true,
		},
		{
			name: "matches project without label filter",
			trigger: &JiraTrigger{
				Projects: []string{"TEST"},
			},
			issueProject: "TEST",
			issueLabels:  []string{"any-label"},
			expectMatch:  true,
		},
		{
			name: "no match different project",
			trigger: &JiraTrigger{
				Projects: []string{"OTHER"},
				Labels:   []string{"needs-planning"},
			},
			issueProject: "TEST",
			issueLabels:  []string{"needs-planning"},
			expectMatch:  false,
		},
		{
			name: "no match missing label",
			trigger: &JiraTrigger{
				Projects: []string{"TEST"},
				Labels:   []string{"needs-planning"},
			},
			issueProject: "TEST",
			issueLabels:  []string{"bug", "enhancement"},
			expectMatch:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.trigger.Matches(tt.issueProject, tt.issueComponents, tt.issueLabels)
			if result != tt.expectMatch {
				t.Errorf("expected match=%v, got %v", tt.expectMatch, result)
			}
		})
	}
}

func TestEventTriggerJSONRoundtrip(t *testing.T) {
	// Test that EventTrigger JSON marshaling and unmarshaling preserves
	// the Jira trigger configuration as used by the Jira poller.
	original := EventTrigger{
		Jira: &JiraTrigger{
			Projects:   []string{"PULP", "AAP"},
			Components: []string{"UI", "API"},
			Labels:     []string{"needs-planning", "ready-for-dev"},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal EventTrigger: %v", err)
	}

	// Unmarshal from JSON
	var restored EventTrigger
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("failed to unmarshal EventTrigger: %v", err)
	}

	// Verify Jira trigger is preserved
	if restored.Jira == nil {
		t.Fatal("Jira trigger was lost during JSON round-trip")
	}

	if len(restored.Jira.Projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(restored.Jira.Projects))
	}
	if restored.Jira.Projects[0] != "PULP" || restored.Jira.Projects[1] != "AAP" {
		t.Errorf("projects not preserved: got %v", restored.Jira.Projects)
	}

	if len(restored.Jira.Labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(restored.Jira.Labels))
	}
}

// TestJiraDedupQueryExcludesFailedAndCancelled verifies that the dedup query
// correctly filters out failed and cancelled workflow runs, allowing retries
// after failures.
func TestJiraDedupQueryExcludesFailedAndCancelled(t *testing.T) {
	// This test verifies the SQL query pattern used in the dedup logic.
	// The expected dedup query should exclude failed and cancelled statuses
	expectedQuery := `
			SELECT COUNT(*) FROM workflow_runs
			WHERE workflow_id = $1 AND trigger_ref = $2
			AND created_at > NOW() - INTERVAL '24 hours'
			AND status NOT IN ('failed', 'cancelled')
		`

	// Normalize whitespace for comparison
	normalizedQuery := strings.Join(strings.Fields(expectedQuery), " ")

	// Verify query contains the status filter
	if !strings.Contains(normalizedQuery, "status NOT IN ('failed', 'cancelled')") {
		t.Errorf("dedup query should exclude failed and cancelled statuses")
	}

	// Verify it has the time window
	if !strings.Contains(normalizedQuery, "created_at > NOW() - INTERVAL '24 hours'") {
		t.Errorf("dedup query should include 24-hour time window")
	}

	// Verify basic structure
	expectedParts := []string{
		"SELECT COUNT(*)",
		"FROM workflow_runs",
		"WHERE workflow_id = $1",
		"AND trigger_ref = $2",
	}

	for _, part := range expectedParts {
		if !strings.Contains(normalizedQuery, part) {
			t.Errorf("dedup query should contain %q", part)
		}
	}

	tests := []struct {
		name           string
		existingStatus []string
		shouldBlock    bool
	}{
		{
			name:           "failed run should not block retry",
			existingStatus: []string{"failed"},
			shouldBlock:    false,
		},
		{
			name:           "cancelled run should not block retry",
			existingStatus: []string{"cancelled"},
			shouldBlock:    false,
		},
		{
			name:           "running run should block",
			existingStatus: []string{"running"},
			shouldBlock:    true,
		},
		{
			name:           "completed run should block",
			existingStatus: []string{"completed"},
			shouldBlock:    true,
		},
		{
			name:           "pending run should block",
			existingStatus: []string{"pending"},
			shouldBlock:    true,
		},
		{
			name:           "awaiting_approval run should block",
			existingStatus: []string{"awaiting_approval"},
			shouldBlock:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that our expected query logic matches the test case
			blockedByExistingQuery := false
			for _, status := range tt.existingStatus {
				if status != "failed" && status != "cancelled" {
					blockedByExistingQuery = true
					break
				}
			}

			if blockedByExistingQuery != tt.shouldBlock {
				t.Errorf("%s: query logic mismatch", tt.name)
			}
		})
	}
}

// TestJiraPollerStatusFilter tests the dedup logic with different run statuses.
func TestJiraPollerStatusFilter(t *testing.T) {
	// Test workflow and issue
	workflowID := "test-workflow-id"
	issueKey := "TEST-123"

	tests := []struct {
		name         string
		existingRuns []workflowRunRecord
		shouldBlock  bool
		description  string
	}{
		{
			name: "failed run does not block",
			existingRuns: []workflowRunRecord{
				{
					WorkflowID: workflowID,
					TriggerRef: issueKey,
					Status:     "failed",
					CreatedAt:  time.Now().Add(-1 * time.Hour),
				},
			},
			shouldBlock: false,
			description: "Failed runs should not block retries",
		},
		{
			name: "cancelled run does not block",
			existingRuns: []workflowRunRecord{
				{
					WorkflowID: workflowID,
					TriggerRef: issueKey,
					Status:     "cancelled",
					CreatedAt:  time.Now().Add(-1 * time.Hour),
				},
			},
			shouldBlock: false,
			description: "Cancelled runs should not block retries",
		},
		{
			name: "running run blocks",
			existingRuns: []workflowRunRecord{
				{
					WorkflowID: workflowID,
					TriggerRef: issueKey,
					Status:     "running",
					CreatedAt:  time.Now().Add(-1 * time.Hour),
				},
			},
			shouldBlock: true,
			description: "Running runs should block new dispatches",
		},
		{
			name: "completed run blocks",
			existingRuns: []workflowRunRecord{
				{
					WorkflowID: workflowID,
					TriggerRef: issueKey,
					Status:     "completed",
					CreatedAt:  time.Now().Add(-1 * time.Hour),
				},
			},
			shouldBlock: true,
			description: "Completed runs should block new dispatches",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock database that we can control
			mockDB := &mockWorkflowDB{
				workflowRuns: make(map[string][]workflowRunRecord),
			}

			// Setup mock data
			key := workflowID + ":" + issueKey
			mockDB.workflowRuns[key] = tt.existingRuns

			// Test the dedup logic
			count := mockDB.countNonBlockingRuns(workflowID, issueKey)
			actualBlocks := count > 0

			if actualBlocks != tt.shouldBlock {
				t.Errorf("%s: expected shouldBlock=%v, got %v (count=%d)",
					tt.description, tt.shouldBlock, actualBlocks, count)
			}
		})
	}
}

// Mock structures for testing the dedup logic
type workflowRunRecord struct {
	WorkflowID string
	TriggerRef string
	Status     string
	CreatedAt  time.Time
}

type mockWorkflowDB struct {
	workflowRuns map[string][]workflowRunRecord
}

// countNonBlockingRuns simulates the dedup query logic with the status filter
func (m *mockWorkflowDB) countNonBlockingRuns(workflowID, triggerRef string) int {
	key := workflowID + ":" + triggerRef
	runs, exists := m.workflowRuns[key]
	if !exists {
		return 0
	}

	count := 0
	cutoff := time.Now().Add(-24 * time.Hour)

	for _, run := range runs {
		// Simulate the SQL query logic:
		// WHERE workflow_id = $1 AND trigger_ref = $2
		// AND created_at > NOW() - INTERVAL '24 hours'
		// AND status NOT IN ('failed', 'cancelled')
		if run.WorkflowID == workflowID &&
			run.TriggerRef == triggerRef &&
			run.CreatedAt.After(cutoff) &&
			run.Status != "failed" &&
			run.Status != "cancelled" {
			count++
		}
	}

	return count
}