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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// k8sSecretPrefix marks an agent credential value that references a key in a
// Kubernetes Secret mounted into the Bridge pod: "k8s-secret:<name>/<key>".
const k8sSecretPrefix = "k8s-secret:"

var (
	// dns1123SubdomainRe matches valid Kubernetes Secret names.
	dns1123SubdomainRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
	// secretKeyRe matches valid Kubernetes Secret data keys.
	secretKeyRe = regexp.MustCompile(`^[-._a-zA-Z0-9]+$`)
)

// validK8sSecretName reports whether name is a valid Kubernetes Secret name.
func validK8sSecretName(name string) bool {
	return len(name) <= 253 && dns1123SubdomainRe.MatchString(name)
}

// validK8sSecretKey reports whether key is a valid Kubernetes Secret data key
// that is also safe to join onto a filesystem path.
func validK8sSecretKey(key string) bool {
	return len(key) <= 253 && secretKeyRe.MatchString(key) && key != "." && key != ".."
}

// ParseK8sSecretRef parses a "k8s-secret:<name>/<key>" credential reference.
// ok is false when value does not use the k8s-secret prefix (the caller should
// fall back to the credential store). err is non-nil when the prefix is present
// but the reference is malformed.
func ParseK8sSecretRef(value string) (name, key string, ok bool, err error) {
	rest, found := strings.CutPrefix(value, k8sSecretPrefix)
	if !found {
		return "", "", false, nil
	}
	name, key, found = strings.Cut(rest, "/")
	if !found || name == "" || key == "" {
		return "", "", true, fmt.Errorf("invalid credential reference %q: expected %s<secret-name>/<key>", value, k8sSecretPrefix)
	}
	if !validK8sSecretName(name) {
		return "", "", true, fmt.Errorf("invalid credential reference %q: %q is not a valid Secret name", value, name)
	}
	if !validK8sSecretKey(key) {
		return "", "", true, fmt.Errorf("invalid credential reference %q: %q is not a valid Secret key", value, key)
	}
	return name, key, true, nil
}

// validate checks the k8s_secrets configuration.
func (c K8sSecretsConfig) validate() error {
	for name, policy := range c.Secrets {
		if !validK8sSecretName(name) {
			return fmt.Errorf("k8s_secrets: %q is not a valid Secret name", name)
		}
		if len(policy.AllowedTeams) == 0 {
			return fmt.Errorf("k8s_secrets: secret %q must list at least one team in allowed_teams", name)
		}
		for _, team := range policy.AllowedTeams {
			if strings.TrimSpace(team) == "" {
				return fmt.Errorf("k8s_secrets: secret %q has an empty allowed_teams entry", name)
			}
		}
	}
	return nil
}

// K8sSecretResolver reads credential values from Kubernetes Secrets mounted
// into the Bridge pod at <root>/<secret-name>/<key>. Only Secrets listed in the
// configuration are reachable, and only by the teams listed for each Secret.
// Values are read from disk on every call so rotated Secrets take effect on the
// next dispatch without restarting Bridge.
type K8sSecretResolver struct {
	root     string
	policies map[string]K8sSecretPolicy
}

// NewK8sSecretResolver builds a resolver from the k8s_secrets configuration.
func NewK8sSecretResolver(cfg K8sSecretsConfig) *K8sSecretResolver {
	root := cfg.Root
	if root == "" {
		root = DefaultK8sSecretsRoot
	}
	return &K8sSecretResolver{root: root, policies: cfg.Secrets}
}

// Resolve returns the value of key in the mounted Secret name, provided the
// Secret is configured and teamID is allowed to use it. Errors never include
// the Secret value.
func (r *K8sSecretResolver) Resolve(teamID, name, key string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("secret %q is not configured in k8s_secrets", name)
	}
	policy, ok := r.policies[name]
	if !ok {
		return "", fmt.Errorf("secret %q is not configured in k8s_secrets", name)
	}
	if teamID == "" || !slices.Contains(policy.AllowedTeams, teamID) {
		return "", fmt.Errorf("team is not allowed to use secret %q", name)
	}
	if !validK8sSecretName(name) {
		return "", fmt.Errorf("%q is not a valid Secret name", name)
	}
	if !validK8sSecretKey(key) {
		return "", fmt.Errorf("%q is not a valid Secret key", key)
	}
	data, err := os.ReadFile(filepath.Join(r.root, name, key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("key %q not found in secret %q (is the Secret mounted under %s?)", key, name, r.root)
		}
		return "", fmt.Errorf("reading key %q of secret %q: %w", key, name, err)
	}
	return string(data), nil
}
