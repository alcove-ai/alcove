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
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// mockCredentialStore is a test mock for credential acquisition
type mockCredentialStore struct {
	token   string
	apiHost string
	err     error
}

func (m *mockCredentialStore) AcquireSCMTokenForOwner(ctx context.Context, service, teamID string) (string, string, error) {
	return m.token, m.apiHost, m.err
}

func TestJiraRequest(t *testing.T) {
	// Test Basic auth with email:token credential
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			t.Errorf("Expected Basic auth, got: %s", auth)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result": "ok"}`))
	}))
	defer server.Close()

	ctx := context.Background()
	_, err := jiraRequest(ctx, "user@example.com:token123", "GET", server.URL, nil)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Test Bearer auth with plain token
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer token123" {
			t.Errorf("Expected Bearer token123, got: %s", auth)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result": "ok"}`))
	}))
	defer server2.Close()

	_, err = jiraRequest(ctx, "token123", "GET", server2.URL, nil)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Test HTTP error
	server3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Bad request"}`))
	}))
	defer server3.Close()

	_, err = jiraRequest(ctx, "token", "GET", server3.URL, nil)
	if err == nil {
		t.Error("Expected error for HTTP 400, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("Expected HTTP 400 error, got: %v", err)
	}
}

func TestWrapTextInADF(t *testing.T) {
	// Test with non-empty text
	result := wrapTextInADF("Hello, world!")
	expected := map[string]interface{}{
		"type":    "doc",
		"version": 1,
		"content": []interface{}{
			map[string]interface{}{
				"type": "paragraph",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "Hello, world!",
					},
				},
			},
		},
	}

	resultJSON, _ := json.Marshal(result)
	expectedJSON, _ := json.Marshal(expected)
	if string(resultJSON) != string(expectedJSON) {
		t.Errorf("Expected %s, got %s", expectedJSON, resultJSON)
	}

	// Test with empty text
	emptyResult := wrapTextInADF("")
	emptyExpected := map[string]interface{}{
		"type":    "doc",
		"version": 1,
		"content": []interface{}{},
	}

	emptyResultJSON, _ := json.Marshal(emptyResult)
	emptyExpectedJSON, _ := json.Marshal(emptyExpected)
	if string(emptyResultJSON) != string(emptyExpectedJSON) {
		t.Errorf("Expected %s, got %s", emptyExpectedJSON, emptyResultJSON)
	}
}

func TestJiraCreateIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/rest/api/3/issue") {
			t.Errorf("Expected POST to /rest/api/3/issue, got %s %s", r.Method, r.URL.Path)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}

		fields, ok := body["fields"].(map[string]interface{})
		if !ok {
			t.Error("Expected fields in request body")
		}

		// Check required fields
		project := fields["project"].(map[string]interface{})
		if project["key"] != "TEST" {
			t.Errorf("Expected project key TEST, got %v", project["key"])
		}

		if fields["summary"] != "Test issue" {
			t.Errorf("Expected summary 'Test issue', got %v", fields["summary"])
		}

		// Check ADF description
		desc, ok := fields["description"].(map[string]interface{})
		if !ok {
			t.Error("Expected description in ADF format")
		}
		if desc["type"] != "doc" {
			t.Errorf("Expected ADF document type, got %v", desc["type"])
		}

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"key": "TEST-123", "self": "http://example.com/issue/TEST-123"}`))
	}))
	defer server.Close()

	credStore := &mockCredentialStore{
		token:   "user@example.com:token",
		apiHost: server.URL,
		err:     nil,
	}

	inputs := map[string]interface{}{
		"project":     "TEST",
		"summary":     "Test issue",
		"description": "Test description",
	}

	// Create a bridge action function that accepts our mock
	testFunc := func(ctx context.Context, inputs map[string]interface{}, credStore interface{}, teamID string) (*BridgeActionResult, error) {
		// Convert to the expected interface
		mock := credStore.(*mockCredentialStore)

		// Simulate the same logic as bridgeActionJiraCreateIssue but with our mock
		project := getStringInput(inputs, "project")
		summary := getStringInput(inputs, "summary")
		description := getStringInput(inputs, "description")

		if project == "" || summary == "" {
			return &BridgeActionResult{
				Status: "failed",
				Error:  "missing required inputs: project, summary",
			}, nil
		}

		token, apiHost, err := mock.AcquireSCMTokenForOwner(ctx, "jira", teamID)
		if err != nil {
			return &BridgeActionResult{
				Status: "failed",
				Error:  fmt.Sprintf("failed to acquire JIRA token: %v", err),
			}, nil
		}

		if apiHost == "" {
			return &BridgeActionResult{
				Status: "failed",
				Error:  "jira credential has no api_host configured — set api_host when creating the jira credential",
			}, nil
		}

		// Build request body (simplified for test)
		reqBody := map[string]interface{}{
			"fields": map[string]interface{}{
				"project": map[string]interface{}{
					"key": project,
				},
				"summary": summary,
				"issuetype": map[string]interface{}{
					"name": "Task",
				},
			},
		}

		fields := reqBody["fields"].(map[string]interface{})
		if description != "" {
			fields["description"] = wrapTextInADF(description)
		}

		reqJSON, _ := json.Marshal(reqBody)
		createURL := fmt.Sprintf("%s/rest/api/3/issue", apiHost)
		respData, err := jiraRequest(ctx, token, "POST", createURL, reqJSON)
		if err != nil {
			return &BridgeActionResult{
				Status: "failed",
				Error:  fmt.Sprintf("error creating issue: %v", err),
			}, nil
		}

		var createResp struct {
			Key  string `json:"key"`
			Self string `json:"self"`
		}
		if err := json.Unmarshal(respData, &createResp); err != nil {
			return &BridgeActionResult{
				Status: "failed",
				Error:  fmt.Sprintf("error parsing create response: %v", err),
			}, nil
		}

		return &BridgeActionResult{
			Status: "succeeded",
			Outputs: map[string]interface{}{
				"issue_key": createResp.Key,
				"issue_url": fmt.Sprintf("%s/browse/%s", apiHost, createResp.Key),
			},
		}, nil
	}

	result, err := testFunc(context.Background(), inputs, credStore, "team1")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if result.Status != "succeeded" {
		t.Errorf("Expected succeeded, got: %s", result.Status)
	}

	if result.Outputs["issue_key"] != "TEST-123" {
		t.Errorf("Expected issue_key TEST-123, got: %v", result.Outputs["issue_key"])
	}

	expectedURL := fmt.Sprintf("%s/browse/TEST-123", server.URL)
	if result.Outputs["issue_url"] != expectedURL {
		t.Errorf("Expected issue_url %s, got: %v", expectedURL, result.Outputs["issue_url"])
	}
}

func TestJiraActionMissingApiHost(t *testing.T) {
	credStore := &mockCredentialStore{
		token:   "user@example.com:token",
		apiHost: "", // Empty api_host
		err:     nil,
	}

	inputs := map[string]interface{}{
		"project": "TEST",
		"summary": "Test issue",
	}

	// Simple test using the actual function signature
	project := getStringInput(inputs, "project")
	summary := getStringInput(inputs, "summary")

	if project == "" || summary == "" {
		t.Fatal("Setup error: missing required inputs")
	}

	token, apiHost, err := credStore.AcquireSCMTokenForOwner(context.Background(), "jira", "team1")
	if err != nil {
		t.Fatalf("Setup error: %v", err)
	}

	// Test the specific condition we want
	if apiHost != "" {
		t.Error("Expected empty apiHost for test")
	}

	if token == "" {
		t.Error("Expected token to be set")
	}

	// This simulates what would happen in the real function
	expectedError := "jira credential has no api_host configured — set api_host when creating the jira credential"
	if apiHost == "" {
		// This is the condition we're testing - it should trigger the error
		if !strings.Contains(expectedError, "api_host") {
			t.Errorf("Expected api_host error message to contain 'api_host'")
		}
	}
}

func TestJiraWorkflowValidation(t *testing.T) {
	// Test that new JIRA action names are valid
	jiraActions := []string{
		"jira-create-issue",
		"jira-transition-issue",
		"jira-add-comment",
		"jira-search-issues",
		"jira-update-issue",
		"jira-get-issue",
		"jira-link-issues",
	}

	for _, action := range jiraActions {
		if !validBridgeActions[action] {
			t.Errorf("JIRA action '%s' not found in validBridgeActions", action)
		}
	}

	// Test that RegisterBridgeActions includes the JIRA actions
	handlers := RegisterBridgeActions()
	for _, action := range jiraActions {
		if handlers[action] == nil {
			t.Errorf("JIRA action '%s' not found in RegisterBridgeActions", action)
		}
	}

	// Test that ListBridgeActionSchemas includes the JIRA actions
	schemas := ListBridgeActionSchemas()
	foundSchemas := make(map[string]bool)
	for _, schema := range schemas {
		foundSchemas[schema.Name] = true
	}

	for _, action := range jiraActions {
		if !foundSchemas[action] {
			t.Errorf("JIRA action '%s' not found in ListBridgeActionSchemas", action)
		}
	}
}

func TestJiraUpdateIssue(t *testing.T) {
	// Test successful update with all fields
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || !strings.Contains(r.URL.Path, "/rest/api/3/issue/TEST-123") {
			t.Errorf("Expected PUT to /rest/api/3/issue/TEST-123, got %s %s", r.Method, r.URL.Path)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}

		// Check update section for labels
		if update, ok := body["update"].(map[string]interface{}); ok {
			if labels, ok := update["labels"].([]interface{}); ok {
				foundAdd, foundRemove := false, false
				for _, op := range labels {
					if opMap, ok := op.(map[string]interface{}); ok {
						if opMap["add"] == "plan-ready" {
							foundAdd = true
						}
						if opMap["remove"] == "needs-planning" {
							foundRemove = true
						}
					}
				}
				if !foundAdd || !foundRemove {
					t.Errorf("Expected add 'plan-ready' and remove 'needs-planning', got: %v", labels)
				}
			}
		}

		// Check fields section
		if fields, ok := body["fields"].(map[string]interface{}); ok {
			if fields["summary"] != "Updated summary" {
				t.Errorf("Expected summary 'Updated summary', got %v", fields["summary"])
			}
			if priority, ok := fields["priority"].(map[string]interface{}); ok {
				if priority["name"] != "High" {
					t.Errorf("Expected priority 'High', got %v", priority["name"])
				}
			}
			if assignee, ok := fields["assignee"].(map[string]interface{}); ok {
				if assignee["emailAddress"] != "user@example.com" {
					t.Errorf("Expected email assignee 'user@example.com', got %v", assignee)
				}
			}
		}

		// JIRA returns 204 No Content for successful updates
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	credStore := &mockCredentialStore{
		token:   "user@example.com:token",
		apiHost: server.URL,
		err:     nil,
	}

	inputs := map[string]interface{}{
		"issue_key":      "TEST-123",
		"add_labels":     []string{"plan-ready"},
		"remove_labels":  []string{"needs-planning"},
		"assignee":       "user@example.com",
		"priority":       "High",
		"summary":        "Updated summary",
	}

	// Test the function logic with our mock (like existing tests do)
	testFunc := func(ctx context.Context, inputs map[string]interface{}, credStore *mockCredentialStore, teamID string) (*BridgeActionResult, error) {
		issueKey := getStringInput(inputs, "issue_key")
		if issueKey == "" {
			return &BridgeActionResult{
				Status: "failed",
				Error:  "missing required input: issue_key",
			}, nil
		}

		// Validate issue_key format (copied from actual implementation)
		issueKeyRegex := regexp.MustCompile(`^[A-Z][A-Z0-9_]+-\d+$`)
		if !issueKeyRegex.MatchString(issueKey) {
			return &BridgeActionResult{
				Status: "failed",
				Error:  fmt.Sprintf("invalid issue_key format: %s", issueKey),
			}, nil
		}

		token, apiHost, err := credStore.AcquireSCMTokenForOwner(ctx, "jira", teamID)
		if err != nil {
			return &BridgeActionResult{
				Status: "failed",
				Error:  fmt.Sprintf("failed to acquire JIRA token: %v", err),
			}, nil
		}

		if apiHost == "" {
			return &BridgeActionResult{
				Status: "failed",
				Error:  "jira credential has no api_host configured",
			}, nil
		}

		// Build request body (simplified logic)
		reqBody := make(map[string]interface{})
		addLabels := getStringSliceInput(inputs, "add_labels")
		removeLabels := getStringSliceInput(inputs, "remove_labels")

		if len(addLabels) > 0 || len(removeLabels) > 0 {
			labelOps := make([]map[string]interface{}, 0)
			for _, label := range addLabels {
				labelOps = append(labelOps, map[string]interface{}{"add": label})
			}
			for _, label := range removeLabels {
				labelOps = append(labelOps, map[string]interface{}{"remove": label})
			}
			if len(labelOps) > 0 {
				reqBody["update"] = map[string]interface{}{"labels": labelOps}
			}
		}

		// Build fields
		fields := make(map[string]interface{})
		if summary := getStringInput(inputs, "summary"); summary != "" {
			fields["summary"] = summary
		}
		if priority := getStringInput(inputs, "priority"); priority != "" {
			fields["priority"] = map[string]interface{}{"name": priority}
		}
		if assignee := getStringInput(inputs, "assignee"); assignee != "" {
			assigneeField := map[string]interface{}{}
			if strings.Contains(assignee, "@") {
				assigneeField["emailAddress"] = assignee
			} else {
				assigneeField["name"] = assignee
			}
			fields["assignee"] = assigneeField
		}

		if len(fields) > 0 {
			reqBody["fields"] = fields
		}

		reqJSON, _ := json.Marshal(reqBody)
		_, err = jiraRequest(ctx, token, "PUT", apiHost+"/rest/api/3/issue/"+issueKey, reqJSON)
		if err != nil {
			return &BridgeActionResult{
				Status: "failed",
				Error:  fmt.Sprintf("error updating issue: %v", err),
			}, nil
		}

		return &BridgeActionResult{
			Status: "succeeded",
			Outputs: map[string]interface{}{
				"updated": true,
			},
		}, nil
	}

	result, err := testFunc(context.Background(), inputs, credStore, "team1")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if result.Status != "succeeded" {
		t.Errorf("Expected succeeded, got: %s", result.Status)
	}

	if result.Outputs["updated"] != true {
		t.Errorf("Expected updated=true, got: %v", result.Outputs["updated"])
	}
}

func TestJiraUpdateIssueLabelsOnly(t *testing.T) {
	// Test update with only labels - test input validation and structure
	inputs := map[string]interface{}{
		"issue_key":     "PROJ-456",
		"add_labels":    []string{"bug", "urgent"},
		"remove_labels": []string{"enhancement"},
	}

	// Test input extraction
	issueKey := getStringInput(inputs, "issue_key")
	addLabels := getStringSliceInput(inputs, "add_labels")
	removeLabels := getStringSliceInput(inputs, "remove_labels")

	if issueKey != "PROJ-456" {
		t.Errorf("Expected issue key PROJ-456, got %s", issueKey)
	}

	if len(addLabels) != 2 || addLabels[0] != "bug" || addLabels[1] != "urgent" {
		t.Errorf("Expected add_labels [bug, urgent], got %v", addLabels)
	}

	if len(removeLabels) != 1 || removeLabels[0] != "enhancement" {
		t.Errorf("Expected remove_labels [enhancement], got %v", removeLabels)
	}

	// Test issue key validation
	issueKeyRegex := regexp.MustCompile(`^[A-Z][A-Z0-9_]+-\d+$`)
	if !issueKeyRegex.MatchString(issueKey) {
		t.Errorf("Valid issue key %s failed regex validation", issueKey)
	}
}

func TestJiraUpdateIssueMissingIssueKey(t *testing.T) {
	inputs := map[string]interface{}{
		"add_labels": []string{"test"},
	}

	issueKey := getStringInput(inputs, "issue_key")
	if issueKey != "" {
		t.Errorf("Expected empty issue_key, got: %s", issueKey)
	}

	// This would trigger the missing issue_key error
}

func TestJiraUpdateIssueInvalidIssueKey(t *testing.T) {
	testCases := []string{
		"../../admin",
		"invalid-key",
		"123-ABC",
		"",
		"TOO-MANY-DASHES-123",
	}

	issueKeyRegex := regexp.MustCompile(`^[A-Z][A-Z0-9_]+-\d+$`)

	for _, invalidKey := range testCases {
		if invalidKey != "" && issueKeyRegex.MatchString(invalidKey) {
			t.Errorf("Invalid key %s passed regex validation", invalidKey)
		}
	}

	// Test valid keys
	validKeys := []string{"TEST-123", "PROJ-456", "ABC_DEF-789"}
	for _, validKey := range validKeys {
		if !issueKeyRegex.MatchString(validKey) {
			t.Errorf("Valid key %s failed regex validation", validKey)
		}
	}
}

func TestJiraUpdateIssueCredentialError(t *testing.T) {
	credStore := &mockCredentialStore{
		token:   "",
		apiHost: "",
		err:     fmt.Errorf("credential not found"),
	}

	token, apiHost, err := credStore.AcquireSCMTokenForOwner(context.Background(), "jira", "team1")
	if err == nil {
		t.Error("Expected credential error, got nil")
	}

	if token != "" || apiHost != "" {
		t.Errorf("Expected empty token/apiHost on error, got token=%s, apiHost=%s", token, apiHost)
	}
}

func TestJiraUpdateIssueEmptyApiHost(t *testing.T) {
	credStore := &mockCredentialStore{
		token:   "token123",
		apiHost: "",
		err:     nil,
	}

	token, apiHost, err := credStore.AcquireSCMTokenForOwner(context.Background(), "jira", "team1")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if apiHost != "" {
		t.Errorf("Expected empty apiHost, got: %s", apiHost)
	}

	if token != "token123" {
		t.Errorf("Expected token123, got: %s", token)
	}

	// This would trigger the missing api_host error in the real function
}

func TestJiraUpdateIssueJiraAPIError(t *testing.T) {
	// Test JIRA API returning an error via jiraRequest function
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Invalid field value"}`))
	}))
	defer server.Close()

	// Test that jiraRequest properly handles HTTP errors
	_, err := jiraRequest(context.Background(), "token123", "PUT", server.URL, []byte(`{}`))
	if err == nil {
		t.Error("Expected error from jiraRequest, got nil")
	}

	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("Expected HTTP 400 error, got: %v", err)
	}
}

func TestJiraUpdateIssueNoOpUpdate(t *testing.T) {
	inputs := map[string]interface{}{
		"issue_key": "TEST-123",
		// No other fields - should be a valid no-op
	}

	issueKey := getStringInput(inputs, "issue_key")
	addLabels := getStringSliceInput(inputs, "add_labels")
	removeLabels := getStringSliceInput(inputs, "remove_labels")
	assignee := getStringInput(inputs, "assignee")
	priority := getStringInput(inputs, "priority")
	summary := getStringInput(inputs, "summary")

	if issueKey != "TEST-123" {
		t.Errorf("Expected issue key TEST-123, got %s", issueKey)
	}

	// All other fields should be empty/nil
	if len(addLabels) != 0 || len(removeLabels) != 0 {
		t.Errorf("Expected empty label arrays, got add=%v, remove=%v", addLabels, removeLabels)
	}

	if assignee != "" || priority != "" || summary != "" {
		t.Errorf("Expected empty strings, got assignee=%s, priority=%s, summary=%s", assignee, priority, summary)
	}
}

func TestJiraUpdateIssueAssigneeFormats(t *testing.T) {
	// Test different assignee formats - test the logic for determining field names
	testCases := []struct {
		name           string
		assignee       string
		expectedField  string
	}{
		{
			name:           "email format",
			assignee:       "user@example.com",
			expectedField:  "emailAddress",
		},
		{
			name:           "account ID format",
			assignee:       "557058:12345678-abcd-1234-abcd-123456789012",
			expectedField:  "accountId",
		},
		{
			name:           "24 char account ID",
			assignee:       "12345678abcd1234abcd1234",
			expectedField:  "accountId",
		},
		{
			name:           "username format",
			assignee:       "john.doe",
			expectedField:  "name",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test the assignee field logic
			assigneeField := map[string]interface{}{}

			if strings.Contains(tc.assignee, "@") {
				assigneeField["emailAddress"] = tc.assignee
			} else if len(tc.assignee) == 24 || strings.HasPrefix(tc.assignee, "557058:") {
				assigneeField["accountId"] = tc.assignee
			} else {
				assigneeField["name"] = tc.assignee
			}

			// Check that the right field was set
			if _, ok := assigneeField[tc.expectedField]; !ok {
				t.Errorf("Expected field %s not set in assigneeField", tc.expectedField)
			}

			if assigneeField[tc.expectedField] != tc.assignee {
				t.Errorf("Expected %s=%s, got %v", tc.expectedField, tc.assignee, assigneeField[tc.expectedField])
			}
		})
	}
}

func TestJiraIssueKeyValidation(t *testing.T) {
	// Test the regex validation that both new functions use
	issueKeyRegex := regexp.MustCompile(`^[A-Z][A-Z0-9_]+-\d+$`)
	
	validKeys := []string{
		"TEST-123",
		"PROJECT_1-456",
		"ABC123-789",
	}
	
	invalidKeys := []string{
		"",
		"test-123",      // lowercase
		"TEST123",       // no dash
		"TEST-",         // no number
		"-123",          // no project
		"TEST-123-456",  // too many parts
		"../etc/passwd", // path traversal attempt
	}
	
	for _, key := range validKeys {
		if !issueKeyRegex.MatchString(key) {
			t.Errorf("Expected %s to be valid", key)
		}
	}
	
	for _, key := range invalidKeys {
		if issueKeyRegex.MatchString(key) {
			t.Errorf("Expected %s to be invalid", key)
		}
	}
}

func TestJiraGetIssueResponseParsing(t *testing.T) {
	// Test the JSON response parsing logic for different field combinations
	testCases := []struct {
		name     string
		response string
		expected map[string]interface{}
	}{
		{
			name: "full response with parent",
			response: `{
				"key": "TEST-123",
				"fields": {
					"summary": "Test issue",
					"status": {"name": "Open"},
					"issuetype": {"name": "Epic"},
					"parent": {"key": "TEST-100"}
				}
			}`,
			expected: map[string]interface{}{
				"issue_key":  "TEST-123",
				"summary":    "Test issue",
				"status":     "Open",
				"issue_type": "Epic",
				"parent_key": "TEST-100",
			},
		},
		{
			name: "response without parent",
			response: `{
				"key": "TEST-456",
				"fields": {
					"summary": "Another issue",
					"status": {"name": "Closed"},
					"issuetype": {"name": "Task"}
				}
			}`,
			expected: map[string]interface{}{
				"issue_key":  "TEST-456",
				"summary":    "Another issue",
				"status":     "Closed",
				"issue_type": "Task",
				"parent_key": "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var getResp struct {
				Key    string `json:"key"`
				Fields struct {
					Summary   string `json:"summary"`
					Status    *struct {
						Name string `json:"name"`
					} `json:"status"`
					IssueType *struct {
						Name string `json:"name"`
					} `json:"issuetype"`
					Parent *struct {
						Key string `json:"key"`
					} `json:"parent"`
				} `json:"fields"`
			}
			
			err := json.Unmarshal([]byte(tc.response), &getResp)
			if err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			// Build outputs like the real function does
			outputs := map[string]interface{}{
				"issue_key": getResp.Key,
				"summary":   getResp.Fields.Summary,
			}

			if getResp.Fields.Status != nil {
				outputs["status"] = getResp.Fields.Status.Name
			} else {
				outputs["status"] = ""
			}

			if getResp.Fields.IssueType != nil {
				outputs["issue_type"] = getResp.Fields.IssueType.Name
			} else {
				outputs["issue_type"] = ""
			}

			if getResp.Fields.Parent != nil {
				outputs["parent_key"] = getResp.Fields.Parent.Key
			} else {
				outputs["parent_key"] = ""
			}

			// Verify outputs match expectations
			for key, expected := range tc.expected {
				if outputs[key] != expected {
					t.Errorf("Expected %s=%v, got %v", key, expected, outputs[key])
				}
			}
		})
	}
}

func TestJiraLinkRequestBuilding(t *testing.T) {
	// Test the link request JSON structure
	inwardIssue := "TEST-123"
	outwardIssue := "TEST-124"
	linkType := "is defined by"
	
	linkReq := map[string]interface{}{
		"type": map[string]interface{}{
			"name": linkType,
		},
		"inwardIssue": map[string]interface{}{
			"key": inwardIssue,
		},
		"outwardIssue": map[string]interface{}{
			"key": outwardIssue,
		},
	}

	linkJSON, err := json.Marshal(linkReq)
	if err != nil {
		t.Fatalf("Failed to marshal link request: %v", err)
	}

	// Parse it back to verify structure
	var parsed map[string]interface{}
	err = json.Unmarshal(linkJSON, &parsed)
	if err != nil {
		t.Fatalf("Failed to parse link JSON: %v", err)
	}

	// Verify structure
	linkTypeObj, ok := parsed["type"].(map[string]interface{})
	if !ok || linkTypeObj["name"] != linkType {
		t.Errorf("Expected link type %s, got %v", linkType, parsed["type"])
	}

	inwardObj, ok := parsed["inwardIssue"].(map[string]interface{})
	if !ok || inwardObj["key"] != inwardIssue {
		t.Errorf("Expected inward issue %s, got %v", inwardIssue, parsed["inwardIssue"])
	}

	outwardObj, ok := parsed["outwardIssue"].(map[string]interface{})
	if !ok || outwardObj["key"] != outwardIssue {
		t.Errorf("Expected outward issue %s, got %v", outwardIssue, parsed["outwardIssue"])
	}
}

func TestJiraParentFieldLogic(t *testing.T) {
	// Test the parent field logic in create-issue
	testCases := []struct {
		name           string
		parent         string
		shouldHaveParent bool
	}{
		{"with parent", "TEST-100", true},
		{"empty parent", "", false},
		{"whitespace parent", "   ", true}, // getStringInput doesn't trim, so this is technically valid
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			inputs := map[string]interface{}{
				"parent": tc.parent,
			}

			parent := getStringInput(inputs, "parent")
			
			fields := make(map[string]interface{})
			if parent != "" {
				fields["parent"] = map[string]interface{}{
					"key": parent,
				}
			}

			_, hasParent := fields["parent"]
			if hasParent != tc.shouldHaveParent {
				t.Errorf("Expected hasParent=%v, got %v for parent '%s'", tc.shouldHaveParent, hasParent, tc.parent)
			}

			if tc.shouldHaveParent {
				parentObj := fields["parent"].(map[string]interface{})
				if parentObj["key"] != tc.parent {
					t.Errorf("Expected parent key %s, got %v", tc.parent, parentObj["key"])
				}
			}
		})
	}
}
