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

package gate

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"

	"github.com/alcove-ai/alcove/internal"
)

func TestIsSumoHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"api.sumologic.com", true},
		{"api.us2.sumologic.com", true},
		{"API.EU.SUMOLOGIC.COM", true},
		{"sumologic.com", true},
		{"sumologic.com.evil.example", false},
		{"notsumologic.com", false},
		{"logs.example.com", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := IsSumoHost(tt.host); got != tt.want {
				t.Errorf("IsSumoHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestMapSumoOperation(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{"POST", "api/v1/search/jobs", "search"},
		{"GET", "api/v1/search/jobs/ABC123", "search"},
		{"DELETE", "api/v1/search/jobs/ABC123", "search"},
		{"GET", "api/v1/search/jobs/ABC123/messages", "read_results"},
		{"GET", "api/v1/search/jobs/ABC123/records", "read_results"},
		{"POST", "api/v1/search/jobs/ABC123/messages", "write"},
		{"PUT", "api/v1/search/jobs/ABC123", "write"},
		{"GET", "api/v2/content/folders/personal", "read_content"},
		{"GET", "api/v2/content/0000000000ABC123", "read_content"},
		{"POST", "api/v2/content/folders", "write"},
		{"GET", "api/v1/monitors", "read_monitors"},
		{"GET", "api/v1/monitors/root", "read_monitors"},
		{"DELETE", "api/v1/monitors/0000000000ABC123", "write"},
		{"GET", "api/v1/collectors", "read"},
		{"HEAD", "api/v1/collectors", "read"},
		{"POST", "api/v1/collectors", "write"},
		{"GET", "api/v1/search/jobs/ABC123/", "search"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			if got := mapSumoOperation(tt.method, tt.path); got != tt.want {
				t.Errorf("mapSumoOperation(%q, %q) = %q, want %q", tt.method, tt.path, got, tt.want)
			}
		})
	}
}

func TestCheckAccess_Sumo(t *testing.T) {
	scope := internal.Scope{
		Services: map[string]internal.ServiceScope{
			"sumo": {Operations: []string{"search", "read_results"}},
		},
	}

	tests := []struct {
		name    string
		method  string
		url     string
		allowed bool
		op      string
	}{
		{"create search job", "POST", "https://api.sumologic.com/api/v1/search/jobs", true, "search"},
		{"read messages on regional deployment", "GET", "https://api.us2.sumologic.com/api/v1/search/jobs/X/messages", true, "read_results"},
		{"read monitors not in scope", "GET", "https://api.sumologic.com/api/v1/monitors", false, "read_monitors"},
		{"write denied", "POST", "https://api.sumologic.com/api/v1/collectors", false, "write"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := CheckAccess(tt.method, tt.url, scope)
			if res.Allowed != tt.allowed {
				t.Errorf("Allowed = %v, want %v (reason %q)", res.Allowed, tt.allowed, res.Reason)
			}
			if res.Service != "sumo" {
				t.Errorf("Service = %q, want %q", res.Service, "sumo")
			}
			if res.Operation != tt.op {
				t.Errorf("Operation = %q, want %q", res.Operation, tt.op)
			}
		})
	}
}

func TestCheckAccess_SumoNotInScope(t *testing.T) {
	scope := internal.Scope{Services: map[string]internal.ServiceScope{}}
	res := CheckAccess("POST", "https://api.sumologic.com/api/v1/search/jobs", scope)
	if res.Allowed {
		t.Fatal("expected Sumo request to be denied when sumo is not in scope")
	}
	if res.Service != "sumo" {
		t.Errorf("Service = %q, want %q", res.Service, "sumo")
	}
}

func TestCheckServiceAccess_Sumo(t *testing.T) {
	scope := internal.Scope{
		Services: map[string]internal.ServiceScope{
			"sumo": {Operations: []string{"search"}},
		},
	}
	if res := CheckServiceAccess("POST", "/api/v1/search/jobs", "sumo", scope); !res.Allowed {
		t.Errorf("expected search to be allowed, got reason %q", res.Reason)
	}
	if res := CheckServiceAccess("GET", "/api/v1/search/jobs/X/records", "sumo", scope); res.Allowed {
		t.Error("expected read_results to be denied when only search is in scope")
	}
}

func TestMITMHandler_Sumo(t *testing.T) {
	certPEM, keyPEM := mustGenerateTestCA(t)
	cfg := &Config{
		Scope: internal.Scope{
			Services: map[string]internal.ServiceScope{
				"sumo": {Operations: []string{"*"}},
			},
		},
		Credentials: map[string]string{
			"sumo": "suACCESSID:sumo-access-key",
		},
		ToolConfigs: map[string]ToolConfig{
			"sumo": {APIHost: "api.us2.sumologic.com", AuthHeader: "Authorization", AuthFormat: "basic"},
		},
	}
	m, err := NewMITMHandler(certPEM, keyPEM, cfg)
	if err != nil {
		t.Fatalf("creating MITM handler: %v", err)
	}

	for _, host := range []string{"api.us2.sumologic.com", "api.sumologic.com", "api.eu.sumologic.com"} {
		if !m.IsMITMDomain(host) {
			t.Errorf("IsMITMDomain(%q) = false, want true", host)
		}
	}
	if m.IsMITMDomain("logs.example.com") {
		t.Error("IsMITMDomain(logs.example.com) = true, want false")
	}

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("suACCESSID:sumo-access-key"))
	// Both the configured host (ToolConfig path) and another Sumo deployment
	// (hostname fallback path) must receive the real Basic credential.
	for _, host := range []string{"api.us2.sumologic.com", "api.sumologic.com"} {
		req := httptest.NewRequest("POST", "https://"+host+"/api/v1/search/jobs", nil)
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("alcove-session-x:alcove-session-x")))
		if err := m.injectMITMCredential(req, host); err != nil {
			t.Fatalf("injectMITMCredential(%q): %v", host, err)
		}
		if got := req.Header.Get("Authorization"); got != want {
			t.Errorf("%s: Authorization = %q, want %q", host, got, want)
		}
	}
}
