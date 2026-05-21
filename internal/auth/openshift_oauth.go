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

package auth

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OpenShiftOAuthStore implements Authenticator and UserManager for the
// openshift-oauth auth backend. It trusts X-Forwarded-User and X-Forwarded-Email
// headers from an OpenShift OAuth Proxy sidecar and provisions users automatically
// on first access.
type OpenShiftOAuthStore struct {
	db *pgxpool.Pool
}

// NewOpenShiftOAuthStore creates a new OpenShift OAuth auth store backed by PostgreSQL.
func NewOpenShiftOAuthStore(db *pgxpool.Pool) *OpenShiftOAuthStore {
	return &OpenShiftOAuthStore{db: db}
}

// UpsertUser creates or updates a user based on the X-Forwarded-User and
// X-Forwarded-Email headers from OAuth Proxy. It uses username as the primary
// lookup key and creates a personal team for new users. Returns the username.
func (s *OpenShiftOAuthStore) UpsertUser(ctx context.Context, username, email string) (string, error) {
	log.Printf("openshift-oauth: upsert user=%s email=%s", username, email)

	// Use email as display name if provided, otherwise use username
	displayName := email
	if displayName == "" {
		displayName = username
	}

	// Try to find existing user by username first.
	var existingUsername string
	err := s.db.QueryRow(ctx,
		"SELECT username FROM auth_users WHERE username = $1", username).Scan(&existingUsername)
	if err == nil {
		// User exists — update display_name and last access time.
		_, err = s.db.Exec(ctx,
			"UPDATE auth_users SET display_name = $1, updated_at = NOW() WHERE username = $2",
			displayName, username)
		if err != nil {
			return "", fmt.Errorf("updating user display name: %w", err)
		}
		log.Printf("openshift-oauth: updated existing user=%s", username)
		return username, nil
	}

	// User doesn't exist — insert user and create personal team.
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`INSERT INTO auth_users (username, password, external_id, display_name, auth_source, is_admin, created_at, updated_at)
		 VALUES ($1, NULL, NULL, $2, 'openshift-oauth', false, NOW(), NOW())`,
		username, displayName)
	if err != nil {
		return "", fmt.Errorf("creating user: %w", err)
	}

	// Create personal team for the new user.
	teamID := uuid.New().String()
	teamName := username + "'s workspace"
	_, err = tx.Exec(ctx,
		"INSERT INTO teams (id, name, is_personal, created_at) VALUES ($1, $2, true, NOW())",
		teamID, teamName)
	if err != nil {
		return "", fmt.Errorf("creating personal team: %w", err)
	}
	_, err = tx.Exec(ctx,
		"INSERT INTO team_members (team_id, username) VALUES ($1, $2)",
		teamID, username)
	if err != nil {
		return "", fmt.Errorf("adding user to personal team: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("committing transaction: %w", err)
	}

	log.Printf("openshift-oauth: created new user=%s with personal team", username)
	return username, nil
}

// --- Authenticator interface (no-ops for openshift-oauth) ---

// Authenticate is not supported with the openshift-oauth backend.
func (s *OpenShiftOAuthStore) Authenticate(username, password string) (string, error) {
	return "", fmt.Errorf("login not supported with openshift-oauth backend")
}

// ValidateCredentials is not supported with the openshift-oauth backend.
func (s *OpenShiftOAuthStore) ValidateCredentials(username, password string) (string, error) {
	return "", fmt.Errorf("basic auth not supported with openshift-oauth backend")
}

// ValidateToken is not used with the openshift-oauth backend.
func (s *OpenShiftOAuthStore) ValidateToken(token string) (string, bool) {
	return "", false
}

// InvalidateToken is a no-op for the openshift-oauth backend.
func (s *OpenShiftOAuthStore) InvalidateToken(token string) {}

// --- UserManager interface ---

// CreateUser is not supported with the openshift-oauth backend.
func (s *OpenShiftOAuthStore) CreateUser(ctx context.Context, username, password string, isAdmin bool) error {
	return fmt.Errorf("user creation not supported with openshift-oauth backend; users are auto-provisioned")
}

// DeleteUser removes a user by username.
func (s *OpenShiftOAuthStore) DeleteUser(ctx context.Context, username string) error {
	tag, err := s.db.Exec(ctx,
		"DELETE FROM auth_users WHERE username = $1", username)
	if err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found: %s", username)
	}
	log.Printf("openshift-oauth: deleted user=%s", username)
	return nil
}

// ListUsers returns all users ordered by creation time.
func (s *OpenShiftOAuthStore) ListUsers(ctx context.Context) ([]UserInfo, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			u.username,
			u.created_at,
			u.is_admin,
			COALESCE(s.session_count, 0) as session_count
		FROM auth_users u
		LEFT JOIN (
			SELECT submitter, COUNT(*) as session_count
			FROM sessions
			GROUP BY submitter
		) s ON u.username = s.submitter
		ORDER BY u.created_at`)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	users := []UserInfo{}
	for rows.Next() {
		var u UserInfo
		if err := rows.Scan(&u.Username, &u.CreatedAt, &u.IsAdmin, &u.SessionCount); err != nil {
			return nil, fmt.Errorf("scanning user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating users: %w", err)
	}
	return users, nil
}

// ChangePassword is not supported with the openshift-oauth backend.
func (s *OpenShiftOAuthStore) ChangePassword(ctx context.Context, username, newPassword string) error {
	return fmt.Errorf("password change not supported with openshift-oauth backend")
}

// SetAdmin updates the admin flag for a user.
func (s *OpenShiftOAuthStore) SetAdmin(ctx context.Context, username string, isAdmin bool) error {
	tag, err := s.db.Exec(ctx,
		"UPDATE auth_users SET is_admin = $1, updated_at = NOW() WHERE username = $2",
		isAdmin, username)
	if err != nil {
		return fmt.Errorf("setting admin flag: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found: %s", username)
	}
	log.Printf("openshift-oauth: set admin=%v for user=%s", isAdmin, username)
	return nil
}

// IsAdmin returns whether the given user has admin privileges.
func (s *OpenShiftOAuthStore) IsAdmin(ctx context.Context, username string) (bool, error) {
	var isAdmin bool
	err := s.db.QueryRow(ctx,
		"SELECT is_admin FROM auth_users WHERE username = $1", username).Scan(&isAdmin)
	if err != nil {
		return false, fmt.Errorf("checking admin status: %w", err)
	}
	return isAdmin, nil
}

// VerifyUserPassword is not supported with the openshift-oauth backend.
func (s *OpenShiftOAuthStore) VerifyUserPassword(ctx context.Context, username, password string) (bool, error) {
	return false, fmt.Errorf("password verification not supported with openshift-oauth backend")
}

// BootstrapAdmins ensures the given usernames exist as admin users.
// For each username: if the user exists, set is_admin=true; if not, create a
// new user with is_admin=true and auth_source="openshift-oauth".
func (s *OpenShiftOAuthStore) BootstrapAdmins(ctx context.Context, admins []string) error {
	for _, username := range admins {
		_, err := s.db.Exec(ctx,
			`INSERT INTO auth_users (username, password, is_admin, auth_source, created_at, updated_at)
			 VALUES ($1, NULL, true, 'openshift-oauth', NOW(), NOW())
			 ON CONFLICT (username) DO UPDATE SET is_admin = true, updated_at = NOW()`,
			username)
		if err != nil {
			return fmt.Errorf("bootstrapping admin %s: %w", username, err)
		}
		log.Printf("openshift-oauth: bootstrapped admin user=%s", username)
	}
	return nil
}