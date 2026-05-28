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
	"testing"
)

func TestSanitizeStepID(t *testing.T) {
	tests := []struct {
		name     string
		stepID   string
		expected string
	}{
		{
			name:     "normal step ID",
			stepID:   "implement-feature",
			expected: "implement-feature",
		},
		{
			name:     "step ID with underscores",
			stepID:   "create_pr_branch",
			expected: "create_pr_branch",
		},
		{
			name:     "step ID with path separators",
			stepID:   "step/with/slashes",
			expected: "step_with_slashes",
		},
		{
			name:     "step ID with parent directory references",
			stepID:   "step/../escape",
			expected: "step____escape",
		},
		{
			name:     "step ID with special characters",
			stepID:   "step@with#special$chars%",
			expected: "step_with_special_chars",
		},
		{
			name:     "step ID with spaces",
			stepID:   "step with spaces",
			expected: "step_with_spaces",
		},
		{
			name:     "step ID with leading/trailing underscores",
			stepID:   "___step___",
			expected: "step",
		},
		{
			name:     "very long step ID",
			stepID:   "this-is-a-very-long-step-id-that-exceeds-fifty-characters-and-should-be-truncated",
			expected: "this-is-a-very-long-step-id-that-exceeds-fifty-cha",
		},
		{
			name:     "empty step ID",
			stepID:   "",
			expected: "unknown",
		},
		{
			name:     "step ID with only special characters",
			stepID:   "@#$%^&*()",
			expected: "unknown",
		},
		{
			name:     "step ID with dots",
			stepID:   "step.with.dots",
			expected: "step_with_dots",
		},
		{
			name:     "step ID with multiple path traversal attempts",
			stepID:   "../../etc/passwd",
			expected: "etc_passwd",
		},
		{
			name:     "step ID with mixed valid and invalid chars",
			stepID:   "valid-step_123!@#invalid",
			expected: "valid-step_123___invalid",
		},
		{
			name:     "step ID with unicode characters",
			stepID:   "step-with-émoji-🚀",
			expected: "step-with-_moji",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeStepID(tt.stepID)
			if result != tt.expected {
				t.Errorf("sanitizeStepID(%q) = %q, want %q", tt.stepID, result, tt.expected)
			}

			// Verify the result doesn't contain dangerous path elements
			if result != "unknown" && (result == "" || result == "." || result == "..") {
				t.Errorf("sanitizeStepID(%q) returned dangerous path element: %q", tt.stepID, result)
			}
		})
	}
}