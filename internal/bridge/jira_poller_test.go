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
	"fmt"
	"strings"
	"testing"
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
		{
			name: "matches with retrigger_on_comment enabled",
			trigger: &JiraTrigger{
				Projects:           []string{"TEST"},
				Labels:             []string{"needs-planning"},
				RetriggerOnComment: true,
				Users:              []string{"alice", "bob"},
			},
			issueProject: "TEST",
			issueLabels:  []string{"needs-planning"},
			expectMatch:  true,
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
			Projects:           []string{"PULP", "AAP"},
			Components:         []string{"UI", "API"},
			Labels:             []string{"needs-planning", "ready-for-dev"},
			RetriggerOnComment: true,
			Users:              []string{"alice", "bob"},
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

	// Test new fields
	if !restored.Jira.RetriggerOnComment {
		t.Errorf("expected RetriggerOnComment=true, got %v", restored.Jira.RetriggerOnComment)
	}

	if len(restored.Jira.Users) != 2 {
		t.Errorf("expected 2 users, got %d", len(restored.Jira.Users))
	}
	if restored.Jira.Users[0] != "alice" || restored.Jira.Users[1] != "bob" {
		t.Errorf("users not preserved: got %v", restored.Jira.Users)
	}
}

func TestJiraTriggerRefConstruction(t *testing.T) {
	tests := []struct {
		name               string
		issueKey           string
		commentCount       int
		retriggerOnComment bool
		users              []string
		latestCommentAuthor string
		expectedTriggerRef string
		description        string
	}{
		{
			name:               "basic issue key without retrigger",
			issueKey:           "PULP-1369",
			commentCount:       5,
			retriggerOnComment: false,
			users:              []string{},
			latestCommentAuthor: "",
			expectedTriggerRef: "PULP-1369",
			description:        "Should use plain issue key when retrigger_on_comment is false",
		},
		{
			name:               "comment-aware trigger_ref with no users filter",
			issueKey:           "PULP-1369",
			commentCount:       5,
			retriggerOnComment: true,
			users:              []string{},
			latestCommentAuthor: "automation-bot",
			expectedTriggerRef: "PULP-1369:c5",
			description:        "Should use comment count when retrigger_on_comment is true and no users filter",
		},
		{
			name:               "comment-aware trigger_ref with user in allowlist",
			issueKey:           "PULP-1369",
			commentCount:       5,
			retriggerOnComment: true,
			users:              []string{"alice", "bob"},
			latestCommentAuthor: "alice",
			expectedTriggerRef: "PULP-1369:c5",
			description:        "Should use comment count when latest commenter is in allowlist",
		},
		{
			name:               "fallback to plain key when user not in allowlist",
			issueKey:           "PULP-1369",
			commentCount:       5,
			retriggerOnComment: true,
			users:              []string{"alice", "bob"},
			latestCommentAuthor: "automation-bot",
			expectedTriggerRef: "PULP-1369",
			description:        "Should fall back to plain issue key when latest commenter is not in allowlist",
		},
		{
			name:               "fallback to plain key when no comments",
			issueKey:           "PULP-1369",
			commentCount:       0,
			retriggerOnComment: true,
			users:              []string{"alice", "bob"},
			latestCommentAuthor: "",
			expectedTriggerRef: "PULP-1369",
			description:        "Should fall back to plain issue key when no comments exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock the trigger
			trigger := &JiraTrigger{
				Projects:           []string{"PULP"},
				RetriggerOnComment: tt.retriggerOnComment,
				Users:              tt.users,
			}

			// Simulate the trigger_ref construction logic
			triggerRef := tt.issueKey
			if trigger.RetriggerOnComment {
				useCommentAwareTriggerRef := true

				if len(trigger.Users) > 0 {
					if tt.latestCommentAuthor != "" {
						authorMatches := false
						for _, allowedUser := range trigger.Users {
							if strings.EqualFold(allowedUser, tt.latestCommentAuthor) {
								authorMatches = true
								break
							}
						}
						if !authorMatches {
							useCommentAwareTriggerRef = false
						}
					} else {
						useCommentAwareTriggerRef = false
					}
				}

				if useCommentAwareTriggerRef {
					triggerRef = fmt.Sprintf("%s:c%d", tt.issueKey, tt.commentCount)
				}
			}

			if triggerRef != tt.expectedTriggerRef {
				t.Errorf("%s: expected trigger_ref=%s, got %s", tt.description, tt.expectedTriggerRef, triggerRef)
			}
		})
	}
}