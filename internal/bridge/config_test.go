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
	"testing"
)

func TestParseConfigFile_NoMCPSection(t *testing.T) {
	yaml := `
database_encryption_key: test-key
nats_url: nats://localhost:4222
`
	dir := t.TempDir()
	path := filepath.Join(dir, "alcove.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := &Config{}
	if err := cfg.parseConfigFile(path); err != nil {
		t.Fatalf("parseConfigFile: %v", err)
	}
	if cfg.MCPServers != nil {
		t.Errorf("expected MCPServers to be nil when mcp section is absent, got %v", cfg.MCPServers)
	}
}

func TestParseConfigFile_EmptyMCPSection(t *testing.T) {
	yaml := `
database_encryption_key: test-key
mcp:
  servers: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "alcove.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := &Config{}
	if err := cfg.parseConfigFile(path); err != nil {
		t.Fatalf("parseConfigFile: %v", err)
	}
	// An empty map in YAML results in a non-nil but empty map; len should be 0.
	if len(cfg.MCPServers) != 0 {
		t.Errorf("expected 0 MCP servers for empty section, got %d", len(cfg.MCPServers))
	}
}

func TestParseConfigFile_ValidMCPServer(t *testing.T) {
	yaml := `
database_encryption_key: test-key
mcp:
  servers:
    browser-mcp:
      image: quay.io/alcove/browser-mcp:latest
      allowed_versions:
        - "1.0"
        - "1.1"
      allowed_plugins:
        browser:
          tools:
            - click
            - navigate
      resource_limits:
        cpu_request: "100m"
        memory_request: "128Mi"
        cpu_limit: "500m"
        memory_limit: "512Mi"
      env:
        LOG_LEVEL: debug
`
	dir := t.TempDir()
	path := filepath.Join(dir, "alcove.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := &Config{}
	if err := cfg.parseConfigFile(path); err != nil {
		t.Fatalf("parseConfigFile: %v", err)
	}

	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(cfg.MCPServers))
	}

	server, ok := cfg.MCPServers["browser-mcp"]
	if !ok {
		t.Fatal("expected 'browser-mcp' server to be present")
	}
	if server.Image != "quay.io/alcove/browser-mcp:latest" {
		t.Errorf("Image: got %q, want %q", server.Image, "quay.io/alcove/browser-mcp:latest")
	}
	if len(server.AllowedVersions) != 2 {
		t.Errorf("AllowedVersions length: got %d, want 2", len(server.AllowedVersions))
	}
	if server.ResourceLimits.CPURequest != "100m" {
		t.Errorf("CPURequest: got %q, want %q", server.ResourceLimits.CPURequest, "100m")
	}
	if server.ResourceLimits.MemoryLimit != "512Mi" {
		t.Errorf("MemoryLimit: got %q, want %q", server.ResourceLimits.MemoryLimit, "512Mi")
	}

	plugin, ok := server.AllowedPlugins["browser"]
	if !ok {
		t.Fatal("expected 'browser' plugin in AllowedPlugins")
	}
	if len(plugin.Tools) != 2 {
		t.Fatalf("plugin.Tools length: got %d, want 2", len(plugin.Tools))
	}
	if plugin.Tools[0] != "click" || plugin.Tools[1] != "navigate" {
		t.Errorf("plugin.Tools: got %v, want [click navigate]", plugin.Tools)
	}

	if server.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("Env[LOG_LEVEL]: got %q, want %q", server.Env["LOG_LEVEL"], "debug")
	}
}

func TestParseConfigFile_MultipleMCPServers(t *testing.T) {
	yaml := `
database_encryption_key: test-key
mcp:
  servers:
    server-a:
      image: quay.io/alcove/server-a:v1
    server-b:
      image: quay.io/alcove/server-b:v2
      allowed_versions:
        - "2.0"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "alcove.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := &Config{}
	if err := cfg.parseConfigFile(path); err != nil {
		t.Fatalf("parseConfigFile: %v", err)
	}

	if len(cfg.MCPServers) != 2 {
		t.Fatalf("expected 2 MCP servers, got %d", len(cfg.MCPServers))
	}
	if _, ok := cfg.MCPServers["server-a"]; !ok {
		t.Error("expected 'server-a' in MCPServers")
	}
	if _, ok := cfg.MCPServers["server-b"]; !ok {
		t.Error("expected 'server-b' in MCPServers")
	}
}
