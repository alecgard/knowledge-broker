# ACME Org — Organizational Structure

## Engineering (120 people)

**VP of Engineering:** Dana Chen

**Teams:**

### Platform Team (18 people)
**Lead:** Marcus Webb
- Owns shared infrastructure: Kubernetes cluster, CI/CD pipelines, observability stack, database operations
- Maintains internal Go libraries: `acme-go-kit` (logging, tracing, auth middleware), `acme-kafka` (consumer/producer wrappers), `acme-pgx` (PostgreSQL helpers)
- On-call rotation: 1 primary + 1 secondary, weekly rotation, PagerDuty escalation after 15 min

### Nexus Core Team (22 people)
**Lead:** Priya Sharma
- Owns: Inventory service, Shipment service, Forecast engine, Alert service, Replenishment engine
- Split into 3 squads:
  - **Inventory Squad** (7 people): Real-time inventory positions, CDC pipeline, inventory reconciliation
  - **Logistics Squad** (8 people): Shipment tracking, carrier integrations, delivery estimation
  - **Intelligence Squad** (7 people): Forecast engine (Oracle), replenishment recommendations, anomaly detection

### Relay Team (15 people)
**Lead:** James Okafor
- Owns: Relay agent, all connectors, schema mapping engine, data pipeline
- Responsible for connector certification (each integration goes through a 3-week certification process: unit tests → staging validation → production canary with 3 beta customers)

### Beacon Team (10 people)
**Lead:** Sarah Kim
- Owns: Analytics warehouse (ClickHouse), dashboard service, report generator, data export pipelines
- Note: This team also maintains the embedded Metabase instance and is responsible for quarterly Metabase upgrades

### Sentinel Team (12 people)
**Lead:** Raj Patel
- Owns: Classification service, screening service, certificate management, audit trail
- Works closely with Legal & Compliance on sanctions list updates (lists are refreshed daily via automated pipeline, but Legal reviews additions manually within 24 hours)

### Frontend Team (16 people)
**Lead:** Emma Torres
- Owns: Nexus web app, Beacon dashboards, Sentinel UI, mobile app (React Native)
- Design system: "Forge" — internal component library built on Radix UI. Storybook at forge.internal.acme.dev

### QA & Release Engineering (8 people)
**Lead:** Viktor Nowak
- Owns: Test infrastructure, E2E test suites (Playwright), release pipelines, feature flag system (custom, built on Redis)
- Release cadence: weekly releases to production (Tuesdays), hotfixes as needed
- Feature flags: all new features behind flags, graduated rollout (internal → 10% → 50% → 100%)

### Security Team (6 people)
**Lead:** Aisha Mohammed
- Owns: AppSec, infrastructure security, SOC 2 compliance, penetration testing, bug bounty program (via HackerOne)
- Security review required for: any new external API endpoint, any change to auth/authz, any new data store, any new third-party dependency

### Data Team (8 people)
**Lead:** Chen Wei
- Owns: Data warehouse (Snowflake), internal analytics, ML model training infrastructure, data pipeline quality
- Provides internal dashboards for product metrics, customer health scores, system performance

### SRE Team (5 people)
**Lead:** Tom Bradley
- Owns: Production reliability, incident management, capacity planning, disaster recovery
- SLA target: 99.95% uptime for Nexus API (currently at 99.97% trailing 12 months)
- Incident commander rotation: weekly, covers all severity levels

---

## Product (15 people)

**VP of Product:** Lisa Nakamura

- **Nexus PM:** Michael Torres
- **Relay PM:** Amy Zhang
- **Beacon PM:** David Park
- **Sentinel PM:** Nadia Hassan
- **Design Lead:** Carlos Mendez (5 designers)
- **Product Ops:** Jennifer Liu (2 people — manages roadmap tooling, customer feedback pipeline, beta programs)

---

## Customer Success (45 people)

**VP of Customer Success:** Robert Kim

- **Enterprise CSMs** (12 people): Each manages 8–12 enterprise accounts. Quarterly business reviews, health score monitoring, expansion pipeline.
- **Mid-Market CSMs** (18 people): Each manages 25–40 accounts. Monthly check-ins, automated health alerts.
- **Implementation Team** (10 people): Handles new customer onboarding. Average implementation takes 6–8 weeks for Nexus + Relay. Sentinel adds 2–3 weeks.
- **Support Team** (5 people): L1/L2 support via Zendesk. SLA: P1 (system down) = 30 min response, P2 (degraded) = 2 hours, P3 (general) = 8 hours.

---

## Sales (55 people)

**VP of Sales:** Katherine Park

- **Enterprise AEs** (8 people): Deals > $100K ACV, 6–9 month sales cycle
- **Mid-Market AEs** (15 people): Deals $25K–$100K ACV, 2–4 month sales cycle
- **SDRs** (20 people): Outbound prospecting, 50 activities/day target
- **Sales Engineering** (8 people): Technical demos, POC management, RFP responses
- **Revenue Operations** (4 people): Salesforce admin, territory planning, comp plans, forecasting

---

## Marketing (20 people)

**VP of Marketing:** Hiroshi Tanaka

- **Content & SEO** (5 people): Blog, whitepapers, case studies. Target: 2 blog posts/week, 1 case study/month.
- **Demand Gen** (6 people): Paid campaigns, webinars, events. Target: 400 MQLs/month.
- **Product Marketing** (4 people): Positioning, competitive intel, launch plans. Maintains battle cards for top 5 competitors.
- **Brand & Design** (3 people): Website, brand guidelines, event materials.
- **Developer Relations** (2 people): API documentation, developer blog, community Slack. Manages docs.acme.dev.

---

## Finance & Operations (25 people)

**CFO:** Maria Santos

- **Finance** (8 people): FP&A, accounting, billing. NetSuite for ERP, Stripe for payment processing.
- **Legal** (4 people): Contracts, IP, compliance, privacy (GDPR/CCPA).
- **People Ops/HR** (8 people): Recruiting, L&D, comp & benefits, culture. Greenhouse for ATS.
- **IT** (5 people): Okta for SSO, Jamf for device management, Google Workspace, Slack Enterprise Grid.
