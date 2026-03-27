# HIPAA Compliance Context

This context overlay applies to all agents working on code in this project. The codebase handles Protected Health Information (PHI) and must comply with the HIPAA Security Rule and Privacy Rule.

## PHI Field Rules

PHI includes any individually identifiable health information: names, dates of birth, medical record numbers, diagnoses, treatment records, SSNs, email addresses, phone numbers, IP addresses (when linked to a patient), and any combination of fields that could identify a patient.

### Absolute Rules (Never Violate)

1. **Never log PHI.** No patient names, DOBs, MRNs, diagnoses, or identifiers in `console.log`, `console.error`, `logger.*`, or any logging output. Use opaque identifiers (e.g., hashed IDs) in logs.

2. **Never store PHI in client-side state** that persists beyond the session. No `localStorage`, `sessionStorage`, `IndexedDB`, or cookies containing PHI. In-memory state (e.g., Svelte stores) is acceptable only during an active authenticated session.

3. **Never include PHI in URLs.** No patient identifiers in route params, query strings, or URL fragments. Use opaque tokens that resolve server-side.

4. **Never expose PHI in browser storage.** This includes service worker caches, browser history entries, and `window.name`.

5. **Encrypt PHI at rest.** Firestore data containing PHI must use encrypted fields or be stored in collections with server-side encryption enabled. Backups must be encrypted.

6. **Encrypt PHI in transit.** All API calls must use HTTPS. Verify TLS configuration. No HTTP fallbacks.

7. **Minimum Necessary principle.** Only fetch and display the PHI fields required for the current operation. Do not load full patient records when only a name is needed.

8. **Audit all PHI access.** Every read/write of PHI must produce an audit log entry with: who accessed it, when, what was accessed, and why (purpose/context).

## Authentication Requirements

- All endpoints serving PHI require authentication
- Sessions must time out after 15 minutes of inactivity
- Re-authentication required for sensitive operations (viewing full records, exporting data)
- Multi-factor authentication required for administrative access
- Failed login attempts must be logged and rate-limited
- Password requirements: minimum 12 characters, complexity rules enforced

## Authorization Patterns

- Role-based access control (RBAC) for all PHI access
- Principle of least privilege: default deny, explicit grants
- Provider-patient relationships must be verified before granting access
- Administrative overrides must be logged and auditable
- Authorization checks happen server-side, never client-only
- Firestore security rules must enforce access control at the document level

## Session Security

- Session tokens must be cryptographically random, minimum 128 bits
- Bind sessions to client fingerprint (IP + user-agent) where feasible
- Invalidate sessions on password change
- Provide explicit logout that clears all session artifacts
- No session data in URLs

## Secret Management

- No secrets in source code, environment files committed to VCS, or client bundles
- Use environment variables or a secret manager (e.g., GCP Secret Manager)
- Rotate secrets on a schedule and after any suspected compromise
- API keys for PHI-serving endpoints must be scoped and rotatable

## Firestore Security Rules

When writing or modifying Firestore security rules for PHI collections:

- Require `request.auth != null` on all PHI documents
- Validate `request.auth.token` claims for role-based access
- Use `resource.data` checks to enforce ownership/relationship constraints
- Never use wildcard rules (`allow read, write: if true`) on PHI collections
- Log rule denials for security monitoring

## Encryption Requirements

- AES-256 for data at rest
- TLS 1.2+ for data in transit
- Field-level encryption for highly sensitive PHI (SSN, full medical records)
- Key management via cloud KMS; never store encryption keys alongside encrypted data
- Key rotation policy: at least annually, immediately after suspected compromise

## Code Review Checklist

When reviewing code that touches PHI, verify:

- [ ] No PHI in log statements
- [ ] No PHI in URLs or query parameters
- [ ] No PHI in client-side persistent storage
- [ ] Authentication required on all PHI endpoints
- [ ] Authorization checks are server-side
- [ ] Minimum necessary data fetched
- [ ] Audit logging present for PHI access
- [ ] Error responses do not leak PHI
- [ ] Input validation prevents injection attacks
- [ ] HTTPS enforced, no mixed content
