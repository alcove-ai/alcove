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

	"github.com/alcove-ai/alcove/internal"
)

// TestResolveEffectiveLLMDefaultModel guards the fallback model used when no
// model is configured. A stale value here 404s at dispatch on providers that
// have retired the snapshot (the failure this const centralization fixes), so
// pin it to the shared constant. A unit test only guards against accidental
// drift of the literal — it cannot verify the model resolves at the provider
// endpoint; that requires a live preflight check (tracked separately).
func TestResolveEffectiveLLMDefaultModel(t *testing.T) {
	eff := ResolveEffectiveLLM(&Config{})
	if eff.Model != internal.DefaultModel {
		t.Errorf("default Model: got %q, want %q", eff.Model, internal.DefaultModel)
	}
	if eff.ModelSrc != "default" {
		t.Errorf("default ModelSrc: got %q, want %q", eff.ModelSrc, "default")
	}
}
