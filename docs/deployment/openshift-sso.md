# OpenShift SSO Deployment Guide

This guide covers deploying Alcove with OpenShift SSO authentication using the 
`openshift-oauth` auth backend and OAuth Proxy sidecar.

## Overview

The OpenShift SSO deployment enables users to authenticate with their existing 
OpenShift credentials without creating separate Alcove accounts. This is achieved 
through an OAuth Proxy sidecar that handles authentication and sets trusted headers 
for the Bridge application.

## Architecture

```
User → OpenShift Route (port 8443) → OAuth Proxy → Bridge (port 8080)
                    ↓
             OpenShift SSO
```

Key components:

- **OAuth Proxy sidecar**: Handles OpenShift SSO authentication on port 8443
- **Bridge application**: Trusts `X-Forwarded-User`/`X-Forwarded-Email` headers
- **OpenShift Route**: Routes external traffic to OAuth Proxy (port 8443 only)
- **TLS certificate**: Auto-provisioned by OpenShift service serving certificate

## Prerequisites

- OpenShift cluster with proper OAuth configuration
- `alcove-config` Secret with database credentials and encryption key
- Optional: `alcove-vertex-ai` Secret for Google Vertex AI credentials

## Deployment Steps

### 1. Prepare Configuration

Create the `alcove-config` Secret:

```bash
oc create secret generic alcove-config \
  --from-literal=ledger-database-url="postgres://user:pass@host:5432/alcove" \
  --from-literal=database-encryption-key="$(openssl rand -base64 32)"
```

### 2. Deploy Using Template

Deploy Alcove with OAuth Proxy:

```bash
# Basic deployment
oc process -f deploy/openshift/template-oauth.yaml \
  -p BRIDGE_IMAGE_TAG=v1.0.0 \
  -p OPENSHIFT_OAUTH_ADMINS="admin-user1,admin-user2" \
  | oc apply -f -

# With custom LLM configuration
oc process -f deploy/openshift/template-oauth.yaml \
  -p BRIDGE_IMAGE_TAG=v1.0.0 \
  -p OPENSHIFT_OAUTH_ADMINS="admin-user1,admin-user2" \
  -p BRIDGE_LLM_PROVIDER="google-vertex" \
  -p BRIDGE_LLM_PROJECT="my-gcp-project" \
  -p BRIDGE_LLM_REGION="us-central1" \
  | oc apply -f -
```

### 3. Template Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `BRIDGE_IMAGE_TAG` | Bridge container image tag | `latest` |
| `OPENSHIFT_OAUTH_ADMINS` | Comma-separated admin usernames | `""` |
| `OAUTH_PROXY_IMAGE` | OAuth Proxy image | `quay.io/openshift/origin-oauth-proxy` |
| `BRIDGE_LLM_PROVIDER` | System LLM provider | `""` |
| `BRIDGE_LLM_PROJECT` | GCP project for Vertex AI | `""` |
| `BRIDGE_LLM_REGION` | GCP region for Vertex AI | `""` |
| `CREATE_ROUTE` | Whether to create Route | `"true"` |

### 4. Verify Deployment

Check that all components are running:

```bash
# Check deployment status
oc get deployment alcove-bridge -o wide

# Check pods
oc get pods -l app.kubernetes.io/name=bridge

# Check services
oc get svc alcove-bridge

# Check route
oc get route alcove-bridge
```

### 5. Access Application

Once deployed, access Alcove through the Route:

```bash
# Get the route URL
ROUTE_URL=$(oc get route alcove-bridge -o jsonpath='{.spec.host}')
echo "Access Alcove at: https://$ROUTE_URL"
```

When you visit the URL, you'll be redirected to OpenShift SSO for authentication.

## Security Considerations

### Header Spoofing Protection

- **CRITICAL**: The Route MUST target port 8443 (OAuth Proxy), never port 8080 (Bridge)
- Bridge only trusts headers when `AUTH_BACKEND=openshift-oauth`
- OAuth Proxy shares pod network namespace with Bridge — port 8080 is not externally accessible

### OAuth Proxy Configuration

- Uses ServiceAccount-based OAuth (no cluster-admin required)
- Cookie secret is automatically generated and stored in Kubernetes Secret
- TLS certificate is auto-provisioned by OpenShift service serving certificate
- Skips OAuth provider selection button for streamlined UX

### Network Policies

The template includes NetworkPolicies that:
- Allow communication between Alcove components
- Allow ingress from OpenShift Routes
- Restrict other network access

## Admin Management

### Bootstrap Admins

Initial admin users are configured via the `OPENSHIFT_OAUTH_ADMINS` parameter:

```bash
oc process ... -p OPENSHIFT_OAUTH_ADMINS="alice,bob,charlie" ...
```

These users will automatically receive admin privileges when they first log in.

### Runtime Admin Management

After deployment, existing admins can promote/demote users through the Alcove dashboard:
1. Log in as an admin user
2. Navigate to Users tab
3. Use the admin toggle to grant or revoke admin privileges

## Troubleshooting

### Authentication Failures

Check OAuth Proxy logs:
```bash
oc logs deployment/alcove-bridge -c oauth-proxy
```

Check Bridge logs for auth errors:
```bash
oc logs deployment/alcove-bridge -c bridge | grep "auth:"
```

### Common Issues

1. **"missing X-Forwarded-User header"**: Route is bypassing OAuth Proxy
   - Verify Route targets port 8443, not 8080
   - Check OAuth Proxy container is running

2. **OAuth redirect loops**: OAuth redirect reference misconfiguration
   - Verify ServiceAccount annotation is correct
   - Ensure Route name matches the annotation

3. **Cookie secret errors**: OAuth Proxy can't read cookie secret
   - Check `alcove-oauth-cookie-secret` Secret exists
   - Verify volume mount in OAuth Proxy container

### Health Checks

Bridge health endpoint (internal only):
```bash
oc exec deployment/alcove-bridge -c bridge -- curl localhost:8080/api/v1/health
```

## Rollback

To rollback to password-based authentication:

1. Switch to the regular template:
   ```bash
   oc process -f deploy/openshift/template.yaml \
     -p AUTH_BACKEND=postgres \
     -p ADMIN_RESET_PASSWORD="admin-password" \
     | oc apply -f -
   ```

2. Update the Route to target port 8080:
   ```bash
   oc patch route alcove-bridge --type='json' \
     -p='[{"op": "replace", "path": "/spec/port/targetPort", "value": 8080}]'
   ```

3. Remove OAuth-specific resources:
   ```bash
   oc delete secret alcove-oauth-cookie-secret
   oc delete secret alcove-bridge-tls
   ```

## Migration Notes

### From RH Identity Backend

If migrating from `rh-identity` to `openshift-oauth`:

- Usernames may differ (RH Identity uses email, OAuth uses OpenShift username)
- Users will need to be re-added to teams if usernames don't match
- Consider mapping strategy for username normalization

### From Postgres Backend

When migrating from `postgres` to `openshift-oauth`:

- Existing password-based users remain in database but cannot log in
- Admin users must be reconfigured via `OPENSHIFT_OAUTH_ADMINS`
- Use `ADMIN_RESET_PASSWORD` during rollback if needed