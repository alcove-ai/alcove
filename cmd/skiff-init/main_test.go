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
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
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

// mockLedgerClient provides a controllable mock for testing flushBatch retry logic.
type mockLedgerClient struct {
	calls    [][]json.RawMessage
	errors   []error
	callIdx  int
	mu       sync.Mutex
}

func (m *mockLedgerClient) AppendTranscript(sessionID string, batch []json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, batch)
	if m.callIdx < len(m.errors) {
		err := m.errors[m.callIdx]
		m.callIdx++
		return err
	}
	m.callIdx++
	return nil
}

// testFlushBatch is a modified version that accepts a LedgerClient interface
func testFlushBatch(lc *mockLedgerClient, sessionID string, batch *[]json.RawMessage) {
	for attempt := 1; attempt <= 3; attempt++ {
		if err := lc.AppendTranscript(sessionID, *batch); err != nil {
			if attempt < 3 {
				time.Sleep(time.Duration(attempt) * time.Millisecond) // Fast for tests
				continue
			}
			return // keep events in batch for next flush cycle
		}
		*batch = nil
		return
	}
}

// TestFlushBatch tests the retry logic and event preservation.
func TestFlushBatch(t *testing.T) {
	tests := []struct {
		name           string
		errors         []error
		initialEvents  int
		expectedCalls  int
		expectEmpty    bool
		expectedEvents int
	}{
		{
			name:           "success on first attempt",
			errors:         []error{nil},
			initialEvents:  3,
			expectedCalls:  1,
			expectEmpty:    true,
			expectedEvents: 0,
		},
		{
			name:           "success on second attempt",
			errors:         []error{errors.New("network error"), nil},
			initialEvents:  3,
			expectedCalls:  2,
			expectEmpty:    true,
			expectedEvents: 0,
		},
		{
			name:           "success on third attempt",
			errors:         []error{errors.New("error1"), errors.New("error2"), nil},
			initialEvents:  3,
			expectedCalls:  3,
			expectEmpty:    true,
			expectedEvents: 0,
		},
		{
			name:           "fail all attempts - preserve events",
			errors:         []error{errors.New("error1"), errors.New("error2"), errors.New("error3")},
			initialEvents:  3,
			expectedCalls:  3,
			expectEmpty:    false,
			expectedEvents: 3,
		},
		{
			name:           "empty batch",
			errors:         []error{nil},
			initialEvents:  0,
			expectedCalls:  1,
			expectEmpty:    true,
			expectedEvents: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockLedgerClient{errors: tt.errors}

			// Create test batch
			batch := make([]json.RawMessage, tt.initialEvents)
			for i := 0; i < tt.initialEvents; i++ {
				batch[i] = json.RawMessage(`{"event":"test"}`)
			}

			testFlushBatch(mock, "test-session", &batch)

			// Verify call count
			if len(mock.calls) != tt.expectedCalls {
				t.Errorf("Expected %d calls, got %d", tt.expectedCalls, len(mock.calls))
			}

			// Verify batch state
			if tt.expectEmpty && len(batch) != 0 {
				t.Errorf("Expected empty batch, got %d events", len(batch))
			} else if !tt.expectEmpty && len(batch) != tt.expectedEvents {
				t.Errorf("Expected %d events in batch, got %d", tt.expectedEvents, len(batch))
			}
		})
	}
}

// TestBatchCapEnforcement tests the max batch size enforcement logic.
func TestBatchCapEnforcement(t *testing.T) {
	tests := []struct {
		name          string
		initialCount  int
		expectedCount int
	}{
		{
			name:          "under cap",
			initialCount:  100,
			expectedCount: 101,
		},
		{
			name:          "at cap",
			initialCount:  500,
			expectedCount: 500,
		},
		{
			name:          "over cap",
			initialCount:  501,
			expectedCount: 500,
		},
		{
			name:          "way over cap",
			initialCount:  1000,
			expectedCount: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create initial batch
			batch := make([]json.RawMessage, tt.initialCount)
			for i := 0; i < tt.initialCount; i++ {
				batch[i] = json.RawMessage(`{"event":"` + strconv.Itoa(i) + `"}`)
			}

			// Simulate adding one more event with cap enforcement
			if len(batch) >= maxBatchSize {
				// Keep the newest 499 events
				batch = batch[len(batch)-maxBatchSize+1:]
			}
			batch = append(batch, json.RawMessage(`{"event":"new"}`))

			if len(batch) != tt.expectedCount {
				t.Errorf("Expected batch size %d, got %d", tt.expectedCount, len(batch))
			}

			// Verify the newest event is preserved
			lastEvent := batch[len(batch)-1]
			if string(lastEvent) != `{"event":"new"}` {
				t.Errorf("Expected newest event to be preserved, got %s", lastEvent)
			}
		})
	}
}

// TestRetryWithBackoff tests the retry helper function.
func TestRetryWithBackoff(t *testing.T) {
	tests := []struct {
		name          string
		errors        []error
		maxAttempts   int
		expectSuccess bool
		expectCalls   int
	}{
		{
			name:          "success on first attempt",
			errors:        []error{nil},
			maxAttempts:   3,
			expectSuccess: true,
			expectCalls:   1,
		},
		{
			name:          "success on second attempt",
			errors:        []error{errors.New("temp error"), nil},
			maxAttempts:   3,
			expectSuccess: true,
			expectCalls:   2,
		},
		{
			name:          "fail all attempts",
			errors:        []error{errors.New("error1"), errors.New("error2"), errors.New("error3")},
			maxAttempts:   3,
			expectSuccess: false,
			expectCalls:   3,
		},
		{
			name:          "single attempt success",
			errors:        []error{nil},
			maxAttempts:   1,
			expectSuccess: true,
			expectCalls:   1,
		},
		{
			name:          "single attempt failure",
			errors:        []error{errors.New("single error")},
			maxAttempts:   1,
			expectSuccess: false,
			expectCalls:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			ctx := context.Background()

			fn := func() error {
				if callCount < len(tt.errors) {
					err := tt.errors[callCount]
					callCount++
					return err
				}
				callCount++
				return nil
			}

			retryWithBackoff(ctx, "test", tt.maxAttempts, time.Millisecond, fn)

			if callCount != tt.expectCalls {
				t.Errorf("Expected %d calls, got %d", tt.expectCalls, callCount)
			}
		})
	}
}

// TestRetryWithBackoffContextCancellation tests context cancellation during retry.
func TestRetryWithBackoffContextCancellation(t *testing.T) {
	t.Run("cancel before first attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		callCount := 0
		fn := func() error {
			callCount++
			return errors.New("always fail")
		}

		retryWithBackoff(ctx, "test", 5, time.Millisecond, fn)

		if callCount != 0 {
			t.Errorf("Expected 0 calls, got %d", callCount)
		}
	})

	t.Run("cancel with timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()

		callCount := 0
		fn := func() error {
			callCount++
			time.Sleep(10 * time.Millisecond) // Ensure we hit the timeout
			return errors.New("always fail")
		}

		retryWithBackoff(ctx, "test", 5, time.Millisecond, fn)

		// Should be 1 or 2 calls depending on timing, but not all 5
		if callCount >= 5 {
			t.Errorf("Expected fewer than 5 calls due to context timeout, got %d", callCount)
		}
	})
}

// TestPackUnpackReapedStatus tests round-trip encoding of WaitStatus values.
func TestPackUnpackReapedStatus(t *testing.T) {
	tests := []struct {
		name       string
		status     syscall.WaitStatus
		wantReaped bool
	}{
		{
			name:       "exit 0",
			status:     syscall.WaitStatus(0),
			wantReaped: true,
		},
		{
			name:       "exit 1",
			status:     syscall.WaitStatus(1 << 8),
			wantReaped: true,
		},
		{
			name:       "SIGKILL (signal 9)",
			status:     syscall.WaitStatus(9),
			wantReaped: true,
		},
		{
			name:       "exit 2",
			status:     syscall.WaitStatus(2 << 8),
			wantReaped: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packed := packReapedStatus(tt.status)
			reaped, unpacked := unpackReapedStatus(packed)

			if reaped != tt.wantReaped {
				t.Errorf("reaped: got %v, want %v", reaped, tt.wantReaped)
			}
			if unpacked != tt.status {
				t.Errorf("status: got 0x%x, want 0x%x", uint32(unpacked), uint32(tt.status))
			}
		})
	}

	// Zero value means "not reaped"
	t.Run("zero value not reaped", func(t *testing.T) {
		reaped, _ := unpackReapedStatus(0)
		if reaped {
			t.Error("zero packed value should indicate not reaped")
		}
	})
}

// TestRecoverExitCode tests the ECHILD recovery helper.
func TestRecoverExitCode(t *testing.T) {
	// Helper to set a WaitStatus representing a normal exit with the given code.
	makeExitStatus := func(code int) syscall.WaitStatus {
		return syscall.WaitStatus(uint32(code) << 8)
	}
	// SIGKILL is signal 9; WaitStatus for signal death = signal number (low byte, high byte zero).
	sigkillStatus := syscall.WaitStatus(9)

	tests := []struct {
		name         string
		err          error
		eventCount   int
		setupAtomic  func()
		wantExitCode int
		wantOk       bool
	}{
		{
			name:       "ECHILD + handler reaped with exit 0",
			err:        syscall.ECHILD,
			eventCount: 5,
			setupAtomic: func() {
				primaryReapedStatus.Store(packReapedStatus(makeExitStatus(0)))
			},
			wantExitCode: 0,
			wantOk:       true,
		},
		{
			name:       "ECHILD + handler reaped with exit 1",
			err:        syscall.ECHILD,
			eventCount: 5,
			setupAtomic: func() {
				primaryReapedStatus.Store(packReapedStatus(makeExitStatus(1)))
			},
			wantExitCode: 1,
			wantOk:       true,
		},
		{
			name:       "ECHILD + handler reaped with SIGKILL",
			err:        syscall.ECHILD,
			eventCount: 5,
			setupAtomic: func() {
				primaryReapedStatus.Store(packReapedStatus(sigkillStatus))
			},
			wantExitCode: 128 + 9, // 137
			wantOk:       true,
		},
		{
			name:       "ECHILD + not reaped + eventCount > 0 (fast-exit race fallback)",
			err:        syscall.ECHILD,
			eventCount: 12,
			setupAtomic: func() {
				primaryReapedStatus.Store(0)
			},
			wantExitCode: 0,
			wantOk:       true,
		},
		{
			name:       "ECHILD + not reaped + eventCount == 0 (silent crash)",
			err:        syscall.ECHILD,
			eventCount: 0,
			setupAtomic: func() {
				primaryReapedStatus.Store(0)
			},
			wantExitCode: 1,
			wantOk:       false,
		},
		{
			name:       "non-ECHILD error passthrough",
			err:        errors.New("some other error"),
			eventCount: 5,
			setupAtomic: func() {
				primaryReapedStatus.Store(0)
			},
			wantExitCode: 1,
			wantOk:       false,
		},
		{
			name:       "os.ErrPermission is not ECHILD",
			err:        errors.New("permission denied"),
			eventCount: 10,
			setupAtomic: func() {
				primaryReapedStatus.Store(packReapedStatus(makeExitStatus(0)))
			},
			wantExitCode: 1,
			wantOk:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset atomic before each test.
			primaryReapedStatus.Store(0)
			if tt.setupAtomic != nil {
				tt.setupAtomic()
			}

			gotCode, gotOk := recoverExitCode(tt.err, tt.eventCount)

			if gotCode != tt.wantExitCode {
				t.Errorf("exitCode: got %d, want %d", gotCode, tt.wantExitCode)
			}
			if gotOk != tt.wantOk {
				t.Errorf("recovered: got %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}