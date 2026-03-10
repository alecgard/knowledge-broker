# ACME Org — Development Practices

## Code Review

All code changes require at least 1 approval. Security-sensitive changes (auth, authz, data access) require 2 approvals including one from the Security team.

PR conventions:
- Branch naming: `{type}/{ticket}-{short-description}` (e.g., `feat/NEX-1234-add-bulk-inventory-api`)
- Commit messages: conventional commits format (`feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`)
- PR description must include: what changed, why, how to test, any rollback considerations
- PRs should be < 400 lines of diff. Larger changes should be split into a stack.

---

## Testing

- **Unit tests:** Required for all business logic. Coverage target: 80%. Measured by Codecov, reported on PRs.
- **Integration tests:** Required for any service that talks to a database or external API. Use testcontainers for database tests.
- **E2E tests:** Maintained by QA team. Run on every PR against staging. Playwright-based, ~200 test scenarios covering critical user flows.
- **Load tests:** Run quarterly by SRE team using k6. Baseline: Nexus API handles 5,000 RPS with P99 < 200ms.
- **Chaos tests:** Monthly. Gremlin used to inject failures (network partition, pod kill, CPU stress). Results reviewed in SRE weekly meeting.

---

## Incident Management

**Severity levels:**
- **SEV-1 (Critical):** Complete system outage or data loss. All hands on deck. CEO notified. Target resolution: 1 hour.
- **SEV-2 (Major):** Significant degradation affecting >50% of customers. IC + relevant team. VP Eng notified. Target resolution: 4 hours.
- **SEV-3 (Minor):** Degradation affecting <50% of customers or non-critical feature unavailable. Team handles. Target resolution: 24 hours.
- **SEV-4 (Low):** Cosmetic issues, minor bugs, monitoring noise. Handled in normal sprint workflow.

**Incident process:**
1. Declare incident in #incidents Slack channel with severity
2. Incident Commander (IC) assigns roles: Comms Lead, Technical Lead
3. Technical Lead coordinates investigation and remediation
4. Comms Lead updates status page (status.acme.dev) and notifies affected customers
5. Post-incident: blameless post-mortem within 48 hours. Template in Confluence. Action items tracked in Jira with SLA: SEV-1 action items within 1 week, SEV-2 within 2 weeks.

---

## On-Call

**Primary rotation:** Each team has a weekly primary on-call who responds to PagerDuty alerts during business hours (9am–6pm PT) and is first responder for after-hours P1/P2.

**Secondary rotation:** Platform team + SRE provide 24/7 secondary on-call as escalation point.

**Compensation:** $500/week on-call stipend. $200 per after-hours page. Additional day off per on-call week.

**Tooling:** PagerDuty for alerting, OpsGenie as backup. Incident.io for incident management workflow.
