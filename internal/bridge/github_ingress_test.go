// Copyright 2026 Brian Bouterse
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/alcove-ai/alcove/internal"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type recordingEventDispatcher struct {
	mu    sync.Mutex
	tasks []TaskRequest
}

func (d *recordingEventDispatcher) DispatchTask(_ context.Context, task TaskRequest, _ string, _ ...string) (*internal.Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tasks = append(d.tasks, task)
	return &internal.Session{}, nil
}

func TestGitHubPollerExcludedActorDoesNotConsumeDedup(t *testing.T) {
	databaseURL := os.Getenv("ALCOVE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ALCOVE_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	suffix := uuid.NewString()
	githubRepo := "example/repo-" + suffix
	botEventID := "bot-event-" + suffix
	humanEventID := "human-event-" + suffix
	teamID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO teams (id, name) VALUES ($1, $2)`, teamID, "github ingress test"); err != nil {
		t.Fatalf("create test team: %v", err)
	}
	credStore := NewCredentialStore(db, "github-ingress-test-key")
	cred := &Credential{Name: "github-ingress-test", Provider: "github", AuthType: "api_key"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/"+githubRepo+"/events" {
			w.Header().Set("Content-Type", "application/json")
			events := []map[string]any{githubPollEvent(botEventID, githubRepo, "alcove-bot", "2026-09-03T10:00:00Z"), githubPollEvent(humanEventID, githubRepo, "human", "2026-09-03T10:01:00Z")}
			body, _ := json.Marshal(events)
			_, _ = w.Write(body)
			return
		}
		if r.URL.Path == "/repos/"+githubRepo+"/issues/42" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"open"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	cred.APIHost = server.URL
	if err := credStore.CreateCredential(ctx, cred, []byte("test-token"), teamID); err != nil {
		t.Fatalf("create GitHub credential: %v", err)
	}

	scheduleID := uuid.NewString()
	defer func() {
		_, _ = db.Exec(ctx, `DELETE FROM webhook_deliveries WHERE delivery_id IN ($1, $2)`, "poll-"+botEventID, "poll-"+humanEventID)
		_, _ = db.Exec(ctx, `DELETE FROM dispatched_dedup WHERE schedule_id = $1`, scheduleID)
		_, _ = db.Exec(ctx, `DELETE FROM github_poll_state WHERE repo = $1`, githubRepo)
		_, _ = db.Exec(ctx, `DELETE FROM provider_credentials WHERE id = $1`, cred.ID)
		_, _ = db.Exec(ctx, `DELETE FROM teams WHERE id = $1`, teamID)
	}()

	dispatcher := &recordingEventDispatcher{}
	poller := &GitHubPoller{
		db:         db,
		dispatcher: dispatcher,
		credStore:  credStore,
		client:     server.Client(),
	}
	schedule := pollSchedule{
		ID:       scheduleID,
		Name:     "github ingress test",
		Prompt:   "test",
		Provider: "test",
		TeamID:   teamID,
		Trigger: &GitHubTrigger{
			Events:       []string{"issue_comment"},
			Actions:      []string{"created"},
			Repos:        []string{githubRepo},
			ExcludeUsers: []string{"alcove-bot"},
		},
	}

	poller.pollRepo(ctx, githubRepo, teamID, []pollSchedule{schedule})

	var deliveryCount, dedupCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM webhook_deliveries WHERE delivery_id = $1`, "poll-"+botEventID).Scan(&deliveryCount); err != nil {
		t.Fatalf("count bot delivery: %v", err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM dispatched_dedup WHERE schedule_id = $1`, scheduleID).Scan(&dedupCount); err != nil {
		t.Fatalf("count dedup rows: %v", err)
	}
	if deliveryCount != 0 || dedupCount != 1 {
		t.Fatalf("bot delivery/dedup counts = %d/%d, want 0/1", deliveryCount, dedupCount)
	}

	dispatcher.mu.Lock()
	gotTasks := len(dispatcher.tasks)
	dispatcher.mu.Unlock()
	if gotTasks != 1 {
		t.Fatalf("dispatch count = %d, want 1 human dispatch", gotTasks)
	}

	// The second poll sees the same event IDs and must not dispatch again.
	poller.pollRepo(ctx, githubRepo, teamID, []pollSchedule{schedule})
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if len(dispatcher.tasks) != 1 {
		t.Fatalf("replayed events dispatched %d tasks, want 1", len(dispatcher.tasks))
	}

}

func githubPollEvent(id, repo, actor, createdAt string) map[string]any {
	return map[string]any{
		"id": id, "type": "IssueCommentEvent", "actor": map[string]any{"login": actor},
		"repo": map[string]any{"name": repo}, "created_at": createdAt,
		"payload": map[string]any{
			"action":  "created",
			"issue":   map[string]any{"number": 42, "user": map[string]any{"login": "issue-author"}, "labels": []any{}},
			"comment": map[string]any{"user": map[string]any{"login": "comment-author"}},
		},
	}
}
