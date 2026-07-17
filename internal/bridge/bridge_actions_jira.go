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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// bridgeActionJiraCreateIssue creates a new JIRA issue.
func bridgeActionJiraCreateIssue(ctx context.Context, inputs map[string]interface{}, credStore *CredentialStore, teamID string) (*BridgeActionResult, error) {
	project := getStringInput(inputs, "project")
	summary := getStringInput(inputs, "summary")
	issueType := getStringInput(inputs, "issue_type")
	description := getStringInput(inputs, "description")
	priority := getStringInput(inputs, "priority")

	if project == "" || summary == "" {
		return &BridgeActionResult{
			Status: "failed",
			Error:  "missing required inputs: project, summary",
		}, nil
	}

	if issueType == "" {
		issueType = "Task"
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
			Error:  "jira credential has no api_host configured — set api_host when creating the jira credential",
		}, nil
	}

	// Build request body
	reqBody := map[string]interface{}{
		"fields": map[string]interface{}{
			"project": map[string]interface{}{
				"key": project,
			},
			"summary": summary,
			"issuetype": map[string]interface{}{
				"name": issueType,
			},
		},
	}

	fields := reqBody["fields"].(map[string]interface{})

	// Add optional description (converted to ADF)
	if description != "" {
		fields["description"] = wrapTextInADF(description)
	}

	// Add optional priority
	if priority != "" {
		fields["priority"] = map[string]interface{}{
			"name": priority,
		}
	}

	// Add optional parent
	parent := getStringInput(inputs, "parent")
	if parent != "" {
		fields["parent"] = map[string]interface{}{
			"key": parent,
		}
	}

	// Handle labels ([]string)
	if labelsRaw, ok := inputs["labels"]; ok && labelsRaw != nil {
		switch v := labelsRaw.(type) {
		case []interface{}:
			var labels []string
			for _, label := range v {
				if str, ok := label.(string); ok {
					labels = append(labels, str)
				}
			}
			if len(labels) > 0 {
				fields["labels"] = labels
			}
		case []string:
			if len(v) > 0 {
				fields["labels"] = v
			}
		}
	}

	// Handle components ([]string)
	if componentsRaw, ok := inputs["components"]; ok && componentsRaw != nil {
		switch v := componentsRaw.(type) {
		case []interface{}:
			var components []map[string]interface{}
			for _, comp := range v {
				if str, ok := comp.(string); ok {
					components = append(components, map[string]interface{}{
						"name": str,
					})
				}
			}
			if len(components) > 0 {
				fields["components"] = components
			}
		case []string:
			var components []map[string]interface{}
			for _, comp := range v {
				components = append(components, map[string]interface{}{
					"name": comp,
				})
			}
			if len(components) > 0 {
				fields["components"] = components
			}
		}
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error marshaling request: %v", err),
		}, nil
	}

	// Create issue
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

// bridgeActionJiraTransitionIssue transitions a JIRA issue to a new status.
func bridgeActionJiraTransitionIssue(ctx context.Context, inputs map[string]interface{}, credStore *CredentialStore, teamID string) (*BridgeActionResult, error) {
	issueKey := getStringInput(inputs, "issue_key")
	transition := getStringInput(inputs, "transition")

	if issueKey == "" || transition == "" {
		return &BridgeActionResult{
			Status: "failed",
			Error:  "missing required inputs: issue_key, transition",
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
			Error:  "jira credential has no api_host configured — set api_host when creating the jira credential",
		}, nil
	}

	// First, get available transitions
	transitionsURL := fmt.Sprintf("%s/rest/api/3/issue/%s/transitions", apiHost, issueKey)
	respData, err := jiraRequest(ctx, token, "GET", transitionsURL, nil)
	if err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error getting transitions for %s: %v", issueKey, err),
		}, nil
	}

	var transitionsResp struct {
		Transitions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"transitions"`
	}
	if err := json.Unmarshal(respData, &transitionsResp); err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error parsing transitions response: %v", err),
		}, nil
	}

	// Find transition ID by name, or try using transition as ID directly
	var transitionID string
	for _, t := range transitionsResp.Transitions {
		if strings.EqualFold(t.Name, transition) {
			transitionID = t.ID
			break
		}
	}

	// If not found by name, try treating input as numeric ID
	if transitionID == "" {
		if _, err := strconv.Atoi(transition); err == nil {
			transitionID = transition
		} else {
			return &BridgeActionResult{
				Status: "failed",
				Error:  fmt.Sprintf("transition '%s' not found for issue %s", transition, issueKey),
			}, nil
		}
	}

	// Perform the transition
	transitionReq := map[string]interface{}{
		"transition": map[string]interface{}{
			"id": transitionID,
		},
	}

	transitionJSON, err := json.Marshal(transitionReq)
	if err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error marshaling transition request: %v", err),
		}, nil
	}

	_, err = jiraRequest(ctx, token, "POST", transitionsURL, transitionJSON)
	if err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error transitioning issue %s: %v", issueKey, err),
		}, nil
	}

	return &BridgeActionResult{
		Status: "succeeded",
		Outputs: map[string]interface{}{
			"transitioned": true,
		},
	}, nil
}

// bridgeActionJiraAddComment adds a comment to a JIRA issue.
func bridgeActionJiraAddComment(ctx context.Context, inputs map[string]interface{}, credStore *CredentialStore, teamID string) (*BridgeActionResult, error) {
	issueKey := getStringInput(inputs, "issue_key")
	body := getStringInput(inputs, "body")

	if issueKey == "" || body == "" {
		return &BridgeActionResult{
			Status: "failed",
			Error:  "missing required inputs: issue_key, body",
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
			Error:  "jira credential has no api_host configured — set api_host when creating the jira credential",
		}, nil
	}

	// Build comment request with ADF body
	commentReq := map[string]interface{}{
		"body": wrapTextInADF(body),
	}

	commentJSON, err := json.Marshal(commentReq)
	if err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error marshaling comment request: %v", err),
		}, nil
	}

	// Add comment
	commentURL := fmt.Sprintf("%s/rest/api/3/issue/%s/comment", apiHost, issueKey)
	respData, err := jiraRequest(ctx, token, "POST", commentURL, commentJSON)
	if err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error adding comment to %s: %v", issueKey, err),
		}, nil
	}

	var commentResp struct {
		ID   string `json:"id"`
		Self string `json:"self"`
	}
	if err := json.Unmarshal(respData, &commentResp); err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error parsing comment response: %v", err),
		}, nil
	}

	return &BridgeActionResult{
		Status: "succeeded",
		Outputs: map[string]interface{}{
			"comment_id":  commentResp.ID,
			"comment_url": fmt.Sprintf("%s/browse/%s?focusedCommentId=%s", apiHost, issueKey, commentResp.ID),
		},
	}, nil
}

// bridgeActionJiraSearchIssues searches JIRA issues using JQL.
func bridgeActionJiraSearchIssues(ctx context.Context, inputs map[string]interface{}, credStore *CredentialStore, teamID string) (*BridgeActionResult, error) {
	jql := getStringInput(inputs, "jql")

	if jql == "" {
		return &BridgeActionResult{
			Status: "failed",
			Error:  "missing required input: jql",
		}, nil
	}

	maxResults := getIntInput(inputs, "max_results")
	if maxResults == 0 {
		maxResults = 50
	}
	if maxResults > 100 {
		maxResults = 100
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
			Error:  "jira credential has no api_host configured — set api_host when creating the jira credential",
		}, nil
	}

	// Search issues
	searchURL := fmt.Sprintf("%s/rest/api/3/search?jql=%s&maxResults=%d&fields=key,summary,status,issuetype,priority",
		apiHost, url.QueryEscape(jql), maxResults)

	respData, err := jiraRequest(ctx, token, "GET", searchURL, nil)
	if err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error searching issues with JQL '%s': %v", jql, err),
		}, nil
	}

	var searchResp struct {
		Total  int `json:"total"`
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary   string `json:"summary"`
				Status    struct {
					Name string `json:"name"`
				} `json:"status"`
				IssueType struct {
					Name string `json:"name"`
				} `json:"issuetype"`
				Priority *struct {
					Name string `json:"name"`
				} `json:"priority"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(respData, &searchResp); err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error parsing search response: %v", err),
		}, nil
	}

	// Build structured output
	var issueKeys []string
	var issues []map[string]interface{}

	for _, issue := range searchResp.Issues {
		issueKeys = append(issueKeys, issue.Key)

		issueObj := map[string]interface{}{
			"key":     issue.Key,
			"summary": issue.Fields.Summary,
			"status":  issue.Fields.Status.Name,
			"type":    issue.Fields.IssueType.Name,
			"url":     fmt.Sprintf("%s/browse/%s", apiHost, issue.Key),
		}

		// Priority is optional in JIRA
		if issue.Fields.Priority != nil {
			issueObj["priority"] = issue.Fields.Priority.Name
		} else {
			issueObj["priority"] = ""
		}

		issues = append(issues, issueObj)
	}

	return &BridgeActionResult{
		Status: "succeeded",
		Outputs: map[string]interface{}{
			"issues":     issues,
			"issue_keys": issueKeys,
			"total":      searchResp.Total,
		},
	}, nil
}

// jiraRequest performs an authenticated HTTP request to the JIRA API.
func jiraRequest(ctx context.Context, credential, method, reqURL string, body []byte) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}

	// JIRA Cloud uses Basic auth for email:api_token credentials, Bearer for plain tokens
	if strings.Contains(credential, ":") {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credential)))
	} else {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "alcove-bridge-action")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// bridgeActionJiraUpdateIssue updates JIRA issue metadata (labels, assignee, priority, summary).
func bridgeActionJiraUpdateIssue(ctx context.Context, inputs map[string]interface{}, credStore *CredentialStore, teamID string) (*BridgeActionResult, error) {
	issueKey := getStringInput(inputs, "issue_key")
	if issueKey == "" {
		return &BridgeActionResult{
			Status: "failed",
			Error:  "missing required input: issue_key",
		}, nil
	}

	// Validate issue_key format to prevent path traversal
	issueKeyRegex := regexp.MustCompile(`^[A-Z][A-Z0-9_]+-\d+$`)
	if !issueKeyRegex.MatchString(issueKey) {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("invalid issue_key format: %s (must match [A-Z][A-Z0-9_]+-\\d+)", issueKey),
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
			Error:  "jira credential has no api_host configured — set api_host when creating the jira credential",
		}, nil
	}

	// Extract inputs
	addLabels := getStringSliceInput(inputs, "add_labels")
	removeLabels := getStringSliceInput(inputs, "remove_labels")
	assignee := getStringInput(inputs, "assignee")
	priority := getStringInput(inputs, "priority")
	summary := getStringInput(inputs, "summary")

	// Build request body with update and fields sections
	reqBody := make(map[string]interface{})

	// Build update section for incremental label changes
	if len(addLabels) > 0 || len(removeLabels) > 0 {
		labelOps := make([]map[string]interface{}, 0)

		for _, label := range addLabels {
			labelOps = append(labelOps, map[string]interface{}{
				"add": label,
			})
		}

		for _, label := range removeLabels {
			labelOps = append(labelOps, map[string]interface{}{
				"remove": label,
			})
		}

		if len(labelOps) > 0 {
			reqBody["update"] = map[string]interface{}{
				"labels": labelOps,
			}
		}
	}

	// Build fields section for direct field replacement
	fields := make(map[string]interface{})

	if summary != "" {
		fields["summary"] = summary
	}

	// Add optional description
	description := getStringInput(inputs, "description")
	if description != "" {
		fields["description"] = wrapTextInADF(description)
	}

	if priority != "" {
		fields["priority"] = map[string]interface{}{
			"name": priority,
		}
	}

	if assignee != "" {
		// Auto-detect assignee format and build appropriate assignee object
		assigneeField := map[string]interface{}{}

		if strings.Contains(assignee, "@") {
			// Looks like an email - try email lookup first, then treat as emailAddress
			assigneeField["emailAddress"] = assignee
		} else if len(assignee) == 24 || strings.HasPrefix(assignee, "557058:") {
			// Looks like Atlassian account ID format
			assigneeField["accountId"] = assignee
		} else {
			// Treat as username/displayName for legacy/self-hosted Jira
			assigneeField["name"] = assignee
		}

		fields["assignee"] = assigneeField
	}

	if len(fields) > 0 {
		reqBody["fields"] = fields
	}

	// If both update and fields are empty, that's a valid no-op update
	if len(reqBody) == 0 {
		return &BridgeActionResult{
			Status: "succeeded",
			Outputs: map[string]interface{}{
				"updated": true,
			},
		}, nil
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error marshaling request: %v", err),
		}, nil
	}

	// Update issue
	updateURL := fmt.Sprintf("%s/rest/api/3/issue/%s", apiHost, url.PathEscape(issueKey))
	respData, err := jiraRequest(ctx, token, "PUT", updateURL, reqJSON)
	if err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error updating issue %s: %v", issueKey, err),
		}, nil
	}

	// Handle 204 No Content response - JIRA PUT returns empty body on success
	if len(respData) == 0 {
		return &BridgeActionResult{
			Status: "succeeded",
			Outputs: map[string]interface{}{
				"updated": true,
			},
		}, nil
	}

	// If there's response data, try to parse it (some JIRA configurations might return data)
	var updateResp interface{}
	if err := json.Unmarshal(respData, &updateResp); err != nil {
		// If parsing fails, but we got here, the update likely succeeded
		return &BridgeActionResult{
			Status: "succeeded",
			Outputs: map[string]interface{}{
				"updated": true,
			},
		}, nil
	}

	return &BridgeActionResult{
		Status: "succeeded",
		Outputs: map[string]interface{}{
			"updated": true,
		},
	}, nil
}

// wrapTextInADF converts plain text to minimal Atlassian Document Format.
func wrapTextInADF(text string) map[string]interface{} {
	if text == "" {
		return map[string]interface{}{
			"type":    "doc",
			"version": 1,
			"content": []interface{}{},
		}
	}

	return map[string]interface{}{
		"type":    "doc",
		"version": 1,
		"content": []interface{}{
			map[string]interface{}{
				"type": "paragraph",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": text,
					},
				},
			},
		},
	}
}

// bridgeActionJiraGetIssue retrieves a single issue's fields from JIRA.
func bridgeActionJiraGetIssue(ctx context.Context, inputs map[string]interface{}, credStore *CredentialStore, teamID string) (*BridgeActionResult, error) {
	issueKey := getStringInput(inputs, "issue_key")
	if issueKey == "" {
		return &BridgeActionResult{
			Status: "failed",
			Error:  "missing required input: issue_key",
		}, nil
	}

	// Validate issue_key format to prevent path traversal
	issueKeyRegex := regexp.MustCompile(`^[A-Z][A-Z0-9_]+-\d+$`)
	if !issueKeyRegex.MatchString(issueKey) {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("invalid issue_key format: %s (must match [A-Z][A-Z0-9_]+-\\d+)", issueKey),
		}, nil
	}

	fields := getStringInput(inputs, "fields")
	if fields == "" {
		fields = "summary,status,issuetype,parent"
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
			Error:  "jira credential has no api_host configured — set api_host when creating the jira credential",
		}, nil
	}

	// Get issue
	getURL := fmt.Sprintf("%s/rest/api/3/issue/%s?fields=%s", apiHost, url.PathEscape(issueKey), url.QueryEscape(fields))
	respData, err := jiraRequest(ctx, token, "GET", getURL, nil)
	if err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error getting issue %s: %v", issueKey, err),
		}, nil
	}

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
	if err := json.Unmarshal(respData, &getResp); err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error parsing get response: %v", err),
		}, nil
	}

	// Build structured output with safe nil handling
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

	return &BridgeActionResult{
		Status:  "succeeded",
		Outputs: outputs,
	}, nil
}

// bridgeActionJiraLinkIssues creates a link between two JIRA issues.
func bridgeActionJiraLinkIssues(ctx context.Context, inputs map[string]interface{}, credStore *CredentialStore, teamID string) (*BridgeActionResult, error) {
	inwardIssue := getStringInput(inputs, "inward_issue")
	outwardIssue := getStringInput(inputs, "outward_issue")
	linkType := getStringInput(inputs, "link_type")

	if inwardIssue == "" || outwardIssue == "" || linkType == "" {
		return &BridgeActionResult{
			Status: "failed",
			Error:  "missing required inputs: inward_issue, outward_issue, link_type",
		}, nil
	}

	// Validate both issue keys
	issueKeyRegex := regexp.MustCompile(`^[A-Z][A-Z0-9_]+-\d+$`)
	if !issueKeyRegex.MatchString(inwardIssue) {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("invalid inward_issue format: %s (must match [A-Z][A-Z0-9_]+-\\d+)", inwardIssue),
		}, nil
	}
	if !issueKeyRegex.MatchString(outwardIssue) {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("invalid outward_issue format: %s (must match [A-Z][A-Z0-9_]+-\\d+)", outwardIssue),
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
			Error:  "jira credential has no api_host configured — set api_host when creating the jira credential",
		}, nil
	}

	// Build link request
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
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error marshaling link request: %v", err),
		}, nil
	}

	// Create link
	linkURL := fmt.Sprintf("%s/rest/api/3/issueLink", apiHost)
	_, err = jiraRequest(ctx, token, "POST", linkURL, linkJSON)
	if err != nil {
		return &BridgeActionResult{
			Status: "failed",
			Error:  fmt.Sprintf("error linking issues %s and %s: %v", inwardIssue, outwardIssue, err),
		}, nil
	}

	return &BridgeActionResult{
		Status: "succeeded",
		Outputs: map[string]interface{}{
			"linked": true,
		},
	}, nil
}
