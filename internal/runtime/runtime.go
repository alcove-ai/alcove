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

// Package runtime provides the abstraction layer for running Skiff pods
// on both Kubernetes and Podman.
package runtime

import "context"

// TaskHandle identifies a running Skiff task.
type TaskHandle struct {
	ID        string
	PodName   string
	GatePort  int
}

// MCPResourceLimits configures CPU and memory constraints for an MCP server container.
// Zero-value string fields use the runtime defaults (100m/256Mi requests, 2/4Gi limits).
type MCPResourceLimits struct {
	CPURequest    string // e.g., "100m" — default: "100m"
	MemoryRequest string // e.g., "256Mi" — default: "256Mi"
	CPULimit      string // e.g., "2" — default: "2"
	MemoryLimit   string // e.g., "4Gi" — default: "4Gi"
}

// TaskSpec describes how to run a Skiff pod.
type TaskSpec struct {
	TaskID       string
	Image        string
	GateImage    string
	Env          map[string]string // env vars for skiff container
	GateEnv      map[string]string // env vars for gate sidecar
	Timeout      int64             // seconds
	Network      string            // podman network name (podman only); used as the internal network
	ExternalNet    string            // podman external network for gate egress (podman only)
	Debug          bool              // if true, containers are not auto-removed on exit
	DirectOutbound    bool              // if true, skiff gets both networks and no HTTP(S)_PROXY
	DevContainerImage         string            // Container image for the dev container sidecar
	DevContainerEnv           map[string]string // env vars for dev container (includes SHIM_TOKEN)
	DevContainerNetworkAccess string            // "internal" or "external"; defaults to "internal"

	// MCP server sidecar fields. All gated on MCPServerImage != "".
	// Values must come from operator config (Config.MCPServers); never from agent author YAML.
	MCPServerImage  string            // MCP server container image (from operator config)
	MCPServerEnv    map[string]string // env vars for MCP server container (from operator config)
	MCPServerName   string            // name of the MCP server (for logging/observability)
	MCPFailureMode  string            // "" or "fail" → error; "continue" → warn and start Skiff without MCP
	MCPResourceLimits MCPResourceLimits // CPU/memory limits for MCP server container (optional overrides)
}

// MCPContainerName returns the container name for the MCP server sidecar.
// Example: MCPContainerName("abc123") → "mcp-abc123"
func MCPContainerName(taskID string) string {
	return "mcp-" + taskID
}

// ServiceSpec describes a long-lived infrastructure service (Hail, Ledger).
type ServiceSpec struct {
	Name    string
	Image   string
	Env     map[string]string
	Ports   map[int]int // container port -> host port
	Network string
	Volumes map[string]string // volume name -> mount path
}

// Runtime abstracts over Kubernetes and Podman for managing Alcove components.
type Runtime interface {
	// RunTask creates and starts a Skiff pod (Job on k8s, container on podman).
	RunTask(ctx context.Context, spec TaskSpec) (TaskHandle, error)

	// CancelTask terminates a running Skiff pod.
	CancelTask(ctx context.Context, handle TaskHandle) error

	// TaskStatus returns the current status of a Skiff task.
	TaskStatus(ctx context.Context, handle TaskHandle) (string, error)

	// EnsureService starts a long-lived service if not already running.
	EnsureService(ctx context.Context, spec ServiceSpec) error

	// StopService stops a long-lived service.
	StopService(ctx context.Context, name string) error

	// CreateVolume creates a named volume for persistent storage.
	CreateVolume(ctx context.Context, name string) (string, error)

	// Info returns runtime metadata (type, version, etc.).
	Info(ctx context.Context) (RuntimeInfo, error)

	// CleanupOrphanedContainers finds and removes containers matching the
	// given name prefix (e.g., "gate-") whose corresponding partner container
	// is gone. For "gate-" prefix, it checks whether the matching "skiff-"
	// container still exists. Returns the count of cleaned-up containers.
	CleanupOrphanedContainers(ctx context.Context, prefix string) (int, error)
}

// RuntimeInfo describes the container runtime.
type RuntimeInfo struct {
	Type    string // "kubernetes" or "podman"
	Version string
}
