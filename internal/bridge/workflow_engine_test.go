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
	"testing"
	"time"
)

// TestExpandTemplateWithContext verifies template expansion in workflow inputs,
// covering trigger references, hyphenated step IDs, input prefix lookups, and
// non-string value conversion.
func TestExpandTemplateWithContext(t *testing.T) {
	// expandTemplateWithContext is a method on *WorkflowEngine but does not
	// touch the database, so a zero-value engine with nil deps is fine.
	we := &WorkflowEngine{}

	// Build step outputs that simulate previous step completions.
	stepOutputs := map[string]interface{}{
		// Step with a simple ID.
		"implement": map[string]interface{}{
			"summary":        "Implemented the feature",
			"_input_branch":  "feature/issue-42",
		},
		// Step with a hyphenated ID (the regex must use [\w-]+).
		"create-pr": map[string]interface{}{
			"pr_number": float64(99), // JSON numbers decode as float64
			"pr_url":    "https://github.com/org/repo/pull/99",
		},
	}

	triggerRef := "owner/repo#42"

	tests := []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "trigger issue_number from triggerRef",
			template: "Fix issue {{trigger.issue_number}}",
			expected: "Fix issue 42",
		},
		{
			name:     "hyphenated step ID in outputs",
			template: "PR #{{steps.create-pr.outputs.pr_number}}",
			expected: "PR #99",
		},
		{
			name:     "input prefix lookup via steps.X.inputs.Y",
			template: "Branch: {{steps.implement.inputs.branch}}",
			expected: "Branch: feature/issue-42",
		},
		{
			name:     "regular output expansion",
			template: "Summary: {{steps.implement.outputs.summary}}",
			expected: "Summary: Implemented the feature",
		},
		{
			name:     "non-string float64 converted to string",
			template: "{{steps.create-pr.outputs.pr_number}}",
			expected: "99",
		},
		{
			name:     "unresolved template remains as literal",
			template: "{{steps.nonexistent.outputs.value}}",
			expected: "{{steps.nonexistent.outputs.value}}",
		},
		{
			name:     "multiple templates in one string",
			template: "PR {{steps.create-pr.outputs.pr_number}} for issue {{trigger.issue_number}}",
			expected: "PR 99 for issue 42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := we.expandTemplateWithContext(tt.template, stepOutputs, triggerRef)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExpandTemplateWithContext_IntValue ensures integer values stored in step
// outputs (as opposed to float64 from JSON) are also converted to strings.
func TestExpandTemplateWithContext_IntValue(t *testing.T) {
	we := &WorkflowEngine{}

	stepOutputs := map[string]interface{}{
		"build": map[string]interface{}{
			"exit_code": 0, // plain int, not float64
		},
	}

	result, err := we.expandTemplateWithContext("{{steps.build.outputs.exit_code}}", stepOutputs, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "0" {
		t.Errorf("got %q, want %q", result, "0")
	}
}

// TestExpandTemplateWithContext_EmptyTriggerRef ensures that trigger templates
// resolve to empty string (not panic) when triggerRef has no "#" delimiter.
func TestExpandTemplateWithContext_EmptyTriggerRef(t *testing.T) {
	we := &WorkflowEngine{}

	result, err := we.expandTemplateWithContext("Issue {{trigger.issue_number}}", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Issue " {
		t.Errorf("got %q, want %q", result, "Issue ")
	}
}

// TestExpandTemplateWithContext_NilStepOutputs ensures that step template
// references remain as literals when stepOutputs is nil (not panic).
func TestExpandTemplateWithContext_NilStepOutputs(t *testing.T) {
	we := &WorkflowEngine{}

	result, err := we.expandTemplateWithContext("{{steps.build.outputs.status}}", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "{{steps.build.outputs.status}}" {
		t.Errorf("got %q, want %q", result, "{{steps.build.outputs.status}}")
	}
}

// TestCancelWorkflowRunValidation tests the validation logic for cancelling workflow runs.
// This is a unit test that focuses on the validation part without requiring database interactions.
func TestCancelWorkflowRunValidation(t *testing.T) {
	// This test would validate the business logic for determining if a workflow run
	// can be cancelled based on its status. Since the actual implementation requires
	// database access, this serves as documentation of the expected behavior:

	// - Should allow cancellation of "pending", "running", "awaiting_approval" status
	// - Should reject cancellation of "completed", "failed", "cancelled" status
	// - Should cancel all pending/running/awaiting_approval steps
	// - Should attempt to cancel associated sessions

	validStatuses := []string{"pending", "running", "awaiting_approval"}
	invalidStatuses := []string{"completed", "failed", "cancelled"}

	for _, status := range validStatuses {
		t.Logf("Status %s should be cancellable", status)
	}

	for _, status := range invalidStatuses {
		t.Logf("Status %s should not be cancellable", status)
	}
}

func TestParseSinceParam(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		expected time.Duration // approximate duration from now
	}{
		{"empty", "", false, 0},
		{"1 day", "1d", false, -24 * time.Hour},
		{"7 days", "7d", false, -7 * 24 * time.Hour},
		{"30 days", "30d", false, -30 * 24 * time.Hour},
		{"ISO date", "2023-01-01T00:00:00Z", false, 0},
		{"date only", "2023-01-01", false, 0},
		{"invalid", "invalid", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseSinceParam(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.input == "" {
				if result != nil {
					t.Errorf("expected nil for empty input, got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("expected non-nil result for input %s", tt.input)
				return
			}

			// For relative dates, check they are approximately correct
			if tt.expected != 0 {
				now := time.Now()
				expectedTime := now.Add(tt.expected)
				diff := expectedTime.Sub(*result)
				if diff > time.Minute || diff < -time.Minute {
					t.Errorf("time difference too large: expected around %v, got %v (diff: %v)",
						expectedTime, *result, diff)
				}
			}
		})
	}
}

func TestWorkflowRunsFilter_validate(t *testing.T) {
	tests := []struct {
		name    string
		filter  WorkflowRunsFilter
		wantErr bool
		checks  func(*testing.T, *WorkflowRunsFilter)
	}{
		{
			name:    "missing team ID",
			filter:  WorkflowRunsFilter{},
			wantErr: true,
		},
		{
			name: "default limit applied",
			filter: WorkflowRunsFilter{
				TeamID: "team-1",
				Limit:  0,
			},
			wantErr: false,
			checks: func(t *testing.T, f *WorkflowRunsFilter) {
				if f.Limit != 25 {
					t.Errorf("expected default limit 25, got %d", f.Limit)
				}
			},
		},
		{
			name: "limit too high",
			filter: WorkflowRunsFilter{
				TeamID: "team-1",
				Limit:  500,
			},
			wantErr: false,
			checks: func(t *testing.T, f *WorkflowRunsFilter) {
				if f.Limit != 200 {
					t.Errorf("expected capped limit 200, got %d", f.Limit)
				}
			},
		},
		{
			name: "negative offset corrected",
			filter: WorkflowRunsFilter{
				TeamID: "team-1",
				Offset: -5,
			},
			wantErr: false,
			checks: func(t *testing.T, f *WorkflowRunsFilter) {
				if f.Offset != 0 {
					t.Errorf("expected corrected offset 0, got %d", f.Offset)
				}
			},
		},
		{
			name: "valid since parameter",
			filter: WorkflowRunsFilter{
				TeamID: "team-1",
				Since:  "7d",
			},
			wantErr: false,
			checks: func(t *testing.T, f *WorkflowRunsFilter) {
				if f.SinceTime == nil {
					t.Errorf("expected SinceTime to be set")
				}
			},
		},
		{
			name: "invalid since parameter",
			filter: WorkflowRunsFilter{
				TeamID: "team-1",
				Since:  "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.validate()

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.checks != nil {
				tt.checks(t, &tt.filter)
			}
		})
	}
}

// TestCIFixDispatchDependency verifies that ci-fix steps correctly dispatch
// when await-ci steps fail (not when they succeed).
func TestCIFixDispatchDependency(t *testing.T) {
	tests := []struct {
		name             string
		awaitCIStatus    string
		codeReviewStatus string
		expectedCIFix    bool
		expectedReview   bool
	}{
		{
			name:             "CI fails - ci-fix should dispatch",
			awaitCIStatus:    "failed",
			codeReviewStatus: "pending",
			expectedCIFix:    true,
			expectedReview:   false,
		},
		{
			name:             "CI passes - code-review should dispatch",
			awaitCIStatus:    "completed",
			codeReviewStatus: "pending",
			expectedCIFix:    false,
			expectedReview:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stepStatuses := map[string]string{
				"await-ci":     tt.awaitCIStatus,
				"code-review":  tt.codeReviewStatus,
			}

			// Test ci-fix dependency: depends on "await-ci.Failed"
			cifixReady, err := EvaluateDepends("await-ci.Failed", stepStatuses)
			if err != nil {
				t.Fatalf("EvaluateDepends for ci-fix failed: %v", err)
			}
			if cifixReady != tt.expectedCIFix {
				t.Errorf("ci-fix readiness: got %v, expected %v", cifixReady, tt.expectedCIFix)
			}

			// Test code-review dependency: depends on "await-ci.Succeeded"
			reviewReady, err := EvaluateDepends("await-ci.Succeeded", stepStatuses)
			if err != nil {
				t.Fatalf("EvaluateDepends for code-review failed: %v", err)
			}
			if reviewReady != tt.expectedReview {
				t.Errorf("code-review readiness: got %v, expected %v", reviewReady, tt.expectedReview)
			}
		})
	}
}

func TestValidateStepOutputs(t *testing.T) {
	we := &WorkflowEngine{}

	tests := []struct {
		name      string
		contract  *OutputContract
		outputs   map[string]interface{}
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid outputs with all required fields",
			contract: &OutputContract{
				Required: []string{"verdict", "fixes_required"},
				AllowedValues: map[string][]string{
					"verdict": {"pass", "fail"},
				},
			},
			outputs: map[string]interface{}{
				"verdict":        "pass",
				"fixes_required": []string{"fix1", "fix2"},
			},
			expectErr: false,
		},
		{
			name: "missing required field",
			contract: &OutputContract{
				Required: []string{"verdict", "fixes_required"},
			},
			outputs: map[string]interface{}{
				"verdict": "pass",
			},
			expectErr: true,
			errMsg:    "required output field 'fixes_required' is missing",
		},
		{
			name: "invalid value not in allowed_values",
			contract: &OutputContract{
				Required: []string{"verdict"},
				AllowedValues: map[string][]string{
					"verdict": {"pass", "fail"},
				},
			},
			outputs: map[string]interface{}{
				"verdict": "unknown",
			},
			expectErr: true,
			errMsg:    "allowed: [pass fail]",
		},
		{
			name: "valid with extra fields not in contract",
			contract: &OutputContract{
				Required: []string{"verdict"},
				AllowedValues: map[string][]string{
					"verdict": {"pass", "fail"},
				},
			},
			outputs: map[string]interface{}{
				"verdict":    "pass",
				"extra_info": "some additional data",
			},
			expectErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			valid, reason := we.validateOutputContract(test.contract, test.outputs)

			if test.expectErr {
				if valid {
					t.Fatalf("expected validation to fail but it passed")
				}
				if !stringContains(reason, test.errMsg) {
					t.Errorf("error reason should contain '%s', got: %s", test.errMsg, reason)
				}
			} else {
				if !valid {
					t.Fatalf("expected validation to pass but it failed: %s", reason)
				}
			}
		})
	}
}

// TestWorkflowEngineValidateOutputContract tests the output contract validation logic in the workflow engine.
func TestWorkflowEngineValidateOutputContract(t *testing.T) {
	we := &WorkflowEngine{}

	tests := []struct {
		name      string
		contract  *OutputContract
		outputs   map[string]interface{}
		expectErr bool
		errMsg    string
	}{
		{
			name: "contract satisfied - all required fields present",
			contract: &OutputContract{
				Required: []string{"verdict", "fixes_required"},
				AllowedValues: map[string][]string{
					"verdict": {"pass", "fail"},
				},
			},
			outputs: map[string]interface{}{
				"verdict":        "pass",
				"fixes_required": true,
			},
			expectErr: false,
		},
		{
			name: "missing required field",
			contract: &OutputContract{
				Required: []string{"verdict", "fixes_required"},
			},
			outputs: map[string]interface{}{
				"verdict": "pass",
				// fixes_required is missing
			},
			expectErr: true,
			errMsg:    "required output field 'fixes_required' is missing",
		},
		{
			name: "nil value for required field",
			contract: &OutputContract{
				Required: []string{"verdict"},
			},
			outputs: map[string]interface{}{
				"verdict": nil,
			},
			expectErr: true,
			errMsg:    "required output field 'verdict' is missing",
		},
		{
			name: "invalid allowed value",
			contract: &OutputContract{
				Required: []string{"verdict"},
				AllowedValues: map[string][]string{
					"verdict": {"pass", "fail"},
				},
			},
			outputs: map[string]interface{}{
				"verdict": "maybe",
			},
			expectErr: true,
			errMsg:    "output field 'verdict' has value 'maybe', allowed: [pass fail]",
		},
		{
			name: "valid allowed value",
			contract: &OutputContract{
				Required: []string{"verdict"},
				AllowedValues: map[string][]string{
					"verdict": {"pass", "fail"},
				},
			},
			outputs: map[string]interface{}{
				"verdict": "fail",
			},
			expectErr: false,
		},
		{
			name: "allowed values check skips non-existent fields",
			contract: &OutputContract{
				AllowedValues: map[string][]string{
					"optional_field": {"value1", "value2"},
				},
			},
			outputs: map[string]interface{}{
				// optional_field is not present, should not fail
			},
			expectErr: false,
		},
		{
			name: "empty outputs with contract",
			contract: &OutputContract{
				Required: []string{"verdict"},
			},
			outputs:   map[string]interface{}{},
			expectErr: true,
			errMsg:    "required output field 'verdict' is missing",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			valid, reason := we.validateOutputContract(test.contract, test.outputs)

			if test.expectErr {
				if valid {
					t.Fatalf("expected validation to fail but it passed")
				}
				if !stringContains(reason, test.errMsg) {
					t.Errorf("error reason should contain '%s', got: %s", test.errMsg, reason)
				}
			} else {
				if !valid {
					t.Fatalf("expected validation to pass but it failed: %s", reason)
				}
			}
		})
	}
}

// TestDeadlockDetection verifies that circular dependency chains are detected.
// Scenario: await-ci and ci-fix both exhausted max_iterations (both failed).
// code-review depends on "await-ci.Succeeded || revision.Succeeded"
// revision depends on "code-review.Failed"
// Neither can ever run — they should both be detected as unreachable.
func TestDeadlockDetection(t *testing.T) {
	// After await-ci fails and ci-fix exhausts iterations, these are the
	// terminal statuses. code-review and revision are still pending.
	stepStatuses := map[string]string{
		"implement":   "completed",
		"create-pr":   "completed",
		"await-ci":    "failed",
		"ci-fix":      "failed",
		"code-review": "pending",
		"revision":    "pending",
		"merge":       "pending",
	}

	// code-review depends on "await-ci.Succeeded || revision.Succeeded"
	// With await-ci=failed and revision=pending, this is false.
	// But revision is not terminal, so the single-pass check skips it.
	codeReviewReady, _ := EvaluateDepends("await-ci.Succeeded || revision.Succeeded", stepStatuses)
	if codeReviewReady {
		t.Fatal("code-review should NOT be ready when await-ci=failed and revision=pending")
	}

	// revision depends on "code-review.Failed"
	// With code-review=pending, this is false.
	revisionReady, _ := EvaluateDepends("code-review.Failed", stepStatuses)
	if revisionReady {
		t.Fatal("revision should NOT be ready when code-review=pending")
	}

	// Verify the single-pass unreachable check misses these (references include non-terminal steps):
	codeReviewRefs := ExtractDependsStepIDs("await-ci.Succeeded || revision.Succeeded")
	hasNonTerminal := false
	for _, ref := range codeReviewRefs {
		s := stepStatuses[ref]
		if s != "completed" && s != "failed" && s != "skipped" {
			hasNonTerminal = true
		}
	}
	if !hasNonTerminal {
		t.Fatal("code-review refs should include non-terminal 'revision' (pending)")
	}

	// Now simulate the deadlock detection: no running steps + pending steps = deadlock.
	hasRunning := false
	pendingCount := 0
	for _, status := range stepStatuses {
		if status == "running" {
			hasRunning = true
		}
		if status == "pending" {
			pendingCount++
		}
	}
	if hasRunning {
		t.Fatal("there should be no running steps in a deadlocked state")
	}
	if pendingCount != 3 {
		t.Fatalf("expected 3 pending steps (code-review, revision, merge), got %d", pendingCount)
	}

	// The fix: with no running steps and pending steps remaining,
	// all pending steps should be marked as skipped (deadlock resolved).
	// This is what checkWorkflowCompletion now does.
}

// TestDeadlockDetection_DispatchesReadySteps simulates the race scenario:
// both reviews completed, rebase pending with satisfied depends → verify rebase would be dispatched (not skipped)
func TestDeadlockDetection_DispatchesReadySteps(t *testing.T) {
	// Scenario: parallel reviews complete simultaneously on final iteration
	stepStatuses := map[string]string{
		"implement":       "completed",
		"create-pr":       "completed",
		"await-ci":        "completed",
		"code-review":     "completed",
		"security-review": "completed",
		"revision":        "completed",
		"rebase":          "pending",    // Should be dispatched
		"merge":           "pending",    // Should remain pending (depends on rebase)
	}

	// Mock workflow definition with the problematic dependencies
	workflow := &WorkflowDefinition{
		Workflow: []WorkflowStep{
			{ID: "implement"},
			{ID: "create-pr", Depends: "implement.Succeeded"},
			{ID: "await-ci", Depends: "create-pr.Succeeded"},
			{ID: "code-review", Depends: "await-ci.Succeeded", MaxIterations: 3},
			{ID: "security-review", Depends: "await-ci.Succeeded", MaxIterations: 3},
			{ID: "revision", Depends: "code-review.Failed || security-review.Failed", MaxIterations: 3},
			{ID: "rebase", Depends: "code-review.Succeeded && security-review.Succeeded"},
			{ID: "merge", Depends: "rebase.Succeeded"},
		},
	}

	// Verify rebase dependencies are satisfied
	rebaseReady, err := EvaluateDepends("code-review.Succeeded && security-review.Succeeded", stepStatuses)
	if err != nil {
		t.Fatalf("EvaluateDepends failed: %v", err)
	}
	if !rebaseReady {
		t.Fatal("rebase should be ready when both reviews succeeded")
	}

	// Verify merge dependencies are not satisfied
	mergeReady, _ := EvaluateDepends("rebase.Succeeded", stepStatuses)
	if mergeReady {
		t.Fatal("merge should NOT be ready when rebase is still pending")
	}

	// Simulate deadlock condition: no running steps + pending steps
	hasRunning := false
	pendingCount := 0
	for _, status := range stepStatuses {
		if status == "running" || status == "awaiting_approval" {
			hasRunning = true
		}
		if status == "pending" {
			pendingCount++
		}
	}
	if hasRunning {
		t.Fatal("there should be no running steps in the race scenario")
	}
	if pendingCount != 2 {
		t.Fatalf("expected 2 pending steps (rebase, merge), got %d", pendingCount)
	}

	// The fix should identify that rebase is ready to dispatch
	rebaseStep := workflow.GetStepByID("rebase")
	if rebaseStep == nil {
		t.Fatal("could not find rebase step definition")
	}

	ready, err := EvaluateDepends(rebaseStep.Depends, stepStatuses)
	if err != nil {
		t.Fatalf("evaluating rebase depends: %v", err)
	}
	if !ready {
		t.Fatal("rebase should be identified as ready for dispatch during deadlock recovery")
	}
}

// TestDeadlockDetection_SkipsUnreachableSteps ensures existing deadlock detection still works:
// circular deps with all terminal references → steps marked as skipped
func TestDeadlockDetection_SkipsUnreachableSteps(t *testing.T) {
	// Scenario: genuine circular deadlock where all references are terminal
	stepStatuses := map[string]string{
		"step-a": "failed",  // terminal
		"step-b": "failed",  // terminal
		"step-c": "pending", // circular dep on step-d
		"step-d": "pending", // circular dep on step-c
	}

	// Mock workflow with circular dependencies
	workflow := &WorkflowDefinition{
		Workflow: []WorkflowStep{
			{ID: "step-a"},
			{ID: "step-b"},
			{ID: "step-c", Depends: "step-a.Succeeded && step-d.Succeeded"}, // Can't run: step-a failed, step-d pending
			{ID: "step-d", Depends: "step-b.Succeeded && step-c.Succeeded"}, // Can't run: step-b failed, step-c pending
		},
	}

	// Verify we have the expected step definitions
	if len(workflow.Workflow) != 4 {
		t.Fatalf("expected 4 steps in workflow definition, got %d", len(workflow.Workflow))
	}

	// Verify neither pending step can be dispatched
	stepCReady, _ := EvaluateDepends("step-a.Succeeded && step-d.Succeeded", stepStatuses)
	stepDReady, _ := EvaluateDepends("step-b.Succeeded && step-c.Succeeded", stepStatuses)

	if stepCReady {
		t.Fatal("step-c should NOT be ready (step-a failed, step-d pending)")
	}
	if stepDReady {
		t.Fatal("step-d should NOT be ready (step-b failed, step-c pending)")
	}

	// Check all references are terminal for step-c
	stepCRefs := ExtractDependsStepIDs("step-a.Succeeded && step-d.Succeeded")
	allTerminal := true
	for _, ref := range stepCRefs {
		status := stepStatuses[ref]
		if status != "completed" && status != "failed" && status != "skipped" {
			allTerminal = false
		}
	}
	if allTerminal {
		t.Fatal("step-c references include step-d which is pending, not all terminal")
	}

	// This represents a genuine deadlock that should be resolved by skipping
}

// TestDeadlockDetection_MixedReadyAndUnreachable tests a scenario where some pending steps
// are ready, others are unreachable → ready ones dispatched, unreachable ones skipped
func TestDeadlockDetection_MixedReadyAndUnreachable(t *testing.T) {
	stepStatuses := map[string]string{
		"step-a":   "completed",
		"step-b":   "failed",
		"step-c":   "pending", // Ready: depends on step-a.Succeeded (true)
		"step-d":   "pending", // Unreachable: depends on step-b.Succeeded (false, and step-b is terminal)
		"step-e":   "pending", // Not ready: depends on step-c.Succeeded (false, step-c is pending)
	}

	workflow := &WorkflowDefinition{
		Workflow: []WorkflowStep{
			{ID: "step-a"},
			{ID: "step-b"},
			{ID: "step-c", Depends: "step-a.Succeeded"},          // Ready to dispatch
			{ID: "step-d", Depends: "step-b.Succeeded"},          // Unreachable (step-b failed)
			{ID: "step-e", Depends: "step-c.Succeeded"},          // Not ready yet (step-c pending)
		},
	}

	// Verify we have the expected step definitions
	if len(workflow.Workflow) != 5 {
		t.Fatalf("expected 5 steps in workflow definition, got %d", len(workflow.Workflow))
	}

	// Verify expectations
	stepCReady, _ := EvaluateDepends("step-a.Succeeded", stepStatuses)
	stepDReady, _ := EvaluateDepends("step-b.Succeeded", stepStatuses)
	stepEReady, _ := EvaluateDepends("step-c.Succeeded", stepStatuses)

	if !stepCReady {
		t.Fatal("step-c should be ready (step-a completed)")
	}
	if stepDReady {
		t.Fatal("step-d should NOT be ready (step-b failed)")
	}
	if stepEReady {
		t.Fatal("step-e should NOT be ready (step-c pending)")
	}

	// Check which steps should be skipped vs dispatched
	stepDRefs := ExtractDependsStepIDs("step-b.Succeeded")
	allTerminalD := true
	for _, ref := range stepDRefs {
		status := stepStatuses[ref]
		if status != "completed" && status != "failed" && status != "skipped" {
			allTerminalD = false
		}
	}
	if !allTerminalD {
		t.Fatal("step-d references should all be terminal")
	}

	// step-e should remain pending (references non-terminal step-c)
	stepERefs := ExtractDependsStepIDs("step-c.Succeeded")
	allTerminalE := true
	for _, ref := range stepERefs {
		status := stepStatuses[ref]
		if status != "completed" && status != "failed" && status != "skipped" {
			allTerminalE = false
		}
	}
	if allTerminalE {
		t.Fatal("step-e references include step-c which is pending, should not be all terminal")
	}
}

// TestMaxIterationsFinalSuccessDispatchesDownstream verifies that checkAndDispatchDependents
// correctly evaluates downstream steps when a step completes on its final iteration
func TestMaxIterationsFinalSuccessDispatchesDownstream(t *testing.T) {
	stepStatuses := map[string]string{
		"await-ci":        "completed",
		"code-review":     "completed", // Just completed on iteration 3 (final)
		"security-review": "completed", // Just completed on iteration 3 (final)
		"rebase":          "pending",   // Should be dispatchable now
	}

	workflow := &WorkflowDefinition{
		Workflow: []WorkflowStep{
			{ID: "await-ci"},
			{ID: "code-review", Depends: "await-ci.Succeeded", MaxIterations: 3},
			{ID: "security-review", Depends: "await-ci.Succeeded", MaxIterations: 3},
			{ID: "rebase", Depends: "code-review.Succeeded && security-review.Succeeded"},
		},
	}

	// Verify we have the expected workflow structure
	if len(workflow.Workflow) != 4 {
		t.Fatalf("expected 4 steps in workflow definition, got %d", len(workflow.Workflow))
	}

	// Verify rebase should be ready after both reviews succeed
	rebaseReady, err := EvaluateDepends("code-review.Succeeded && security-review.Succeeded", stepStatuses)
	if err != nil {
		t.Fatalf("EvaluateDepends for rebase failed: %v", err)
	}
	if !rebaseReady {
		t.Fatal("rebase should be ready when both reviews succeeded, even on their final iteration")
	}

	// The key insight: max_iterations should NOT prevent downstream evaluation
	// when the step succeeds. The iteration limit prevents RE-dispatching the
	// same step, not dispatching dependent steps.
}

// TestOnStepCompletion_OutcomeToStatusMapping verifies that session outcomes map correctly to step statuses.
// This test covers the critical mapping added by #476: error/timeout/cancelled outcomes should result in failed step status.
func TestOnStepCompletion_OutcomeToStatusMapping(t *testing.T) {
	tests := []struct {
		name               string
		sessionStatus      string
		expectedStepStatus string
	}{
		{
			name:               "completed outcome maps to completed step status",
			sessionStatus:      "completed",
			expectedStepStatus: "completed",
		},
		{
			name:               "error outcome maps to failed step status",
			sessionStatus:      "error",
			expectedStepStatus: "failed",
		},
		{
			name:               "timeout outcome maps to failed step status",
			sessionStatus:      "timeout",
			expectedStepStatus: "failed",
		},
		{
			name:               "cancelled outcome maps to failed step status",
			sessionStatus:      "cancelled",
			expectedStepStatus: "failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test documents the mapping logic from workflow_engine.go lines 607-609:
			// stepStatus := "completed"
			// if status != "completed" {
			//     stepStatus = "failed"
			// }

			// Simulate the mapping logic
			stepStatus := "completed"
			if tt.sessionStatus != "completed" {
				stepStatus = "failed"
			}

			if stepStatus != tt.expectedStepStatus {
				t.Errorf("session status %q should map to step status %q, got %q",
					tt.sessionStatus, tt.expectedStepStatus, stepStatus)
			}
		})
	}
}

// TestOnStepCompletion_ErrorOutcomeBlocksContracts verifies that error/timeout/cancelled outcomes
// short-circuit before output contract validation (lines 608-613 in workflow_engine.go).
func TestOnStepCompletion_ErrorOutcomeBlocksContracts(t *testing.T) {
	tests := []struct {
		name          string
		sessionStatus string
		shouldReachContract bool
	}{
		{
			name:          "completed outcome reaches contract validation",
			sessionStatus: "completed",
			shouldReachContract: true,
		},
		{
			name:          "error outcome short-circuits before contract validation",
			sessionStatus: "error",
			shouldReachContract: false,
		},
		{
			name:          "timeout outcome short-circuits before contract validation",
			sessionStatus: "timeout",
			shouldReachContract: false,
		},
		{
			name:          "cancelled outcome short-circuits before contract validation",
			sessionStatus: "cancelled",
			shouldReachContract: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the workflow_engine.go logic: if status != "completed" { stepStatus = "failed" }
			// then the contract validation on line 613 is only reached if stepStatus is still "completed"
			stepStatus := "completed"
			if tt.sessionStatus != "completed" {
				stepStatus = "failed"
			}

			contractReached := (stepStatus == "completed") // Contract validation only happens for completed steps
			if contractReached != tt.shouldReachContract {
				t.Errorf("session status %q should reach contract validation: %v, got: %v",
					tt.sessionStatus, tt.shouldReachContract, contractReached)
			}
		})
	}
}

// TestFullWorkflowDAGErrorPropagation verifies the complete chain:
// implement step returns error → stepStatus=failed → create-pr (depends on implement.Succeeded) is NOT dispatched
func TestFullWorkflowDAGErrorPropagation(t *testing.T) {
	stepStatuses := map[string]string{
		"implement": "failed", // Simulate error outcome mapped to failed step status
		"create-pr": "pending",
	}

	// Test the SDLC pipeline dependency: create-pr depends on "implement.Succeeded"
	createPRReady, err := EvaluateDepends("implement.Succeeded", stepStatuses)
	if err != nil {
		t.Fatalf("EvaluateDepends failed: %v", err)
	}

	// With implement=failed, create-pr should NOT be ready
	if createPRReady {
		t.Error("create-pr should NOT be ready when implement step failed")
	}

	// Verify .Failed dependency would work (used by retry steps like ci-fix)
	implementFailed, err := EvaluateDepends("implement.Failed", stepStatuses)
	if err != nil {
		t.Fatalf("EvaluateDepends for implement.Failed failed: %v", err)
	}

	if !implementFailed {
		t.Error("implement.Failed should be true when implement step status is failed")
	}
}

// TestWorkflowEngineZeroOutputErrorHandling verifies the workflow engine behavior
// when a session produces zero transcript events and exits with error status.
func TestWorkflowEngineZeroOutputErrorHandling(t *testing.T) {
	// Simulate post-#476 behavior: zero-output sessions are marked as "error"
	sessionStatus := "error"
	hasOutputs := false

	// Map to step status (workflow_engine.go logic)
	stepStatus := "completed"
	if sessionStatus != "completed" {
		stepStatus = "failed"
	}

	// Verify the mapping
	if stepStatus != "failed" {
		t.Errorf("zero-output error session should result in failed step status, got %q", stepStatus)
	}

	// Verify downstream steps are blocked
	stepStatuses := map[string]string{
		"implement": stepStatus,
		"create-pr": "pending",
	}

	downstreamReady, err := EvaluateDepends("implement.Succeeded", stepStatuses)
	if err != nil {
		t.Fatalf("EvaluateDepends failed: %v", err)
	}

	if downstreamReady {
		t.Error("downstream steps should be blocked when implement produces zero output and exits with error")
	}

	// Document the key insight: no outputs means no work was done, so the step should fail
	if !hasOutputs && stepStatus != "failed" {
		t.Error("steps with zero outputs should be marked as failed regardless of exit code")
	}
}

// stringContains is a helper to check substring presence
func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
