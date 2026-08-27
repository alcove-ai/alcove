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

func TestParseConfigFile_EmptyMCPSection(t *testing.T) {
	yaml := `
database_encryption_key: test-key
`
	tmpFile := writeTempYAML(t, yaml)

	cfg := &Config{}
	if err := cfg.parseConfigFile(tmpFile); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.MCPServers) != 0 {
		t.Errorf("expected no MCPServers, got %d", len(cfg.MCPServers))
	}
}

func TestParseConfigFile_ValidMCPSection(t *testing.T) {
	yaml := `
database_encryption_key: test-key
mcp:
  servers:
    my-server:
      image: quay.io/myorg/mcp-server:v1.0
      allowed_versions:
        - v1.0
        - v1.1
      env:
        LOG_LEVEL: debug
      resource_limits:
        cpu_request: "100m"
        memory_request: "128Mi"
        cpu_limit: "500m"
        memory_limit: "512Mi"
`
	tmpFile := writeTempYAML(t, yaml)

	cfg := &Config{}
	if err := cfg.parseConfigFile(tmpFile); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 MCPServer, got %d", len(cfg.MCPServers))
	}
	srv, ok := cfg.MCPServers["my-server"]
	if !ok {
		t.Fatal("expected 'my-server' in MCPServers")
	}
	if srv.Image != "quay.io/myorg/mcp-server:v1.0" {
		t.Errorf("Image: got %q, want %q", srv.Image, "quay.io/myorg/mcp-server:v1.0")
	}
	if len(srv.AllowedVersions) != 2 {
		t.Errorf("AllowedVersions: got %d, want 2", len(srv.AllowedVersions))
	}
	if srv.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("Env[LOG_LEVEL]: got %q, want %q", srv.Env["LOG_LEVEL"], "debug")
	}
	if srv.ResourceLimits.CPURequest != "100m" {
		t.Errorf("ResourceLimits.CPURequest: got %q, want %q", srv.ResourceLimits.CPURequest, "100m")
	}
	if srv.ResourceLimits.MemoryLimit != "512Mi" {
		t.Errorf("ResourceLimits.MemoryLimit: got %q, want %q", srv.ResourceLimits.MemoryLimit, "512Mi")
	}
}

func TestParseConfigFile_MCPWithAllowedPlugins(t *testing.T) {
	yaml := `
database_encryption_key: test-key
mcp:
  servers:
    my-server:
      image: quay.io/myorg/mcp-server:latest
      allowed_plugins:
        browser:
          tools:
            - navigate
            - screenshot
        search:
          tools:
            - web_search
`
	tmpFile := writeTempYAML(t, yaml)

	cfg := &Config{}
	if err := cfg.parseConfigFile(tmpFile); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv := cfg.MCPServers["my-server"]
	if len(srv.AllowedPlugins) != 2 {
		t.Fatalf("expected 2 AllowedPlugins, got %d", len(srv.AllowedPlugins))
	}
	browserPlugin, ok := srv.AllowedPlugins["browser"]
	if !ok {
		t.Fatal("expected 'browser' in AllowedPlugins")
	}
	if len(browserPlugin.Tools) != 2 {
		t.Errorf("browser.Tools: got %d, want 2", len(browserPlugin.Tools))
	}
}

// writeTempYAML writes the given YAML content to a temp file and returns its path.
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "alcove.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing temp yaml: %v", err)
	}
	return path
}
