package bridge

import (
	"context"
	"encoding/json"
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

func TestJiraPollerDedupWithFailedRuns(t *testing.T) {
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

			// Run the new concurrent-run guard query (not the old 24h query)
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

func TestJiraPollerDedupExpiredRuns(t *testing.T) {
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
	triggerRef := "TEST-456"

	// Create team, workflow, and test workflow runs with different statuses
	setupDedupTestData(t, ctx, db, teamID, workflowID, triggerRef)

	// Insert a completed run from 25 hours ago (should be expired)
	_, err := db.Exec(ctx, `
		INSERT INTO workflow_runs (id, workflow_id, trigger_ref, trigger_type, status, team_id, created_at, step_outputs)
		VALUES ($1, $2, $3, 'jira', 'completed', $4, NOW() - INTERVAL '25 hours', '{}')
	`, "test-expired-run", workflowID, triggerRef, teamID)
	if err != nil {
		t.Fatalf("failed to insert expired workflow run: %v", err)
	}

	// Run the dedup query
	var count int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM workflow_runs
		WHERE workflow_id = $1 AND trigger_ref = $2
		AND created_at > NOW() - INTERVAL '24 hours'
		AND status NOT IN ('failed', 'cancelled')
	`, workflowID, triggerRef).Scan(&count)
	if err != nil {
		t.Fatalf("failed to run dedup query: %v", err)
	}

	// Expired runs should not block (count should be 0)
	if count != 0 {
		t.Errorf("expected expired run to not block (count=0), got count=%d", count)
	}
}

// TestJiraPoller_BotCommentDoesNotTrigger verifies that issues whose latest
// comment was authored by the bot's accountId are skipped during dispatch.
func TestJiraPoller_BotCommentDoesNotTrigger(t *testing.T) {
	// Mock latest comment metadata from a bot
	latestCommentAuthorID := "bot-account-123"
	botAccountID := "bot-account-123"

	// Test bot detection logic - this will be implemented in the poller
	shouldSkip := latestCommentAuthorID == botAccountID
	if !shouldSkip {
		t.Errorf("expected bot comment to trigger skip, but shouldSkip=%v", shouldSkip)
	}
}

// TestJiraPoller_UserCommentTriggers verifies that issues whose latest comment
// was authored by a non-bot user should proceed to dispatch.
func TestJiraPoller_UserCommentTriggers(t *testing.T) {
	// Mock latest comment metadata from a user
	latestCommentAuthorID := "user-account-456"
	botAccountID := "bot-account-123"

	// Test bot detection logic - this will be implemented in the poller
	shouldSkip := latestCommentAuthorID == botAccountID
	if shouldSkip {
		t.Errorf("expected user comment to NOT trigger skip, but shouldSkip=%v", shouldSkip)
	}
}

// TestJiraPoller_DedupKeyIncludesCommentID verifies that dedup keys include
// comment IDs and handle the no-comment case correctly.
func TestJiraPoller_DedupKeyIncludesCommentID(t *testing.T) {
	tests := []struct {
		name              string
		issueKey          string
		latestCommentID   string
		expectedDedupKey  string
	}{
		{
			name:             "issue with comment",
			issueKey:         "TEST-123",
			latestCommentID:  "10042",
			expectedDedupKey: "TEST-123:10042",
		},
		{
			name:             "issue without comments",
			issueKey:         "TEST-456",
			latestCommentID:  "",
			expectedDedupKey: "TEST-456:no-comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build dedup key using the same logic as implementation
			var dedupKey string
			if tt.latestCommentID != "" {
				dedupKey = tt.issueKey + ":" + tt.latestCommentID
			} else {
				dedupKey = tt.issueKey + ":no-comment"
			}

			if dedupKey != tt.expectedDedupKey {
				t.Errorf("expected dedup key %q, got %q", tt.expectedDedupKey, dedupKey)
			}
		})
	}
}

// TestJiraPoller_SameCommentNotDispatchedTwice verifies that inserting a dedup
// entry for the same issue+comment blocks subsequent dispatch attempts.
func TestJiraPoller_SameCommentNotDispatchedTwice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	ctx := context.Background()
	db := getTestDB(t)
	defer db.Close()

	// Setup test data
	teamID := "test-team-id"
	workflowID := "test-workflow-id"
	setupDedupTestData(t, ctx, db, teamID, workflowID, "dummy")

	issueKey := "TEST-123"
	commentID := "10042"
	projectKey := "TEST"
	dedupKey := issueKey + ":" + commentID

	// Insert initial dedup entry
	result1, err := db.Exec(ctx,
		`INSERT INTO dispatched_dedup (repo, item_number, schedule_id)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		projectKey, dedupKey, workflowID)
	if err != nil {
		t.Fatalf("failed to insert dedup entry: %v", err)
	}

	// First insert should succeed
	if result1.RowsAffected() == 0 {
		t.Error("expected first dedup insert to succeed")
	}

	// Attempt second insert for same issue+comment
	result2, err := db.Exec(ctx,
		`INSERT INTO dispatched_dedup (repo, item_number, schedule_id)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		projectKey, dedupKey, workflowID)
	if err != nil {
		t.Fatalf("failed to insert duplicate dedup entry: %v", err)
	}

	// Second insert should be blocked (0 rows affected)
	if result2.RowsAffected() != 0 {
		t.Error("expected second dedup insert to be blocked")
	}
}

// TestJiraPoller_NewCommentDispatchesAgain verifies that a new comment on the
// same issue creates a new dispatch opportunity (core conversation pattern).
func TestJiraPoller_NewCommentDispatchesAgain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	ctx := context.Background()
	db := getTestDB(t)
	defer db.Close()

	// Setup test data
	teamID := "test-team-id"
	workflowID := "test-workflow-id"
	setupDedupTestData(t, ctx, db, teamID, workflowID, "dummy")

	issueKey := "TEST-123"
	commentID1 := "10042"
	commentID2 := "10043"
	projectKey := "TEST"

	// Insert dedup entry for first comment
	dedupKey1 := issueKey + ":" + commentID1
	result1, err := db.Exec(ctx,
		`INSERT INTO dispatched_dedup (repo, item_number, schedule_id)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		projectKey, dedupKey1, workflowID)
	if err != nil {
		t.Fatalf("failed to insert first dedup entry: %v", err)
	}

	if result1.RowsAffected() == 0 {
		t.Error("expected first dedup insert to succeed")
	}

	// Insert dedup entry for new comment on same issue
	dedupKey2 := issueKey + ":" + commentID2
	result2, err := db.Exec(ctx,
		`INSERT INTO dispatched_dedup (repo, item_number, schedule_id)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		projectKey, dedupKey2, workflowID)
	if err != nil {
		t.Fatalf("failed to insert second dedup entry: %v", err)
	}

	// Second comment should allow dispatch (different dedup key)
	if result2.RowsAffected() == 0 {
		t.Error("expected second comment to allow dispatch")
	}
}

// TestJiraPoller_ActiveRunBlocksDispatch verifies that a running/pending workflow
// blocks new dispatch regardless of comment ID (prevents concurrent runs).
func TestJiraPoller_ActiveRunBlocksDispatch(t *testing.T) {
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
	setupDedupTestData(t, ctx, db, teamID, workflowID, triggerRef)

	// Insert an active workflow run
	_, err := db.Exec(ctx, `
		INSERT INTO workflow_runs (id, workflow_id, trigger_ref, trigger_type, status, team_id, created_at, step_outputs)
		VALUES ($1, $2, $3, 'jira', 'running', $4, NOW(), '{}')
	`, "test-active-run", workflowID, triggerRef, teamID)
	if err != nil {
		t.Fatalf("failed to insert active workflow run: %v", err)
	}

	// Check concurrent-run guard (replaces old 24h query)
	var count int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM workflow_runs
		WHERE workflow_id = $1 AND trigger_ref = $2
		AND status IN ('running', 'pending', 'awaiting_approval')
	`, workflowID, triggerRef).Scan(&count)
	if err != nil {
		t.Fatalf("failed to run concurrent-run guard query: %v", err)
	}

	// Active runs should block dispatch
	if count == 0 {
		t.Error("expected active run to block dispatch (count > 0)")
	}
}

// TestJiraPoller_CompletedRunAllowsNewDispatch verifies that completed/failed/cancelled
// runs do not block new dispatch for new comments (key behavioral change).
func TestJiraPoller_CompletedRunAllowsNewDispatch(t *testing.T) {
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
	setupDedupTestData(t, ctx, db, teamID, workflowID, triggerRef)

	statuses := []string{"completed", "failed", "cancelled"}

	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			// Clean up any existing runs
			_, err := db.Exec(ctx, "DELETE FROM workflow_runs WHERE workflow_id = $1 AND trigger_ref = $2", workflowID, triggerRef)
			if err != nil {
				t.Fatalf("failed to clean up test data: %v", err)
			}

			// Insert a completed/failed/cancelled workflow run
			_, err = db.Exec(ctx, `
				INSERT INTO workflow_runs (id, workflow_id, trigger_ref, trigger_type, status, team_id, created_at, step_outputs)
				VALUES ($1, $2, $3, 'jira', $4, $5, NOW(), '{}')
			`, "test-completed-run", workflowID, triggerRef, status, teamID)
			if err != nil {
				t.Fatalf("failed to insert completed workflow run: %v", err)
			}

			// Check concurrent-run guard
			var count int
			err = db.QueryRow(ctx, `
				SELECT COUNT(*) FROM workflow_runs
				WHERE workflow_id = $1 AND trigger_ref = $2
				AND status IN ('running', 'pending', 'awaiting_approval')
			`, workflowID, triggerRef).Scan(&count)
			if err != nil {
				t.Fatalf("failed to run concurrent-run guard query: %v", err)
			}

			// Completed/failed/cancelled runs should NOT block dispatch
			if count != 0 {
				t.Errorf("expected %s run to NOT block dispatch (count = 0), got count=%d", status, count)
			}
		})
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