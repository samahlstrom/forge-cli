# SOC2 Compliance Context

This context overlay applies to all agents working on code in this project. The codebase must meet SOC2 Type II Trust Services Criteria across Security, Availability, Processing Integrity, Confidentiality, and Privacy.

## Access Control (CC6)

### Authentication

- All user-facing endpoints require authentication
- Service-to-service calls use short-lived tokens or mutual TLS
- No shared accounts or credentials
- Password policy: minimum 12 characters, complexity enforced, no reuse of last 12 passwords
- MFA required for all administrative and production access
- SSO integration required for enterprise users where available

### Authorization

- Role-based access control (RBAC) enforced server-side
- Principle of least privilege: users get minimum permissions for their role
- Privilege escalation requires approval and is logged
- API keys are scoped to specific resources and operations
- Regularly review and revoke unused access grants
- Document all roles and their permission boundaries

### Session Management

- Session timeout after 30 minutes of inactivity (15 minutes for admin sessions)
- Sessions invalidated on logout, password change, and role change
- Concurrent session limits enforced where appropriate
- Session tokens are cryptographically random, not guessable

## Audit Logging (CC7)

### What to Log

Every security-relevant event must produce a structured audit log entry:

- **Authentication events**: login, logout, failed login, MFA challenge, password change
- **Authorization events**: access granted, access denied, privilege escalation
- **Data events**: create, read, update, delete of sensitive records
- **Admin events**: configuration changes, user management, role changes
- **System events**: deployments, restarts, error spikes, health check failures

### Log Format

Each audit log entry must include:

- `timestamp` (ISO 8601, UTC)
- `actor` (user ID or service identity)
- `action` (what was done)
- `resource` (what was affected)
- `result` (success/failure)
- `ip_address` (source IP)
- `context` (additional relevant metadata)

### Log Protection

- Audit logs are append-only; never delete or modify in place
- Logs stored in a separate system/collection from application data
- Log retention: minimum 1 year
- Logs must not contain secrets, passwords, tokens, or PII beyond user IDs
- Alerting on log tampering or gaps

## Change Management (CC8)

### Code Changes

- All changes go through pull requests with at least one reviewer
- CI/CD pipeline must pass before merge (tests, lint, type checks)
- No direct commits to main/production branches
- Commit messages reference the ticket/issue being addressed
- Changes to security-sensitive code require security-aware reviewer

### Deployment

- All deployments are automated via CI/CD
- No manual production deployments
- Deployment artifacts are immutable and versioned
- Rollback procedure documented and tested
- Deployment logs captured for audit trail

### Infrastructure Changes

- Infrastructure as Code (IaC) for all environments
- Infrastructure changes go through the same review process as code
- Environment parity: staging mirrors production configuration
- Secrets rotated on schedule and after incidents

## Data Protection (CC6.1)

### Encryption

- Data at rest: AES-256 or equivalent
- Data in transit: TLS 1.2+ for all connections
- Database connections use TLS
- Backup encryption required
- Key management via cloud KMS

### Data Classification

- Classify data by sensitivity: public, internal, confidential, restricted
- Apply controls proportional to classification
- Document data flows showing where each classification level is stored and transmitted
- Data retention policies enforced per classification

### Data Disposal

- Secure deletion when data retention period expires
- Cryptographic erasure acceptable for encrypted data
- Document disposal procedures and evidence

## Availability (CC9)

### Uptime

- Define and monitor SLA targets
- Health check endpoints for all services
- Automated alerting on downtime or degradation
- Incident response runbook documented

### Disaster Recovery

- Backup frequency: at least daily for critical data
- Backup restoration tested quarterly
- Recovery Time Objective (RTO) and Recovery Point Objective (RPO) documented
- Multi-region or multi-zone deployment for critical services

### Capacity

- Monitor resource utilization (CPU, memory, storage, network)
- Auto-scaling configured where applicable
- Capacity planning reviewed quarterly

## Code Review Checklist

When reviewing code for SOC2 compliance, verify:

- [ ] Authentication required on all endpoints
- [ ] Authorization checks are server-side with least privilege
- [ ] Audit logging present for security-relevant operations
- [ ] Audit logs do not contain secrets or excessive PII
- [ ] Error responses do not leak internal details
- [ ] Input validation prevents injection attacks
- [ ] HTTPS enforced, no mixed content
- [ ] Secrets managed via environment variables or secret manager
- [ ] Change goes through standard PR review process
- [ ] Tests cover the new functionality
