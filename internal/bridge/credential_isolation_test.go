package bridge

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCredentialTeamIsolation(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set — run with a real PostgreSQL to test team isolation")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	encKey := make([]byte, 32)
	if _, err := rand.Read(encKey); err != nil {
		t.Fatalf("generating key: %v", err)
	}

	cs := &CredentialStore{db: pool, key: encKey}

	// Use real team IDs from the database (created during dev setup).
	// These must exist in the teams table due to FK constraint.
	alphaTeamID := os.Getenv("TEST_ALPHA_TEAM_ID")
	bravoTeamID := os.Getenv("TEST_BRAVO_TEAM_ID")
	if alphaTeamID == "" || bravoTeamID == "" {
		t.Skip("TEST_ALPHA_TEAM_ID and TEST_BRAVO_TEAM_ID must be set")
	}
	_ = uuid.New() // keep import used

	alphaEncrypted, err := encrypt(encKey, []byte("ALPHA_JIRA_DUMMY"))
	if err != nil {
		t.Fatalf("encrypting alpha credential: %v", err)
	}
	bravoEncrypted, err := encrypt(encKey, []byte("BRAVO_JIRA_DUMMY"))
	if err != nil {
		t.Fatalf("encrypting bravo credential: %v", err)
	}

	alphaID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO provider_credentials (id, name, provider, auth_type, credential, team_id, created_at, updated_at)
		VALUES ($1, 'test-jira', 'jira', 'api_key', $2, $3, NOW() - interval '1 hour', NOW() - interval '1 hour')`,
		alphaID, alphaEncrypted, alphaTeamID)
	if err != nil {
		t.Fatalf("inserting alpha credential: %v", err)
	}

	bravoID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO provider_credentials (id, name, provider, auth_type, credential, team_id, created_at, updated_at)
		VALUES ($1, 'test-jira', 'jira', 'api_key', $2, $3, NOW(), NOW())`,
		bravoID, bravoEncrypted, bravoTeamID)
	if err != nil {
		t.Fatalf("inserting bravo credential: %v", err)
	}

	defer func() {
		pool.Exec(ctx, `DELETE FROM provider_credentials WHERE id IN ($1, $2)`, alphaID, bravoID)
	}()

	// On the BROKEN code (main branch), GetRawCredential takes (ctx, name) — no teamID.
	// It will return whichever credential was created most recently (Bravo's).
	// On the FIXED code, GetRawCredential takes (ctx, name, teamID) and returns
	// the correct team's credential.
	//
	// This test calls the function and reports what it got. The test PASSES on
	// both branches — but the output shows whether isolation works.

	t.Run("GetRawCredential_ReturnsWhat", func(t *testing.T) {
		raw, err := cs.GetRawCredential(ctx, "test-jira", alphaTeamID)
		if err != nil {
			t.Fatalf("GetRawCredential: %v", err)
		}
		got := string(raw)
		fmt.Printf("GetRawCredential('test-jira', alphaTeamID) = %q\n", got)
		if got == "BRAVO_JIRA_DUMMY" {
			t.Errorf("BUG REPRODUCED: Alpha's request returned Bravo's credential (%q)", got)
		} else if got == "ALPHA_JIRA_DUMMY" {
			fmt.Println("CORRECT: Alpha got Alpha's credential")
		}
	})

	t.Run("AcquireToken_ReturnsWhat", func(t *testing.T) {
		result, err := cs.AcquireToken(ctx, "test-jira", alphaTeamID)
		if err != nil {
			t.Fatalf("AcquireToken: %v", err)
		}
		fmt.Printf("AcquireToken('test-jira', alphaTeamID) = %q\n", result.Token)
		if result.Token == "BRAVO_JIRA_DUMMY" {
			t.Errorf("BUG REPRODUCED: Alpha's request returned Bravo's token (%q)", result.Token)
		} else if result.Token == "ALPHA_JIRA_DUMMY" {
			fmt.Println("CORRECT: Alpha got Alpha's token")
		}
	})
}
