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

// Command skiff-init is the PID 1 process inside Skiff containers.
// It reads the task from environment variables, runs Claude Code as a child process,
// streams transcript events to Ledger, and handles timeouts and cancellation.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alcove-ai/alcove/internal"
	"github.com/alcove-ai/alcove/internal/hail"
	"github.com/alcove-ai/alcove/internal/ledger"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// primaryChildPID tracks the main process PID to prevent the SIGCHLD zombie reaper
// from racing with cmd.Wait() and losing the real exit code.
var primaryChildPID atomic.Int64

// primaryReapedStatus packs the reaper state into a single atomic to avoid
// cross-variable ordering hazards. Bit 32 = reaped flag, bits 0-31 = WaitStatus.
// Zero value means "not reaped by SIGCHLD handler".
//
// Race scenario: Wait4(-1, WNOHANG) in the SIGCHLD handler reaps any available child,
// including the primary child, before the PID check on the next line. If the primary
// child exits before primaryChildPID.Store() executes (fast-exiting processes < 3s),
// the handler reaps it and cmd.Wait() gets ECHILD. This atomic captures the real
// WaitStatus so recoverExitCode() can restore it.
var primaryReapedStatus atomic.Int64

// packReapedStatus packs a WaitStatus into the primaryReapedStatus atomic.
// Bit 32 acts as the reaped flag; bits 0-31 hold the raw WaitStatus value.
func packReapedStatus(status syscall.WaitStatus) int64 {
	return (1 << 32) | int64(uint32(status))
}

// unpackReapedStatus unpacks the primaryReapedStatus atomic value.
func unpackReapedStatus(packed int64) (reaped bool, status syscall.WaitStatus) {
	return packed != 0, syscall.WaitStatus(uint32(packed))
}

// recoverExitCode handles cmd.Wait() errors in the SIGCHLD race scenario.
// Returns the recovered exit code and whether recovery succeeded.
// Only recovers from ECHILD — other errors are genuine failures.
func recoverExitCode(err error, eventCount int) (exitCode int, recovered bool) {
	// Only recover from ECHILD — other errors are genuine failures.
	if !errors.Is(err, syscall.ECHILD) {
		return 1, false
	}
	// Check if the SIGCHLD handler captured the real exit status.
	if packed := primaryReapedStatus.Load(); packed != 0 {
		_, ws := unpackReapedStatus(packed)
		if ws.Exited() {
			return ws.ExitStatus(), true
		}
		if ws.Signaled() {
			return 128 + int(ws.Signal()), true
		}
		// Stopped or other — treat as error.
		return 1, true
	}
	// Safety net: process produced output but we lost the exit code.
	// Treat as success since the process ran and produced output.
	if eventCount > 0 {
		return 0, true
	}
	// Truly unknown failure — no output and no captured status.
	// Preserve #476 behavior: silent exits are treated as errors.
	return 1, false
}

// skillPluginDirs holds paths to cloned skill/agent repos for --plugin-dir flags.
var skillPluginDirs []string

// lolaModuleDirs holds paths to cloned lola module repos for deferred installation.
// Lola modules are installed after the project repo is cloned so that lola writes
// skills/agents/commands into the correct project directory.
var lolaModuleDirs []string

const (
	defaultHeartbeatTimeout = 10 * time.Minute
	walBatchSize            = 50
	walFlushInterval        = 5 * time.Second
	shutdownGrace           = 10 * time.Second
	maxBatchSize            = 500  // Max events in memory to prevent unbounded growth
)

func main() {
	log.SetPrefix("skiff-init: ")
	log.SetFlags(log.Ltime | log.Lmsgprefix)

	// --- Read task from environment variables (injected by Bridge) ---
	taskID := requireEnv("TASK_ID")
	sessionID := envOrDefault("SESSION_ID", taskID)
	prompt := os.Getenv("PROMPT")
	if prompt == "" && os.Getenv("ALCOVE_EXECUTABLE") == "" {
		log.Fatal("required environment variable PROMPT is not set")
	}
	provider := envOrDefault("PROVIDER", "anthropic")
	model := os.Getenv("CLAUDE_MODEL")
	budgetStr := os.Getenv("TASK_BUDGET")

	var budget float64
	if budgetStr != "" {
		budget, _ = strconv.ParseFloat(budgetStr, 64)
	}

	timeoutStr := envOrDefault("TASK_TIMEOUT", "3600")
	timeoutSecs, _ := strconv.Atoi(timeoutStr)
	if timeoutSecs <= 0 {
		timeoutSecs = 3600
	}

	heartbeatTimeout := parseDuration(os.Getenv("HEARTBEAT_TIMEOUT"), defaultHeartbeatTimeout)

	var repos []internal.RepoSpec
	if reposJSON := os.Getenv("REPOS"); reposJSON != "" {
		if err := json.Unmarshal([]byte(reposJSON), &repos); err != nil {
			log.Fatalf("invalid REPOS JSON: %v", err)
		}
	}

	task := internal.Task{
		ID:       taskID,
		Prompt:   prompt,
		Repos:    repos,
		Provider: provider,
		Model:    model,
		Budget:   budget,
		Timeout:  time.Duration(timeoutSecs) * time.Second,
	}

	repoDisplay := ""
	if len(task.Repos) > 0 {
		repoDisplay = task.Repos[0].URL
	}
	log.Printf("task %s received: prompt=%q repo=%s", task.ID, truncate(task.Prompt, 60), repoDisplay)

	// --- Connect to NATS (Hail) for status updates and cancellation ---
	hailURL := envOrDefault("HAIL_URL", "nats://localhost:4222")
	log.Printf("connecting to Hail at %s", hailURL)
	hailClient, err := hail.Connect(hailURL)
	if err != nil {
		log.Printf("warning: could not connect to Hail: %v (continuing without status updates)", err)
		hailClient = nil
	}
	if hailClient != nil {
		defer hailClient.Close()
	}

	// --- Subscribe to cancellation ---
	var cancelCh <-chan struct{}
	if hailClient != nil {
		cancelCh, err = hailClient.SubscribeCancel(sessionID)
		if err != nil {
			log.Printf("warning: failed to subscribe to cancel topic: %v", err)
		}
	}
	if cancelCh == nil {
		// No-op cancel channel
		ch := make(chan struct{})
		cancelCh = ch
	}

	// --- Set up CA trust for MITM proxy ---
	if err := setupCATrust(); err != nil {
		log.Printf("warning: CA trust setup failed: %v", err)
	}

	// --- Send running status ---
	if hailClient != nil {
		_ = hailClient.PublishStatus(task.ID, hail.StatusUpdate{
			TaskID:    task.ID,
			SessionID: sessionID,
			Status:    "running",
			Outputs:   nil,
		})
	}

	// --- Create Ledger client ---
	ledgerURL := envOrDefault("LEDGER_URL", "http://localhost:8081")
	ledgerToken := os.Getenv("SESSION_TOKEN")
	lc := ledger.NewClient(ledgerURL, ledgerToken)

	// --- Set up environment ---
	setupEnv(task)

	// --- Clone repos if specified ---
	if len(task.Repos) > 0 {
		for i, r := range task.Repos {
			dir := r.Name
			if dir == "" {
				dir = repoNameFromURL(r.URL)
			}
			// Single repo: clone directly into /workspace.
			// Multiple repos: clone into /workspace/<name>.
			var target string
			if len(task.Repos) == 1 {
				target = "/workspace"
			} else {
				target = filepath.Join("/workspace", dir)
			}
			// Retry clone with backoff — Gate sidecar may not be DNS-resolvable yet.
			var cloneErr error
			for attempt := 1; attempt <= 5; attempt++ {
				cloneErr = cloneRepoToDir(r.URL, r.Ref, target)
				if cloneErr == nil {
					break
				}
				log.Printf("warning: repo clone attempt %d/5 failed for %s: %v", attempt, r.URL, cloneErr)
				if attempt < 5 {
					time.Sleep(time.Duration(attempt) * 2 * time.Second)
					os.RemoveAll(target)
				}
			}
			if cloneErr != nil {
				log.Printf("error: repo clone failed after 5 attempts for %s: %v", r.URL, cloneErr)
				continue
			}
			log.Printf("cloned repo %d/%d: %s -> %s", i+1, len(task.Repos), r.URL, target)
		}
		// Set CWD: single repo -> /workspace, multi -> /workspace
		os.Chdir("/workspace")
	}

	// --- Inject CLAUDE.md from cloned repos ---
	// Claude Code runs with --bare which disables native CLAUDE.md discovery.
	// We read it explicitly and prepend it to the prompt.
	task.Prompt = injectClaudeMD(task.Repos, task.Prompt)

	// --- Install lola modules (must run after cloneRepo so cwd is correct) ---
	installLolaModules()

	// --- Capture and send redacted environment snapshot ---
	// Reuse the debug-env binary (baked into the Skiff image) for env
	// classification, redaction, and formatting — one source of truth.
	if envOut, err := exec.Command("/usr/local/bin/debug-env").Output(); err == nil {
		if err := lc.UpdateEnvSnapshot(sessionID, string(envOut)); err != nil {
			log.Printf("warning: failed to send env snapshot to Ledger: %v", err)
		} else {
			log.Printf("env snapshot captured (%d bytes)", len(envOut))
		}
	} else {
		log.Printf("warning: debug-env not available for env snapshot: %v", err)
	}

	// --- Build context with hard timeout ---
	ctx, cancel := context.WithTimeout(context.Background(), task.Timeout)
	defer cancel()

	// --- Set up cooperative SIGTERM handler ---
	termCh := make(chan os.Signal, 1)
	signal.Notify(termCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-termCh
		log.Printf("received %v, initiating graceful shutdown", sig)
		cancel() // triggers cleanup path in main()
		// Safety net: if cleanup hangs, force exit
		time.Sleep(55 * time.Second)
		log.Printf("graceful shutdown timed out, forcing exit")
		os.Exit(128 + int(sig.(syscall.Signal)))
	}()

	// --- Check if this is an executable agent or Claude Code agent ---
	var exitCode int
	var outcome string
	var artifacts []internal.Artifact
	var outputs map[string]string

	if executableConfig := os.Getenv("ALCOVE_EXECUTABLE"); executableConfig != "" {
		// Run executable agent
		exitCode, outcome, artifacts, outputs = runExecutable(ctx, executableConfig, sessionID, hailClient, lc, heartbeatTimeout, cancelCh)
	} else {
		// Run Claude Code
		exitCode, outcome, artifacts, outputs = runClaude(ctx, task, sessionID, hailClient, lc, heartbeatTimeout, cancelCh)
	}

	// --- Send final status with retry logic ---
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()

	if hailClient != nil {
		finalStatus := hail.StatusUpdate{
			TaskID:    task.ID,
			SessionID: sessionID,
			Status:    outcome,
			ExitCode:  &exitCode,
			Artifacts: artifacts,
			Outputs:   outputs,
		}
		retryWithBackoff(cleanupCtx, "NATS status publish", 3, 2*time.Second, func() error {
			return hailClient.PublishStatus(task.ID, finalStatus)
		})
	}

	retryWithBackoff(cleanupCtx, "session update", 3, 2*time.Second, func() error {
		return lc.UpdateSession(sessionID, outcome, &exitCode, artifacts)
	})

	log.Printf("task %s finished: %s (exit %d)", task.ID, outcome, exitCode)
	os.Exit(exitCode)
}

// retryWithBackoff executes fn with exponential backoff retry, checking context cancellation before each attempt.
func retryWithBackoff(ctx context.Context, name string, maxAttempts int, baseDelay time.Duration, fn func() error) {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Check context cancellation before each attempt
		if err := ctx.Err(); err != nil {
			log.Printf("error: %s context cancelled, aborting retry", name)
			return
		}

		if err := fn(); err != nil {
			log.Printf("warning: %s attempt %d/%d failed: %v", name, attempt, maxAttempts, err)
			if attempt < maxAttempts {
				time.Sleep(baseDelay * time.Duration(attempt))
				continue
			}
			log.Printf("error: %s failed after %d attempts", name, maxAttempts)
			return
		}
		log.Printf("%s succeeded on attempt %d", name, attempt)
		return
	}
}

// determineOutcome determines the final outcome string based on context error,
// current outcome state, success result flag, exit code, and event count.
// This unifies outcome determination logic between runClaude() and runExecutable().
func determineOutcome(ctxErr error, currentOutcome string, sawSuccessResult bool, exitCode int, eventCount int) string {
	// Context timeout takes priority
	if ctxErr != nil {
		return "timeout"
	}

	// Preserve cancelled and timeout states from heartbeat/cancellation
	if currentOutcome == "cancelled" {
		return "cancelled"
	}
	if currentOutcome == "timeout" {
		return "timeout"
	}

	// Success result from Claude Code takes priority (indicates "result" event with is_error=false)
	if sawSuccessResult {
		return "completed"
	}

	// Key fix: sessions with zero output events and exit code 0 are errors
	// (likely timeout during startup with no meaningful work done)
	if eventCount == 0 && exitCode == 0 {
		return "error"
	}

	// Non-zero exit code indicates error
	if exitCode != 0 {
		return "error"
	}

	// Default: completed
	return "completed"
}

// runExecutable downloads and executes a pre-compiled executable agent. It returns the exit code,
// outcome string, artifacts, and any outputs.
func runExecutable(
	ctx context.Context,
	execConfigJSON string,
	sessionID string,
	hailClient *hail.Client,
	lc *ledger.Client,
	heartbeatTimeout time.Duration,
	cancelCh <-chan struct{},
) (int, string, []internal.Artifact, map[string]string) {

	// Parse the executable configuration
	var execSpec internal.ExecutableSpec
	if err := json.Unmarshal([]byte(execConfigJSON), &execSpec); err != nil {
		log.Printf("error parsing ALCOVE_EXECUTABLE: %v", err)
		return 1, "error", nil, nil
	}

	// Resolve the executable path: local file or remote download
	agentPath := "/tmp/agent"
	if strings.HasPrefix(execSpec.URL, "file://") {
		agentPath = strings.TrimPrefix(execSpec.URL, "file://")
		log.Printf("using local executable at %s", agentPath)
	} else if strings.HasPrefix(execSpec.URL, "/") {
		agentPath = execSpec.URL
		log.Printf("using local executable at %s", agentPath)
	} else {
		log.Printf("downloading executable from %s", execSpec.URL)

		// Download the executable
		downloadCmd := exec.CommandContext(ctx, "curl", "-sL", execSpec.URL, "-o", agentPath)
		if err := downloadCmd.Run(); err != nil {
			log.Printf("error downloading executable: %v", err)
			return 1, "error", nil, nil
		}

		// Make it executable (only needed for downloaded files)
		if err := os.Chmod(agentPath, 0755); err != nil {
			log.Printf("error making executable: %v", err)
			return 1, "error", nil, nil
		}
	}

	log.Printf("running executable: %s %v", agentPath, execSpec.Args)

	// Build command
	cmd := exec.CommandContext(ctx, agentPath, execSpec.Args...)

	// Set additional environment variables from execSpec.Env
	cmd.Env = os.Environ()
	for k, v := range execSpec.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Create pipes for both stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("error creating stdout pipe: %v", err)
		return 1, "error", nil, nil
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("error creating stderr pipe: %v", err)
		return 1, "error", nil, nil
	}

	if err := cmd.Start(); err != nil {
		log.Printf("error starting executable: %v", err)
		return 1, "error", nil, nil
	}

	// Store the primary child PID to prevent the SIGCHLD reaper from racing with cmd.Wait()
	primaryChildPID.Store(int64(cmd.Process.Pid))

	// WAL file for local transcript persistence
	walPath := fmt.Sprintf("/tmp/alcove-transcript-%s.jsonl", sessionID)
	walFile, err := os.Create(walPath)
	if err != nil {
		log.Printf("warning: could not create WAL file %s: %v", walPath, err)
	}
	defer func() {
		if walFile != nil {
			walFile.Close()
		}
	}()

	// Shared channel for stdout and stderr lines
	type outputLine struct {
		text   string
		stream string // "stdout" or "stderr"
	}
	ch := make(chan outputLine, 256)

	// Scan stdout and stderr in separate goroutines, both feeding ch
	var scanWg sync.WaitGroup
	scanWg.Add(2)

	go func() {
		defer scanWg.Done()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB line buffer
		for scanner.Scan() {
			ch <- outputLine{text: scanner.Text(), stream: "stdout"}
		}
	}()

	go func() {
		defer scanWg.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB line buffer
		for scanner.Scan() {
			line := scanner.Text()
			// Echo stderr to os.Stderr for container-level debugging (oc logs)
			fmt.Fprintln(os.Stderr, line)
			ch <- outputLine{text: line, stream: "stderr"}
		}
	}()

	// Close ch when both scanners are done
	go func() {
		scanWg.Wait()
		close(ch)
	}()

	var (
		batch      []json.RawMessage
		batchMu    sync.Mutex
		artifacts  []internal.Artifact
		lastEvent  = time.Now()
		ticker     = time.NewTicker(walFlushInterval)
		doneCh     = make(chan struct{})
		outcome    = "completed"
		lineNumber = 0
		eventCount = 0  // Track number of output events
	)
	defer ticker.Stop()

	// Monitor heartbeat timeout, periodic batch flush, and cancellation
	go func() {
		for {
			select {
			case <-doneCh:
				return
			case <-cancelCh:
				log.Println("cancellation received, sending SIGTERM to executable")
				outcome = "cancelled"
				_ = cmd.Process.Signal(syscall.SIGTERM)
				time.Sleep(shutdownGrace)
				_ = cmd.Process.Kill()
				return
			case <-ticker.C:
				if time.Since(lastEvent) > heartbeatTimeout {
					log.Printf("heartbeat timeout (%v without output), sending SIGTERM", heartbeatTimeout)
					outcome = "timeout"
					_ = cmd.Process.Signal(syscall.SIGTERM)
					time.Sleep(shutdownGrace)
					_ = cmd.Process.Kill()
					return
				}
				// Periodic flush
				batchMu.Lock()
				if len(batch) > 0 {
					flushBatch(lc, sessionID, &batch)
				}
				batchMu.Unlock()
			}
		}
	}()

	// Process output lines from both stdout and stderr
	for ol := range ch {
		lastEvent = time.Now()
		lineNumber++
		eventCount++  // Increment event count for each output line

		// Create transcript event for this line
		transcriptEvent := map[string]any{
			"type":      "text",
			"content":   ol.text,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"source":    "executable",
		}
		// Add stream field only for stderr (backward compatibility)
		if ol.stream == "stderr" {
			transcriptEvent["stream"] = "stderr"
		}

		eventJSON, err := json.Marshal(transcriptEvent)
		if err != nil {
			continue // skip malformed events
		}

		// Write to WAL
		if walFile != nil {
			_, _ = walFile.Write(eventJSON)
			_, _ = walFile.Write([]byte("\n"))
		}

		// Publish to NATS for real-time SSE streaming
		if hailClient != nil {
			_ = hailClient.PublishTranscript(sessionID, eventJSON)
		}

		batchMu.Lock()
		// Enforce max batch cap: drop oldest events to prevent unbounded memory growth
		if len(batch) >= maxBatchSize {
			// Keep the newest 499 events
			batch = batch[len(batch)-maxBatchSize+1:]
		}
		batch = append(batch, json.RawMessage(eventJSON))

		// Flush batch when it reaches the batch size
		if len(batch) >= walBatchSize {
			flushBatch(lc, sessionID, &batch)
		}
		batchMu.Unlock()
	}

	close(doneCh)

	// Flush remaining events
	batchMu.Lock()
	if len(batch) > 0 {
		flushBatch(lc, sessionID, &batch)
	}
	batchMu.Unlock()

	// Wait for process to exit
	err = cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// cmd.Wait() returned a non-ExitError — check for ECHILD from the SIGCHLD race.
			// The SIGCHLD handler may have reaped the primary child before cmd.Wait() could,
			// causing ECHILD. recoverExitCode() restores the captured WaitStatus if available.
			if recovered, ok2 := recoverExitCode(err, eventCount); ok2 {
				log.Printf("recovered exit code from SIGCHLD handler: %d", recovered)
				exitCode = recovered
			} else {
				log.Printf("warning: unexpected cmd.Wait() error: %T: %v", err, err)
				exitCode = 1
			}
		}
	}

	log.Printf("executable completed: exit=%d lines=%d events=%d", exitCode, lineNumber, eventCount)

	// Step 3: Add explicit logging for silent agent crashes
	if exitCode != 0 && eventCount == 0 {
		log.Printf("ERROR: ALERT: agent exited %d with zero output events — likely startup crash", exitCode)
	} else if exitCode == 0 && eventCount == 0 {
		log.Printf("WARNING: agent exited 0 with zero output events — marking as error")
	}

	// Step 2b: Use unified outcome determination
	outcome = determineOutcome(ctx.Err(), outcome, false, exitCode, eventCount)

	// Check for PR artifact from task (same as Claude Code)
	if prArtifact := readPRArtifact(); prArtifact != nil {
		artifacts = append(artifacts, *prArtifact)
	}

	// Check for outputs from agent
	var outputs map[string]string
	if agentOutputs := readOutputArtifact(); agentOutputs != nil {
		outputs = agentOutputs
	}

	return exitCode, outcome, artifacts, outputs
}

// runClaude executes Claude Code and streams its output. It returns the exit code,
// outcome string, artifacts, and any outputs.
func runClaude(
	ctx context.Context,
	task internal.Task,
	sessionID string,
	hailClient *hail.Client,
	lc *ledger.Client,
	heartbeatTimeout time.Duration,
	cancelCh <-chan struct{},
) (int, string, []internal.Artifact, map[string]string) {

	// Build command arguments
	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--dangerously-skip-permissions",
		"--bare",
		"--session-id", task.ID,
	}
	if task.Model != "" {
		args = append(args, "--model", task.Model)
	}
	if task.Budget > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(task.Budget, 'f', 2, 64))
	}
	for _, dir := range skillPluginDirs {
		args = append(args, "--plugin-dir", dir)
	}
	args = append(args, task.Prompt)

	log.Printf("DEBUG: running claude with args: %v", args)
	log.Printf("DEBUG: HOME=%s", os.Getenv("HOME"))
	log.Printf("DEBUG: PATH=%s", os.Getenv("PATH"))

	// Check if claude exists
	claudePath, pathErr := exec.LookPath("claude")
	if pathErr != nil {
		log.Printf("DEBUG: claude not found in PATH: %v", pathErr)
	} else {
		log.Printf("DEBUG: claude found at: %s", claudePath)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)

	// Capture stderr to a buffer so we can log it
	var stderrBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("error creating stdout pipe: %v", err)
		return 1, "error", nil, nil
	}

	if err := cmd.Start(); err != nil {
		log.Printf("error starting claude: %v", err)
		return 1, "error", nil, nil
	}

	// Store the primary child PID to prevent the SIGCHLD reaper from racing with cmd.Wait()
	primaryChildPID.Store(int64(cmd.Process.Pid))

	// WAL file for local transcript persistence
	walPath := fmt.Sprintf("/tmp/alcove-transcript-%s.jsonl", task.ID)
	walFile, err := os.Create(walPath)
	if err != nil {
		log.Printf("warning: could not create WAL file %s: %v", walPath, err)
	}
	defer func() {
		if walFile != nil {
			walFile.Close()
		}
	}()

	// Read stdout line-by-line (NDJSON)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB line buffer

	var (
		batch            []json.RawMessage
		batchMu          sync.Mutex
		artifacts        []internal.Artifact
		lastEvent        = time.Now()
		ticker           = time.NewTicker(walFlushInterval)
		doneCh           = make(chan struct{})
		outcome          = "completed"
		sawSuccessResult bool
		eventCount       = 0  // Track number of output events
	)
	defer ticker.Stop()

	// Monitor heartbeat timeout, periodic batch flush, and cancellation
	go func() {
		for {
			select {
			case <-doneCh:
				return
			case <-cancelCh:
				log.Println("cancellation received, sending SIGTERM to claude")
				outcome = "cancelled"
				_ = cmd.Process.Signal(syscall.SIGTERM)
				time.Sleep(shutdownGrace)
				_ = cmd.Process.Kill()
				return
			case <-ticker.C:
				if time.Since(lastEvent) > heartbeatTimeout {
					log.Printf("heartbeat timeout (%v without output), sending SIGTERM", heartbeatTimeout)
					outcome = "timeout"
					_ = cmd.Process.Signal(syscall.SIGTERM)
					time.Sleep(shutdownGrace)
					_ = cmd.Process.Kill()
					return
				}
				// Periodic flush: write buffered transcript events to the database
				// so polling clients see data before the batch reaches 50 events.
				batchMu.Lock()
				if len(batch) > 0 {
					flushBatch(lc, sessionID, &batch)
				}
				batchMu.Unlock()
			}
		}
	}()

	// Process output lines
	for scanner.Scan() {
		line := scanner.Bytes()
		lastEvent = time.Now()
		eventCount++  // Increment event count for each output line

		// Write to WAL
		if walFile != nil {
			_, _ = walFile.Write(line)
			_, _ = walFile.Write([]byte("\n"))
		}

		// Store the raw JSON line directly to preserve all fields
		lineCopy := make([]byte, len(line))
		copy(lineCopy, line)

		// Publish to NATS for real-time SSE streaming
		if hailClient != nil {
			_ = hailClient.PublishTranscript(sessionID, lineCopy)
		}

		// Check for result events to determine success
		var rawMap map[string]any
		if json.Unmarshal(lineCopy, &rawMap) == nil {
			if rawMap["type"] == "result" {
				if isErr, ok := rawMap["is_error"].(bool); ok && !isErr {
					sawSuccessResult = true
				}
			}
		} else {
			continue // skip malformed lines
		}

		batchMu.Lock()
		// Enforce max batch cap: drop oldest events to prevent unbounded memory growth
		if len(batch) >= maxBatchSize {
			// Keep the newest 499 events
			batch = batch[len(batch)-maxBatchSize+1:]
		}
		batch = append(batch, json.RawMessage(lineCopy))

		// Flush batch when it reaches the batch size
		if len(batch) >= walBatchSize {
			flushBatch(lc, sessionID, &batch)
		}
		batchMu.Unlock()
	}

	close(doneCh)

	// Flush remaining events
	batchMu.Lock()
	if len(batch) > 0 {
		flushBatch(lc, sessionID, &batch)
	}
	batchMu.Unlock()

	// Wait for process to exit
	err = cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// cmd.Wait() returned a non-ExitError — check for ECHILD from the SIGCHLD race.
			// The SIGCHLD handler may have reaped the primary child before cmd.Wait() could,
			// causing ECHILD. recoverExitCode() restores the captured WaitStatus if available.
			if recovered, ok2 := recoverExitCode(err, eventCount); ok2 {
				log.Printf("recovered exit code from SIGCHLD handler: %d", recovered)
				exitCode = recovered
			} else {
				log.Printf("warning: unexpected cmd.Wait() error: %T: %v", err, err)
				exitCode = 1
			}
		}
	}

	// Log stderr from Claude for debugging
	if stderrStr := stderrBuf.String(); stderrStr != "" {
		log.Printf("DEBUG: claude stderr:\n%s", stderrStr)
	} else {
		log.Printf("DEBUG: claude stderr: (empty)")
	}
	log.Printf("DEBUG: claude exit code: %d events: %d", exitCode, eventCount)

	// Step 3: Add explicit logging for silent agent crashes
	if exitCode != 0 && eventCount == 0 {
		log.Printf("ERROR: ALERT: agent exited %d with zero output events — likely startup crash", exitCode)
	} else if exitCode == 0 && eventCount == 0 {
		log.Printf("WARNING: agent exited 0 with zero output events — marking as error")
	}

	// Step 2b: Use unified outcome determination
	outcome = determineOutcome(ctx.Err(), outcome, sawSuccessResult, exitCode, eventCount)

	// Check for PR artifact from task.
	if prArtifact := readPRArtifact(); prArtifact != nil {
		artifacts = append(artifacts, *prArtifact)
	}

	// Check for outputs from agent
	var outputs map[string]string
	if agentOutputs := readOutputArtifact(); agentOutputs != nil {
		outputs = agentOutputs
	}

	return exitCode, outcome, artifacts, outputs
}

// flushBatch sends a batch of transcript events to Ledger and clears the batch on success.
// On failure, it retries with exponential backoff and preserves events in the batch.
func flushBatch(lc *ledger.Client, sessionID string, batch *[]json.RawMessage) {
	for attempt := 1; attempt <= 3; attempt++ {
		if err := lc.AppendTranscript(sessionID, *batch); err != nil {
			log.Printf("warning: flush transcript attempt %d/3 failed: %v", attempt, err)
			if attempt < 3 {
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			log.Printf("error: all flush attempts failed, retaining %d events in batch", len(*batch))
			return // keep events in batch for next flush cycle
		}
		*batch = nil
		return
	}
}

// readOutputArtifact checks for an outputs file written by the agent.
// Agents write JSON to /tmp/alcove-outputs.json to report structured outputs.
func readOutputArtifact() map[string]string {
	path := "/tmp/alcove-outputs.json"
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("outputs: %s not found (normal for most tasks)", path)
		return nil
	}

	log.Printf("outputs: read %d bytes from %s: %s", len(data), path, string(data))

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Printf("warning: invalid %s: %v (raw: %s)", path, err, string(data))
		return nil
	}

	if len(raw) == 0 {
		log.Printf("outputs: file exists but empty map")
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

	log.Printf("outputs detected: %d field(s): %v", len(outputs), outputs)
	return outputs
}

// readPRArtifact checks for a PR artifact file written by the task.
// Tasks write {"repo": "owner/repo", "number": 123} to /tmp/alcove-pr.json
// to report the PR they created for CI Gate monitoring.
func readPRArtifact() *internal.Artifact {
	data, err := os.ReadFile("/tmp/alcove-pr.json")
	if err != nil {
		return nil // No PR artifact file — normal for most tasks.
	}

	var pr struct {
		Repo   string `json:"repo"`
		Number int    `json:"number"`
	}
	if err := json.Unmarshal(data, &pr); err != nil {
		log.Printf("warning: invalid /tmp/alcove-pr.json: %v", err)
		return nil
	}
	if pr.Repo == "" || pr.Number == 0 {
		return nil
	}

	log.Printf("PR artifact detected: %s#%d", pr.Repo, pr.Number)
	return &internal.Artifact{
		Type: "pull_request",
		URL:  pr.Repo,
		Ref:  strconv.Itoa(pr.Number),
	}
}

// setupEnv configures the environment for Claude Code execution.
func setupEnv(task internal.Task) {
	// Git configuration for non-interactive use
	setEnvIfMissing("GIT_TERMINAL_PROMPT", "0")
	setEnvIfMissing("GIT_AUTHOR_NAME", "Alcove")
	setEnvIfMissing("GIT_AUTHOR_EMAIL", "alcove@localhost")
	setEnvIfMissing("GIT_COMMITTER_NAME", "Alcove")
	setEnvIfMissing("GIT_COMMITTER_EMAIL", "alcove@localhost")

	// Set Gate credential URL for the git credential helper.
	// The credential helper script reads GATE_CREDENTIAL_URL to know where to POST.
	if gateURL := os.Getenv("ANTHROPIC_BASE_URL"); gateURL != "" {
		// ANTHROPIC_BASE_URL points to http://gate-<taskID>:8443
		// The credential helper needs the same base URL
		setEnvIfMissing("GATE_CREDENTIAL_URL", gateURL)
	}

	// Force HTTPS for git operations (SSH bypasses Gate credential helper).
	setEnvIfMissing("GIT_SSH_COMMAND", "echo 'SSH disabled — use HTTPS' && exit 1")

	// Configure Claude Code: skip onboarding (prevents startup API key validation
	// that bypasses ANTHROPIC_BASE_URL) and set up MCP servers if specified.
	configureClaude(os.Getenv("ALCOVE_MCP_CONFIG"))

	// Load skill/agent repos if specified.
	loadSkillRepos()

	// Install plugins declared in agent definition.
	installPlugins()

	// Apply task-specific env vars
	for k, v := range task.Env {
		os.Setenv(k, v)
	}
}

// configureClaude writes ~/.claude.json with onboarding flag and optional MCP servers.
// hasCompletedOnboarding prevents Claude Code from validating the API key at startup
// via a direct CONNECT tunnel to api.anthropic.com, which bypasses Gate's credential
// injection.
func configureClaude(mcpConfigJSON string) {
	// Build the Claude Code config structure
	claudeConfig := map[string]any{
		"hasCompletedOnboarding": true,
	}

	// Collect all MCP servers, starting with ALCOVE_MCP_CONFIG entries.
	mcpServers := make(map[string]any)

	if mcpConfigJSON != "" {
		var configEntries map[string]any
		if err := json.Unmarshal([]byte(mcpConfigJSON), &configEntries); err != nil {
			log.Printf("warning: invalid ALCOVE_MCP_CONFIG: %v", err)
		} else {
			for k, v := range configEntries {
				mcpServers[k] = v
			}
		}
	}

	// Add MCP_SERVER_URL as an SSE transport entry.
	if mcpServerURL := os.Getenv("MCP_SERVER_URL"); mcpServerURL != "" {
		serverName := os.Getenv("MCP_SERVER_NAME")
		if serverName == "" {
			serverName = "mcp-server"
		}
		mcpServers[serverName] = map[string]any{
			"url": mcpServerURL,
		}
	}

	// Log MCP_TOOL_FILTER if set (enforcement deferred to runtime).
	if mcpToolFilter := os.Getenv("MCP_TOOL_FILTER"); mcpToolFilter != "" {
		log.Printf("MCP_TOOL_FILTER set: %s (filtering deferred to runtime)", mcpToolFilter)
	}

	if len(mcpServers) > 0 {
		claudeConfig["mcpServers"] = mcpServers
	}

	// Determine home directory
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = "/home/skiff"
	}

	// Write to ~/.claude.json
	configPath := filepath.Join(homeDir, ".claude.json")
	data, err := json.MarshalIndent(claudeConfig, "", "  ")
	if err != nil {
		log.Printf("warning: failed to marshal MCP config: %v", err)
		return
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		log.Printf("warning: failed to write MCP config to %s: %v", configPath, err)
		return
	}

	log.Printf("configured claude at %s (onboarding=true)", configPath)

	// Also write settings to auto-approve MCP servers
	settingsPath := filepath.Join(homeDir, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0755)
	settings := map[string]any{
		"enableAllProjectMcpServers": true,
	}
	settingsData, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(settingsPath, settingsData, 0644)
}

// skillRepo represents a skill/agent repository to clone and load as a plugin.
type skillRepo struct {
	URL  string `json:"url"`
	Ref  string `json:"ref,omitempty"`
	Name string `json:"name,omitempty"`
}

// isLolaModule returns true if the given directory looks like a lola module
// (contains module/, skills/, or agents/ directories).
func isLolaModule(dir string) bool {
	for _, sub := range []string{"module", "skills", "agents"} {
		info, err := os.Stat(filepath.Join(dir, sub))
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// loadSkillRepos reads ALCOVE_SKILL_REPOS, clones each repo, and classifies it
// as either a lola module or a Claude Code plugin. Plugins are added to
// skillPluginDirs immediately; lola modules are added to lolaModuleDirs for
// deferred installation (after the project repo is cloned).
func loadSkillRepos() {
	reposJSON := os.Getenv("ALCOVE_SKILL_REPOS")
	if reposJSON == "" {
		return
	}

	var repos []skillRepo
	if err := json.Unmarshal([]byte(reposJSON), &repos); err != nil {
		log.Printf("warning: invalid ALCOVE_SKILL_REPOS JSON: %v", err)
		return
	}

	if len(repos) == 0 {
		return
	}

	baseDir := "/tmp/alcove-skills"
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		log.Printf("warning: failed to create skill repos directory: %v", err)
		return
	}

	for _, repo := range repos {
		if repo.URL == "" {
			log.Printf("warning: skipping skill repo with empty URL")
			continue
		}

		// Determine directory name: use Name if provided, otherwise derive from URL
		dirName := repo.Name
		if dirName == "" {
			dirName = filepath.Base(repo.URL)
			// Strip .git suffix if present
			if ext := filepath.Ext(dirName); ext == ".git" {
				dirName = dirName[:len(dirName)-len(ext)]
			}
		}

		cloneDir := filepath.Join(baseDir, dirName)

		args := []string{"clone", "--depth=1"}
		if repo.Ref != "" {
			args = append(args, "--branch", repo.Ref)
		}
		args = append(args, repo.URL, cloneDir)

		cmd := exec.Command("git", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			log.Printf("warning: failed to clone skill repo %s: %v", repo.URL, err)
			continue
		}

		log.Printf("cloned skill repo %s to %s", repo.URL, cloneDir)

		// Classify as lola module or Claude Code plugin
		if isLolaModule(cloneDir) {
			// Remove mcps.json to prevent lola from injecting MCP configs.
			// Alcove uses Gate for all external API access.
			os.Remove(filepath.Join(cloneDir, "module", "mcps.json"))

			lolaModuleDirs = append(lolaModuleDirs, cloneDir)
			log.Printf("detected lola module: %s", dirName)
		} else {
			skillPluginDirs = append(skillPluginDirs, cloneDir)
			log.Printf("loaded plugin: %s", dirName)
		}
	}

	if len(skillPluginDirs) > 0 {
		log.Printf("loaded %d plugin(s)", len(skillPluginDirs))
	}
	if len(lolaModuleDirs) > 0 {
		log.Printf("detected %d lola module(s) (will install after repo clone)", len(lolaModuleDirs))
	}
}

// installLolaModules runs "lola mod add" and "lola install" for each detected
// lola module. This must be called after cloneRepo so that the current working
// directory is the project directory where Claude Code will run.
func installLolaModules() {
	if len(lolaModuleDirs) == 0 {
		return
	}

	for _, dir := range lolaModuleDirs {
		name := filepath.Base(dir)

		// Register the module from the local path
		addCmd := exec.Command("lola", "mod", "add", dir)
		addCmd.Stdout = os.Stdout
		addCmd.Stderr = os.Stderr
		if err := addCmd.Run(); err != nil {
			log.Printf("warning: failed to register lola module %s: %v", name, err)
			continue
		}

		// Install targeting claude-code
		installCmd := exec.Command("lola", "install", name, "-a", "claude-code")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			log.Printf("warning: failed to install lola module %s: %v", name, err)
			continue
		}

		log.Printf("loaded lola module: %s", name)
	}

	log.Printf("installed %d lola module(s)", len(lolaModuleDirs))
}

// installPlugins reads ALCOVE_PLUGINS and installs each plugin.
// Marketplace plugins use "claude plugin install <name>".
// Git-sourced plugins are cloned and loaded via --plugin-dir.
func installPlugins() {
	pluginsJSON := os.Getenv("ALCOVE_PLUGINS")
	if pluginsJSON == "" {
		return
	}

	type pluginSpec struct {
		Name   string `json:"name"`
		Source string `json:"source,omitempty"`
		Ref    string `json:"ref,omitempty"`
	}

	var plugins []pluginSpec
	if err := json.Unmarshal([]byte(pluginsJSON), &plugins); err != nil {
		log.Printf("warning: invalid ALCOVE_PLUGINS JSON: %v", err)
		return
	}

	for _, p := range plugins {
		if p.Name == "" {
			continue
		}

		switch {
		case p.Source == "" || p.Source == "marketplace":
			// Install from Claude Code marketplace.
			log.Printf("installing plugin from marketplace: %s", p.Name)
			cmd := exec.Command("claude", "plugin", "install", p.Name)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				log.Printf("warning: failed to install marketplace plugin %s: %v", p.Name, err)
			}

		case p.Source == "claude-plugins-official":
			// Install from the official Anthropic plugin repo.
			log.Printf("installing official plugin: %s", p.Name)
			cmd := exec.Command("claude", "plugin", "install", p.Name+"@claude-plugins-official")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				log.Printf("warning: failed to install official plugin %s: %v", p.Name, err)
			}

		default:
			// Git URL source -- clone and use as --plugin-dir.
			log.Printf("cloning plugin from git: %s (%s)", p.Name, p.Source)
			cloneDir := filepath.Join("/tmp/alcove-plugins", p.Name)
			args := []string{"clone", "--depth=1"}
			if p.Ref != "" {
				args = append(args, "--branch", p.Ref)
			}
			args = append(args, p.Source, cloneDir)

			cmd := exec.Command("git", args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				log.Printf("warning: failed to clone plugin %s: %v", p.Name, err)
				continue
			}

			// Add to plugin dirs for --plugin-dir flag.
			skillPluginDirs = append(skillPluginDirs, cloneDir)
			log.Printf("loaded git plugin: %s from %s", p.Name, p.Source)
		}
	}
}

// cloneRepoToDir performs a shallow clone of the given repo into the specified directory.
func cloneRepoToDir(repo, ref, targetDir string) error {
	if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
		return fmt.Errorf("creating parent dir for %s: %w", targetDir, err)
	}

	// Mark the target directory as safe to avoid "dubious ownership" errors when the
	// directory is owned by a different UID (e.g., root-created in the image).
	safeDir := exec.Command("git", "config", "--global", "--add", "safe.directory", targetDir)
	safeDir.Run()

	args := []string{"clone", "--depth=1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, repo, targetDir)

	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone %s: %w", repo, err)
	}

	return nil
}

// repoNameFromURL derives a short repo name from a URL by taking the
// basename and stripping any ".git" suffix.
func repoNameFromURL(rawURL string) string {
	base := filepath.Base(strings.TrimRight(rawURL, "/"))
	return strings.TrimSuffix(base, ".git")
}

// injectClaudeMD reads CLAUDE.md from cloned repos and appends it to the prompt.
// Project instructions go at the end so agents read their actual task first.
// For single-repo clones, it reads /workspace/CLAUDE.md.
// For multi-repo clones, it reads /workspace/<name>/CLAUDE.md from each repo.
func injectClaudeMD(repos []internal.RepoSpec, prompt string) string {
	var claudeMDs []string

	if len(repos) == 0 {
		return prompt
	}

	if len(repos) == 1 {
		content, err := os.ReadFile("/workspace/CLAUDE.md")
		if err == nil && len(content) > 0 {
			log.Printf("injected CLAUDE.md from /workspace/CLAUDE.md (%d bytes)", len(content))
			claudeMDs = append(claudeMDs, string(content))
		}
	} else {
		for _, r := range repos {
			dir := r.Name
			if dir == "" {
				dir = repoNameFromURL(r.URL)
			}
			path := filepath.Join("/workspace", dir, "CLAUDE.md")
			content, err := os.ReadFile(path)
			if err == nil && len(content) > 0 {
				log.Printf("injected CLAUDE.md from %s (%d bytes)", path, len(content))
				claudeMDs = append(claudeMDs, string(content))
			}
		}
	}

	if len(claudeMDs) == 0 {
		return prompt
	}

	return prompt + "\n\n---\n\n" + strings.Join(claudeMDs, "\n\n---\n\n")
}


// setupCATrust installs the ephemeral CA certificate into the trust store so that
// tools like gh, curl, git, Python requests, and Node.js fetch all trust Gate's
// MITM certificates. The CA cert PEM is passed via ALCOVE_CA_CERT_PEM (base64-encoded).
func setupCATrust() error {
	caCertB64 := os.Getenv("ALCOVE_CA_CERT_PEM")
	if caCertB64 == "" {
		return nil // No CA cert, MITM not enabled
	}

	caCertPEM, err := base64.StdEncoding.DecodeString(caCertB64)
	if err != nil {
		return fmt.Errorf("decoding ALCOVE_CA_CERT_PEM: %w", err)
	}

	// Create the TLS directory
	tlsDir := "/etc/alcove-tls"
	if err := os.MkdirAll(tlsDir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", tlsDir, err)
	}

	// Write the CA cert
	caPath := filepath.Join(tlsDir, "ca.pem")
	if err := os.WriteFile(caPath, caCertPEM, 0644); err != nil {
		return fmt.Errorf("writing CA cert: %w", err)
	}

	// Create a combined bundle: system CAs + ephemeral CA
	// UBI9/RHEL system CA bundle location
	systemBundle := "/etc/pki/tls/certs/ca-bundle.crt"
	bundlePath := filepath.Join(tlsDir, "ca-bundle.pem")

	systemCAs, err := os.ReadFile(systemBundle)
	if err != nil {
		// Fallback: try the Debian/Ubuntu location
		systemCAs, err = os.ReadFile("/etc/ssl/certs/ca-certificates.crt")
		if err != nil {
			// If no system bundle found, just use the CA cert alone
			systemCAs = nil
		}
	}

	bundle := append(systemCAs, '\n')
	bundle = append(bundle, caCertPEM...)
	if err := os.WriteFile(bundlePath, bundle, 0644); err != nil {
		return fmt.Errorf("writing CA bundle: %w", err)
	}

	// Set trust store env vars for all common tools
	os.Setenv("SSL_CERT_FILE", bundlePath)
	os.Setenv("NODE_EXTRA_CA_CERTS", caPath)
	os.Setenv("CURL_CA_BUNDLE", bundlePath)
	os.Setenv("GIT_SSL_CAINFO", bundlePath)
	os.Setenv("REQUESTS_CA_BUNDLE", bundlePath)

	log.Printf("installed ephemeral CA trust (%d bytes) at %s", len(caCertPEM), tlsDir)

	return nil
}

// requireEnv returns the value of an environment variable or exits fatally.
func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

// envOrDefault returns the environment variable value or a default.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// setEnvIfMissing sets an environment variable only if it is not already set.
func setEnvIfMissing(key, value string) {
	if os.Getenv(key) == "" {
		os.Setenv(key, value)
	}
}

// parseDuration parses a duration string, returning the default on failure.
func parseDuration(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}

// truncate shortens a string to n characters.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func init() {
	// As PID 1, we need to reap zombie children to prevent zombie accumulation.
	//
	// Race scenario (see primaryReapedStatus above):
	// Wait4(-1, WNOHANG) reaps ANY available child, including the primary child,
	// before the PID check below. Two sub-cases:
	//
	// (a) primaryChildPID is already set: pid matches, we capture the WaitStatus
	//     into primaryReapedStatus, and recoverExitCode() restores the real exit code
	//     when cmd.Wait() returns ECHILD.
	//
	// (b) primaryChildPID is still 0 (fast-exit race): pid != 0 so the match fails;
	//     recoverExitCode() falls back to the eventCount > 0 safety net in this case.
	sigCh := make(chan os.Signal, 16)
	signal.Notify(sigCh, syscall.SIGCHLD)
	go func() {
		for range sigCh {
			for {
				var status syscall.WaitStatus
				pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
				if pid <= 0 || err != nil {
					break
				}
				// If we reaped the primary child, capture its exit status so that
				// recoverExitCode() can recover the real exit code when cmd.Wait()
				// returns ECHILD (sub-case a above).
				if pid == int(primaryChildPID.Load()) {
					primaryReapedStatus.Store(packReapedStatus(status))
					log.Printf("WARNING: SIGCHLD handler reaped primary child pid=%d status=0x%x", pid, uint32(status))
					continue
				}
			}
		}
	}()
}
