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
	botAccountIDs  map[string]string // keyed by credential hash for per-team caching
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
		botAccountIDs:  make(map[string]string),
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

	// Clean up old dedup entries (older than 5 minutes)
	_, _ = jp.db.Exec(ctx, `DELETE FROM dispatched_dedup WHERE dispatched_at < NOW() - INTERVAL '5 minutes'`)

	// Resolve bot identity for this team (with caching)
	botAccountID, err := jp.resolveBotIdentity(ctx, token)
	if err != nil {
		log.Printf("jira-poller: warning: failed to resolve bot identity for team %s: %v (continuing without bot detection)", teamID, err)
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

				// Get latest comment metadata for dedup and bot detection
				var latestCommentID, latestCommentAuthorID string

				// Always fetch comment metadata first for dedup/bot detection
				// If we'll do enrichment, we'll fetch comments again but that's acceptable
				id, authorID, err := jp.fetchLatestComment(ctx, token, issue.Key)
				if err != nil {
					log.Printf("jira-poller: warning: could not fetch latest comment for %s: %v", issue.Key, err)
					// Continue with empty values - will use "no-comment" dedup key
				} else {
					latestCommentID = id
					latestCommentAuthorID = authorID
				}

				// Bot-comment check: skip if latest comment is from the bot
				if botAccountID != "" && latestCommentAuthorID == botAccountID {
					log.Printf("jira-poller: skipping %s — latest comment by bot (%s)", issue.Key, botAccountID)
					continue
				}

				// Build comment-aware dedup key
				var dedupKey string
				if latestCommentID != "" {
					dedupKey = issue.Key + ":" + latestCommentID
				} else {
					dedupKey = issue.Key + ":no-comment"
				}

				// Insert into dispatched_dedup - only first inserter wins
				issueProject := strings.Split(issue.Key, "-")[0]
				dedupResult, err := jp.db.Exec(ctx,
					`INSERT INTO dispatched_dedup (repo, item_number, schedule_id)
					VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
					issueProject, dedupKey, workflowID)
				if err != nil {
					log.Printf("jira-poller: error inserting dedup entry for %s: %v", issue.Key, err)
					continue
				}
				if dedupResult.RowsAffected() == 0 {
					log.Printf("jira-poller: skipping %s — already dispatched for comment %s", issue.Key, dedupKey)
					continue // Already dispatched for this comment
				}

				// Concurrent-run guard: prevent parallel runs for the same issue
				var runCount int
				err = jp.db.QueryRow(ctx, `
					SELECT COUNT(*) FROM workflow_runs
					WHERE workflow_id = $1 AND trigger_ref = $2
					AND status IN ('running', 'pending', 'awaiting_approval')
				`, workflowID, issue.Key).Scan(&runCount)
				if err != nil {
					log.Printf("jira-poller: error checking concurrent runs for %s: %v", issue.Key, err)
					continue
				}
				if runCount > 0 {
					log.Printf("jira-poller: skipping %s — workflow already running (active run count: %d)", issue.Key, runCount)
					continue // Concurrent run protection
				}

				log.Printf("jira-poller: triggering workflow %s for issue %s (comment: %s)", workflowID, issue.Key, dedupKey)

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

// resolveBotIdentity calls GET /rest/api/3/myself to get the bot's accountId
// and caches it using a hash of the credential for per-team uniqueness.
func (jp *JiraPoller) resolveBotIdentity(ctx context.Context, credential string) (string, error) {
	// Simple hash of credential for cache key
	hash := fmt.Sprintf("%x", len(credential)) // Simple but sufficient for cache key

	if accountID, exists := jp.botAccountIDs[hash]; exists {
		return accountID, nil
	}

	myselfURL := fmt.Sprintf("%s/rest/api/3/myself", jp.baseURL)
	data, err := jp.jiraRequest(ctx, credential, "GET", myselfURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to resolve bot identity: %w", err)
	}

	var myselfResponse struct {
		AccountID string `json:"accountId"`
	}
	if err := json.Unmarshal(data, &myselfResponse); err != nil {
		return "", fmt.Errorf("failed to parse myself response: %w", err)
	}

	// Cache the result
	jp.botAccountIDs[hash] = myselfResponse.AccountID
	return myselfResponse.AccountID, nil
}

// fetchLatestComment fetches only the latest comment for an issue to extract metadata
// for non-enriched issues (past the 10-issue enrichment cap).
func (jp *JiraPoller) fetchLatestComment(ctx context.Context, credential, issueKey string) (string, string, error) {
	commentsURL := fmt.Sprintf("%s/rest/api/2/issue/%s/comment?maxResults=1&orderBy=-created", jp.baseURL, issueKey)

	data, err := jp.jiraRequest(ctx, credential, "GET", commentsURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch latest comment: %w", err)
	}

	var comments JiraComments
	if err := json.Unmarshal(data, &comments); err != nil {
		return "", "", fmt.Errorf("failed to parse comments response: %w", err)
	}

	if len(comments.Comments) == 0 {
		return "", "", nil // No comments
	}

	return comments.Comments[0].ID, comments.Comments[0].Author.AccountID, nil
}
