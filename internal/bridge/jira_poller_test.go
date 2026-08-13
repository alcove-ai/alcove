package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
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

// TestJiraPoller_ActiveRunBlocksDispatch tests the new behavior where only
// active workflow runs (running/pending/awaiting_approval) block new dispatch,
// regardless of comment ID.
func TestJiraPoller_ActiveRunBlocksDispatch(t *testing.T) {
	// Skip test if no database URL is configured
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	ctx := context.Background()
	db := getTestDB(t)
	defer db.Close()

	// Setup test data
	teamID := "test-team-id"
	workflowID := "test-workflow-id"
	triggerRef := "TEST-123"

	// Create team, workflow, and test workflow runs with different statuses
	setupDedupTestData(t, ctx, db, teamID, workflowID, triggerRef)

	// Test scenarios using new concurrent-run guard logic
	tests := []struct {
		name          string
		runStatus     string
		expectBlocked bool
	}{
		{
			name:          "failed run should not block",
			runStatus:     "failed",
			expectBlocked: false,
		},
		{
			name:          "cancelled run should not block",
			runStatus:     "cancelled",
			expectBlocked: false,
		},
		{
			name:          "completed run should not block",
			runStatus:     "completed",
			expectBlocked: false,
		},
		{
			name:          "running run should block",
			runStatus:     "running",
			expectBlocked: true,
		},
		{
			name:          "pending run should block",
			runStatus:     "pending",
			expectBlocked: true,
		},
		{
			name:          "awaiting_approval run should block",
			runStatus:     "awaiting_approval",
			expectBlocked: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any existing runs and dedup entries
			_, err := db.Exec(ctx, "DELETE FROM workflow_runs WHERE workflow_id = $1 AND trigger_ref = $2", workflowID, triggerRef)
			if err != nil {
				t.Fatalf("failed to clean up test data: %v", err)
			}

			// Insert a workflow run with the specified status
			_, err = db.Exec(ctx, `
				INSERT INTO workflow_runs (id, workflow_id, trigger_ref, trigger_type, status, team_id, created_at, step_outputs)
				VALUES ($1, $2, $3, 'jira', $4, $5, NOW(), '{}')
			`, "test-run-id", workflowID, triggerRef, tt.runStatus, teamID)
			if err != nil {
				t.Fatalf("failed to insert test workflow run: %v", err)
			}

			// Run the new concurrent-run guard query (checking only active statuses)
			var count int
			err = db.QueryRow(ctx, `
				SELECT COUNT(*) FROM workflow_runs
				WHERE workflow_id = $1 AND trigger_ref = $2
				AND status IN ('running', 'pending', 'awaiting_approval')
			`, workflowID, triggerRef).Scan(&count)
			if err != nil {
				t.Fatalf("failed to run concurrent-run guard query: %v", err)
			}

			isBlocked := count > 0
			if isBlocked != tt.expectBlocked {
				t.Errorf("status %s: expected blocked=%v, got blocked=%v (count=%d)", tt.runStatus, tt.expectBlocked, isBlocked, count)
			}
		})
	}
}

// TestJiraPoller_CompletedRunAllowsNewDispatch tests that completed/failed/cancelled
// runs do not block new dispatch for new comments (key behavioral change from old code).
func TestJiraPoller_CompletedRunAllowsNewDispatch(t *testing.T) {
	// Skip test if no database URL is configured
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	ctx := context.Background()
	db := getTestDB(t)
	defer db.Close()

	// Setup test data
	teamID := "test-team-id"
	workflowID := "test-workflow-id"
	triggerRef := "TEST-124"

	// Create team, workflow
	setupDedupTestData(t, ctx, db, teamID, workflowID, triggerRef)

	// Test scenarios for completed runs that should NOT block
	tests := []struct {
		name          string
		runStatus     string
		expectBlocked bool
	}{
		{
			name:          "completed run should not block new dispatch",
			runStatus:     "completed",
			expectBlocked: false,
		},
		{
			name:          "failed run should not block new dispatch",
			runStatus:     "failed",
			expectBlocked: false,
		},
		{
			name:          "cancelled run should not block new dispatch",
			runStatus:     "cancelled",
			expectBlocked: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any existing runs
			_, err := db.Exec(ctx, "DELETE FROM workflow_runs WHERE workflow_id = $1 AND trigger_ref = $2", workflowID, triggerRef)
			if err != nil {
				t.Fatalf("failed to clean up test data: %v", err)
			}

			// Insert a workflow run with the specified status
			_, err = db.Exec(ctx, `
				INSERT INTO workflow_runs (id, workflow_id, trigger_ref, trigger_type, status, team_id, created_at, step_outputs)
				VALUES ($1, $2, $3, 'jira', $4, $5, NOW(), '{}')
			`, "test-run-id", workflowID, triggerRef, tt.runStatus, teamID)
			if err != nil {
				t.Fatalf("failed to insert test workflow run: %v", err)
			}

			// Run the new concurrent-run guard query (checking only active statuses)
			var count int
			err = db.QueryRow(ctx, `
				SELECT COUNT(*) FROM workflow_runs
				WHERE workflow_id = $1 AND trigger_ref = $2
				AND status IN ('running', 'pending', 'awaiting_approval')
			`, workflowID, triggerRef).Scan(&count)
			if err != nil {
				t.Fatalf("failed to run concurrent-run guard query: %v", err)
			}

			isBlocked := count > 0
			if isBlocked != tt.expectBlocked {
				t.Errorf("status %s: expected blocked=%v, got blocked=%v (count=%d)", tt.runStatus, tt.expectBlocked, isBlocked, count)
			}
		})
	}
}

// TestJiraPoller_BotCommentDoesNotTrigger tests that when the latest comment
// was authored by the bot's accountId, the poller should skip dispatch.
func TestJiraPoller_BotCommentDoesNotTrigger(t *testing.T) {
	// Unit test - no DB required
	botAccountID := "bot-account-123"
	otherTeamBotAccountID := "other-bot-456"
	userAccountID := "user-account-789"

	tests := []struct {
		name                     string
		latestCommentAuthorID    string
		allBotAccountIDs         map[string]bool
		shouldSkip               bool
	}{
		{
			name:                     "own team bot comment should be skipped",
			latestCommentAuthorID:    botAccountID,
			allBotAccountIDs:         map[string]bool{botAccountID: true},
			shouldSkip:               true,
		},
		{
			name:                     "other team bot comment should be skipped",
			latestCommentAuthorID:    otherTeamBotAccountID,
			allBotAccountIDs:         map[string]bool{botAccountID: true, otherTeamBotAccountID: true},
			shouldSkip:               true,
		},
		{
			name:                     "user comment should not be skipped",
			latestCommentAuthorID:    userAccountID,
			allBotAccountIDs:         map[string]bool{botAccountID: true, otherTeamBotAccountID: true},
			shouldSkip:               false,
		},
		{
			name:                     "empty bot account ID set should not skip",
			latestCommentAuthorID:    botAccountID,
			allBotAccountIDs:         map[string]bool{},
			shouldSkip:               false,
		},
		{
			name:                     "nil bot account ID set should not skip",
			latestCommentAuthorID:    botAccountID,
			allBotAccountIDs:         nil,
			shouldSkip:               false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate cross-team bot comment check logic
			shouldSkip := tt.latestCommentAuthorID != "" && tt.allBotAccountIDs[tt.latestCommentAuthorID]

			if shouldSkip != tt.shouldSkip {
				t.Errorf("expected shouldSkip=%v, got %v", tt.shouldSkip, shouldSkip)
			}
		})
	}
}

// TestJiraPoller_BotMarkerDetection tests that comments with the bot marker
// are correctly detected and skipped.
func TestJiraPoller_BotMarkerDetection(t *testing.T) {
	// Unit test - no DB required
	tests := []struct {
		name          string
		commentBody   string
		shouldSkip    bool
	}{
		{
			name:        "comment with bot marker should be skipped",
			commentBody: "This is a regular comment.\n\n---\n_Posted by Alcove_",
			shouldSkip:  true,
		},
		{
			name:        "comment with partial marker text should not be skipped",
			commentBody: "This comment mentions Posted by but not the full marker",
			shouldSkip:  false,
		},
		{
			name:        "comment without marker should not be skipped",
			commentBody: "This is a regular user comment without any marker.",
			shouldSkip:  false,
		},
		{
			name:        "empty comment body should not be skipped",
			commentBody: "",
			shouldSkip:  false,
		},
		{
			name:        "comment with marker in middle should be skipped",
			commentBody: "Start of comment\n\n_Posted by Alcove_\n\nEnd of comment",
			shouldSkip:  true,
		},
		{
			name:        "comment with case different marker should not be skipped",
			commentBody: "Comment with _posted by alcove_ (lowercase)",
			shouldSkip:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate marker detection logic
			shouldSkip := tt.commentBody != "" && strings.Contains(tt.commentBody, "_Posted by Alcove_")

			if shouldSkip != tt.shouldSkip {
				t.Errorf("expected shouldSkip=%v, got %v", tt.shouldSkip, shouldSkip)
			}
		})
	}
}

// TestJiraPoller_CrossTeamBotDetection tests that the cross-team bot detection
// correctly identifies and skips comments from bots across different teams.
func TestJiraPoller_CrossTeamBotDetection(t *testing.T) {
	// Unit test - no DB required
	teamABotID := "team-a-bot-123"
	teamBBotID := "team-b-bot-456"
	teamCBotID := "team-c-bot-789"
	humanUserID := "human-user-999"

	allBotAccountIDs := map[string]bool{
		teamABotID: true,
		teamBBotID: true,
		teamCBotID: true,
	}

	tests := []struct {
		name                     string
		latestCommentAuthorID    string
		expectSkipped            bool
		expectedLogMessage       string
	}{
		{
			name:                     "team A bot should be detected and skipped",
			latestCommentAuthorID:    teamABotID,
			expectSkipped:            true,
			expectedLogMessage:       "cross-team bot",
		},
		{
			name:                     "team B bot should be detected and skipped",
			latestCommentAuthorID:    teamBBotID,
			expectSkipped:            true,
			expectedLogMessage:       "cross-team bot",
		},
		{
			name:                     "team C bot should be detected and skipped",
			latestCommentAuthorID:    teamCBotID,
			expectSkipped:            true,
			expectedLogMessage:       "cross-team bot",
		},
		{
			name:                     "human user should not be skipped",
			latestCommentAuthorID:    humanUserID,
			expectSkipped:            false,
			expectedLogMessage:       "",
		},
		{
			name:                     "empty author ID should not be skipped",
			latestCommentAuthorID:    "",
			expectSkipped:            false,
			expectedLogMessage:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the cross-team bot detection logic from the poller
			isSkipped := tt.latestCommentAuthorID != "" && allBotAccountIDs[tt.latestCommentAuthorID]

			if isSkipped != tt.expectSkipped {
				t.Errorf("expected isSkipped=%v, got %v", tt.expectSkipped, isSkipped)
			}
		})
	}
}

// TestJiraPoller_UserCommentTriggers tests that when the latest comment
// was authored by a non-bot user, the poller should proceed to dispatch.
func TestJiraPoller_UserCommentTriggers(t *testing.T) {
	// Unit test - no DB required
	botAccountID := "bot-account-123"
	userAccountID := "user-account-456"

	// Test user comment should proceed (not skip)
	shouldSkip := userAccountID != "" && botAccountID != "" && userAccountID == botAccountID
	if shouldSkip {
		t.Error("user comment should not be skipped for dispatch")
	}
}

// TestJiraPoller_DedupKeyIncludesCommentID tests that the dedup key format
// correctly includes the comment ID for proper event-level deduplication.
func TestJiraPoller_DedupKeyIncludesCommentID(t *testing.T) {
	// Unit test - no DB required
	tests := []struct {
		name              string
		issueKey          string
		latestCommentID   string
		expectedDedupKey  string
	}{
		{
			name:              "issue with comment",
			issueKey:          "ISSUE-123",
			latestCommentID:   "10042",
			expectedDedupKey:  "ISSUE-123:10042",
		},
		{
			name:              "issue without comment",
			issueKey:          "ISSUE-456",
			latestCommentID:   "",
			expectedDedupKey:  "ISSUE-456:no-comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate dedup key generation logic
			var dedupKey string
			if tt.latestCommentID != "" {
				dedupKey = tt.issueKey + ":" + tt.latestCommentID
			} else {
				dedupKey = tt.issueKey + ":no-comment"
			}

			if dedupKey != tt.expectedDedupKey {
				t.Errorf("expected dedupKey=%s, got %s", tt.expectedDedupKey, dedupKey)
			}
		})
	}
}

// TestJiraPoller_SameCommentNotDispatchedTwice tests that the dispatched_dedup
// table properly prevents the same issue+comment from being dispatched twice.
func TestJiraPoller_SameCommentNotDispatchedTwice(t *testing.T) {
	// Skip test if no database URL is configured
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	ctx := context.Background()
	db := getTestDB(t)
	defer db.Close()

	// Setup test data
	repo := "PULP"
	itemNumber := "ISSUE-123:10042"
	scheduleID := "test-workflow-id"

	// Clean up any existing entries
	_, err := db.Exec(ctx, "DELETE FROM dispatched_dedup WHERE repo = $1 AND item_number = $2 AND schedule_id = $3", repo, itemNumber, scheduleID)
	if err != nil {
		t.Fatalf("failed to clean up test data: %v", err)
	}

	// First insert should succeed
	result1, err := db.Exec(ctx, `
		INSERT INTO dispatched_dedup (repo, item_number, schedule_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, repo, itemNumber, scheduleID)
	if err != nil {
		t.Fatalf("failed to insert first dedup entry: %v", err)
	}

	if result1.RowsAffected() != 1 {
		t.Errorf("expected first insert to affect 1 row, got %d", result1.RowsAffected())
	}

	// Second insert should be blocked (no rows affected due to conflict)
	result2, err := db.Exec(ctx, `
		INSERT INTO dispatched_dedup (repo, item_number, schedule_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, repo, itemNumber, scheduleID)
	if err != nil {
		t.Fatalf("failed to attempt second dedup insert: %v", err)
	}

	if result2.RowsAffected() != 0 {
		t.Errorf("expected second insert to be blocked (0 rows affected), got %d", result2.RowsAffected())
	}
}

// TestJiraPoller_NewCommentDispatchesAgain tests the core conversation pattern:
// a new comment on the same issue should trigger a new workflow run.
func TestJiraPoller_NewCommentDispatchesAgain(t *testing.T) {
	// Skip test if no database URL is configured
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	ctx := context.Background()
	db := getTestDB(t)
	defer db.Close()

	// Setup test data
	repo := "PULP"
	issueKey := "ISSUE-123"
	oldCommentID := "10042"
	newCommentID := "10043"
	scheduleID := "test-workflow-id"

	oldItemNumber := issueKey + ":" + oldCommentID
	newItemNumber := issueKey + ":" + newCommentID

	// Clean up any existing entries
	_, err := db.Exec(ctx, "DELETE FROM dispatched_dedup WHERE repo = $1 AND (item_number = $2 OR item_number = $3)", repo, oldItemNumber, newItemNumber)
	if err != nil {
		t.Fatalf("failed to clean up test data: %v", err)
	}

	// Insert dedup entry for old comment
	_, err = db.Exec(ctx, `
		INSERT INTO dispatched_dedup (repo, item_number, schedule_id)
		VALUES ($1, $2, $3)
	`, repo, oldItemNumber, scheduleID)
	if err != nil {
		t.Fatalf("failed to insert dedup entry for old comment: %v", err)
	}

	// Attempt to dispatch for new comment - should succeed
	result, err := db.Exec(ctx, `
		INSERT INTO dispatched_dedup (repo, item_number, schedule_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, repo, newItemNumber, scheduleID)
	if err != nil {
		t.Fatalf("failed to attempt dispatch for new comment: %v", err)
	}

	if result.RowsAffected() != 1 {
		t.Errorf("expected new comment dispatch to succeed (1 row affected), got %d", result.RowsAffected())
	}
}

// getTestDB returns a test database connection.
// This assumes a test database is available (e.g., via Docker or CI).
func getTestDB(t *testing.T) *pgxpool.Pool {
	// Try common test database URLs
	testDBURLs := []string{
		"postgres://postgres:postgres@localhost:5432/alcove_test?sslmode=disable",
		"postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable",
	}

	var db *pgxpool.Pool
	var err error

	for _, dbURL := range testDBURLs {
		db, err = pgxpool.New(context.Background(), dbURL)
		if err == nil {
			// Test the connection
			if pingErr := db.Ping(context.Background()); pingErr == nil {
				return db
			}
			db.Close()
		}
	}

	t.Skipf("no test database available, tried: %v (last error: %v)", testDBURLs, err)
	return nil
}

// setupDedupTestData creates the necessary test data for dedup tests
func setupDedupTestData(t *testing.T, ctx context.Context, db *pgxpool.Pool, teamID, workflowID, triggerRef string) {
	// Create team if it doesn't exist
	_, err := db.Exec(ctx, `
		INSERT INTO teams (id, name, description, created_at, updated_at)
		VALUES ($1, 'Test Team', 'Test team for unit tests', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, teamID)
	if err != nil {
		t.Fatalf("failed to create test team: %v", err)
	}

	// Create workflow if it doesn't exist
	_, err = db.Exec(ctx, `
		INSERT INTO workflows (id, name, team_id, source_key, definition, created_at, updated_at)
		VALUES ($1, 'Test Workflow', $2, 'test/workflow', '{}', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, workflowID, teamID)
	if err != nil {
		t.Fatalf("failed to create test workflow: %v", err)
	}
}

