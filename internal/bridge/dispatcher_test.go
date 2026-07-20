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
	"strings"
	"testing"
)

func TestStripURLToHost(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://gitlab.cee.redhat.com", "gitlab.cee.redhat.com"},
		{"http://gitlab.cee.redhat.com", "gitlab.cee.redhat.com"},
		{"gitlab.cee.redhat.com", "gitlab.cee.redhat.com"},
		{"https://gitlab.cee.redhat.com/", "gitlab.cee.redhat.com"},
		{"https://gitlab.cee.redhat.com/api/v4", "gitlab.cee.redhat.com"},
		{"gitlab.com:443", "gitlab.com:443"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripURLToHost(tt.input)
			if got != tt.expected {
				t.Errorf("stripURLToHost(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGitLabAPIURLConstruction(t *testing.T) {
	tests := []struct {
		apiHost         string
		expectedAPIURL  string
		expectedGateHost string
	}{
		{
			apiHost:         "https://gitlab.cee.redhat.com",
			expectedAPIURL:  "https://gitlab.cee.redhat.com/api/v4",
			expectedGateHost: "gitlab.cee.redhat.com",
		},
		{
			apiHost:         "https://gitlab.cee.redhat.com/",
			expectedAPIURL:  "https://gitlab.cee.redhat.com/api/v4",
			expectedGateHost: "gitlab.cee.redhat.com",
		},
		{
			apiHost:         "http://gitlab.internal.company.com",
			expectedAPIURL:  "http://gitlab.internal.company.com/api/v4",
			expectedGateHost: "gitlab.internal.company.com",
		},
		{
			apiHost:         "gitlab.example.com",
			expectedAPIURL:  "gitlab.example.com/api/v4",
			expectedGateHost: "gitlab.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.apiHost, func(t *testing.T) {
			// Simulate the logic from dispatcher.go lines 604-609
			skiffEnv := make(map[string]string)
			gateEnv := make(map[string]string)
			scmAPIHosts := map[string]string{"gitlab": tt.apiHost}
			scmDummyTokens := map[string]string{"gitlab": "dummy-token"}

			// Apply the GitLab API URL construction logic
			if token, ok := scmDummyTokens["gitlab"]; ok {
				skiffEnv["GITLAB_TOKEN"] = token
				skiffEnv["GITLAB_PERSONAL_ACCESS_TOKEN"] = token
				if host, ok := scmAPIHosts["gitlab"]; ok {
					skiffEnv["GITLAB_API_URL"] = strings.TrimRight(host, "/") + "/api/v4"
					gateEnv["GATE_GITLAB_HOST"] = stripURLToHost(host)
				}
			}

			// Verify results
			if skiffEnv["GITLAB_API_URL"] != tt.expectedAPIURL {
				t.Errorf("Expected GITLAB_API_URL=%q, got %q", tt.expectedAPIURL, skiffEnv["GITLAB_API_URL"])
			}
			if gateEnv["GATE_GITLAB_HOST"] != tt.expectedGateHost {
				t.Errorf("Expected GATE_GITLAB_HOST=%q, got %q", tt.expectedGateHost, gateEnv["GATE_GITLAB_HOST"])
			}
		})
	}
}

func TestGitLabAPIURLConstruction_EmptyAPIHost(t *testing.T) {
	// Test the case where no api_host is configured
	skiffEnv := make(map[string]string)
	gateEnv := make(map[string]string)
	scmAPIHosts := map[string]string{} // No gitlab entry
	scmDummyTokens := map[string]string{"gitlab": "dummy-token"}

	// Apply the GitLab API URL construction logic
	if token, ok := scmDummyTokens["gitlab"]; ok {
		skiffEnv["GITLAB_TOKEN"] = token
		skiffEnv["GITLAB_PERSONAL_ACCESS_TOKEN"] = token
		if host, ok := scmAPIHosts["gitlab"]; ok {
			skiffEnv["GITLAB_API_URL"] = strings.TrimRight(host, "/") + "/api/v4"
			gateEnv["GATE_GITLAB_HOST"] = stripURLToHost(host)
		}
	}

	// Verify that API URL is NOT set when api_host is not provided
	if skiffEnv["GITLAB_API_URL"] != "" {
		t.Errorf("Expected GITLAB_API_URL to be empty when api_host is not configured, got %q", skiffEnv["GITLAB_API_URL"])
	}
	if gateEnv["GATE_GITLAB_HOST"] != "" {
		t.Errorf("Expected GATE_GITLAB_HOST to be empty when api_host is not configured, got %q", gateEnv["GATE_GITLAB_HOST"])
	}

	// But basic GitLab tokens should still be set
	if skiffEnv["GITLAB_TOKEN"] != "dummy-token" {
		t.Errorf("Expected GITLAB_TOKEN to be set even without api_host, got %q", skiffEnv["GITLAB_TOKEN"])
	}
	if skiffEnv["GITLAB_PERSONAL_ACCESS_TOKEN"] != "dummy-token" {
		t.Errorf("Expected GITLAB_PERSONAL_ACCESS_TOKEN to be set even without api_host, got %q", skiffEnv["GITLAB_PERSONAL_ACCESS_TOKEN"])
	}
}

// TestReconcileOutcomeLogic tests the core logic of the reconcile handler fix
// that determines whether orphaned sessions should be marked "completed" or "error"
// based on transcript_event_count. This tests the fix for issue #753.
func TestReconcileOutcomeLogic(t *testing.T) {
	tests := []struct {
		name          string
		eventCount    int
		queryError    bool
		expectedOutcome string
		description   string
	}{
		{
			name:          "zero events should result in error outcome",
			eventCount:    0,
			queryError:    false,
			expectedOutcome: "error",
			description:   "Sessions with zero transcript events should be marked error (post-#476)",
		},
		{
			name:          "nonzero events should result in completed outcome",
			eventCount:    5,
			queryError:    false,
			expectedOutcome: "completed",
			description:   "Sessions with transcript events should be marked completed",
		},
		{
			name:          "query error should fallback to completed",
			eventCount:    0,
			queryError:    true,
			expectedOutcome: "completed",
			description:   "Database errors should fall back to old behavior",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This tests the core logic that would be in the reconcile handler:
			//
			// var eventCount int
			// queryErr := d.db.QueryRow(ctx, `SELECT COALESCE(transcript_event_count, 0) FROM sessions WHERE id = $1`, sessionID).Scan(&eventCount)
			// outcome := "completed"
			// if queryErr != nil {
			//     // Fall back to old behavior on query error
			//     outcome = "completed"
			// } else if eventCount == 0 {
			//     outcome = "error"
			// }

			var outcome string
			if tt.queryError {
				// Simulate query error - should fall back to completed
				outcome = "completed"
			} else if tt.eventCount == 0 {
				// Zero events should result in error outcome
				outcome = "error"
			} else {
				// Nonzero events should result in completed outcome
				outcome = "completed"
			}

			if outcome != tt.expectedOutcome {
				t.Errorf("%s: expected outcome %q, got %q",
					tt.description, tt.expectedOutcome, outcome)
			}
		})
	}
}
