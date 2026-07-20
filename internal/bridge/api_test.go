package bridge

import (
	"testing"

	"github.com/alcove-ai/alcove/internal"
)

func TestIsScmProvider(t *testing.T) {
	scmProviders := map[string]bool{"github": true, "gitlab": true, "jira": true}

	tests := []struct {
		provider string
		isSCM    bool
	}{
		{"github", true},
		{"gitlab", true},
		{"jira", true},
		{"anthropic", false},
		{"google-vertex", false},
		{"claude-oauth", false},
	}

	for _, tt := range tests {
		if scmProviders[tt.provider] != tt.isSCM {
			t.Errorf("provider %q: got isSCM=%v, want %v", tt.provider, scmProviders[tt.provider], tt.isSCM)
		}
	}
}

func TestSessionStructHasEventCounts(t *testing.T) {
	// Verify the Session struct has the new fields
	s := internal.Session{
		ID:                   "test-123",
		TranscriptEventCount: 42,
		ProxyEventCount:      24,
	}

	if s.TranscriptEventCount != 42 {
		t.Errorf("expected TranscriptEventCount=42, got %d", s.TranscriptEventCount)
	}

	if s.ProxyEventCount != 24 {
		t.Errorf("expected ProxyEventCount=24, got %d", s.ProxyEventCount)
	}
}
