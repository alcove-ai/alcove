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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alcove-ai/alcove/internal"
)

const (
	testTeamA       = "11111111-1111-1111-1111-111111111111"
	testTeamB       = "22222222-2222-2222-2222-222222222222"
	testSecretValue = "super-secret-value-do-not-leak"
)

func TestParseK8sSecretRef(t *testing.T) {
	tests := []struct {
		in       string
		wantName string
		wantKey  string
		wantOK   bool
		wantErr  bool
	}{
		{in: "github-pat", wantOK: false},
		{in: "", wantOK: false},
		{in: "k8s-secret:s3-bucket/aws_access_key_id", wantName: "s3-bucket", wantKey: "aws_access_key_id", wantOK: true},
		{in: "k8s-secret:my.secret/key.with-dots", wantName: "my.secret", wantKey: "key.with-dots", wantOK: true},
		{in: "k8s-secret:", wantOK: true, wantErr: true},
		{in: "k8s-secret:s3-bucket", wantOK: true, wantErr: true},
		{in: "k8s-secret:s3-bucket/", wantOK: true, wantErr: true},
		{in: "k8s-secret:/key", wantOK: true, wantErr: true},
		{in: "k8s-secret:S3_Bucket/key", wantOK: true, wantErr: true},
		{in: "k8s-secret:../etc/passwd", wantOK: true, wantErr: true},
		{in: "k8s-secret:s3-bucket/a/b", wantOK: true, wantErr: true},
		{in: "k8s-secret:s3-bucket/..", wantOK: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			name, key, ok, err := ParseK8sSecretRef(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if name != tt.wantName || key != tt.wantKey {
				t.Errorf("got (%q, %q), want (%q, %q)", name, key, tt.wantName, tt.wantKey)
			}
		})
	}
}

// writeSecretKey writes <root>/<name>/<key> the way kubelet lays out a mounted Secret.
func writeSecretKey(t *testing.T, root, name, key, value string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, key), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newTestResolver(t *testing.T) (*K8sSecretResolver, string) {
	t.Helper()
	root := t.TempDir()
	writeSecretKey(t, root, "s3-bucket", "aws_access_key_id", testSecretValue)
	// A mounted but unconfigured Secret must not be reachable.
	writeSecretKey(t, root, "other-secret", "token", testSecretValue)
	r := NewK8sSecretResolver(K8sSecretsConfig{
		Root: root,
		Secrets: map[string]K8sSecretPolicy{
			"s3-bucket": {AllowedTeams: []string{testTeamA}},
		},
	})
	return r, root
}

func TestK8sSecretResolver_Resolve(t *testing.T) {
	r, _ := newTestResolver(t)

	got, err := r.Resolve(testTeamA, "s3-bucket", "aws_access_key_id")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != testSecretValue {
		t.Errorf("Resolve = %q, want %q", got, testSecretValue)
	}

	errCases := []struct {
		desc, team, name, key, wantMsg string
	}{
		{"unconfigured secret", testTeamA, "other-secret", "token", "not configured"},
		{"team not allowed", testTeamB, "s3-bucket", "aws_access_key_id", "not allowed"},
		{"empty team", "", "s3-bucket", "aws_access_key_id", "not allowed"},
		{"missing key", testTeamA, "s3-bucket", "nope", "not found"},
		{"traversal key", testTeamA, "s3-bucket", "../other-secret/token", "not a valid Secret key"},
		{"dotdot key", testTeamA, "s3-bucket", "..", "not a valid Secret key"},
	}
	for _, tc := range errCases {
		t.Run(tc.desc, func(t *testing.T) {
			v, err := r.Resolve(tc.team, tc.name, tc.key)
			if err == nil {
				t.Fatalf("expected error, got value %q", v)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q does not contain %q", err, tc.wantMsg)
			}
			if strings.Contains(err.Error(), testSecretValue) {
				t.Errorf("error leaks secret value: %q", err)
			}
		})
	}
}

func TestK8sSecretResolver_MissingSecretDir(t *testing.T) {
	r := NewK8sSecretResolver(K8sSecretsConfig{
		Root:    t.TempDir(),
		Secrets: map[string]K8sSecretPolicy{"not-mounted": {AllowedTeams: []string{testTeamA}}},
	})
	if _, err := r.Resolve(testTeamA, "not-mounted", "key"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestK8sSecretResolver_RotationPickedUp(t *testing.T) {
	r, root := newTestResolver(t)
	writeSecretKey(t, root, "s3-bucket", "aws_access_key_id", "rotated")
	got, err := r.Resolve(testTeamA, "s3-bucket", "aws_access_key_id")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "rotated" {
		t.Errorf("Resolve after rotation = %q, want %q", got, "rotated")
	}
}

func TestK8sSecretResolver_NilResolver(t *testing.T) {
	var r *K8sSecretResolver
	if _, err := r.Resolve(testTeamA, "s3-bucket", "key"); err == nil {
		t.Fatal("expected error from nil resolver")
	}
}

func TestResolveK8sSecretCredential(t *testing.T) {
	r, _ := newTestResolver(t)
	exe := &internal.ExecutableSpec{}

	tests := []struct {
		desc    string
		req     TaskRequest
		team    string
		wantErr string
	}{
		{"executable direct_outbound allowed", TaskRequest{Executable: exe, DirectOutbound: true}, testTeamA, ""},
		{"claude code agent rejected", TaskRequest{DirectOutbound: true}, testTeamA, "only available to executable agents"},
		{"executable without direct_outbound rejected", TaskRequest{Executable: exe}, testTeamA, "only available to executable agents"},
		{"wrong team rejected", TaskRequest{Executable: exe, DirectOutbound: true}, testTeamB, "not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			v, err := resolveK8sSecretCredential(r, tt.req, tt.team, "AWS_ACCESS_KEY_ID", "s3-bucket", "aws_access_key_id", nil)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if v != testSecretValue {
					t.Errorf("value = %q, want %q", v, testSecretValue)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") {
				t.Errorf("error should name the env var: %v", err)
			}
			if strings.Contains(err.Error(), testSecretValue) {
				t.Errorf("error leaks secret value: %v", err)
			}
		})
	}

	_, _, _, parseErr := ParseK8sSecretRef("k8s-secret:bad")
	if _, err := resolveK8sSecretCredential(r, TaskRequest{Executable: exe, DirectOutbound: true}, testTeamA, "X", "", "", parseErr); err == nil {
		t.Fatal("expected parse error to be returned")
	}
}

func TestK8sSecretsConfig_Validate(t *testing.T) {
	tests := []struct {
		desc    string
		cfg     K8sSecretsConfig
		wantErr bool
	}{
		{"empty", K8sSecretsConfig{}, false},
		{"valid", K8sSecretsConfig{Secrets: map[string]K8sSecretPolicy{"s3-bucket": {AllowedTeams: []string{testTeamA}}}}, false},
		{"invalid name", K8sSecretsConfig{Secrets: map[string]K8sSecretPolicy{"../etc": {AllowedTeams: []string{testTeamA}}}}, true},
		{"no teams", K8sSecretsConfig{Secrets: map[string]K8sSecretPolicy{"s3-bucket": {}}}, true},
		{"blank team", K8sSecretsConfig{Secrets: map[string]K8sSecretPolicy{"s3-bucket": {AllowedTeams: []string{" "}}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if err := tt.cfg.validate(); (err != nil) != tt.wantErr {
				t.Errorf("validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseConfigFile_K8sSecrets(t *testing.T) {
	yaml := `
database_encryption_key: test-key
k8s_secrets:
  root: /tmp/secrets
  secrets:
    s3-bucket:
      allowed_teams: ["` + testTeamA + `"]
`
	path := filepath.Join(t.TempDir(), "alcove.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	if err := cfg.parseConfigFile(path); err != nil {
		t.Fatalf("parseConfigFile: %v", err)
	}
	if cfg.K8sSecrets.Root != "/tmp/secrets" {
		t.Errorf("Root = %q", cfg.K8sSecrets.Root)
	}
	if got := cfg.K8sSecrets.Secrets["s3-bucket"].AllowedTeams; len(got) != 1 || got[0] != testTeamA {
		t.Errorf("AllowedTeams = %v", got)
	}
}

func TestLoadConfig_K8sSecretsEnv(t *testing.T) {
	t.Setenv("ALCOVE_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("ALCOVE_DATABASE_ENCRYPTION_KEY", "test-key")
	t.Setenv("ALCOVE_K8S_SECRETS", `{"s3-bucket":{"allowed_teams":["`+testTeamA+`"]}}`)
	t.Setenv("ALCOVE_K8S_SECRETS_ROOT", "/custom/root")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.K8sSecrets.Root != "/custom/root" {
		t.Errorf("Root = %q, want /custom/root", cfg.K8sSecrets.Root)
	}
	if got := cfg.K8sSecrets.Secrets["s3-bucket"].AllowedTeams; len(got) != 1 || got[0] != testTeamA {
		t.Errorf("AllowedTeams = %v", got)
	}
}

func TestLoadConfig_K8sSecretsDefaultsAndErrors(t *testing.T) {
	t.Setenv("ALCOVE_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("ALCOVE_DATABASE_ENCRYPTION_KEY", "test-key")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.K8sSecrets.Root != DefaultK8sSecretsRoot {
		t.Errorf("Root = %q, want default %q", cfg.K8sSecrets.Root, DefaultK8sSecretsRoot)
	}

	t.Setenv("ALCOVE_K8S_SECRETS", `not-json`)
	if _, err := LoadConfig(); err == nil {
		t.Error("expected error for malformed ALCOVE_K8S_SECRETS")
	}

	t.Setenv("ALCOVE_K8S_SECRETS", `{"s3-bucket":{"allowed_teams":[]}}`)
	if _, err := LoadConfig(); err == nil {
		t.Error("expected error for secret without allowed_teams")
	}
}
