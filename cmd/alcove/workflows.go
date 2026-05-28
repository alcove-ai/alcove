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

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// workflowInfo represents a workflow from the API.
type workflowInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SourceRepo string `json:"source_repo"`
	SourceFile string `json:"source_file"`
	SyncError  string `json:"sync_error,omitempty"`
	LastSynced string `json:"last_synced"`
	TeamID     string `json:"team_id"`
}

// workflowsListResponse is the response from GET /api/v1/workflows.
type workflowsListResponse struct {
	Workflows []workflowInfo `json:"workflows"`
	Count     int            `json:"count"`
}

// workflowRunInfo represents a workflow run from the API.
type workflowRunInfo struct {
	ID          string `json:"id"`
	WorkflowID  string `json:"workflow_id"`
	Status      string `json:"status"`
	TriggerType string `json:"trigger_type,omitempty"`
	TriggerRef  string `json:"trigger_ref,omitempty"`
	CurrentStep string `json:"current_step,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	FinishedAt  string `json:"finished_at,omitempty"`
	TeamID      string `json:"team_id"`
	CreatedAt   string `json:"created_at"`
}

// workflowRunStep represents a workflow run step from the API.
type workflowRunStep struct {
	ID         string                 `json:"id"`
	RunID      string                 `json:"run_id"`
	StepID     string                 `json:"step_id"`
	SessionID  string                 `json:"session_id,omitempty"`
	Status     string                 `json:"status"`
	Outputs    map[string]interface{} `json:"outputs,omitempty"`
	Iteration  int                    `json:"iteration"`
	RetryCount int                    `json:"retry_count"`
	StartedAt  *time.Time             `json:"started_at,omitempty"`
	FinishedAt *time.Time             `json:"finished_at,omitempty"`
	// Fields enriched from workflow definition
	Type          string            `json:"type,omitempty"`
	Action        string            `json:"action,omitempty"`
	Depends       string            `json:"depends,omitempty"`
	MaxIterations int               `json:"max_iterations,omitempty"`
	Credentials   map[string]string `json:"credentials,omitempty"`
}

// workflowRunDetailResponse is the response from GET /api/v1/workflow-runs/{id}.
type workflowRunDetailResponse struct {
	WorkflowRun workflowRunInfo   `json:"workflow_run"`
	Steps       []workflowRunStep `json:"steps"`
}

// transcriptResponse is the response from GET /api/v1/sessions/{id}/transcript.
type transcriptResponse struct {
	SessionID  string          `json:"session_id"`
	Transcript json.RawMessage `json:"transcript"`
}

// workflowRunsListResponse is the response from GET /api/v1/workflow-runs.
type workflowRunsListResponse struct {
	WorkflowRuns []workflowRunInfo      `json:"workflow_runs"`
	Count        int                    `json:"count"`
	Total        int                    `json:"total"`
	Summary      *workflowRunsSummary   `json:"summary,omitempty"`
}

// workflowRunsSummary contains status counts for workflow runs.
type workflowRunsSummary struct {
	Running          int `json:"running"`
	Pending          int `json:"pending"`
	Completed        int `json:"completed"`
	Failed           int `json:"failed"`
	Cancelled        int `json:"cancelled"`
	AwaitingApproval int `json:"awaiting_approval"`
}

func newWorkflowsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflows",
		Short: "Manage workflows and workflow runs",
	}
	cmd.AddCommand(
		newWorkflowsListCmd(),
		newWorkflowsRunCmd(),
		newWorkflowsRunsCmd(),
		newWorkflowsCancelCmd(),
		newWorkflowsExportCmd(),
	)
	return cmd
}

// ---------- workflows list ----------

func newWorkflowsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all workflows",
		RunE:  runWorkflowsList,
	}
}

func runWorkflowsList(cmd *cobra.Command, _ []string) error {
	resp, err := apiRequest(cmd, http.MethodGet, "/api/v1/workflows", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return formatAPIError("listing workflows", resp.StatusCode, body)
	}

	var result workflowsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if isJSONOutput(cmd) {
		return outputJSON(result)
	}

	if len(result.Workflows) == 0 {
		fmt.Fprintln(os.Stderr, "No workflows found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSOURCE\tLAST SYNCED\tERROR")
	for _, wf := range result.Workflows {
		lastSynced := wf.LastSynced
		if t, err := time.Parse(time.RFC3339Nano, wf.LastSynced); err == nil {
			lastSynced = t.Local().Format("2006-01-02 15:04")
		}
		syncError := wf.SyncError
		if len(syncError) > 30 {
			syncError = syncError[:27] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			wf.ID, wf.Name, wf.SourceRepo, lastSynced, syncError)
	}
	return w.Flush()
}

// ---------- workflows run ----------

func newWorkflowsRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <id-or-name>",
		Short: "Trigger a workflow run",
		Long:  "Start a new workflow run by workflow ID or name. If a name is provided, it will be resolved to a workflow ID.",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorkflowsRun,
	}
	cmd.Flags().String("trigger-ref", "", "Optional trigger reference (e.g., branch name, PR number)")
	return cmd
}

func runWorkflowsRun(cmd *cobra.Command, args []string) error {
	idOrName := args[0]
	triggerRef, _ := cmd.Flags().GetString("trigger-ref")

	// Resolve name to ID if needed: first try to list workflows and match by name.
	workflowID, err := resolveWorkflowID(cmd, idOrName)
	if err != nil {
		return err
	}

	reqBody := map[string]string{
		"workflow_id": workflowID,
	}
	if triggerRef != "" {
		reqBody["trigger_ref"] = triggerRef
	}

	resp, err := apiRequest(cmd, http.MethodPost, "/api/v1/workflow-runs", reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return formatAPIError("running workflow", resp.StatusCode, body)
	}

	var result workflowRunInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if isJSONOutput(cmd) {
		return outputJSON(result)
	}

	fmt.Fprintf(os.Stderr, "Workflow run started: %s\n", result.ID)
	fmt.Println(result.ID)
	return nil
}

// resolveWorkflowID resolves a workflow ID or name to an ID.
// If the argument looks like a UUID (contains hyphens), it is used directly.
// Otherwise, workflows are listed and matched by name.
func resolveWorkflowID(cmd *cobra.Command, idOrName string) (string, error) {
	// If it looks like a UUID, use it directly.
	if looksLikeUUID(idOrName) {
		return idOrName, nil
	}

	// Otherwise, list workflows and match by name.
	resp, err := apiRequest(cmd, http.MethodGet, "/api/v1/workflows", nil)
	if err != nil {
		return "", fmt.Errorf("listing workflows: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", formatAPIError("listing workflows", resp.StatusCode, body)
	}

	var result workflowsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding workflows: %w", err)
	}

	for _, wf := range result.Workflows {
		if wf.Name == idOrName {
			return wf.ID, nil
		}
	}

	return "", fmt.Errorf("workflow %q not found", idOrName)
}

// looksLikeUUID returns true if the string contains hyphens, suggesting it is a UUID.
func looksLikeUUID(s string) bool {
	hyphenCount := 0
	for _, c := range s {
		if c == '-' {
			hyphenCount++
		}
	}
	return hyphenCount >= 4 && len(s) >= 32
}

// ---------- workflows runs ----------

func newWorkflowsRunsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List workflow runs with pagination and filtering",
		Long: `List workflow runs with advanced filtering options.

Examples:
  alcove workflows runs                           # List recent runs (default 25)
  alcove workflows runs --limit 50 --offset 25   # Pagination
  alcove workflows runs --status failed          # Failed runs only
  alcove workflows runs --workflow "SDLC Pipeline" # Filter by workflow name
  alcove workflows runs --since 7d               # Last 7 days
  alcove workflows runs --search "owner/repo#42" # Search by trigger ref
  alcove workflows runs --summary                # Include status summary`,
		RunE: runWorkflowsRuns,
	}
	cmd.Flags().String("status", "", "Filter by status (pending, running, completed, failed, cancelled, awaiting_approval)")
	cmd.Flags().Int("limit", 0, "Number of results per page (default 25, max 200)")
	cmd.Flags().Int("offset", 0, "Number of results to skip (default 0)")
	cmd.Flags().String("workflow", "", "Filter by workflow name (partial match)")
	cmd.Flags().String("since", "", "Filter by date: 1d, 7d, 30d, or YYYY-MM-DD")
	cmd.Flags().String("search", "", "Search by trigger ref (exact match)")
	cmd.Flags().Bool("summary", false, "Include status summary")
	return cmd
}

func runWorkflowsRuns(cmd *cobra.Command, _ []string) error {
	// Build query parameters
	params := make([]string, 0)

	if status, _ := cmd.Flags().GetString("status"); status != "" {
		params = append(params, "status="+status)
	}
	if limit, _ := cmd.Flags().GetInt("limit"); limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", limit))
	}
	if offset, _ := cmd.Flags().GetInt("offset"); offset > 0 {
		params = append(params, fmt.Sprintf("offset=%d", offset))
	}
	if workflow, _ := cmd.Flags().GetString("workflow"); workflow != "" {
		params = append(params, "workflow="+workflow)
	}
	if since, _ := cmd.Flags().GetString("since"); since != "" {
		params = append(params, "since="+since)
	}
	if search, _ := cmd.Flags().GetString("search"); search != "" {
		params = append(params, "search="+search)
	}
	if summary, _ := cmd.Flags().GetBool("summary"); summary {
		params = append(params, "summary=true")
	}

	path := "/api/v1/workflow-runs"
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	resp, err := apiRequest(cmd, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return formatAPIError("listing workflow runs", resp.StatusCode, body)
	}

	var result workflowRunsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if isJSONOutput(cmd) {
		return outputJSON(result)
	}

	// Display summary if available
	if result.Summary != nil {
		total := result.Summary.Running + result.Summary.Pending + result.Summary.Completed +
			result.Summary.Failed + result.Summary.Cancelled + result.Summary.AwaitingApproval

		fmt.Printf("Status Summary: %d running · %d pending · %d completed · %d failed",
			result.Summary.Running, result.Summary.Pending, result.Summary.Completed, result.Summary.Failed)
		if result.Summary.Cancelled > 0 {
			fmt.Printf(" · %d cancelled", result.Summary.Cancelled)
		}
		if result.Summary.AwaitingApproval > 0 {
			fmt.Printf(" · %d awaiting approval", result.Summary.AwaitingApproval)
		}
		fmt.Printf(" (total: %d)\n\n", total)
	}

	if len(result.WorkflowRuns) == 0 {
		fmt.Fprintln(os.Stderr, "No workflow runs found.")
		return nil
	}

	// Display pagination info
	if result.Total > 0 {
		start := 1
		if offset, _ := cmd.Flags().GetInt("offset"); offset > 0 {
			start = offset + 1
		}
		end := start + result.Count - 1
		fmt.Printf("Showing %d-%d of %d workflow runs\n", start, end, result.Total)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tWORKFLOW\tSTATUS\tTRIGGER\tCURRENT STEP\tCREATED")
	for _, run := range result.WorkflowRuns {
		created := run.CreatedAt
		if t, err := time.Parse(time.RFC3339Nano, run.CreatedAt); err == nil {
			created = t.Local().Format("2006-01-02 15:04")
		}
		trigger := run.TriggerType
		if run.TriggerRef != "" {
			trigger += ":" + run.TriggerRef
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			run.ID, run.WorkflowID, run.Status, trigger, run.CurrentStep, created)
	}
	return w.Flush()
}

// ---------- workflows cancel ----------

func newWorkflowsCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel a workflow run",
		Long:  "Cancel a workflow run and all its pending/running steps. Only workflow runs in pending, running, or awaiting_approval status can be cancelled.",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorkflowsCancel,
	}
}

func runWorkflowsCancel(cmd *cobra.Command, args []string) error {
	runID := args[0]

	// Use DELETE method to cancel the workflow run
	resp, err := apiRequest(cmd, http.MethodDelete, "/api/v1/workflow-runs/"+runID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return formatAPIError("cancelling workflow run", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if isJSONOutput(cmd) {
		return outputJSON(result)
	}

	fmt.Fprintf(os.Stderr, "Workflow run %s has been cancelled\n", runID)
	return nil
}

// ---------- workflows export ----------

func newWorkflowsExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <run-id>",
		Short: "Export workflow run data with session transcripts",
		Long: `Export a workflow run's metadata and all associated session transcripts
to a structured directory for offline analysis.

The export creates a directory with:
- run.json: Full workflow run metadata and steps
- NN-step-id/transcript.json: Session transcripts for agent steps
- NN-step-id/step.json: Metadata for bridge/skipped steps

Examples:
  alcove workflows export 81be9b17-1234-5678-9abc-def012345678
  alcove workflows export <run-id> --output-dir ./my-export/`,
		Args: cobra.ExactArgs(1),
		RunE: runWorkflowsExport,
	}
	cmd.Flags().String("output-dir", "", "Target directory (default: ./alcove-export-<short-id>/)")
	return cmd
}

func runWorkflowsExport(cmd *cobra.Command, args []string) error {
	runID := args[0]
	outputDir, _ := cmd.Flags().GetString("output-dir")

	// Determine output directory
	if outputDir == "" {
		shortID := runID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		outputDir = fmt.Sprintf("./alcove-export-%s", shortID)
	}

	// Check if directory exists and is non-empty
	if dirExists(outputDir) {
		isEmpty, err := isDirEmpty(outputDir)
		if err != nil {
			return fmt.Errorf("checking output directory: %w", err)
		}
		if !isEmpty {
			return fmt.Errorf("output directory %s already exists and is not empty", outputDir)
		}
	}

	fmt.Fprintf(os.Stderr, "Fetching workflow run %s...\n", runID)

	// Fetch workflow run and steps
	resp, err := apiRequest(cmd, http.MethodGet, "/api/v1/workflow-runs/"+runID, nil)
	if err != nil {
		return fmt.Errorf("fetching workflow run: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return formatAPIError("fetching workflow run", resp.StatusCode, body)
	}

	var runDetail workflowRunDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&runDetail); err != nil {
		return fmt.Errorf("decoding workflow run: %w", err)
	}

	// Warn if run is not completed
	if runDetail.WorkflowRun.Status != "completed" && runDetail.WorkflowRun.Status != "failed" && runDetail.WorkflowRun.Status != "cancelled" {
		fmt.Fprintf(os.Stderr, "Warning: workflow run status is '%s' - transcript data may be incomplete\n", runDetail.WorkflowRun.Status)
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Write run.json
	runJSONPath := filepath.Join(outputDir, "run.json")
	runFile, err := os.Create(runJSONPath)
	if err != nil {
		return fmt.Errorf("creating run.json: %w", err)
	}
	defer runFile.Close()

	encoder := json.NewEncoder(runFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(runDetail); err != nil {
		return fmt.Errorf("writing run.json: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Written run.json\n")

	// Process each step
	var transcriptCount, stepCount int
	for i, step := range runDetail.Steps {
		stepCount++
		stepDirName := fmt.Sprintf("%02d-%s", i+1, sanitizeStepID(step.StepID))
		stepDir := filepath.Join(outputDir, stepDirName)

		if err := os.MkdirAll(stepDir, 0755); err != nil {
			return fmt.Errorf("creating step directory %s: %w", stepDirName, err)
		}

		// If step has a session ID (agent step), fetch transcript
		if step.SessionID != "" {
			transcriptCount++
			fmt.Fprintf(os.Stderr, "Fetching transcript for step %s (session %s)...\n", step.StepID, step.SessionID)

			transcriptPath := filepath.Join(stepDir, "transcript.json")
			if err := fetchTranscript(cmd, step.SessionID, transcriptPath); err != nil {
				// On error, write a placeholder and continue
				fmt.Fprintf(os.Stderr, "Warning: failed to fetch transcript for session %s: %v\n", step.SessionID, err)

				errorFile, createErr := os.Create(filepath.Join(stepDir, "transcript_error.json"))
				if createErr == nil {
					json.NewEncoder(errorFile).Encode(map[string]interface{}{
						"error":      err.Error(),
						"session_id": step.SessionID,
						"step_id":    step.StepID,
					})
					errorFile.Close()
				}
			}
		} else {
			// Bridge/skipped step - write step.json with metadata
			stepJSONPath := filepath.Join(stepDir, "step.json")
			stepFile, err := os.Create(stepJSONPath)
			if err != nil {
				return fmt.Errorf("creating step.json for %s: %w", step.StepID, err)
			}

			encoder := json.NewEncoder(stepFile)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(step); err != nil {
				stepFile.Close()
				return fmt.Errorf("writing step.json for %s: %w", step.StepID, err)
			}
			stepFile.Close()
		}
	}

	// Print summary to stderr
	fmt.Fprintf(os.Stderr, "Exported %d steps (%d transcripts, %d bridge/skipped) to %s\n",
		stepCount, transcriptCount, stepCount-transcriptCount, outputDir)

	// Print output directory path to stdout (enables `cd $(alcove workflows export <id>)`)
	fmt.Println(outputDir)
	return nil
}

// sanitizeStepID removes dangerous characters from step IDs for filesystem use.
func sanitizeStepID(stepID string) string {
	// Replace dangerous characters with underscores
	reg := regexp.MustCompile(`[/\\:*?"<>|\.\s]+`)
	sanitized := reg.ReplaceAllString(stepID, "_")

	// Remove consecutive underscores
	reg2 := regexp.MustCompile(`_{2,}`)
	sanitized = reg2.ReplaceAllString(sanitized, "_")

	// Trim leading/trailing underscores
	sanitized = strings.Trim(sanitized, "_")

	// Ensure it's not empty
	if sanitized == "" {
		sanitized = "unknown"
	}

	return sanitized
}

// fetchTranscript fetches a session transcript and writes it to the specified file.
func fetchTranscript(cmd *cobra.Command, sessionID, outputPath string) error {
	// Use a longer timeout for transcript fetches (5 minutes)
	proxyConfig, err := resolveProxyConfig(cmd)
	if err != nil {
		return fmt.Errorf("resolving proxy config: %w", err)
	}

	client := &http.Client{
		Timeout: 5 * time.Minute,
	}
	if proxyConfig != nil && proxyConfig.ProxyURL != "" {
		// Configure proxy if needed - simplified version
		transport := &http.Transport{}
		client.Transport = transport
	}

	// Create the request manually to use the custom client
	server, err := resolveServer(cmd)
	if err != nil {
		return fmt.Errorf("resolving server: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/sessions/%s/transcript", server, sessionID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	// Add authentication headers (same logic as apiRequestRaw)
	req.Header.Set("Content-Type", "application/json")

	// Try Basic Auth first
	username, password := resolveBasicAuth(cmd)
	if username != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		req.Header.Set("Authorization", "Basic "+auth)
	} else {
		// Fall back to Bearer token
		token, err := loadToken()
		if err == nil && token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	// Set team header
	teamName := resolveTeamName(cmd)
	if teamName != "" {
		teamID, err := resolveTeamID(cmd, teamName)
		if err != nil {
			return fmt.Errorf("resolving team ID: %w", err)
		}
		req.Header.Set("X-Alcove-Team", teamID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching transcript: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("transcript not found (session may have been deleted)")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer outFile.Close()

	// Stream response directly to file
	if _, err := io.Copy(outFile, resp.Body); err != nil {
		return fmt.Errorf("writing transcript: %w", err)
	}

	return nil
}

// dirExists checks if a directory exists.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isDirEmpty checks if a directory is empty.
func isDirEmpty(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	return false, err
}
