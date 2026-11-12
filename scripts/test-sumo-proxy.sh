#!/bin/bash
# test-sumo-proxy.sh — Tests for Sumo Logic proxy support.
#
# Verifies Sumo Logic credential CRUD, api_host configuration, security profile
# integration, and backward compatibility with existing providers.
#
# Prerequisites:
#   - Bridge running at BRIDGE_URL (default http://localhost:8080)
#   - AUTH_BACKEND=postgres with PostgreSQL accessible
#   - ADMIN_PASSWORD set in the environment
#
# Usage:
#   ADMIN_PASSWORD=<pw> ./scripts/test-sumo-proxy.sh
#
# Tests:
#   Test 1: Sumo Logic credential creation (provider "sumo", credential "accessId:accessKey")
#   Test 2: Sumo Logic credential with custom api_host (regional deployment)
#   Test 3: Sumo Logic in security profile (search, read_results, read operations)
#   Test 4: Sumo Logic credential deletion
#   Test 5: Backward compatibility (GitHub, GitLab, Jira still work)

set -euo pipefail

BRIDGE_URL="${BRIDGE_URL:-http://localhost:8080}"
PASS=0
FAIL=0

log() { echo ">>> $*"; }
pass() { echo "  PASS: $*"; PASS=$((PASS+1)); }
fail() { echo "  FAIL: $*"; FAIL=$((FAIL+1)); }

# --- Setup ---
log "Setting up..."
ADMIN_TOKEN=$(curl -s -X POST "$BRIDGE_URL/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"admin\",\"password\":\"${ADMIN_PASSWORD}\"}" | python3 -c "import json,sys; d=json.load(sys.stdin); t=d.get('token',''); print(t) if t else sys.exit('Login failed: ' + json.dumps(d))")

# Create test user
curl -s -X POST "$BRIDGE_URL/api/v1/users" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"username":"sumotest","password":"sumotest123","is_admin":false}' > /dev/null 2>&1 || true

USER_TOKEN=$(curl -s -X POST "$BRIDGE_URL/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"sumotest","password":"sumotest123"}' | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('token',''))")

echo "  User token: ${USER_TOKEN:0:10}..."

# =====================================================================
# Test 1: Sumo Logic credential creation
# =====================================================================
log "Test 1: Sumo Logic credential creation"

SUMO_RESULT=$(curl -s -w "\n%{http_code}" -X POST "$BRIDGE_URL/api/v1/credentials" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"test-sumo","provider":"sumo","auth_type":"api_key","credential":"suTESTACCESSID:sumo-test-access-key-abc123"}')

SUMO_CODE=$(echo "$SUMO_RESULT" | tail -1)
SUMO_BODY=$(echo "$SUMO_RESULT" | sed '$d')

if [ "$SUMO_CODE" = "200" ] || [ "$SUMO_CODE" = "201" ]; then
  pass "Sumo Logic credential created (HTTP $SUMO_CODE)"
else
  fail "Sumo Logic credential creation returned HTTP $SUMO_CODE (expected 200 or 201)"
fi

SUMO_ID=$(echo "$SUMO_BODY" | python3 -c "import json,sys; print(json.load(sys.stdin).get('id','ERROR'))")
if [ "$SUMO_ID" != "ERROR" ] && [ -n "$SUMO_ID" ]; then
  pass "Sumo Logic credential has valid ID: $SUMO_ID"
else
  fail "Sumo Logic credential missing ID: $SUMO_BODY"
fi

# Verify credential appears in GET /api/v1/credentials
CRED_LIST=$(curl -s "$BRIDGE_URL/api/v1/credentials" \
  -H "Authorization: Bearer $USER_TOKEN")
HAS_SUMO=$(echo "$CRED_LIST" | python3 -c "
import json,sys
d=json.load(sys.stdin)
creds=d.get('credentials',[])
found=[c for c in creds if c.get('provider')=='sumo' and c.get('name')=='test-sumo']
print('yes' if found else 'no')
")
if [ "$HAS_SUMO" = "yes" ]; then
  pass "Sumo Logic credential appears in credentials list"
else
  fail "Sumo Logic credential not found in credentials list"
fi

# Verify provider field is correct
SUMO_PROVIDER=$(echo "$CRED_LIST" | python3 -c "
import json,sys
d=json.load(sys.stdin)
creds=d.get('credentials',[])
for c in creds:
    if c.get('name')=='test-sumo':
        print(c.get('provider',''))
        break
")
if [ "$SUMO_PROVIDER" = "sumo" ]; then
  pass "Sumo Logic credential has provider='sumo'"
else
  fail "Sumo Logic credential has provider='$SUMO_PROVIDER' (expected 'sumo')"
fi

# =====================================================================
# Test 2: Sumo Logic credential with custom api_host
# =====================================================================
log "Test 2: Sumo Logic credential with custom api_host"

# Delete the first credential to avoid one-LLM-per-user conflicts
# (sumo is not an LLM provider, but let's be safe)
curl -s -X DELETE "$BRIDGE_URL/api/v1/credentials/$SUMO_ID" \
  -H "Authorization: Bearer $USER_TOKEN" > /dev/null 2>&1

SUMO_HOST_RESULT=$(curl -s -w "\n%{http_code}" -X POST "$BRIDGE_URL/api/v1/credentials" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"test-sumo-hosted","provider":"sumo","auth_type":"api_key","credential":"suHOSTEDID:sumo-hosted-access-key-xyz","api_host":"api.us2.sumologic.com"}')

SUMO_HOST_CODE=$(echo "$SUMO_HOST_RESULT" | tail -1)
SUMO_HOST_BODY=$(echo "$SUMO_HOST_RESULT" | sed '$d')

if [ "$SUMO_HOST_CODE" = "200" ] || [ "$SUMO_HOST_CODE" = "201" ]; then
  pass "Sumo Logic credential with api_host created (HTTP $SUMO_HOST_CODE)"
else
  fail "Sumo Logic credential with api_host returned HTTP $SUMO_HOST_CODE (expected 200 or 201)"
fi

SUMO_HOST_ID=$(echo "$SUMO_HOST_BODY" | python3 -c "import json,sys; print(json.load(sys.stdin).get('id','ERROR'))")

# Verify api_host is stored correctly
if [ "$SUMO_HOST_ID" != "ERROR" ] && [ -n "$SUMO_HOST_ID" ]; then
  CRED_DETAIL=$(curl -s "$BRIDGE_URL/api/v1/credentials/$SUMO_HOST_ID" \
    -H "Authorization: Bearer $USER_TOKEN")
  STORED_HOST=$(echo "$CRED_DETAIL" | python3 -c "import json,sys; print(json.load(sys.stdin).get('api_host',''))")
  if [ "$STORED_HOST" = "api.us2.sumologic.com" ]; then
    pass "Sumo Logic credential api_host stored correctly"
  else
    fail "Sumo Logic credential api_host is '$STORED_HOST' (expected 'api.us2.sumologic.com')"
  fi
else
  fail "Sumo Logic credential with api_host missing ID"
fi

# =====================================================================
# Test 3: Security profile creation blocked (YAML-only policy)
# =====================================================================
log "Test 3: Security profile creation returns 405 (YAML-only)"

PROFILE_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BRIDGE_URL/api/v1/security-profiles" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-sumo-profile",
    "description": "Security profile with Sumo Logic access for testing",
    "tools": {
      "sumo": {
        "rules": [{
          "repos": ["*"],
          "operations": ["search", "read_results", "read"]
        }]
      }
    }
  }')

if [ "$PROFILE_CODE" = "405" ]; then
  pass "POST /api/v1/security-profiles returns 405 Method Not Allowed"
else
  fail "POST /api/v1/security-profiles returned HTTP $PROFILE_CODE (expected 405)"
fi

# =====================================================================
# Test 4: Sumo Logic credential deletion
# =====================================================================
log "Test 4: Sumo Logic credential deletion"

DEL_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE \
  "$BRIDGE_URL/api/v1/credentials/$SUMO_HOST_ID" \
  -H "Authorization: Bearer $USER_TOKEN")
if [ "$DEL_CODE" = "200" ]; then
  pass "Sumo Logic credential deleted (HTTP 200)"
else
  fail "Sumo Logic credential delete returned HTTP $DEL_CODE (expected 200)"
fi

# Verify it's gone
GONE_CHECK=$(curl -s -o /dev/null -w "%{http_code}" \
  "$BRIDGE_URL/api/v1/credentials/$SUMO_HOST_ID" \
  -H "Authorization: Bearer $USER_TOKEN")
if [ "$GONE_CHECK" = "404" ]; then
  pass "Deleted Sumo Logic credential returns 404"
else
  fail "Deleted Sumo Logic credential returns HTTP $GONE_CHECK (expected 404)"
fi

# Verify it's not in the list
CRED_LIST_AFTER=$(curl -s "$BRIDGE_URL/api/v1/credentials" \
  -H "Authorization: Bearer $USER_TOKEN")
SUMO_GONE=$(echo "$CRED_LIST_AFTER" | python3 -c "
import json,sys
d=json.load(sys.stdin)
creds=d.get('credentials',[])
found=[c for c in creds if c.get('provider')=='sumo']
print('yes' if not found else 'no')
")
if [ "$SUMO_GONE" = "yes" ]; then
  pass "No Sumo Logic credentials remain in list after deletion"
else
  fail "Sumo Logic credential still appears in list after deletion"
fi

# =====================================================================
# Test 5: Backward compatibility
# =====================================================================
log "Test 5: Backward compatibility"

# 5a: GitHub credential still works
GH_RESULT=$(curl -s -w "\n%{http_code}" -X POST "$BRIDGE_URL/api/v1/credentials" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"compat-github","provider":"github","auth_type":"api_key","credential":"ghp-test-token-compat"}')
GH_CODE=$(echo "$GH_RESULT" | tail -1)
GH_BODY=$(echo "$GH_RESULT" | sed '$d')
GH_ID=$(echo "$GH_BODY" | python3 -c "import json,sys; print(json.load(sys.stdin).get('id','ERROR'))")
if [ "$GH_CODE" = "200" ] || [ "$GH_CODE" = "201" ]; then
  pass "GitHub credential creation still works (HTTP $GH_CODE)"
else
  fail "GitHub credential creation returned HTTP $GH_CODE"
fi

# 5b: GitLab credential still works
GL_RESULT=$(curl -s -w "\n%{http_code}" -X POST "$BRIDGE_URL/api/v1/credentials" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"compat-gitlab","provider":"gitlab","auth_type":"api_key","credential":"glpat-test-token-compat"}')
GL_CODE=$(echo "$GL_RESULT" | tail -1)
GL_BODY=$(echo "$GL_RESULT" | sed '$d')
GL_ID=$(echo "$GL_BODY" | python3 -c "import json,sys; print(json.load(sys.stdin).get('id','ERROR'))")
if [ "$GL_CODE" = "200" ] || [ "$GL_CODE" = "201" ]; then
  pass "GitLab credential creation still works (HTTP $GL_CODE)"
else
  fail "GitLab credential creation returned HTTP $GL_CODE"
fi

# 5c: Jira credential still works
JIRA_RESULT=$(curl -s -w "\n%{http_code}" -X POST "$BRIDGE_URL/api/v1/credentials" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"compat-jira","provider":"jira","auth_type":"api_key","credential":"jira-test-token-compat"}')
JIRA_CODE=$(echo "$JIRA_RESULT" | tail -1)
JIRA_BODY=$(echo "$JIRA_RESULT" | sed '$d')
JIRA_ID=$(echo "$JIRA_BODY" | python3 -c "import json,sys; print(json.load(sys.stdin).get('id','ERROR'))")
if [ "$JIRA_CODE" = "200" ] || [ "$JIRA_CODE" = "201" ]; then
  pass "Jira credential creation still works (HTTP $JIRA_CODE)"
else
  fail "Jira credential creation returned HTTP $JIRA_CODE"
fi

# 5d: Credentials list endpoint returns 200
LIST_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BRIDGE_URL/api/v1/credentials" \
  -H "Authorization: Bearer $USER_TOKEN")
if [ "$LIST_CODE" = "200" ]; then
  pass "GET /api/v1/credentials returns 200"
else
  fail "GET /api/v1/credentials returns HTTP $LIST_CODE (expected 200)"
fi

# 5e: Security profiles endpoint returns 200
PROFILES_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BRIDGE_URL/api/v1/security-profiles" \
  -H "Authorization: Bearer $USER_TOKEN")
if [ "$PROFILES_CODE" = "200" ]; then
  pass "GET /api/v1/security-profiles returns 200"
else
  fail "GET /api/v1/security-profiles returns HTTP $PROFILES_CODE (expected 200)"
fi

# --- Cleanup ---
log "Cleaning up..."
curl -s -X DELETE "$BRIDGE_URL/api/v1/credentials/$GH_ID" \
  -H "Authorization: Bearer $USER_TOKEN" > /dev/null 2>&1 || true
curl -s -X DELETE "$BRIDGE_URL/api/v1/credentials/$GL_ID" \
  -H "Authorization: Bearer $USER_TOKEN" > /dev/null 2>&1 || true
curl -s -X DELETE "$BRIDGE_URL/api/v1/credentials/$JIRA_ID" \
  -H "Authorization: Bearer $USER_TOKEN" > /dev/null 2>&1 || true

# --- Summary ---
echo ""
log "=== Test Summary ==="
echo "  Total: $((PASS+FAIL))  Passed: $PASS  Failed: $FAIL"
if [ "$FAIL" -gt 0 ]; then exit 1; else echo "  All tests passed."; fi
