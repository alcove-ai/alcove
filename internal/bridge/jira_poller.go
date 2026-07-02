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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Failure cap constants for preventing infinite retry loops
const (
	// maxConsecutiveFailures is the maximum number of consecutive failed workflow runs
	// allowed for the same trigger_ref within the failure cap window.
	// This could be made configurable per-team or per-workflow in the future.
	maxConsecutiveFailures = 3

	// failureCapWindow defines how far back to look for failed runs when checking
	// the consecutive failure count.
	failureCapWindow = 30 * time.Minute
)

// JiraPoller polls JIRA for recently updated issues and triggers workflows
// whose definitions include a jira trigger that matches.
type JiraPoller struct {
	db             *pgxpool.Pool
	credStore      *CredentialStore
	workflowEngine *WorkflowEngine
	defStore       *AgentDefStore
	baseURL        string // e.g., "https://redhat.atlassian.net"
	pollInterval   time.Duration
	lastPollTime   time.Time
	client         *http.Client
}

// NewJiraPoller creates a JiraPoller with the given dependencies.
func NewJiraPoller(db *pgxpool.Pool, credStore *CredentialStore, we *WorkflowEngine, defStore *AgentDefStore) *JiraPoller {
	return &JiraPoller{
		db:             db,
		credStore:      credStore,
		workflowEngine: we,
		defStore:       defStore,
		baseURL:        "https://redhat.atlassian.net",
		pollInterval:   2 * time.Minute,
		lastPollTime:   time.Now().Add(-5 * time.Minute),
		client:         &http.Client{Timeout: 30 * time.Second},
	}
}

// Start begins the JIRA polling loop in the current goroutine. It blocks until
// the context is cancelled.
func (jp *JiraPoller) Start(ctx context.Context) {
	ticker := time.NewTicker(jp.pollInterval)
	defer ticker.Stop()

	// Initial poll after 30 seconds
	time.Sleep(30 * time.Second)
	jp.PollAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jp.PollAll(ctx)
		}
	}
}

// jiraPollTarget holds the information needed to match and dispatch a JIRA-
// triggered workflow.
type jiraPollTarget struct {
	sourceKey string // Source key to resolve workflow ID at dispatch time
	teamID    string
	trigger   *JiraTrigger
	name      string // For logging
}

// PollAll queries all schedules with JIRA triggers and polls JIRA for matching
// recently-updated issues.
func (jp *JiraPoller) PollAll(ctx context.Context) {
	// Check system mode — skip polling when paused.
	var mode string
	_ = jp.db.QueryRow(ctx, "SELECT value FROM system_state WHERE key = 'mode'").Scan(&mode)
	if mode == "paused" {
		return
	}

	rows, err := jp.db.Query(ctx, `
		SELECT s.name, s.event_config, s.team_id, s.source_key
		FROM schedules s
		WHERE s.enabled = true
		  AND COALESCE(s.trigger_type, 'cron') IN ('event', 'cron-and-event')
		  AND s.event_config IS NOT NULL
		  AND s.event_config::jsonb ? 'jira'
	`)
	if err != nil {
		log.Printf("jira-poller: error querying schedules: %v", err)
		return
	}
	defer rows.Close()

	var targets []jiraPollTarget

	for rows.Next() {
		var name, teamID, sourceKey string
		var eventConfigJSON []byte
		if err := rows.Scan(&name, &eventConfigJSON, &teamID, &sourceKey); err != nil {
			log.Printf("jira-poller: error scanning schedule: %v", err)
			continue
		}

		var trigger EventTrigger
		if err := json.Unmarshal(eventConfigJSON, &trigger); err != nil {
			log.Printf("jira-poller: error unmarshaling event_config for %s: %v", name, err)
			continue
		}
		if trigger.Jira == nil {
			log.Printf("jira-poller: schedule %s has no jira trigger config", name)
			continue
		}

		targets = append(targets, jiraPollTarget{
			sourceKey: sourceKey,
			teamID:    teamID,
			trigger:   trigger.Jira,
			name:      name,
		})
	}

	if len(targets) == 0 {
		return
	}

	// Group by team to minimize credential lookups.
	teamTargets := make(map[string][]jiraPollTarget)
	for _, t := range targets {
		teamTargets[t.teamID] = append(teamTargets[t.teamID], t)
	}

	for teamID, tgts := range teamTargets {
		jp.pollForTeam(ctx, teamID, tgts)
	}

	jp.lastPollTime = time.Now()
}

// extractADFText extracts plain text from an Atlassian Document Format (ADF) JSON object.
// If the input is not valid ADF JSON, it falls back to treating it as plain text.
// This provides compatibility with JIRA Server/DC instances that may still return
// plain text descriptions.
func extractADFText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try to parse as ADF JSON first
	var adfDoc map[string]interface{}
	if err := json.Unmarshal(raw, &adfDoc); err != nil {
		// Fallback: treat as plain text
		return string(raw)
	}

	// Extract text from ADF document
	return extractTextFromADFNode(adfDoc)
}

// extractTextFromADFNode recursively extracts text from an ADF node
func extractTextFromADFNode(node map[string]interface{}) string {
	var text strings.Builder

	// If this node has text, add it
	if nodeText, ok := node["text"].(string); ok {
		text.WriteString(nodeText)
	}

	// If this node has content, recursively process child nodes
	if content, ok := node["content"].([]interface{}); ok {
		for _, child := range content {
			if childNode, ok := child.(map[string]interface{}); ok {
				childText := extractTextFromADFNode(childNode)
				if childText != "" {
					if text.Len() > 0 {
						text.WriteString(" ")
					}
					text.WriteString(childText)
				}
			}
		}
	}

	return text.String()
}

func (jp *JiraPoller) pollForTeam(ctx context.Context, teamID string, targets []jiraPollTarget) {
	token, _, err := jp.credStore.AcquireSCMTokenForOwner(ctx, "jira", teamID)
	if err != nil {
		log.Printf("jira-poller: no jira credential for team %s: %v", teamID, err)
		return
	}

	// Collect all projects from targets.
	projectSet := make(map[string]bool)
	for _, t := range targets {
		for _, p := range t.trigger.Projects {
			projectSet[strings.ToUpper(p)] = true
		}
	}

	// Build JQL for recently updated issues.
	var projects []string
	for p := range projectSet {
		projects = append(projects, p)
	}

	minutesSinceLastPoll := int(time.Since(jp.lastPollTime).Minutes()) + 1
	jql := fmt.Sprintf("project IN (%s) AND updated >= \"-%dm\" ORDER BY updated DESC",
		strings.Join(projects, ","), minutesSinceLastPoll)

	// Search JIRA.
	searchURL := fmt.Sprintf("%s/rest/api/3/search/jql?jql=%s&maxResults=50&fields=key,summary,status,labels,components,description,issuetype,priority,assignee,reporter,issuelinks",
		jp.baseURL, url.QueryEscape(jql))

	data, err := jp.jiraRequest(ctx, token, "GET", searchURL, nil)
	if err != nil {
		log.Printf("jira-poller: search error: %v", err)
		return
	}

	var searchResult struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary     string          `json:"summary"`
				Description json.RawMessage `json:"description"`
				Status      struct {
					Name string `json:"name"`
				} `json:"status"`
				Labels     []string `json:"labels"`
				Components []struct {
					Name string `json:"name"`
				} `json:"components"`
				IssueType struct {
					Name string `json:"name"`
				} `json:"issuetype"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(data, &searchResult); err != nil {
		log.Printf("jira-poller: error parsing search results: %v", err)
		return
	}

	log.Printf("jira-poller: found %d recently updated issues in %v", len(searchResult.Issues), projects)

	// Cap enrichment at 10 issues per poll cycle to prevent API overload
	enrichmentCount := 0
	maxEnrichmentPerPoll := 10

	// Check each issue against each target's trigger.
	for _, issue := range searchResult.Issues {
		issueProject := strings.Split(issue.Key, "-")[0]
		var issueComponents []string
		for _, c := range issue.Fields.Components {
			issueComponents = append(issueComponents, c.Name)
		}

		for _, target := range targets {
			if target.trigger.Matches(issueProject, issueComponents, issue.Fields.Labels) {
				// Look up the workflow ID by source_key
				var workflowID string
				err := jp.db.QueryRow(ctx,
					`SELECT id FROM workflows WHERE source_key = $1 AND team_id = $2`,
					target.sourceKey, target.teamID,
				).Scan(&workflowID)
				if err != nil {
					log.Printf("jira-poller: error looking up workflow for schedule %s: %v", target.name, err)
					continue
				}

				// Check failure cap — prevent infinite retries for workflows that always fail
				var failureCount int
				err = jp.db.QueryRow(ctx, `
					SELECT COUNT(*) FROM workflow_runs
					WHERE workflow_id = $1 AND trigger_ref = $2
					AND created_at > NOW() - $3::interval
					AND status = 'failed'
				`, workflowID, issue.Key, failureCapWindow).Scan(&failureCount)
				if err != nil {
					log.Printf("jira-poller: error checking failure count for %s: %v", issue.Key, err)
					continue
				}

				if failureCount >= maxConsecutiveFailures {
					// Find the trigger label to remove (first matching label from the issue)
					var triggerLabel string
					for _, label := range issue.Fields.Labels {
						for _, triggerLbl := range target.trigger.Labels {
							if label == triggerLbl {
								triggerLabel = label
								break
							}
						}
						if triggerLabel != "" {
							break
						}
					}

					log.Printf("jira-poller: skipping %s — %d failures in 30m window (max: %d), removing label %q",
						issue.Key, failureCount, maxConsecutiveFailures, triggerLabel)

					// Post Jira comment explaining why retries stopped (best-effort)
					jp.postFailureCapComment(ctx, token, issue.Key, failureCount, triggerLabel)

					// Remove trigger label (best-effort)
					if triggerLabel != "" {
						jp.removeTriggerLabel(ctx, token, issue.Key, triggerLabel)
					}

					continue // Skip dispatch
				}

				// Check dedup — don't dispatch the same issue twice for the same workflow.
				var count int
				jp.db.QueryRow(ctx, `
					SELECT COUNT(*) FROM workflow_runs
					WHERE workflow_id = $1 AND trigger_ref = $2
					AND created_at > NOW() - INTERVAL '24 hours'
					AND status NOT IN ('failed', 'cancelled')
				`, workflowID, issue.Key).Scan(&count)

				if count > 0 {
					log.Printf("jira-poller: skipping %s — already dispatched recently (blocking run count: %d)", issue.Key, count)
					continue // Already dispatched recently
				}

				log.Printf("jira-poller: triggering workflow %s for issue %s", workflowID, issue.Key)

				var triggerContext map[string]interface{}

				// Try enrichment if we haven't hit the limit
				if enrichmentCount < maxEnrichmentPerPoll {
					enrichedMarkdown, additionalContext := jp.enrichJiraIssueContext(ctx, token, issue.Key)

					// Create basic trigger context and add enriched fields
					triggerContext = map[string]interface{}{
						"issue_key":        issue.Key,
						"issue_title":      issue.Fields.Summary,
						"issue_body":       extractADFText(issue.Fields.Description),
						"issue_url":        fmt.Sprintf("%s/browse/%s", jp.baseURL, issue.Key),
						"issue_status":     issue.Fields.Status.Name,
						"issue_labels":     issue.Fields.Labels,
						"issue_type":       issue.Fields.IssueType.Name,
						"enriched_context": enrichedMarkdown,
					}

					// Merge additional enriched context fields
					for key, value := range additionalContext {
						triggerContext[key] = value
					}

					enrichmentCount++
				} else {
					// Fall back to basic context if we've hit the enrichment limit
					if enrichmentCount == maxEnrichmentPerPoll {
						log.Printf("jira-poller: enrichment limit reached (%d), using basic context for remaining issues", maxEnrichmentPerPoll)
					}

					triggerContext = map[string]interface{}{
						"issue_key":    issue.Key,
						"issue_title":  issue.Fields.Summary,
						"issue_body":   extractADFText(issue.Fields.Description),
						"issue_url":    fmt.Sprintf("%s/browse/%s", jp.baseURL, issue.Key),
						"issue_status": issue.Fields.Status.Name,
						"issue_labels": issue.Fields.Labels,
						"issue_type":   issue.Fields.IssueType.Name,
					}
				}

				_, err = jp.workflowEngine.StartWorkflowRun(ctx, workflowID, "jira", issue.Key, target.teamID, triggerContext)
				if err != nil {
					log.Printf("jira-poller: error starting workflow for %s: %v", issue.Key, err)
				}
			}
		}
	}
}

func (jp *JiraPoller) jiraRequest(ctx context.Context, credential, method, reqURL string, body []byte) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}

	// JIRA Cloud uses Basic auth: email:api_token
	// The credential is stored as the raw API token; we need the email prefix.
	// Convention: credential stored as "email:token" or just "token".
	if strings.Contains(credential, ":") {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credential)))
	} else {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "alcove-jira-poller")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := jp.client.Do(req)
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

// postFailureCapComment posts a comment to a Jira issue explaining why workflow retries
// have been suspended due to too many consecutive failures.
func (jp *JiraPoller) postFailureCapComment(ctx context.Context, token, issueKey string, failureCount int, triggerLabel string) {
	commentText := fmt.Sprintf("⚠️ Alcove: workflow retries suspended for this ticket — failed %d times in the last 30 minutes. To retry, re-add the `%s` label after 30 minutes.",
		failureCount, triggerLabel)

	commentReq := map[string]interface{}{
		"body": map[string]interface{}{
			"type":    "doc",
			"version": 1,
			"content": []interface{}{
				map[string]interface{}{
					"type": "paragraph",
					"content": []interface{}{
						map[string]interface{}{
							"type": "text",
							"text": commentText,
						},
					},
				},
			},
		},
	}

	commentJSON, err := json.Marshal(commentReq)
	if err != nil {
		log.Printf("jira-poller: error marshaling failure cap comment for %s: %v", issueKey, err)
		return
	}

	commentURL := fmt.Sprintf("%s/rest/api/3/issue/%s/comment", jp.baseURL, issueKey)
	_, err = jp.jiraRequest(ctx, token, "POST", commentURL, commentJSON)
	if err != nil {
		log.Printf("jira-poller: error posting failure cap comment to %s: %v", issueKey, err)
	}
}

// removeTriggerLabel removes a trigger label from a Jira issue to prevent further
// automatic retries after the failure cap has been reached.
func (jp *JiraPoller) removeTriggerLabel(ctx context.Context, token, issueKey, label string) {
	updateReq := map[string]interface{}{
		"update": map[string]interface{}{
			"labels": []map[string]interface{}{
				{
					"remove": label,
				},
			},
		},
	}

	updateJSON, err := json.Marshal(updateReq)
	if err != nil {
		log.Printf("jira-poller: error marshaling label removal request for %s: %v", issueKey, err)
		return
	}

	updateURL := fmt.Sprintf("%s/rest/api/3/issue/%s", jp.baseURL, issueKey)
	_, err = jp.jiraRequest(ctx, token, "PUT", updateURL, updateJSON)
	if err != nil {
		log.Printf("jira-poller: error removing label %s from %s: %v", label, issueKey, err)
	}
}
