# Authentication and Workspace Security Model

## Status

This document defines the security contract for authentication and workspace permissions. S1 (the contract) and S2 (single-principal bearer authentication and token lifecycle) are implemented. Workspace ownership and per-workspace authorization are not yet implemented, so Termind must not be treated as ready for remote or multi-user use.

## Deployment modes

- **Local mode:** unauthenticated access is allowed only when explicitly enabled and the API listens on loopback. It is intended for one trusted local user. It is not a multi-user security boundary.
- **Authenticated mode:** every non-health application endpoint requires a valid bearer token. The API must fail startup if authenticated mode has no usable token-signing/storage configuration, and it must not bind publicly in unauthenticated mode.
- CORS is a browser-origin control, not authentication. It must never be treated as proof of identity.

## Identity and credentials

- A user is the authenticated principal. Request payloads cannot select or override that principal.
- CLI and initial API clients use opaque, randomly generated bearer tokens. Only a cryptographic hash of a token is stored. Plaintext is returned once at creation, is not logged, and is revocable.
- Tokens have an owner, a human-readable label, creation and last-used timestamps, and a revocation timestamp. Expiry is not part of the initial token API.
- S2 currently has one configured bootstrap principal; self-service user creation, invitation, and account login are not implemented.
- Invalid, expired, or revoked credentials receive `401`; a valid identity without access receives `404` for workspace-owned resources and `403` for an explicit permission denial.
- Browser login/OIDC and refresh-token flows are later extensions; they do not change the ownership model.

## Workspace ownership and data scope

- A workspace is an explicit security and execution boundary, owned by a user and containing a canonical filesystem root plus its policy.
- Projects, sessions, messages, command events, embeddings, learned commands, and preferences must be reachable only through an authorized workspace. Database access paths must carry the authenticated user/workspace scope; caller-supplied IDs alone are never authorization.
- Existing local data must be migrated explicitly and recoverably. Migration must not silently make existing data visible to a newly created remote user.
- Workspace paths are canonicalized and checked after symlink resolution. The executor receives only a server-resolved workspace root and a working directory beneath it.

## Command authorization

For each execution, the backend must validate, in order:

1. The caller is authenticated (or is using the explicitly loopback-only local mode).
2. The workspace exists and is owned by, or shared with, the caller.
3. The canonical working directory belongs to that workspace and is within its configured writable roots.
4. The command is permitted by the workspace execution mode and rules (including executable/command class, risk class, runtime, and output limits).
5. Required user confirmation is valid and applies to this exact command, workspace, and request; changing any of these invalidates the approval.
6. The isolated executor accepts the resolved execution request and records the decision and outcome for audit.

Permission denial must happen before process creation and must not be bypassable through the CLI, voice, JSON execution endpoint, SSE endpoint, or a modified request body. Policies default to deny for command classes not explicitly allowed. Existing safety blocks remain in force as an additional layer, not as a substitute for workspace permissions.

Approved commands remain capable of modifying files inside their authorized writable workspace. The product does not promise safe execution of arbitrary untrusted shell code merely because a command was approved.

## Initial workspace policy

The first implementation will use typed, versioned settings rather than a user-authored policy language:

- execution mode: disabled, confirmation-required, or restricted-safe-autocommand;
- canonical workspace root and optional narrower writable roots;
- allowed and denied executable/command classes, with deny taking precedence;
- risk classes requiring explicit confirmation;
- maximum command duration and captured output bytes;
- policy version and update audit fields.

Unknown or malformed policy values fail closed. Destructive and privileged commands remain denied by the baseline policy in the initial release.

## Planned data model

- `users`: stable identity, status, timestamps.
- `api_tokens`: user owner, token hash, label, created/last-used/revocation timestamps.
- `workspaces`: owner, name, canonical root, status, timestamps.
- `workspace_permissions`: workspace, policy version, typed policy settings, update actor and timestamp.
- Existing project/session/event data gains workspace ownership; command events also record the initiating user and the policy version/decision.

Membership/sharing is intentionally excluded from the first multi-user checkpoint. Ownership is one user per workspace until invitation and role semantics are designed.

## API contract

Initial identity endpoints:

- `POST /v1/auth/tokens`: create a labeled token for the authenticated user; plaintext appears only in the creation response.
- `GET /v1/auth/me`: return the current principal.
- `DELETE /v1/auth/tokens/{token_id}`: revoke one of the caller's tokens.

Workspace endpoints:

- `GET /v1/workspaces`, `POST /v1/workspaces`.
- `GET /v1/workspaces/{workspace_id}`, `PATCH /v1/workspaces/{workspace_id}`.
- `GET /v1/workspaces/{workspace_id}/permissions`, `PUT /v1/workspaces/{workspace_id}/permissions`.

All existing session, memory, voice, planning, recording, and execution routes become scoped to the authenticated workspace. The API derives the user from credentials and checks workspace access on every request. Health remains public; configuration metadata must not reveal secrets or internal credentials.

## Checkpoints and acceptance gates

1. **Security contract:** this document and roadmap agree on local/remote modes, ownership, execution authorization order, migration behavior, and API shape.
2. **Identity:** token hashing, creation/revocation, authentication middleware, fail-closed remote startup, and auth unit/integration tests.
3. **Workspace ownership:** explicit workspace persistence, recoverable legacy-data migration, owner-scoped data access, and cross-owner isolation tests.
4. **Workspace permissions:** typed policy persistence and server-side enforcement before executor dispatch; denial and approval-binding tests.
5. **Client integration and hardening:** CLI/frontend token and workspace flows, voice parity, deployment docs, end-to-end tests, and runtime isolation validation.

Each checkpoint must pass its focused tests plus the existing Go, frontend, and integration suites before it is committed. A failed checkpoint is repaired before the next begins.
