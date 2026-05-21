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

package auth

import (
	"testing"
)

func TestNewOpenShiftOAuthStore(t *testing.T) {
	store := NewOpenShiftOAuthStore(nil)
	if store == nil {
		t.Error("NewOpenShiftOAuthStore returned nil")
	}
	if store.db != nil {
		t.Error("Expected nil db when passed nil")
	}
}

func TestOpenShiftOAuthStore_AuthNotSupported(t *testing.T) {
	store := NewOpenShiftOAuthStore(nil)

	// These methods should not be supported
	_, err := store.Authenticate("user", "password")
	if err == nil {
		t.Errorf("Authenticate should not be supported")
	}

	_, err = store.ValidateCredentials("user", "password")
	if err == nil {
		t.Errorf("ValidateCredentials should not be supported")
	}

	_, ok := store.ValidateToken("token")
	if ok {
		t.Errorf("ValidateToken should not validate any tokens")
	}

	// InvalidateToken should be a no-op
	store.InvalidateToken("token")
}