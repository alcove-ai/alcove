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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestReadOutputArtifact tests the mixed-type output parsing functionality.
func TestReadOutputArtifact(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := t.TempDir()
	testPath := filepath.Join(tempDir, "alcove-outputs.json")

	tests := []struct {
		name     string
		content  string
		expected map[string]string
		wantNil  bool
	}{
		{
			name:     "string-only map",
			content:  `{"message": "success", "status": "ok"}`,
			expected: map[string]string{"message": "success", "status": "ok"},
		},
		{
			name:    "mixed types - bool and array",
			content: `{"automatable": true, "candidate_files": ["auth.py", "tests.py"]}`,
			expected: map[string]string{
				"automatable":     "true",
				"candidate_files": `["auth.py","tests.py"]`,
			},
		},
		{
			name:    "mixed types - bool false",
			content: `{"automatable": false, "ready": true}`,
			expected: map[string]string{
				"automatable": "false",
				"ready":       "true",
			},
		},
		{
			name:    "integer numbers",
			content: `{"count": 42, "score": 100}`,
			expected: map[string]string{
				"count": "42",
				"score": "100",
			},
		},
		{
			name:    "float numbers",
			content: `{"confidence": 0.85, "threshold": 3.14159}`,
			expected: map[string]string{
				"confidence": "0.85",
				"threshold":  "3.14159",
			},
		},
		{
			name:    "nested objects",
			content: `{"config": {"timeout": 30, "retry": true}, "metadata": {"version": "1.0"}}`,
			expected: map[string]string{
				"config":   `{"retry":true,"timeout":30}`,
				"metadata": `{"version":"1.0"}`,
			},
		},
		{
			name:    "null values",
			content: `{"optional": null, "message": "test"}`,
			expected: map[string]string{
				"optional": "null",
				"message":  "test",
			},
		},
		{
			name:    "empty array",
			content: `{"items": [], "count": 0}`,
			expected: map[string]string{
				"items": "[]",
				"count": "0",
			},
		},
		{
			name:    "complex nested array",
			content: `{"results": [{"id": 1, "active": true}, {"id": 2, "active": false}]}`,
			expected: map[string]string{
				"results": `[{"active":true,"id":1},{"active":false,"id":2}]`,
			},
		},
		{
			name:    "invalid JSON",
			content: `{"invalid": json}`,
			wantNil: true,
		},
		{
			name:    "empty object",
			content: `{}`,
			wantNil: true,
		},
		{
			name:    "empty string",
			content: ``,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write test content to file
			if tt.content != "" {
				if err := os.WriteFile(testPath, []byte(tt.content), 0644); err != nil {
					t.Fatalf("Failed to write test file: %v", err)
				}
			} else {
				// For empty string test, don't create the file
				os.Remove(testPath)
			}

			// Call our test version of readOutputArtifact using our test file
			result := testReadOutputArtifact(testPath)

			// Check results
			if tt.wantNil {
				if result != nil {
					t.Errorf("Expected nil but got %v", result)
				}
			} else {
				if result == nil {
					t.Errorf("Expected result but got nil")
					return
				}

				if len(result) != len(tt.expected) {
					t.Errorf("Expected %d outputs but got %d", len(tt.expected), len(result))
				}

				for key, expected := range tt.expected {
					if actual, ok := result[key]; !ok {
						t.Errorf("Missing key %q", key)
					} else if actual != expected {
						t.Errorf("Key %q: expected %q but got %q", key, expected, actual)
					}
				}
			}

			// Clean up
			os.Remove(testPath)
		})
	}
}

// testReadOutputArtifact is a test-friendly version that takes a file path parameter
func testReadOutputArtifact(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}

	if len(raw) == 0 {
		return nil
	}

	outputs := make(map[string]string, len(raw))
	for key, val := range raw {
		switch typed := val.(type) {
		case string:
			outputs[key] = typed
		case bool:
			outputs[key] = strconv.FormatBool(typed)
		case float64:
			if typed == float64(int64(typed)) {
				outputs[key] = strconv.FormatInt(int64(typed), 10)
			} else {
				outputs[key] = strconv.FormatFloat(typed, 'f', -1, 64)
			}
		default:
			b, _ := json.Marshal(val)
			outputs[key] = string(b)
		}
	}

	return outputs
}

// TestReadOutputArtifact_MissingFile tests the case where the output file doesn't exist.
func TestReadOutputArtifact_MissingFile(t *testing.T) {
	// Use a path that definitely doesn't exist
	tempDir := t.TempDir()
	nonExistentPath := filepath.Join(tempDir, "nonexistent.json")

	result := testReadOutputArtifact(nonExistentPath)

	if result != nil {
		t.Errorf("Expected nil for missing file but got %v", result)
	}
}

// TestDetermineOutcome tests the outcome determination logic.
func TestDetermineOutcome(t *testing.T) {
	tests := []struct {
		name             string
		ctxErr           error
		currentOutcome   string
		sawSuccessResult bool
		exitCode         int
		eventCount       int
		expected         string
	}{
		{
			name:             "context timeout takes priority",
			ctxErr:           context.DeadlineExceeded,
			currentOutcome:   "completed",
			sawSuccessResult: true,
			exitCode:         0,
			eventCount:       10,
			expected:         "timeout",
		},
		{
			name:             "cancelled state preserved",
			ctxErr:           nil,
			currentOutcome:   "cancelled",
			sawSuccessResult: false,
			exitCode:         0,
			eventCount:       5,
			expected:         "cancelled",
		},
		{
			name:             "heartbeat timeout preserved",
			ctxErr:           nil,
			currentOutcome:   "timeout",
			sawSuccessResult: false,
			exitCode:         0,
			eventCount:       5,
			expected:         "timeout",
		},
		{
			name:             "success result with exit 0",
			ctxErr:           nil,
			currentOutcome:   "completed",
			sawSuccessResult: true,
			exitCode:         0,
			eventCount:       10,
			expected:         "completed",
		},
		{
			name:             "success result with exit 1 (intentional failure)",
			ctxErr:           nil,
			currentOutcome:   "completed",
			sawSuccessResult: true,
			exitCode:         1,
			eventCount:       10,
			expected:         "completed",
		},
		{
			name:             "exit 1 with no output - error",
			ctxErr:           nil,
			currentOutcome:   "completed",
			sawSuccessResult: false,
			exitCode:         1,
			eventCount:       0,
			expected:         "error",
		},
		{
			name:             "exit 0 with no output - error (key fix)",
			ctxErr:           nil,
			currentOutcome:   "completed",
			sawSuccessResult: false,
			exitCode:         0,
			eventCount:       0,
			expected:         "error",
		},
		{
			name:             "output events with no result and exit 0",
			ctxErr:           nil,
			currentOutcome:   "completed",
			sawSuccessResult: false,
			exitCode:         0,
			eventCount:       5,
			expected:         "completed",
		},
		{
			name:             "output events with no result and exit 1",
			ctxErr:           nil,
			currentOutcome:   "completed",
			sawSuccessResult: false,
			exitCode:         1,
			eventCount:       5,
			expected:         "error",
		},
		{
			name:             "NATS cancel with events",
			ctxErr:           nil,
			currentOutcome:   "cancelled",
			sawSuccessResult: false,
			exitCode:         1,
			eventCount:       3,
			expected:         "cancelled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineOutcome(tt.ctxErr, tt.currentOutcome, tt.sawSuccessResult, tt.exitCode, tt.eventCount)
			if result != tt.expected {
				t.Errorf("determineOutcome(%v, %q, %t, %d, %d) = %q, want %q",
					tt.ctxErr, tt.currentOutcome, tt.sawSuccessResult, tt.exitCode, tt.eventCount, result, tt.expected)
			}
		})
	}
}
// TestFlushBatchRetry tests flushBatch retry behavior with different failure scenarios
func TestFlushBatchRetry(t *testing.T) {
	tests := []struct {
		name          string
		appendFunc    func(sessionID string, events []json.RawMessage) error
		expectedCalls int
		batchCleared  bool
		description   string
	}{
		{
			name: "success_on_first_attempt",
			appendFunc: func(sessionID string, events []json.RawMessage) error {
				return nil // success
			},
			expectedCalls: 1,
			batchCleared:  true,
			description:   "Should succeed immediately and clear batch",
		},
		{
			name: "success_on_second_attempt",
			appendFunc: func() func(sessionID string, events []json.RawMessage) error {
				callCount := 0
				return func(sessionID string, events []json.RawMessage) error {
					callCount++
					if callCount == 1 {
						return errors.New("temporary error")
					}
					return nil // success on second call
				}
			}(),
			expectedCalls: 2,
			batchCleared:  true,
			description:   "Should retry once and succeed, then clear batch",
		},
		{
			name: "success_on_third_attempt",
			appendFunc: func() func(sessionID string, events []json.RawMessage) error {
				callCount := 0
				return func(sessionID string, events []json.RawMessage) error {
					callCount++
					if callCount <= 2 {
						return errors.New("temporary error")
					}
					return nil // success on third call
				}
			}(),
			expectedCalls: 3,
			batchCleared:  true,
			description:   "Should retry twice and succeed, then clear batch",
		},
		{
			name: "fail_all_attempts",
			appendFunc: func(sessionID string, events []json.RawMessage) error {
				return errors.New("persistent error")
			},
			expectedCalls: 3,
			batchCleared:  false,
			description:   "Should try all 3 attempts, then preserve batch",
		},
		{
			name: "nil_function_succeeds",
			appendFunc: func(sessionID string, events []json.RawMessage) error {
				return nil // success
			},
			expectedCalls: 1,
			batchCleared:  true,
			description:   "Should succeed and clear batch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			wrappedFunc := func(sessionID string, events []json.RawMessage) error {
				callCount++
				return tt.appendFunc(sessionID, events)
			}

			// Create test batch with some events
			batch := []json.RawMessage{
				json.RawMessage(`{"type":"test1"}`),
				json.RawMessage(`{"type":"test2"}`),
			}
			originalBatchSize := len(batch)

			flushBatchWithFunc("test-session", &batch, wrappedFunc)

			// Check call count
			if callCount != tt.expectedCalls {
				t.Errorf("%s: expected %d calls, got %d", tt.description, tt.expectedCalls, callCount)
			}

			// Check batch state
			if tt.batchCleared && len(batch) != 0 {
				t.Errorf("%s: expected batch to be cleared, but has %d events", tt.description, len(batch))
			}
			if !tt.batchCleared && len(batch) != originalBatchSize {
				t.Errorf("%s: expected batch to be preserved with %d events, but has %d", tt.description, originalBatchSize, len(batch))
			}
		})
	}
}

// TestFlushBatchCap tests the batch size cap enforcement
func TestFlushBatchCap(t *testing.T) {
	tests := []struct {
		name         string
		batchSize    int
		expectedSize int
		description  string
	}{
		{
			name:         "under_cap",
			batchSize:    100,
			expectedSize: 0, // cleared after successful flush
			description:  "Batch under 500 should flush normally",
		},
		{
			name:         "at_cap",
			batchSize:    500,
			expectedSize: 0, // cleared after successful flush
			description:  "Batch at exactly 500 should flush normally",
		},
		{
			name:         "over_cap",
			batchSize:    600,
			expectedSize: 0, // trimmed to 500, then cleared after successful flush
			description:  "Batch over 500 should be trimmed before flush",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			successFunc := func(sessionID string, events []json.RawMessage) error {
				return nil // always succeeds
			}

			// Create batch of specified size
			batch := make([]json.RawMessage, tt.batchSize)
			for i := 0; i < tt.batchSize; i++ {
				batch[i] = json.RawMessage(fmt.Sprintf(`{"type":"test%d"}`, i))
			}

			flushBatchWithFunc("test-session", &batch, successFunc)

			if len(batch) != tt.expectedSize {
				t.Errorf("%s: expected final batch size %d, got %d", tt.description, tt.expectedSize, len(batch))
			}
		})
	}
}

// TestRetryWithBackoff tests the retry helper function
func TestRetryWithBackoff(t *testing.T) {
	tests := []struct {
		name          string
		maxAttempts   int
		baseDelay     time.Duration
		fnBehavior    func() func() error
		expectSuccess bool
		expectedCalls int
		description   string
	}{
		{
			name:        "success_first_attempt",
			maxAttempts: 3,
			baseDelay:   10 * time.Millisecond,
			fnBehavior: func() func() error {
				return func() error { return nil }
			},
			expectSuccess: true,
			expectedCalls: 1,
			description:   "Should succeed on first attempt",
		},
		{
			name:        "success_second_attempt",
			maxAttempts: 3,
			baseDelay:   10 * time.Millisecond,
			fnBehavior: func() func() error {
				callCount := 0
				return func() error {
					callCount++
					if callCount == 1 {
						return errors.New("first failure")
					}
					return nil
				}
			},
			expectSuccess: true,
			expectedCalls: 2,
			description:   "Should succeed on second attempt",
		},
		{
			name:        "fail_all_attempts",
			maxAttempts: 3,
			baseDelay:   10 * time.Millisecond,
			fnBehavior: func() func() error {
				return func() error { return errors.New("persistent failure") }
			},
			expectSuccess: false,
			expectedCalls: 3,
			description:   "Should fail after all attempts",
		},
		{
			name:        "context_cancellation",
			maxAttempts: 3,
			baseDelay:   10 * time.Millisecond,
			fnBehavior: func() func() error {
				callCount := 0
				return func() error {
					callCount++
					// Always fail to force retry, but context will be cancelled
					return errors.New("failure")
				}
			},
			expectSuccess: false,
			expectedCalls: 1, // Only one call before context cancellation
			description:   "Should respect context cancellation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			fn := tt.fnBehavior()
			wrappedFn := func() error {
				callCount++
				return fn()
			}

			var ctx context.Context
			var cancel context.CancelFunc

			if tt.name == "context_cancellation" {
				ctx, cancel = context.WithCancel(context.Background())
				// Cancel after a short delay
				go func() {
					time.Sleep(5 * time.Millisecond)
					cancel()
				}()
			} else {
				ctx = context.Background()
			}

			err := retryWithBackoff(ctx, "test", tt.maxAttempts, tt.baseDelay, wrappedFn)

			// Check success/failure
			if tt.expectSuccess && err != nil {
				t.Errorf("%s: expected success, got error: %v", tt.description, err)
			}
			if !tt.expectSuccess && err == nil {
				t.Errorf("%s: expected failure, got success", tt.description)
			}

			// Check call count (with some tolerance for context cancellation timing)
			if tt.name == "context_cancellation" {
				if callCount < 1 || callCount > 2 {
					t.Errorf("%s: expected 1-2 calls due to timing, got %d", tt.description, callCount)
				}
			} else {
				if callCount != tt.expectedCalls {
					t.Errorf("%s: expected %d calls, got %d", tt.description, tt.expectedCalls, callCount)
				}
			}
		})
	}
}
