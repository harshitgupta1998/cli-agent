# Authentication and Workspace Security Model

## Status

This document defines the security contract for authentication and workspace permissions. S1-S5 are implemented: bearer identity, owner-scoped workspace data, typed workspace permission settings, backend execution enforcement, one-time approval grants, and authenticated client flows. The host CLI remains a trusted local client and is not a server-enforced execution boundary.

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
- The S3.1 migration assigns pre-existing rows only to the reserved `usr_legacy_local` owner, not to the configured bootstrap identity. Before upgrading an existing deployment, take a Postgres backup. A local/authenticated user can explicitly transfer a legacy workspace and its sessions, events, and preferences with `POST /v1/workspaces/{workspace_id}/adopt`; the server derives the new owner from the authenticated principal. Adoption returns `409` if that user already has the same canonical root.
- Workspace paths are canonicalized and checked after symlink resolution. The executor receives only a server-resolved workspace root and a working directory beneath it.
- Execution settings are owner-controlled and versioned. `confirmation_required` allows commands through the existing safety baseline only after explicit approval; `restricted_safe_autocommand` additionally requires a listed executable and rejects shell composition/operators. `disabled` denies execution. Denied executables take precedence.
- Writable roots are relative to the canonical workspace root; an empty list means the workspace root. Runtime and output limits are clamped to the server's global executor limits. Unknown or malformed settings fail closed.
- Remote plans receive a short-lived signed approval token, bound to the authenticated user, session, workspace, request, exact command, and working directory. The grant is stored in Postgres and consumed atomically once before process creation. Remote mode requires a stable `COMMAND_APPROVAL_SECRET` of at least 32 characters.

## Command authorization

For each execution, the backend must validate, in order:

1. The caller is authenticated (or is using the explicitly loopback-only local mode).
2. The workspace exists and is owned by, or shared with, the caller.
3. The canonical working directory belongs to that workspace and is within its configured writable roots.
4. The command is permitted by the workspace execution mode and rules (including executable/command class, risk class, runtime, and output limits).
5. Required user confirmation is valid and applies to this exact command, workspace, and request; changing any of these invalidates the approval.
6. The isolated executor accepts the resolved execution request and records the decision and outcome for audit.

Permission denial happens before process creation on backend execution routes. Remote approval grants are bound to the current policy version, expire after ten minutes, and can be consumed only once. In `restricted_safe_autocommand`, commands default to deny unless their executable is explicitly allowed; `confirmation_required` permits commands through the baseline safety checks only after explicit approval. Existing safety blocks remain in force as an additional layer, not as a substitute for workspace permissions. The host CLI fetches and evaluates workspace policy, but because it executes locally it does not consume the backend's one-time approval grant; see the client execution boundary above.

Approved commands remain capable of modifying files inside their authorized writable workspace. The product does not promise safe execution of arbitrary untrusted shell code merely because a command was approved.

## Client execution boundary

The web client sends execution to the backend worker, where workspace permissions, approval binding, filesystem boundaries, resource limits, and cancellation are enforced server-side. The host CLI instead starts a process on the user's machine. It fetches the selected workspace policy and evaluates it before execution, applies the configured timeout/output bounds, and keeps explicit confirmation even with `-yes` when the workspace mode is `confirmation_required`. These are useful workflow guardrails but cannot constrain a modified CLI or prevent the user from invoking a shell directly. Do not expose host CLI execution as a remotely trusted or multi-user security boundary; use the backend worker when server enforcement is required.

The CLI reads its bearer credential from `TERMIND_API_TOKEN`; it does not persist credentials itself. The browser accepts a bearer token interactively and keeps it in tab-scoped `sessionStorage`. Treat browser XSS as credential compromise, serve the UI over TLS for remote use, and revoke tokens that are no longer needed. Voice transcription follows the same bearer-authenticated API client path as typed requests.

## Initial workspace policy

The first implementation uses typed, versioned settings rather than a user-authored policy language:

- execution mode: disabled, confirmation-required, or restricted-safe-autocommand;
- canonical workspace root and optional narrower writable roots;
- allowed and denied executable names, with deny taking precedence;
- explicit confirmation for backend execution, with approvals bound to the policy version;
- maximum command duration and captured output bytes;
- policy version and update audit fields.

Unknown or malformed policy values fail closed. Destructive and privileged commands remain denied by the baseline policy in the initial release.

## Planned data model

- `users`: stable identity, status, timestamps.
- `api_tokens`: user owner, token hash, label, created/last-used/revocation timestamps.
- `workspaces`: owner, name, canonical root, status, timestamps.
- `workspace_permissions`: workspace, policy version, typed policy settings, update actor and timestamp.
- `command_approvals`: short-lived command binding hash, owner/workspace/session/request/policy version, expiry, and one-time consumption timestamp.
- Existing project/session/event data gains workspace ownership; backend command events record the initiating user, policy version, and whether the workspace policy allowed dispatch. Host CLI events are labeled as externally executed without server authorization.

Membership/sharing is intentionally excluded from the first multi-user checkpoint. Ownership is one user per workspace until invitation and role semantics are designed.

## API contract

Initial identity endpoints:

- `POST /v1/auth/tokens`: create a labeled token for the authenticated user; plaintext appears only in the creation response.
- `GET /v1/auth/me`: return the current principal.
- `DELETE /v1/auth/tokens/{token_id}`: revoke one of the caller's tokens.

Workspace endpoints:

- `GET /v1/workspaces`, `POST /v1/workspaces`.
- `GET /v1/workspaces/{workspace_id}`, `PATCH /v1/workspaces/{workspace_id}`.
- `POST /v1/workspaces/{workspace_id}/adopt` for explicit legacy-data adoption.
- `GET /v1/workspaces/{workspace_id}/permissions`, `PUT /v1/workspaces/{workspace_id}/permissions`.

Workspace and session reads/writes, command recording, memory search/backfill, project context, planning, and execution are scoped to the principal's owned workspace. Requests containing an inaccessible workspace/session receive `404`. Client-supplied owner IDs are ignored/rejected. Permission denials happen before executor dispatch; remote approvals are single-use.

All existing session, memory, voice, planning, recording, and execution routes become scoped to the authenticated workspace. The API derives the user from credentials and checks workspace access on every request. Health remains public; configuration metadata must not reveal secrets or internal credentials.

## Checkpoints and acceptance gates

1. **Security contract:** this document and roadmap agree on local/remote modes, ownership, execution authorization order, migration behavior, and API shape.
2. **Identity:** token hashing, creation/revocation, authentication middleware, fail-closed remote startup, and auth unit/integration tests.
3. **Workspace ownership:** explicit workspace persistence, recoverable legacy-data migration, owner-scoped data access, and cross-owner isolation tests.
4. **Workspace permissions:** typed policy persistence and server-side enforcement before executor dispatch; denial and approval-binding tests.
5. **Client integration and hardening:** CLI/frontend token and workspace flows, voice parity, deployment docs, end-to-end tests, and runtime isolation validation.

Each checkpoint must pass its focused tests plus the existing Go, frontend, and integration suites before it is committed. A failed checkpoint is repaired before the next begins.
