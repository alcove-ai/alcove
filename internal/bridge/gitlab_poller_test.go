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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGitLabPollerIssueContextEnrichment verifies that the GitLab enricher
// correctly extracts issue context for trigger template expansion, which
// is used by the GitLab poller to populate trigger context with issue_title
// and other issue fields.
func TestGitLabPollerIssueContextEnrichment(t *testing.T) {
	// Create a mock GitLab API server
	mux := http.NewServeMux()

	// Mock GitLab Issue API - returns issue details
	mux.HandleFunc("/api/v4/projects/test%2Frepo/issues/789", func(w http.ResponseWriter, r *http.Request) {
		issue := map[string]interface{}{
			"title":       "Add conventional commit format for PR titles",
			"description": "PR titles should use feat(#123): description format",
			"state":       "opened",
			"author": map[string]string{
				"username": "developer",
			},
			"labels":    []string{"ready-for-dev", "enhancement"},
			"assignees": []map[string]string{},
		}
		json.NewEncoder(w).Encode(issue)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	enricher := NewGitLabEnricher(ts.Client())

	// Test the issue context extraction that the GitLab poller now uses
	ctx := context.Background()
	issueContext := enricher.ExtractGitLabIssueContext(ctx, "fake-token", ts.URL, "test/repo", "789")

	// Verify that the context includes the fields expected by workflow templates
	if issueContext == nil {
		t.Fatal("Expected issue context to be returned, got nil")
	}

	expectedFields := map[string]interface{}{
		"issue_title":       "Add conventional commit format for PR titles",
		"issue_description": "PR titles should use feat(#123): description format",
		"issue_state":       "opened",
		"issue_author":      "developer",
		"issue_labels":      "ready-for-dev,enhancement",
		"issue_id":          "789",
		"gitlab_project":    "test/repo",
	}

	for field, expectedValue := range expectedFields {
		actualValue, exists := issueContext[field]
		if !exists {
			t.Errorf("Expected issue context to contain field %q", field)
			continue
		}
		if actualValue != expectedValue {
			t.Errorf("Expected issue context field %q to be %v, got %v", field, expectedValue, actualValue)
		}
	}

	// Verify that issue_body is aliased to issue_description
	issueBody, exists := issueContext["issue_body"]
	if !exists {
		t.Error("Expected issue_body field to be present as alias for issue_description")
	} else if issueBody != expectedFields["issue_description"] {
		t.Errorf("Expected issue_body to equal issue_description, got %v", issueBody)
	}
}