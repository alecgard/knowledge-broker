# ACME Org -- Vendor Integrations

This document covers third-party vendor integrations used by ACME engineering. For each vendor, it describes the integration architecture, configuration, known issues, and operational details. Ownership of each integration is noted -- reach out to the owning team for changes or troubleshooting.

---

## Amazon Web Services (AWS)

**Owner:** Platform Team (Marcus Webb) / SRE Team (Tom Bradley)

### Account Structure

ACME uses AWS Organizations with the following account hierarchy:

| Account | Account ID | Purpose | Primary Region |
|---------|-----------|---------|---------------|
| `acme-root` | 111111111111 | Organization root. No workloads. Billing consolidation only. | us-west-2 |
| `acme-prod` | 222222222222 | Production workloads (EKS, RDS, MSK, ElastiCache, OpenSearch) | us-west-2, eu-west-1 |
| `acme-staging` | 333333333333 | Staging and development environments | us-west-2 |
| `acme-security` | 444444444444 | Security tooling: GuardDuty, Security Hub, CloudTrail aggregation, Secrets Manager (primary) | us-west-2 |
| `acme-log-archive` | 555555555555 | Long-term log storage and compliance archives. CloudTrail logs (90-day retention), VPC flow logs, S3 access logs. | us-west-2 |

**Service Control Policies (SCPs):**
- All accounts: deny access to regions outside us-west-2 and eu-west-1
- Staging: deny creation of instances larger than m6i.2xlarge (cost control)
- Log archive: deny all write access except from CloudTrail and Loki archive pipeline
- Root account: MFA enforced, no programmatic access keys

### IAM & Access Management

Authentication flows through Okta SSO into AWS IAM Identity Center (formerly AWS SSO):

**Permission sets:**
| Role | Access Level | Who Gets It | Notes |
|------|-------------|-------------|-------|
| `developer-readonly` | Read-only access to EKS, CloudWatch, S3 (non-sensitive buckets), ECR | All engineers | Default role for all engineering hires |
| `developer-deploy` | Above + ability to trigger ArgoCD syncs, push to ECR, access staging RDS (read-only) | Engineers after onboarding (week 2) | Requires team lead approval |
| `sre-admin` | Full admin access to production accounts | SRE team (5 people) | Requires break-glass for destructive actions |
| `security-audit` | Read-only access to all accounts + CloudTrail + GuardDuty + Security Hub | Security team (6 people) | No write access to any resource |
| `dba-access` | RDS admin access (production and staging) | Platform team DBAs (2 people) | Time-limited sessions (4 hours max) |
| `billing-readonly` | Cost Explorer, Budgets, billing dashboard | Finance team, VP Eng | Monthly cost review access |

**Access review:** Quarterly access review conducted by Security team. Last review: January 2026. Findings: 3 former employees still had active SSO sessions (terminated within 24 hours), 2 developers had elevated permissions they no longer needed (downgraded).

### Cost Breakdown

**Total monthly AWS spend:** ~$180,000/month (as of February 2026)

| Service | Monthly Cost | % of Total | Owner | Notes |
|---------|-------------|-----------|-------|-------|
| EKS (EC2 node groups) | $45,000 | 25% | Platform / SRE | 3 clusters, ~180 nodes total. Mixed on-demand and spot (70/30 split). |
| RDS (PostgreSQL) | $35,000 | 19% | Platform | 8 production instances. Multi-AZ. Largest: nexus-api DB (r6g.4xlarge, 2TB storage). |
| MSK (Kafka) | $25,000 | 14% | Platform | 6-broker cluster. 30 topics, 15TB retention. |
| Data Transfer | $20,000 | 11% | All | Cross-AZ and internet egress. Largest contributor: Relay agent communication. |
| S3 | $12,000 | 7% | All | ~15TB. Largest buckets: data-exports (5TB), report-artifacts (3TB), ml-models (2TB). |
| ElastiCache (Redis) | $10,000 | 6% | Platform | 3-node r7g.xlarge cluster. |
| OpenSearch | $9,000 | 5% | Sentinel | 3 data nodes for screening index. Growing ~20% QoQ as Sentinel customer base expands. |
| CloudFront | $6,000 | 3% | Frontend / Platform | CDN for static assets and API acceleration. |
| Other (Secrets Manager, Route 53, CloudWatch, etc.) | $18,000 | 10% | Various | Miscellaneous services. |

**Cost optimization efforts:**
- Savings Plans purchased in 2025: 1-year compute savings plan covering 60% of EC2 baseline. Saves ~$15K/month.
- Spot instances for non-critical workloads (CI runners, batch processing). Saves ~$8K/month.
- S3 Intelligent Tiering enabled for all buckets. Saves ~$1.5K/month.
- Identified $12K/month in additional savings from right-sizing 3 over-provisioned RDS instances (pending DBA review).

**Monthly cost optimization review:** Held with AWS TAM (Jessica Wong) on the first Wednesday of each month. AWS provides a Well-Architected review annually (last review: September 2025, 4 recommendations implemented out of 6).

### AWS Support

- **Plan:** Enterprise Support
- **TAM:** Jessica Wong (jessica.wong@amazon.com)
- **Monthly TAM meeting:** First Wednesday, 30 minutes. Review open cases, discuss architecture, cost optimization.
- **Case priority:** Production system impaired cases use "Urgent" priority (15-minute response SLA from AWS).
- **Contact for account issues:** Platform team Slack channel #team-platform or email platform@acme.dev

---

## Stripe

**Owner:** Platform Team (billing infrastructure) / Finance Team (billing operations)

### Integration Overview

Stripe handles all customer billing, subscription management, and usage-based metering for ACME products.

**Service:** `stripe-billing-service` (Go, repo: `acme/stripe-billing-service`)
- Runs as a deployment in the `billing` namespace on EKS
- Syncs customer subscription data bidirectionally between Nexus and Stripe
- Handles usage record submission for metered billing (shipment counts, screening counts)

### Subscription Model

| Product | Stripe Product ID | Billing Model | Notes |
|---------|------------------|---------------|-------|
| Nexus Base | `prod_nexus_base` | Flat monthly ($2,500) | All tiers |
| Nexus Shipment Metering | `prod_nexus_shipments` | Usage-based ($0.50/shipment) | Metered daily, invoiced monthly |
| Beacon | `prod_beacon` | Flat monthly ($800 add-on) | Requires active Nexus subscription |
| Sentinel Base | `prod_sentinel_base` | Flat monthly ($1,200) | All tiers |
| Sentinel Screening | `prod_sentinel_screening` | Usage-based ($0.02/screening) | Metered daily, invoiced monthly |
| Sentinel Cert Management | `prod_sentinel_certs` | Flat monthly ($400 add-on) | Optional add-on |
| Enterprise Override | `prod_enterprise_custom` | Custom pricing | Set per customer by Sales Ops |

### Webhook Integration

The `stripe-billing-service` consumes the following Stripe webhook events:

| Event | Handler | Action |
|-------|---------|--------|
| `invoice.paid` | `handleInvoicePaid` | Update customer billing status in Nexus DB. Trigger receipt email via SendGrid. |
| `invoice.payment_failed` | `handlePaymentFailed` | Alert CSM via Slack (#billing-alerts). If 3rd consecutive failure, flag account in Launchpad for review. Do NOT auto-suspend -- CSM handles communication. |
| `subscription.updated` | `handleSubscriptionUpdate` | Sync new subscription state to Nexus. Update feature flags for tier changes (e.g., Enterprise upgrade enables SQL Playground). |
| `customer.subscription.deleted` | `handleChurn` | Log churn event. Trigger data retention countdown (90 days). Notify CSM and RevOps. |
| `payment_method.attached` | `handlePaymentMethod` | Update stored payment method metadata. Used for billing portal display. |

**Webhook endpoint:** `https://api.acme.dev/webhooks/stripe`
**Webhook signing secret:** Stored in AWS Secrets Manager under `stripe/webhook-signing-secret`
**Webhook delivery:** Stripe retries failed deliveries for up to 72 hours. We have a dead letter queue in SQS for any events that fail processing after 3 internal retries.

### Test Mode

- All staging environments use Stripe test mode
- Test mode API keys stored in Secrets Manager under `stripe/test-api-key`
- Test clock feature used for automated billing integration tests (simulates invoice cycles)
- QA team has a set of test Stripe customers for E2E billing flow testing

### Known Issues

1. **Usage record batching delay.** Usage records are submitted in hourly batches (via a cron job). This means metering can lag behind actual usage by up to 1 hour. For most customers this is invisible (invoiced monthly), but Enterprise customers with real-time usage dashboards in Stripe have noticed discrepancies. Mitigation: we display a "usage data may be delayed by up to 1 hour" disclaimer. Long-term fix: move to real-time event-driven metering via Kafka. Planned for Q3 2026.

2. **Subscription proration on mid-cycle tier change.** When a customer upgrades mid-billing-cycle, Stripe's proration logic sometimes generates credit notes that confuse the Finance team. Workaround: Sales Ops manually reviews all mid-cycle upgrades. Long-term fix: implement custom proration logic in stripe-billing-service. Not yet scheduled.

3. **Currency handling.** All billing is in USD. EU customers (FreshDirect Europe, etc.) have requested EUR billing. Stripe supports multi-currency, but our billing service assumes USD everywhere. Estimated effort to fix: M (2 sprints). In the backlog, priority P3.

---

## Okta

**Owner:** IT Team / Security Team (Aisha Mohammed)

### Integration Overview

Okta serves as the central identity provider for ACME, handling both employee authentication and customer SSO federation.

### Employee SSO

**Okta org:** `acme-org.okta.com`
**Total apps configured:** 23 internal applications

| Application | Integration Type | Notes |
|------------|-----------------|-------|
| AWS SSO (IAM Identity Center) | SAML 2.0 | Federated access to all AWS accounts |
| GitHub (acme-org) | SAML 2.0 + SCIM | Auto-provision/deprovision org membership |
| Google Workspace | SAML 2.0 + SCIM | Email, Drive, Calendar |
| Slack (Enterprise Grid) | SAML 2.0 + SCIM | Auto-provision channels based on team |
| Jira / Confluence | SAML 2.0 | Atlassian Cloud |
| Grafana | SAML 2.0 | Observability dashboards |
| ArgoCD | OIDC | GitOps deployment access |
| PagerDuty | SAML 2.0 | On-call and incident management |
| Stripe Dashboard | SAML 2.0 | Finance and billing team access |
| Greenhouse | SAML 2.0 | Recruiting / ATS |
| Culture Amp | SAML 2.0 | Employee surveys |
| Figma | SAML 2.0 | Design team |
| 1Password Teams | SAML 2.0 | Shared credentials (break-glass, vendor accounts) |
| Internal tools (Launchpad, Relay Studio, Sentinel Review Queue) | OIDC via auth-service | Custom OIDC integration through ACME auth-service |

**MFA Policy:**
- Required for all employees, no exceptions
- Allowed factors: Okta Verify (push notification), FIDO2/WebAuthn hardware keys (YubiKey)
- SMS/voice factors disabled (phishing risk)
- Biometric factor (Okta Verify with biometric) required for admin-level access to production systems
- MFA challenge on every login (no "remember this device" for production tools; 30-day remember allowed for low-sensitivity tools like Google Workspace)

**Lifecycle Management:**
- New hire: IT creates Okta account on Day 1. Group membership auto-provisions access to team-appropriate apps.
- Role change: Manager updates group membership in Okta. SCIM propagates changes to downstream apps within 15 minutes (except GitHub -- see known issues).
- Offboarding: IT deactivates Okta account immediately upon termination notice. All downstream sessions terminated via Okta's Universal Logout (configured for critical apps). Full deprovisioning completed within 1 hour.

### Customer SSO

ACME supports customer SSO for the Nexus web app and API:

**Architecture:** Okta acts as the SAML Service Provider (SP). Customer identity providers (IdPs) federate with ACME's Okta org via SAML 2.0.

**Onboarding process:**
1. Customer provides SAML metadata XML or federation URL
2. ACME Implementation team configures a new Identity Provider in Okta (under the customer's app instance)
3. Configure attribute mapping: email, first name, last name, roles (optional)
4. Test with customer IT team using Okta's SAML test tool
5. Enable for customer users. Provide SSO login URL: `https://app.acme.dev/sso/{customer_slug}`

**Supported customer IdPs:** Azure AD (most common, ~60% of customers), Okta (as customer's IdP, ~20%), Google Workspace (~10%), OneLogin (~5%), other SAML 2.0 compliant IdPs (~5%).

**Customer SSO troubleshooting guide:**
- "SAML assertion invalid": Usually clock skew between customer IdP and ACME Okta. Solution: customer needs to sync NTP on their IdP server. Acceptable skew: +/- 5 minutes.
- "User not found": Attribute mapping issue. Check that email attribute in SAML assertion matches expected NameID format. Most common fix: switch NameID format from `persistent` to `emailAddress`.
- "Forbidden after SSO": User authenticated but lacks role assignment in Nexus. CSM needs to assign user to a role in Launchpad.

### Known Issues

1. **SCIM provisioning delay for GitHub.** When an employee is added to a team in Okta, SCIM provisions their GitHub org membership. However, GitHub's SCIM API has a ~15-minute propagation delay for team membership changes. New hires on Day 1 may not have GitHub access for 15-30 minutes after their Okta account is created. Workaround: IT manually adds to critical repos if needed immediately.

2. **Universal Logout incomplete for ArgoCD.** ArgoCD uses OIDC tokens with 1-hour expiry. Okta's Universal Logout revokes the Okta session but does not invalidate the ArgoCD token. A terminated employee could theoretically access ArgoCD for up to 1 hour after offboarding. Mitigation: SRE monitors ArgoCD audit logs during offboarding events. Long-term fix: implement token introspection in ArgoCD (planned for Q2 2026).

3. **Customer SSO session timeout mismatch.** ACME sets a 12-hour session timeout in Okta, but some customer IdPs set shorter timeouts (e.g., 4 hours). This can cause confusing behavior where users are logged out of the customer IdP but still have an active ACME session. Not a security risk (session is still authenticated) but generates support tickets. Under investigation.

---

## PagerDuty

**Owner:** SRE Team (Tom Bradley)

### Configuration

**Account:** Enterprise plan
**Organization:** ACME Org

### Escalation Policies

Each engineering team has its own escalation policy:

| Level | Role | Response Time | Action |
|-------|------|--------------|--------|
| 1 | Primary on-call (team member) | 5 minutes | Acknowledge and begin investigation |
| 2 | Secondary on-call (team member) | 10 minutes | Auto-escalate if Level 1 does not acknowledge |
| 3 | Team lead | 15 minutes | Auto-escalate if Level 2 does not acknowledge |
| 4 | VP Engineering (Dana Chen) | 20 minutes | Final escalation for unacknowledged alerts |

**Special escalation for SEV-1:** Bypasses normal escalation. Simultaneously pages primary on-call, team lead, SRE primary, and IC on rotation. If not acknowledged in 5 minutes, pages VP Engineering.

### On-Call Schedules

- **Total schedules:** 12 (one primary + one secondary for each of the 6 teams with on-call: Platform, Nexus Core, Relay, Beacon, Sentinel, SRE)
- **Rotation:** Weekly, handoff on Monday 9:00 AM PT
- **Override:** Team members can swap shifts via PagerDuty or by posting in their team Slack channel
- **Holidays:** Voluntary coverage with 2x on-call compensation. If no volunteer, team lead assigns based on rotation fairness.

### Integrations

| Source | Integration Type | Description |
|--------|-----------------|-------------|
| Grafana Alerting | Webhook (Events API v2) | Primary alert source. All P1-P4 alerts route through Grafana to PagerDuty. |
| AWS CloudWatch | CloudWatch → SNS → PagerDuty | RDS failover alerts, EKS node health, Lambda errors |
| Checkly | Webhook | External synthetic monitoring alerts (uptime checks) |
| GitHub Actions | Webhook | CI/CD pipeline failure alerts (critical pipelines only) |
| Custom: Relay control plane | Events API v2 | Agent disconnection alerts |
| Custom: Sentinel screening | Events API v2 | Screening service degradation alerts |

### Maintenance Windows

Configured per customer for their known ERP/WMS maintenance windows. During maintenance windows, alerts from Relay agents for that customer are suppressed.

**Example maintenance windows:**
- GlobalMart SAP maintenance: Sundays 02:00-06:00 PT
- FreshDirect NetSuite maintenance: Saturdays 22:00-02:00 CET
- Pacific Rim Dynamics 365 maintenance: First Saturday of each month, 00:00-04:00 JST

Maintenance windows are managed in Launchpad by CSMs and synced to PagerDuty via the relay-control-plane service.

---

## GitHub

**Owner:** Platform Team (Marcus Webb) / Security Team (Aisha Mohammed)

### Organization

- **Org name:** `acme-org`
- **Plan:** GitHub Enterprise Cloud
- **Total repositories:** 180
- **Active repositories (committed to in last 90 days):** 142
- **Members:** 128 (all engineering + some product and design)

### Team Structure

GitHub teams mirror the engineering org. Each team has write access to their owned repos and read access to all other repos:

| GitHub Team | Repos Owned | Members |
|-------------|-------------|---------|
| `platform` | 12 (terraform-modules, acme-go-kit, acme-kafka, auth-service, gateway, etc.) | 18 |
| `nexus-core` | 8 (nexus-api, inventory-service, shipment-service, forecast-engine, alert-service, replenishment-engine, etc.) | 22 |
| `relay` | 6 (relay-agent, relay-control-plane, relay-studio, connector-sdk, etc.) | 15 |
| `beacon` | 5 (beacon-api, report-generator, beacon-analytics, etc.) | 10 |
| `sentinel` | 5 (sentinel-api, screening-service, classification-service, sentinel-review-ui, etc.) | 12 |
| `frontend` | 8 (forge-ui, forge-design-system, mobile-app, storybook, etc.) | 16 |
| `qa-release` | 6 (e2e-tests, load-tests, release-tools, feature-flags, etc.) | 8 |
| `security` | 4 (security-policies, pen-test-reports, vulnerability-tracker, etc.) | 6 |
| `data` | 5 (data-warehouse, ml-pipelines, analytics-dashboards, data-quality, etc.) | 8 |
| `sre` | 6 (runbooks, chaos-tests, capacity-planner, incident-tools, etc.) | 5 |

### Branch Protection Rules

Applied to `main` branch on all active repositories:

- **Required approvals:** 1 minimum (2 for security-sensitive repos: auth-service, gateway, screening-service, sentinel-api)
- **Required status checks:** CI pipeline must pass (lint, test, build, security scan)
- **No force push:** Force push to main is disabled for everyone, including admins
- **No branch deletion:** Main branch cannot be deleted
- **Require branches to be up to date:** Enabled. PRs must be rebased on latest main before merge.
- **Signed commits:** Not currently required (under discussion for Q2 2026)
- **CODEOWNERS:** Configured for all repos. Team lead + 1 senior engineer listed as code owners.

### GitHub Actions

**Self-hosted runners:**
- Deployed on EKS in the `acme-staging` AWS account
- 8 runner pods (m6i.2xlarge equivalent), auto-scaling 4-16 based on queue depth
- 2-4x faster than GitHub-hosted runners for Go builds (pre-cached Go modules and Docker layers)
- Ephemeral runners: each job gets a fresh pod (security best practice)

**Standard CI pipeline** (defined in `.github/workflows/ci.yml` for Go services):
1. `lint`: golangci-lint with ACME custom config
2. `test`: go test with race detector, coverage report uploaded to Codecov
3. `build`: Docker build, push to ECR
4. `security`: Snyk container scan, Dependabot alerts check, SAST via semgrep
5. `e2e` (on PR to main): trigger E2E test suite in staging (Playwright, managed by QA team)

**Average CI pipeline time:** 8 minutes (target: 6 minutes by end of Q1 -- see Platform team OKR)

### Secrets Management in CI

- **CI secrets:** Stored in GitHub Secrets at the organization level. Examples: `AWS_ACCESS_KEY_ID`, `CODECOV_TOKEN`, `SNYK_TOKEN`, `DOCKER_REGISTRY_URL`
- **Runtime secrets:** Services reference AWS Secrets Manager at runtime. CI does NOT have access to production secrets.
- **Rotation:** CI secrets rotated quarterly by Platform team. Runtime secrets rotated quarterly by Security team via automated rotation lambdas in AWS Secrets Manager.

---

## Slack

**Owner:** IT Team / Platform Team (for bot integrations)

### Workspace

- **URL:** `acme-org.slack.com`
- **Plan:** Enterprise Grid
- **Total members:** 340 (all employees)
- **Total channels:** ~500 (including DMs and archived)
- **Active channels (posted to in last 30 days):** ~180

### Key Channels

| Channel | Purpose | Members | Notifications |
|---------|---------|---------|---------------|
| `#engineering` | General engineering discussion, announcements, all-hands notes | 120 | None (opt-in) |
| `#incidents` | Active incident coordination. All incident communication goes here. | 120 | @channel for SEV-1 |
| `#deployments` | ArgoCD deployment notifications (automated) | 80 | None |
| `#relay-alerts` | Relay agent disconnection and sync failure alerts | 25 | Team mention |
| `#intelligence-alerts` | Forecast accuracy degradation and anomaly detection alerts | 15 | Team mention |
| `#security` | Security advisories, vulnerability alerts, pentest findings | 30 | None |
| `#billing-alerts` | Stripe payment failure and subscription change notifications | 10 | None |
| `#pr-review` | Cross-team PR review requests (24-hour SLA) | 80 | None |
| `#random` | Water cooler, memes, off-topic | 280 | None |
| `#acme-globalmart` | Dedicated channel for GlobalMart (shared with their team via Slack Connect) | 15 (ACME) + 8 (GlobalMart) | None |
| `#team-*` | Per-team channels (e.g., #team-nexus-core, #team-relay) | Varies | Team-specific |
| `#guild-*` | Cross-team interest groups (e.g., #guild-go, #guild-frontend, #guild-data) | Varies | None |

### Bot Integrations

| Bot | Source | Channels | Description |
|-----|--------|----------|-------------|
| PagerDuty Bot | PagerDuty | `#incidents`, team channels | Posts incident alerts, allows acknowledgment and resolution from Slack |
| ArgoCD Notifier | ArgoCD | `#deployments` | Posts deployment status (syncing, healthy, degraded, failed) for all apps |
| GitHub PR Bot | GitHub | `#pr-review`, team channels | Posts new PRs needing review, review status updates, merge notifications |
| Grafana Alert Bot | Grafana | `#incidents`, `#relay-alerts`, `#intelligence-alerts` | Posts alert state changes (firing, resolved) with links to dashboards |
| Nexus Bot (custom) | Custom Go service | `#engineering` | Posts daily system health summary at 9:00 AM PT: uptime stats, key metrics, notable events from the last 24 hours. Built by SRE team. |
| Stripe Bot | Stripe | `#billing-alerts` | Posts payment failures and subscription changes |
| Checkly Bot | Checkly | `#incidents` | Posts synthetic monitoring failures (external uptime checks) |

### Retention & Compliance

- **Message retention:** 1 year for all channels (Enterprise Grid policy)
- **Compliance export:** Enabled. All messages and files exported nightly to S3 (`acme-log-archive` account) for legal and compliance purposes.
- **eDiscovery:** Slack's built-in eDiscovery tool configured for Legal team access. Used twice in 2025 (both for customer contract disputes, resolved without litigation).
- **DLP:** Slack Enterprise DLP rules configured to flag messages containing patterns matching API keys, AWS credentials, or credit card numbers. Alerts go to #security channel.

---

## Datadog (Legacy -- Migrated Away)

**Owner:** SRE Team (Tom Bradley) -- for decommissioning
**Status:** Migrated to Prometheus + Grafana in Q3 2025

### Migration Context

ACME used Datadog from 2020 to Q3 2025 for metrics, logging, and APM. The migration to Prometheus + Grafana + Loki was driven by cost ($35K/month Datadog bill) and a desire for more control over the observability stack.

### Migration Details

- **Migration period:** June 2025 -- September 2025 (3 months)
- **Migration doc:** `acme/sre-runbooks/docs/datadog-migration.md`
- **Dashboard conversion:** Custom Python script (`datadog-to-grafana.py` in sre-runbooks repo) converts Datadog dashboard JSON to Grafana provisioning YAML. Accuracy: ~80% -- remaining 20% requires manual adjustment (custom Datadog widgets with no Grafana equivalent).
- **Alert migration:** All 150 Datadog monitors converted to Grafana Alerting rules. Routing reconfigured to PagerDuty via Grafana webhook integration.
- **APM migration:** Datadog APM replaced with Jaeger + OpenTelemetry. Required instrumentation changes in all Go services (replaced `dd-trace-go` with `opentelemetry-go`). Python forecast-engine still uses Datadog APM library -- migration deferred until Python-to-Go rewrite (TD-103).

### Current State

- **Datadog account:** Still active (read-only). Contract expires June 2026. Will not renew.
- **Remaining references:** 8 internal wiki pages still reference Datadog dashboards. Cleanup tracked in Jira (PLAT-4521).
- **Legacy dashboards:** 12 Datadog dashboards bookmarked by engineers. Grafana equivalents exist for all 12 but some engineers haven't updated their bookmarks. SRE sent a reminder in #engineering on February 15.
- **forecast-engine APM:** The Python forecast-engine service still sends APM traces to Datadog. These traces are the only active data flowing to Datadog. Will be cleaned up when the service is rewritten in Go (TD-103, planned for Q3 2026).

### Lessons Learned from Migration

Documented in `acme/sre-runbooks/docs/datadog-migration-retro.md`:

1. **Start with alerting migration, not dashboards.** Alerting is critical path; dashboards are nice-to-have during transition. We made the mistake of trying to migrate dashboards first, which delayed the alerting cutover.
2. **Run dual-write for at least 4 weeks.** We ran Prometheus and Datadog in parallel for 6 weeks. This was essential for validating metric parity and catching edge cases (e.g., histogram bucket boundaries differ between Datadog and Prometheus).
3. **Budget for instrumentation changes.** Switching APM libraries required code changes in every service. This was the most time-consuming part of the migration. Plan 2-3 sprints for a team of 3 engineers.
4. **Communicate the cutover date clearly.** We announced the Datadog read-only date 4 weeks in advance, sent weekly reminders, and provided a migration guide for updating bookmarks and saved queries. Still had engineers asking "where did my dashboard go?" for 2 weeks after cutover.
5. **Cost savings realized:** Prometheus + Grafana (self-hosted on EKS) costs ~$8K/month in compute and storage, compared to $35K/month for Datadog. Net savings: ~$27K/month ($324K/year). ROI on migration effort (estimated at $150K in engineering time) achieved in under 6 months.

---

## Vendor Management Process

### Approval Process for New Vendors

Any new third-party vendor that will have access to ACME data or integrate with ACME systems must go through the following approval process:

1. **Business justification:** Requesting team submits a 1-page justification: what problem does this solve, why this vendor, what alternatives were considered.
2. **Security questionnaire:** Vendor completes ACME's security questionnaire (based on SIG Lite). Reviewed by Security team within 5 business days.
3. **Legal review:** If vendor will process customer data, Legal reviews DPA and terms of service. If vendor is non-US, additional data transfer assessment required.
4. **Cost approval:** Finance reviews pricing and budget impact. VP-level approval required for annual spend > $50K.
5. **Technical review:** Platform team reviews integration architecture, assesses operational complexity, and confirms monitoring and alerting plan.
6. **Final approval:** VP Eng sign-off for engineering tools. CFO sign-off for spend > $100K/year.

**Current approved vendors with data access:** AWS, Stripe, Okta, PagerDuty, Checkly, HackerOne, Deloitte, Snowflake (ClickHouse Cloud), Anthropic (Claude API for Sentinel).

### Annual Vendor Review

Conducted in Q4 each year by Security team in collaboration with Finance:
- Review all vendor contracts for renewal terms and cost changes
- Verify vendor SOC 2 reports are current (or equivalent certification)
- Assess whether each vendor is still needed and if alternatives should be evaluated
- Update vendor risk register

**2025 annual review outcomes:**
- Datadog: flagged for non-renewal (migration to Prometheus/Grafana)
- Checkly: renewed (critical for external monitoring, no equivalent self-hosted alternative)
- HackerOne: renewed (bug bounty program generating valuable findings)
- Snowflake: new vendor approved (for customer-facing data export in Beacon)
- Anthropic: new vendor approved (Claude API for Sentinel HS code classification)
