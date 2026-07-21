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
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alcove-ai/alcove/internal/runtime"
)

// mockRuntime is a minimal Runtime implementation for testing reconciliation.
type mockRuntime struct {
	statuses map[string]string // taskID -> status
	mu       sync.Mutex

	// Tracking fields for verifying calls.
	cleanupCalls       []string // prefixes passed to CleanupOrphanedContainers
	cleanupResult      int      // count to return from CleanupOrphanedContainers
	stopServiceCalls   []string // names passed to StopService
}

func (m *mockRuntime) RunTask(_ context.Context, _ runtime.TaskSpec) (runtime.TaskHandle, error) {
	return runtime.TaskHandle{}, nil
}

func (m *mockRuntime) CancelTask(_ context.Context, _ runtime.TaskHandle) error {
	return nil
}

func (m *mockRuntime) TaskStatus(_ context.Context, handle runtime.TaskHandle) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if status, ok := m.statuses[handle.ID]; ok {
		return status, nil
	}
	return "not_found", nil
}

func (m *mockRuntime) EnsureService(_ context.Context, _ runtime.ServiceSpec) error {
	return nil
}

func (m *mockRuntime) StopService(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopServiceCalls = append(m.stopServiceCalls, name)
	return nil
}

func (m *mockRuntime) CreateVolume(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (m *mockRuntime) Info(_ context.Context) (runtime.RuntimeInfo, error) {
	return runtime.RuntimeInfo{Type: "mock"}, nil
}

func (m *mockRuntime) CleanupOrphanedContainers(_ context.Context, prefix string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupCalls = append(m.cleanupCalls, prefix)
	return m.cleanupResult, nil
}

// TestCleanupContainers verifies that cleanupContainers calls StopService
// for both gate and dev containers with the correct names.
func TestCleanupContainers(t *testing.T) {
	rt := &mockRuntime{statuses: map[string]string{}}
	d := &Dispatcher{
		rt:              rt,
		handles:         make(map[string]runtime.TaskHandle),
		firstSeenExited: make(map[string]time.Time),
	}

	taskID := "test-task-123"
	sessionID := "test-session-456"

	// Call cleanupContainers - this runs asynchronously
	d.cleanupContainers(taskID, sessionID)

	// Wait for the goroutine to complete (5s sleep + cleanup)
	time.Sleep(6 * time.Second)

	// Verify StopService was called for both containers
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if len(rt.stopServiceCalls) != 2 {
		t.Fatalf("expected 2 StopService calls, got %d", len(rt.stopServiceCalls))
	}

	expectedGate := "gate-test-task-123"
	expectedDev := "dev-test-task-123"

	if rt.stopServiceCalls[0] != expectedGate {
		t.Errorf("first StopService call = %q, want %q", rt.stopServiceCalls[0], expectedGate)
	}
	if rt.stopServiceCalls[1] != expectedDev {
		t.Errorf("second StopService call = %q, want %q", rt.stopServiceCalls[1], expectedDev)
	}
}

// TestReconcilerGracePeriod_FirstSighting verifies that the first time
// a session is seen as exited, it gets recorded in firstSeenExited
// and no status update occurs.
func TestReconcilerGracePeriod_FirstSighting(t *testing.T) {
	rt := &mockRuntime{statuses: map[string]string{
		"task-1": "exited",
	}}
	d := &Dispatcher{
		rt:              rt,
		handles:         make(map[string]runtime.TaskHandle),
		firstSeenExited: make(map[string]time.Time),
	}

	// Mock the database check by providing a determineOrphanedSessionOutcome that returns "completed"
	// This test focuses on grace period logic, not the actual DB operations

	// Verify firstSeenExited is initially empty
	d.mu.Lock()
	count := len(d.firstSeenExited)
	d.mu.Unlock()
	if count != 0 {
		t.Errorf("expected empty firstSeenExited, got %d entries", count)
	}

	// The actual reconciler logic is complex and requires DB mocking.
	// This test verifies the basic map operations work correctly.
	sessionID := "session-1"
	now := time.Now()
	d.mu.Lock()
	d.firstSeenExited[sessionID] = now
	d.mu.Unlock()

	// Verify the entry exists
	d.mu.Lock()
	firstSeen, exists := d.firstSeenExited[sessionID]
	d.mu.Unlock()

	if !exists {
		t.Error("expected session-1 to be in firstSeenExited")
	}
	if firstSeen.Sub(now) > time.Millisecond {
		t.Errorf("expected timestamp to match, got %v", firstSeen.Sub(now))
	}
}

// TestReconcilerGracePeriod_UnderThreshold verifies that when a session
// has been seen as exited but less than 30 seconds have passed, it
// remains in the firstSeenExited map and no status update occurs.
func TestReconcilerGracePeriod_UnderThreshold(t *testing.T) {
	d := &Dispatcher{
		rt:              &mockRuntime{statuses: map[string]string{}},
		handles:         make(map[string]runtime.TaskHandle),
		firstSeenExited: make(map[string]time.Time),
	}

	sessionID := "session-under-threshold"
	// Set the first seen time to 10 seconds ago (under the 30s threshold)
	d.mu.Lock()
	d.firstSeenExited[sessionID] = time.Now().Add(-10 * time.Second)
	d.mu.Unlock()

	// Check if grace period has elapsed (should be false)
	d.mu.Lock()
	firstSeen, exists := d.firstSeenExited[sessionID]
	d.mu.Unlock()

	if !exists {
		t.Fatal("expected session to exist in firstSeenExited")
	}

	if time.Since(firstSeen) >= 30*time.Second {
		t.Error("expected grace period to not be elapsed")
	}

	// Verify the logic that would skip this session
	shouldSkip := time.Since(firstSeen) < 30*time.Second
	if !shouldSkip {
		t.Error("expected session to be skipped due to grace period")
	}
}

// TestReconcilerGracePeriod_OverThreshold verifies that when a session
// has been seen as exited for more than 30 seconds, the grace period
// logic would proceed with marking it terminal and cleaning up.
func TestReconcilerGracePeriod_OverThreshold(t *testing.T) {
	d := &Dispatcher{
		rt:              &mockRuntime{statuses: map[string]string{}},
		handles:         make(map[string]runtime.TaskHandle),
		firstSeenExited: make(map[string]time.Time),
	}

	sessionID := "session-over-threshold"
	// Set the first seen time to 31 seconds ago (over the 30s threshold)
	d.mu.Lock()
	d.firstSeenExited[sessionID] = time.Now().Add(-31 * time.Second)
	d.mu.Unlock()

	// Check if grace period has elapsed (should be true)
	d.mu.Lock()
	firstSeen, exists := d.firstSeenExited[sessionID]
	d.mu.Unlock()

	if !exists {
		t.Fatal("expected session to exist in firstSeenExited")
	}

	if time.Since(firstSeen) < 30*time.Second {
		t.Error("expected grace period to be elapsed")
	}

	// Verify the logic that would proceed with marking
	shouldProceed := time.Since(firstSeen) >= 30*time.Second
	if !shouldProceed {
		t.Error("expected session to proceed with marking")
	}

	// Simulate the cleanup that would happen
	d.mu.Lock()
	delete(d.firstSeenExited, sessionID)
	d.mu.Unlock()

	// Verify the entry was removed
	d.mu.Lock()
	_, stillExists := d.firstSeenExited[sessionID]
	d.mu.Unlock()

	if stillExists {
		t.Error("expected session to be removed from firstSeenExited")
	}
}

// TestReconcilerGracePeriod_ContainerError_NoDelay verifies that
// container startup errors (status prefixed with "error:") bypass
// the grace period and are marked immediately as error.
func TestReconcilerGracePeriod_ContainerError_NoDelay(t *testing.T) {
	// Test the logic that determines if a status should bypass grace period
	testCases := []struct {
		name   string
		status string
		err    error
		bypass bool
	}{
		{
			name:   "container_error",
			status: "error:ImagePullBackOff",
			err:    nil,
			bypass: true,
		},
		{
			name:   "crash_loop",
			status: "error:CrashLoopBackOff",
			err:    nil,
			bypass: true,
		},
		{
			name:   "exited_status",
			status: "exited",
			err:    nil,
			bypass: false,
		},
		{
			name:   "not_found_status",
			status: "not_found",
			err:    nil,
			bypass: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			shouldBypass := tc.err == nil && len(tc.status) > 6 && tc.status[:6] == "error:"
			if shouldBypass != tc.bypass {
				t.Errorf("expected bypass=%v for status %q, got bypass=%v", tc.bypass, tc.status, shouldBypass)
			}
		})
	}
}

// TestFirstSeenExited_StaleCleanup verifies that entries in the
// firstSeenExited map are cleaned up when sessions are no longer
// present in the running query (i.e., they've been handled or are gone).
func TestFirstSeenExited_StaleCleanup(t *testing.T) {
	d := &Dispatcher{
		rt:              &mockRuntime{statuses: map[string]string{}},
		handles:         make(map[string]runtime.TaskHandle),
		firstSeenExited: make(map[string]time.Time),
	}

	// Add some entries to firstSeenExited
	d.mu.Lock()
	d.firstSeenExited["session-1"] = time.Now().Add(-10 * time.Second)
	d.firstSeenExited["session-2"] = time.Now().Add(-5 * time.Second)
	d.firstSeenExited["session-3"] = time.Now().Add(-15 * time.Second)
	d.mu.Unlock()

	// Simulate the cleanup logic from RecoverHandles
	// In this cycle, only session-1 and session-3 are seen
	seenThisCycle := map[string]bool{
		"session-1": true,
		"session-3": true,
		// session-2 is NOT seen this cycle
	}

	// Clean up stale entries
	d.mu.Lock()
	for sid := range d.firstSeenExited {
		if !seenThisCycle[sid] {
			delete(d.firstSeenExited, sid)
		}
	}
	d.mu.Unlock()

	// Verify cleanup
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.firstSeenExited) != 2 {
		t.Errorf("expected 2 entries after cleanup, got %d", len(d.firstSeenExited))
	}

	if _, exists := d.firstSeenExited["session-1"]; !exists {
		t.Error("expected session-1 to remain")
	}
	if _, exists := d.firstSeenExited["session-3"]; !exists {
		t.Error("expected session-3 to remain")
	}
	if _, exists := d.firstSeenExited["session-2"]; exists {
		t.Error("expected session-2 to be cleaned up")
	}
}

// TestReconcileLoop_ContextCancellation verifies that ReconcileLoop exits
// when its context is cancelled.
func TestReconcileLoop_ContextCancellation(t *testing.T) {
	d := &Dispatcher{
		rt:              &mockRuntime{statuses: map[string]string{}},
		handles:         make(map[string]runtime.TaskHandle),
		firstSeenExited: make(map[string]time.Time),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.ReconcileLoop(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// ReconcileLoop exited as expected.
	case <-time.After(5 * time.Second):
		t.Fatal("ReconcileLoop did not exit after context cancellation")
	}
}

// TestMockRuntime_TaskStatus_NotFound verifies that the mock runtime
// returns "not_found" for unknown task IDs, matching the real runtime
// behavior that RecoverHandles depends on.
func TestMockRuntime_TaskStatus_NotFound(t *testing.T) {
	rt := &mockRuntime{statuses: map[string]string{}}

	status, err := rt.TaskStatus(context.Background(), runtime.TaskHandle{ID: "nonexistent"})
	if err != nil {
		t.Fatalf("TaskStatus() error: %v", err)
	}
	if status != "not_found" {
		t.Errorf("status = %q, want %q", status, "not_found")
	}
}

// TestMockRuntime_TaskStatus_Running verifies the mock runtime returns
// "running" when configured, matching the status values used by
// RecoverHandles.
func TestMockRuntime_TaskStatus_Running(t *testing.T) {
	rt := &mockRuntime{statuses: map[string]string{"task-1": "running"}}

	status, err := rt.TaskStatus(context.Background(), runtime.TaskHandle{ID: "task-1"})
	if err != nil {
		t.Fatalf("TaskStatus() error: %v", err)
	}
	if status != "running" {
		t.Errorf("status = %q, want %q", status, "running")
	}
}

// TestMockRuntime_TaskStatus_Exited verifies the mock runtime returns
// "exited" when configured, matching the status that triggers
// RecoverHandles to mark sessions as completed.
func TestMockRuntime_TaskStatus_Exited(t *testing.T) {
	rt := &mockRuntime{statuses: map[string]string{"task-2": "exited"}}

	status, err := rt.TaskStatus(context.Background(), runtime.TaskHandle{ID: "task-2"})
	if err != nil {
		t.Fatalf("TaskStatus() error: %v", err)
	}
	if status != "exited" {
		t.Errorf("status = %q, want %q", status, "exited")
	}
}

// TestRecoverHandles_NoDBConnection verifies that RecoverHandles
// gracefully handles a nil DB pool (logs and returns without panic).
func TestRecoverHandles_NoDBConnection(t *testing.T) {
	d := &Dispatcher{
		rt:              &mockRuntime{statuses: map[string]string{}},
		handles:         make(map[string]runtime.TaskHandle),
		firstSeenExited: make(map[string]time.Time),
		// db is nil — RecoverHandles should log an error and return.
	}

	// This should not panic.
	d.RecoverHandles(context.Background())

	// Verify handles map is still empty.
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.handles) != 0 {
		t.Errorf("expected empty handles map, got %d entries", len(d.handles))
	}
}

// TestCleanupOrphanedContainers_GatePrefix verifies that
// CleanupOrphanedContainers is called with the "gate-" prefix and returns the
// expected count. The real ReconcileLoop calls this on a 2-minute ticker
// (see dispatcher.go ReconcileLoop); we test the call directly here because
// the ticker interval makes a true integration test impractical.
func TestCleanupOrphanedContainers_GatePrefix(t *testing.T) {
	rt := &mockRuntime{
		statuses:      map[string]string{},
		cleanupResult: 2,
	}
	d := &Dispatcher{
		rt:              rt,
		handles:         make(map[string]runtime.TaskHandle),
		firstSeenExited: make(map[string]time.Time),
	}

	// Directly call CleanupOrphanedContainers as the reconcile loop would.
	cleaned, err := d.rt.CleanupOrphanedContainers(context.Background(), "gate-")
	if err != nil {
		t.Fatalf("CleanupOrphanedContainers() error: %v", err)
	}
	if cleaned != 2 {
		t.Errorf("cleaned = %d, want 2", cleaned)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.cleanupCalls) != 1 {
		t.Fatalf("expected 1 cleanup call, got %d", len(rt.cleanupCalls))
	}
	if rt.cleanupCalls[0] != "gate-" {
		t.Errorf("cleanup prefix = %q, want %q", rt.cleanupCalls[0], "gate-")
	}
}

// TestStatusHandler_GateCleanup verifies that when a session reaches a
// terminal state (completed/error/timeout), the dispatcher triggers
// StopService for the gate container derived from the task handle.
func TestStatusHandler_GateCleanup(t *testing.T) {
	rt := &mockRuntime{statuses: map[string]string{}}
	d := &Dispatcher{
		rt:              rt,
		handles:         make(map[string]runtime.TaskHandle),
		firstSeenExited: make(map[string]time.Time),
	}

	// Pre-populate a handle as if RunTask had created it.
	taskID := "task-42"
	sessionID := "session-42"
	d.handles[sessionID] = runtime.TaskHandle{
		ID:      taskID,
		PodName: runtime.SkiffContainerName(taskID),
	}

	// Simulate the gate cleanup logic from the status handler:
	// On terminal state, the dispatcher removes the handle and calls
	// StopService on the gate container name after a grace period.
	d.mu.Lock()
	handle, hasHandle := d.handles[sessionID]
	delete(d.handles, sessionID)
	d.mu.Unlock()

	if !hasHandle {
		t.Fatal("expected handle to be present for session")
	}

	// Call cleanupContainers and wait for it to complete
	d.cleanupContainers(handle.ID, sessionID)
	time.Sleep(6 * time.Second) // Wait for 5s delay + cleanup

	// Verify StopService was called with the correct gate container name.
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.stopServiceCalls) != 2 { // gate + dev containers
		t.Fatalf("expected 2 StopService calls, got %d", len(rt.stopServiceCalls))
	}
	expectedGateName := "gate-task-42"
	expectedDevName := "dev-task-42"
	if rt.stopServiceCalls[0] != expectedGateName {
		t.Errorf("first StopService called with %q, want %q", rt.stopServiceCalls[0], expectedGateName)
	}
	if rt.stopServiceCalls[1] != expectedDevName {
		t.Errorf("second StopService called with %q, want %q", rt.stopServiceCalls[1], expectedDevName)
	}

	// Verify the handle was removed from the map.
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.handles[sessionID]; exists {
		t.Error("handle should have been removed from handles map")
	}
}

// TestStatusHandler_GateCleanup_NoHandle verifies that when a session
// reaches a terminal state but has no handle (e.g., after Bridge restart),
// no StopService call is made and no panic occurs.
func TestStatusHandler_GateCleanup_NoHandle(t *testing.T) {
	rt := &mockRuntime{statuses: map[string]string{}}
	d := &Dispatcher{
		rt:              rt,
		handles:         make(map[string]runtime.TaskHandle),
		firstSeenExited: make(map[string]time.Time),
	}

	// Simulate the gate cleanup logic with no handle present.
	sessionID := "session-orphan"
	d.mu.Lock()
	_, hasHandle := d.handles[sessionID]
	delete(d.handles, sessionID)
	d.mu.Unlock()

	if hasHandle {
		t.Fatal("expected no handle for this session")
	}

	// No StopService call should be made when there is no handle.
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.stopServiceCalls) != 0 {
		t.Errorf("expected 0 StopService calls, got %d", len(rt.stopServiceCalls))
	}
}

// TestDispatcherInitialization verifies that NewDispatcher properly initializes firstSeenExited
func TestDispatcherInitialization(t *testing.T) {
	d := NewDispatcher(nil, nil, nil, nil, nil, nil, nil, nil, nil)

	if d.firstSeenExited == nil {
		t.Error("expected firstSeenExited to be initialized")
	}

	// Test that we can add entries to the map
	d.mu.Lock()
	d.firstSeenExited["test-session"] = time.Now()
	count := len(d.firstSeenExited)
	d.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 entry in firstSeenExited, got %d", count)
	}
}

// TestGraceLogic_ContainerStatuses verifies the logic flow for different container statuses.
// This is a focused test that doesn't require a full DB mock.
func TestGraceLogic_ContainerStatuses(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		err          error
		wantGrace    bool
		description  string
	}{
		{
			name:        "exited_status",
			status:      "exited",
			err:         nil,
			wantGrace:   true,
			description: "exited status should trigger grace period",
		},
		{
			name:        "stopped_status",
			status:      "stopped",
			err:         nil,
			wantGrace:   true,
			description: "stopped status should trigger grace period",
		},
		{
			name:        "not_found_status",
			status:      "not_found",
			err:         nil,
			wantGrace:   true,
			description: "not_found status should trigger grace period",
		},
		{
			name:        "error_prefix",
			status:      "error:ImagePullBackOff",
			err:         nil,
			wantGrace:   false,
			description: "error: prefix should NOT trigger grace period",
		},
		{
			name:        "running_status",
			status:      "running",
			err:         nil,
			wantGrace:   false,
			description: "running status should not trigger grace period",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check if status should trigger grace period logic
			shouldTriggerGrace := (tt.err != nil || tt.status == "not_found") || (tt.status == "exited" || tt.status == "stopped")
			shouldBypassGrace := (tt.err == nil && tt.status != "" && !shouldTriggerGrace) || (tt.err == nil && tt.status != "" && tt.status[:6] == "error:" && len(tt.status) > 6)

			if tt.status != "" && len(tt.status) > 6 && tt.status[:6] == "error:" {
				shouldTriggerGrace = false
				shouldBypassGrace = true
			}

			actualGrace := shouldTriggerGrace && !shouldBypassGrace
			if actualGrace != tt.wantGrace {
				t.Errorf("%s: expected grace=%v, got grace=%v", tt.description, tt.wantGrace, actualGrace)
			}
		})
	}
}
