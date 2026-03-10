# ACME Org -- Quarterly Planning & OKRs

## Planning Cadence

ACME engineering follows a structured planning cadence designed to balance long-term strategy with short-term execution. The cadence was established in 2023 after a period of ad-hoc planning that led to misaligned priorities across teams.

### Annual Strategy Offsite

- **When:** First week of December
- **Where:** Portland HQ, Hawthorne conference center (3rd floor)
- **Duration:** 3 days (Monday through Wednesday)
- **Attendees:** All leadership (VPs, team leads, PMs, design lead). CEO and CTO attend all sessions.
- **Format:**
  - Day 1: Retrospective on the year. Each VP presents their team's wins, misses, and learnings. Open Q&A.
  - Day 2: Strategy sessions. CEO presents company vision and top-level goals. Breakout groups for each product area. Cross-functional workshops on key themes (e.g., "international expansion", "AI strategy", "platform scalability").
  - Day 3: Prioritization and rough annual roadmap. Dot-voting on initiatives. Stack-rank the top 15 engineering investments for the year. Output: a draft annual plan shared with the full company by mid-December.
- **Pre-work:** Each team lead submits a 2-page "State of the Team" document covering: team health, top accomplishments, biggest risks, and proposed initiatives for the next year. Due 2 weeks before the offsite.
- **Post-offsite:** CEO sends company-wide email summarizing key decisions. VP Eng publishes engineering priorities in Confluence. All-hands meeting in January to present the plan.

### Quarterly OKR Setting

The OKR process runs on a 2-week cycle at the start of each quarter:

**Week 1: Proposal Phase**
- Monday: VP Eng shares company-level objectives and key themes for the quarter
- Tuesday-Wednesday: Team leads draft team-level OKRs in collaboration with their PMs
- Thursday: Teams share draft OKRs in a shared Google Doc for cross-team visibility
- Friday: Async feedback round -- anyone in engineering can comment on any team's OKRs

**Week 2: Review and Finalize**
- Monday: Leadership review meeting (VP Eng, all team leads, VP Product). Walk through each team's OKRs. Challenge, refine, identify dependencies.
- Tuesday-Wednesday: Teams revise OKRs based on feedback. Dependencies documented in a shared dependency tracker (Jira epic with linked issues).
- Thursday: Final OKRs published in Confluence under `Engineering/OKRs/Q{N}-{Year}`
- Friday: VP Eng presents finalized OKRs at the monthly engineering all-hands

**OKR Scoring:**
- Scored at end of quarter on a 0.0 to 1.0 scale
- 0.7 is considered "on target" (OKRs are meant to be ambitious)
- Scores below 0.4 trigger a retrospective on why the KR was missed
- Scores are shared transparently across engineering -- no team operates in a vacuum
- OKR scores do not directly impact performance reviews, but patterns of persistent misses are discussed in 1:1s between VP Eng and team leads

**Mid-Quarter Check-in:**
- Week 6 of each quarter: 30-minute check-in per team with VP Eng
- Purpose: flag at-risk KRs early, request help, adjust scope if needed
- Format: traffic light (green/yellow/red) for each KR with brief explanation
- If a KR is red, VP Eng and team lead agree on a recovery plan or scope adjustment

### Sprint Planning

All engineering teams follow a 2-week sprint cadence, synchronized across teams:

- **Sprint start:** Monday morning
- **Sprint end:** Friday afternoon (2 weeks later)
- **Sprint planning:** Monday 10:00-11:30 AM PT. Each team runs their own planning meeting.
  - Review velocity from previous sprint
  - Pull items from the prioritized backlog (maintained by PM + team lead)
  - Capacity planning: account for on-call duty, PTO, tech debt allocation
  - Commitment: team agrees on sprint goal and committed items
- **Daily standup:** 15 minutes, async on Slack for remote-heavy teams (Sentinel, Data), synchronous on Zoom for others
- **Sprint review/demo:** Friday 2:00-3:00 PM PT. Rotating demo slots -- 2-3 teams demo each sprint. Open to all of engineering + product.
- **Sprint retrospective:** Friday 3:00-3:30 PM PT. Each team runs internally.

**Tooling:**
- Jira for sprint boards and backlog management
- Confluence for sprint goals, retro notes, and planning docs
- Slack channel per team for async standups (e.g., #team-nexus-core, #team-relay, etc.)
- Miro for sprint retro boards (virtual sticky notes)

### Monthly Engineering All-Hands

- **When:** Last Friday of each month, 1:00-2:30 PM PT
- **Format:**
  - VP Eng opens with company and engineering updates (10 min)
  - 2-3 team demos of recently shipped work (30 min total)
  - Tech talk: rotating slot for deep-dive on an interesting technical topic (20 min). Recent topics: "How we reduced Kafka consumer lag by 10x" (Marcus Webb), "Building the HS code classifier with Claude" (Raj Patel), "Playwright test parallelization" (Viktor Nowak)
  - Metrics review: key engineering metrics presented by SRE (15 min). Metrics: deploy frequency, MTTR, change failure rate, API uptime, CI build time
  - Open Q&A with VP Eng and team leads (15 min)
- **Recording:** All all-hands are recorded and posted to the #engineering Slack channel for async viewing
- **Attendance:** Optional but strongly encouraged. Typical attendance: 85-90%

---

## Current Quarter: Q1 2026 OKRs

Quarterly theme: **"Faster, broader, more reliable."** Focus on performance improvements across all products, expanding integration coverage, and hardening reliability for enterprise customers.

### Platform Team

**Team Lead:** Marcus Webb | **PM:** (shared with VP Eng)

**Objective: Improve developer productivity across all engineering teams**

| Key Result | Target | Current | Status | Notes |
|-----------|--------|---------|--------|-------|
| Reduce CI build time | 6 min | 8 min (was 12 min) | Yellow | Parallelized unit tests (saved 2 min). Working on Docker layer caching for integration tests. ETA: mid-March. Dependency on GitHub Actions runner upgrade (self-hosted runners need more CPU). |
| 95% of services on Go 1.22 | 95% | 78% | Yellow | 14 of 18 services migrated. Remaining: forecast-engine (Python, N/A), report-generator (blocked on chromedp compatibility), relay-agent (needs extensive testing due to plugin system), screening-service (in progress). |
| Launch internal developer portal | Launched | In progress | Green | Backstage instance deployed to staging. Service catalog populated for all 16 services. Working on CI/CD plugin integration and documentation aggregation. Target launch: March 21. |

**Sprint-level initiatives this quarter:**
- S1-S2: CI pipeline optimization (test parallelization, caching)
- S3-S4: Go 1.22 migration push (relay-agent and screening-service)
- S5-S6: Developer portal launch and adoption campaign
- Ongoing: acme-go-kit v2.0 development (structured logging, improved tracing, context propagation)

### Nexus Core Team

**Team Lead:** Priya Sharma | **PM:** Michael Torres

**Objective: Improve real-time accuracy of core platform data**

| Key Result | Target | Current | Status | Notes |
|-----------|--------|---------|--------|-------|
| Inventory position latency < 30 seconds | 30s | 60s (baseline), currently 45s | Yellow | Inventory Squad rewrote the CDC consumer to use batch processing instead of per-record commits. Reduced latency from 60s to 45s. Next step: move from polling-based CDC to WAL-based streaming (pglogical). Blocked on DBA review -- scheduled for March 12. |
| Carrier tracking coverage to 250 carriers | 250 | 200 (baseline), currently 218 | Yellow | Logistics Squad has integrated 18 new carriers. Remaining 32 carriers are regional carriers in APAC and LATAM. Dependency on Relay team for connector certification bandwidth. 10 connectors in certification pipeline. |
| Oracle forecast MAPE < 10% across all segments | 10% | 11.5% (baseline), currently 10.8% | Yellow | Intelligence Squad deployed updated model (oracle-v3.3) that incorporates weather signals more effectively. Improved from 11.5% to 10.8% overall. Two customer segments still above 12%: perishable goods and promotional/seasonal items. Working on segment-specific model tuning. |

**Key dependencies:**
- Carrier tracking KR depends on Relay team's connector certification capacity
- Inventory latency KR depends on Platform team for PostgreSQL pglogical setup
- Forecast KR requires Data team support for feature engineering pipeline improvements

**Risks:**
- Inventory latency target is aggressive. WAL-based streaming is a significant architectural change. May slip to Q2 if DBA review surfaces concerns.
- Some APAC carriers have poorly documented APIs. Integration velocity may slow as we work through these.

### Relay Team

**Team Lead:** James Okafor | **PM:** Amy Zhang

**Objective: Scale the connector ecosystem to support ACME's growth in new markets**

| Key Result | Target | Current | Status | Notes |
|-----------|--------|---------|--------|-------|
| Ship 5 new certified connectors | 5 | 3 | Green | Shipped: Sage Intacct v2 (full bidirectional sync), Maersk Container Tracking, SF Express. In progress: DB Schenker (certification week 2 of 3), Coupang Fulfillment (development). On track for 5 by end of quarter. |
| Reduce average connector certification time | 2 weeks | 3 weeks (baseline), currently 2.5 weeks | Yellow | Automated the staging validation phase with a new test harness (Relay Studio improvement). Saved ~3 days. Working on parallelizing beta customer canary testing to save another 2-3 days. Need to recruit 2 more beta customers willing to participate in canary testing. |
| Zero SEV-1 incidents caused by Relay | 0 | 1 (INC-2025-042) | Red | INC-2025-042 occurred January 18: SAP RFC connector memory leak caused cascading failure across 12 enterprise customers. Root cause: unbounded IDoc buffer. Mitigated in 45 minutes. Fix deployed in Relay 3.1.2 hotfix. No further SEV-1 incidents since. Action item: add memory consumption monitoring to all connectors (in progress, 8 of 15 connectors instrumented). |

**INC-2025-042 Post-Mortem Summary:**
- **What happened:** SAP RFC connector processing a 65MB IDoc batch for GlobalMart exceeded memory limits, crashed, and restarted in a loop. The restart loop caused Kafka consumer group rebalancing that affected 11 other enterprise customers on the same MSK cluster.
- **Root cause:** Missing memory limit on IDoc processing buffer. The connector allocated memory proportional to IDoc size with no cap.
- **Fix:** Added configurable memory limit (default 2GB, max 4GB). IDocs exceeding limit are split into smaller batches automatically.
- **Follow-up:** Isolate enterprise customer Kafka consumer groups to prevent cascading failures. Targeted for Q2.

**Sprint-level initiatives this quarter:**
- S1-S2: Sage Intacct v2 certification, Maersk connector development
- S3-S4: DB Schenker and SF Express certification, Relay Studio test harness improvements
- S5-S6: Coupang connector development and certification push, connector memory monitoring rollout

### Beacon Team

**Team Lead:** Sarah Kim | **PM:** David Park

**Objective: Make supply chain data self-serve for customers**

| Key Result | Target | Current | Status | Notes |
|-----------|--------|---------|--------|-------|
| 50% of Enterprise customers using SQL Playground | 50% | 32% | Yellow | Launched SQL Playground tutorial series (5 guides). Ran 2 webinars with CSM team. Adoption increased from 28% to 32%. Main blocker: customers find SQL intimidating. Working on a natural language query feature (using Claude API) to lower the barrier. Prototype expected by end of March. |
| Launch real-time streaming export to Snowflake | Launched | In development | Yellow | Architecture finalized: Kafka Connect with Snowflake Sink Connector. Schema registry integration complete. Currently testing with 3 beta customers. Blocker: Snowflake's Kafka connector has a bug with nested JSON arrays that affects our shipment event schema. Workaround: flatten the schema before export. Additional 1-week delay. Target launch: March 28. |
| Dashboard load time < 2 seconds p95 | 2.0s | 3.1s (baseline), currently 2.6s | Yellow | Implemented ClickHouse query caching for frequently-accessed dashboards. Reduced from 3.1s to 2.6s. Next optimization: pre-aggregate daily rollup tables for the 10 most common widget types. Expected to bring load time to ~2.0s. In progress, estimated completion: March 14. |

**Key dependencies:**
- Natural language query feature depends on budget approval for Claude API usage (estimated $2K/month for Enterprise tier customers)
- Snowflake streaming export depends on Snowflake support resolving their Kafka connector bug (ticket open with Snowflake support, escalated to their engineering team)

### Sentinel Team

**Team Lead:** Raj Patel | **PM:** Nadia Hassan

**Objective: Establish ACME as the leader in supply chain compliance**

| Key Result | Target | Current | Status | Notes |
|-----------|--------|---------|--------|-------|
| 100 customers on Sentinel | 100 | 67 | Yellow | 12 new customers onboarded this quarter (was 55 at Q4 end). Pipeline has 40+ qualified leads. Bottleneck: implementation team capacity. Each Sentinel onboarding requires custom screening rule configuration (2-3 days per customer). Working with Implementation team to create self-serve onboarding wizard. |
| HS code classification accuracy > 95% | 95% | 91% | Yellow | Upgraded to claude-sonnet-4-5-20250514 (from claude-3-haiku) for initial classification. Accuracy improved from 88% to 91%. Remaining gap: ambiguous product descriptions and multi-material items. Implementing a confidence threshold system: high-confidence (>0.95) auto-approved, medium (0.80-0.95) fast-track review, low (<0.80) full review. Expected to reach 93% by end of quarter, 95% target may slip to early Q2. |
| SOC 2 audit for Sentinel-specific controls | Complete | In progress | Green | Deloitte engaged. Audit scope defined: data isolation, screening accuracy validation, audit trail immutability, encryption controls. Evidence collection 70% complete. Audit fieldwork scheduled for March 17-21. On track for completion by end of March. |

**Risks:**
- HS code accuracy target is the most at-risk KR across all teams. The 91% to 95% jump requires both model improvements and better input data from customers.
- Sentinel onboarding capacity could limit customer acquisition KR. Self-serve wizard is critical path.

### Frontend Team

**Team Lead:** Emma Torres | **PM:** (shared across product PMs)

**Objective: Unify the customer experience across all ACME products**

| Key Result | Target | Current | Status | Notes |
|-----------|--------|---------|--------|-------|
| Migrate remaining AngularJS pages to React | 0 remaining | 4 of 12 remaining (8 migrated) | Yellow | Migrated 8 pages so far (started quarter with 12 remaining). Remaining 4: Relay configuration page (complex form logic), Beacon report builder (drag-and-drop), legacy admin panel (2 pages, low traffic). Relay config page is in progress (largest effort -- estimated 2 sprints). On track to complete 2-3 more by end of quarter; final 1-2 may slip to Q2. |
| Forge design system adoption > 90% of components | 90% | 74% (baseline), currently 82% | Yellow | Converted 18 more components to Forge this quarter. Remaining non-Forge components are mostly in Beacon (custom chart components) and the mobile app (React Native equivalents needed). Forge Storybook now has 95% documentation coverage. |
| Mobile app rating > 4.5 stars | 4.5 | 4.2 | Yellow | Shipped 3 mobile releases this quarter addressing top user complaints: push notification reliability (fixed), offline mode for shipment tracking (shipped), faster app launch time (reduced from 4.2s to 2.8s). Current rating: 4.3 (up from 4.2). Recent 30-day ratings averaging 4.4. Need to maintain momentum with 2 more releases focused on dashboard performance and search. |

**Sprint-level initiatives this quarter:**
- S1-S2: AngularJS migration (inventory detail page, shipment detail page)
- S3-S4: Forge component conversion push, mobile app performance improvements
- S5-S6: Relay config page migration, Beacon chart components in Forge

### SRE Team

**Team Lead:** Tom Bradley | **PM:** (shared with VP Eng)

**Objective: Bulletproof reliability for enterprise customers**

| Key Result | Target | Current | Status | Notes |
|-----------|--------|---------|--------|-------|
| 99.99% API uptime | 99.99% | 99.97% (trailing 12mo) | Red | Q1 uptime so far: 99.96%. Two incidents contributed to downtime: INC-2025-042 (Relay cascading failure, 23 min) and INC-2026-008 (RDS failover during AWS maintenance, 8 min). To reach 99.99% for Q1, we can afford only ~1.3 more minutes of downtime for the rest of the quarter. Aggressive target. Working on: automated failover testing, connection pool improvements, circuit breaker tuning. |
| MTTR < 15 minutes for SEV-1 | 15 min | 28 min average | Yellow | Q1 SEV-1 incidents: 2. MTTR for INC-2025-042: 45 min. MTTR for INC-2026-008: 8 min. Average: 26.5 min. Improvements: pre-built runbook automation scripts (3 of 6 runbooks automated so far), improved PagerDuty escalation (reduced acknowledgment time from 5 min to 2 min), incident commander training sessions held for all team leads. |
| Complete disaster recovery runbook and conduct drill | Complete | In progress | Green | DR runbook drafted covering: full region failover (us-west-2 to eu-west-1), database restore from backup, Kafka cluster rebuild, DNS failover. Review by VP Eng scheduled for March 14. DR drill planned for March 21 (Saturday, 6:00 AM PT to minimize customer impact). Coordination with AWS TAM (Jessica Wong) for support during drill. |

**Ongoing SRE initiatives:**
- Toil reduction: automating common incident remediation steps (target: 50% of SEV-3/4 incidents auto-remediated)
- Capacity planning: quarterly review of resource utilization. Current cluster utilization: 62% CPU, 71% memory. Headroom is sufficient through Q2.
- Cost optimization: identified $12K/month in savings from right-sizing RDS instances (3 instances over-provisioned). Change request submitted, pending DBA review.

---

## Retrospective Process

### Sprint Retrospectives

Each team conducts a 30-minute retrospective at the end of every sprint (Friday afternoon).

**Format: "What went well / What didn't / Action items"**

- Facilitated by team lead (or rotating facilitator for teams that prefer it)
- Miro board for virtual sticky notes -- each team member adds notes in 3 columns
- 5 minutes: silent brainstorming (add sticky notes)
- 10 minutes: group and discuss themes
- 10 minutes: identify top 2-3 action items
- 5 minutes: assign owners and due dates for action items

**Action item tracking:**
- Action items added to Jira as tasks with "retro-action" label
- Reviewed at the start of the next sprint retro: were they completed?
- If an action item persists for 3+ sprints without completion, it is escalated to VP Eng

**Sample recent retro action items (anonymized):**
- "Flaky E2E test in shipment tracking flow causing CI failures 2-3 times per sprint" -- Owner: Viktor Nowak. Resolution: identified race condition in test setup, fixed with explicit wait. Closed.
- "PR review turnaround time too slow for cross-team PRs" -- Owner: Priya Sharma. Resolution: created #pr-review Slack channel for cross-team review requests, established 24-hour SLA for first review. In progress.
- "On-call alerts too noisy, alert fatigue setting in" -- Owner: Tom Bradley. Resolution: reviewed all P3/P4 alerts, eliminated 15 alerts that were informational only, consolidated 8 duplicate alerts. Closed.

### Quarterly Retrospectives

Engineering-wide retrospective held in the first week of each quarter, looking back at the previous quarter.

- **Duration:** 90 minutes
- **Attendees:** All of engineering (120 people). Virtual via Zoom with breakout rooms.
- **Facilitated by:** VP Eng (Dana Chen) and a rotating co-facilitator from the team leads
- **Format:** Themed discussion. Each quarter focuses on a specific aspect of how engineering operates.

**Recent quarterly retro themes:**
- Q4 2025: "Incident Response" -- How well did we handle incidents last quarter? What can we improve in our process, tooling, and communication?
- Q3 2025: "Cross-Team Collaboration" -- Are teams able to work together effectively? What are the friction points? How can we improve shared ownership?
- Q2 2025: "Developer Experience" -- How productive do engineers feel? What tools, processes, or environments are slowing them down?

**Q4 2025 Quarterly Retro Outcomes (Incident Response theme):**
Key findings:
1. Incident commander rotation is working well, but some ICs feel undertrained. Action: mandatory IC training workshop (completed in January).
2. Post-mortem quality varies widely across teams. Action: VP Eng to review all post-mortems and provide feedback. Template updated with stricter sections.
3. Communication during incidents is inconsistent. Some teams post updates in #incidents, others use team channels. Action: all incident communication must go through #incidents with a standard update cadence (every 15 minutes for SEV-1, every 30 minutes for SEV-2).
4. Runbook coverage is incomplete. Only 6 of 16 services have comprehensive runbooks. Action: each team must have runbooks for all SEV-1 scenarios by end of Q1 2026.

### Annual Engineering Survey

- **Frequency:** Once per year (November)
- **Tool:** Culture Amp
- **Participation rate:** 92% in 2025 (up from 87% in 2024)
- **Anonymity:** Fully anonymous. Results aggregated at team level (minimum 5 responses per team to prevent identification).

**Key results from November 2025 survey:**
- Overall engagement score: 78% (up from 74% in 2024)
- "I understand how my work connects to company goals": 85% favorable
- "I have the tools and resources I need to do my job": 71% favorable (area of focus -- drove the developer portal initiative)
- "I feel comfortable raising concerns with my manager": 82% favorable
- "Cross-team collaboration is effective": 58% favorable (lowest score -- drove Q3 2025 quarterly retro theme)
- "On-call is fair and manageable": 65% favorable (improved from 52% after on-call compensation was introduced in Q3 2025)

**Action plan (published December 2025):**
1. Invest in developer tooling: launch developer portal, improve CI performance, invest in local development environments. Owner: Marcus Webb.
2. Improve cross-team collaboration: establish cross-team "guilds" for shared interests (e.g., Go guild, frontend guild, data guild). Quarterly guild meetups. Owner: Dana Chen.
3. On-call improvements: continue refining alert quality, expand on-call rotation to reduce individual burden, explore follow-the-sun model with London office. Owner: Tom Bradley.

---

## Tech Debt Tracking

### Policy

Each team allocates 20% of sprint capacity to tech debt reduction. This is enforced via sprint planning: PM and team lead agree on the 80/20 split for each sprint. In practice, this means ~2 story points per developer per sprint dedicated to tech debt.

Tech debt items are tracked in Jira with the "tech-debt" label. Each item includes:
- **Description:** What the debt is and why it matters
- **Impact:** What happens if we don't address it (e.g., increased incident risk, slower development velocity, security exposure)
- **Estimated effort:** T-shirt size (S/M/L/XL) and story points
- **Owner:** Team responsible
- **Priority:** P1 (address this quarter) / P2 (address within 6 months) / P3 (nice to have)

### Quarterly Tech Debt Review

Held in Week 3 of each quarter. VP Eng meets with all team leads for a 2-hour review session.

**Agenda:**
1. Review progress on previous quarter's tech debt items
2. Present new tech debt items identified during the quarter
3. Prioritize and assign owners
4. Ensure cross-team dependencies are identified

### Current Top Tech Debt Items (Q1 2026)

| ID | Description | Team | Priority | Effort | Status |
|----|-------------|------|----------|--------|--------|
| TD-101 | **AngularJS removal.** 4 remaining AngularJS pages in the web app. Blocks upgrade to Node 20 for the frontend build pipeline. | Frontend | P1 | XL (6+ sprints) | In progress -- 8 of 12 pages migrated |
| TD-102 | **Kafka consumer group isolation.** All enterprise customers share consumer groups on the same MSK cluster. Cascading failure risk (proven by INC-2025-042). | Relay / Platform | P1 | L (3 sprints) | Planned for Q2 |
| TD-103 | **forecast-engine Python to Go rewrite.** Only Python service in the stack. Different deployment pipeline, different monitoring, different dependency management. Maintenance burden. | Intelligence Squad | P2 | XL (8+ sprints) | Not started -- deferred to Q3 |
| TD-104 | **Metabase version upgrade.** Beacon embeds Metabase 0.47. Current stable is 0.51. Three security patches we are missing. | Beacon | P1 | M (2 sprints) | Scheduled for S5 |
| TD-105 | **Auth service token refresh race condition.** Under high load, concurrent token refresh requests can cause duplicate sessions. Low probability but has been observed in load tests. | Platform | P2 | S (1 sprint) | Not started |
| TD-106 | **ClickHouse schema migration tooling.** Currently manual SQL scripts. No version tracking. Risk of schema drift between staging and production. | Beacon | P2 | M (2 sprints) | In progress -- evaluating goose for ClickHouse |
| TD-107 | **Relay agent config drift.** Agent configs are manually managed per customer. No config versioning or drift detection. 3 incidents in Q4 caused by stale configs. | Relay | P1 | M (2 sprints) | In progress -- building config management in Relay control plane |
| TD-108 | **Deprecated AWS SDK v1 usage.** 5 services still use aws-sdk-go v1 (EOL). Need to migrate to aws-sdk-go-v2. | Platform | P2 | L (3 sprints) | In progress -- 3 of 8 services migrated |
| TD-109 | **Test data factory.** No standardized way to generate test data across services. Each team has their own fixtures, often stale. Causes flaky integration tests. | QA | P2 | M (2 sprints) | Not started -- Viktor drafting RFC |
| TD-110 | **Sentinel audit trail storage optimization.** Current storage model writes one row per event. At scale (FreshDirect generates 500K events/month), query performance degrades. Need to implement partitioning and archival strategy. | Sentinel | P1 | L (3 sprints) | Design phase -- architecture review March 12 |

### Tech Debt Burndown

**Q4 2025 results:**
- Started quarter with 14 P1/P2 tech debt items
- Closed 6 items
- Added 4 new items
- Net reduction: 2 items
- Total at end of Q4: 12 items

**Q1 2026 target:**
- Close 5 items (TD-101 partially, TD-104, TD-107, one more TBD)
- Acceptable additions: up to 3 new items
- Net reduction target: 2 items

---

## Cross-Team Dependency Management

### Dependency Tracker

Cross-team dependencies are tracked in a shared Jira epic: `ENG-DEPS-Q1-2026`. Each dependency is a linked issue with:
- **Requesting team:** Who needs the work done
- **Providing team:** Who owns the deliverable
- **Description:** What is needed
- **Deadline:** When it is needed by
- **Status:** Not started / In progress / Complete / Blocked

### Current Cross-Team Dependencies (Q1 2026)

| Dependency | Requesting Team | Providing Team | Deadline | Status |
|-----------|----------------|----------------|----------|--------|
| PostgreSQL pglogical setup for inventory CDC | Nexus Core | Platform | March 12 | In progress |
| Connector certification bandwidth for APAC carriers | Nexus Core (Logistics) | Relay | End of Q1 | In progress |
| Claude API budget approval for natural language queries | Beacon | Finance / VP Eng | March 15 | Pending |
| Snowflake Kafka connector bug workaround | Beacon | (External: Snowflake) | March 20 | Waiting on vendor |
| Self-serve onboarding wizard for Sentinel | Sentinel | Frontend | March 28 | In progress |
| Forge React Native component parity | Frontend (mobile) | Frontend (web) | Ongoing | In progress |
| Feature flag system migration to LaunchDarkly | QA/Release Eng | Platform | Q2 (descoped from Q1) | Not started |
| DR drill coordination and support | SRE | All teams | March 21 | Scheduled |

### Dependency Review Cadence

- **Weekly:** Team leads review dependency tracker in the Monday team lead sync (15 min standing agenda item)
- **Bi-weekly:** VP Eng reviews blocked dependencies and escalates if needed
- **Quarterly:** Full dependency audit during OKR planning process

---

## Historical OKR Performance

### Q4 2025 OKR Scores (Summary)

| Team | Objective | Score | Notes |
|------|----------|-------|-------|
| Platform | Modernize infrastructure foundations | 0.7 | Completed EKS upgrade, MSK upgrade. Missed: service mesh evaluation (descoped). |
| Nexus Core | Launch replenishment recommendations | 0.8 | GA launched on time. Strong customer adoption. Slightly missed accuracy target (78% vs 80% recommendation acceptance rate). |
| Relay | Improve connector reliability | 0.6 | 2 of 3 KRs met. Missed zero-downtime connector updates (architectural complexity underestimated). |
| Beacon | Launch SQL Playground | 0.9 | Launched ahead of schedule. Strong initial adoption. Exceeded query performance target. |
| Sentinel | Reach GA readiness | 0.8 | GA launched October 2025. 55 customers by end of Q4 (target was 50). Missed: automated screening rule templates. |
| Frontend | Mobile app v2.0 launch | 0.7 | Launched on time. App store rating improved from 3.8 to 4.2. Missed: offline mode (slipped to Q1). |
| SRE | Establish SRE practices | 0.8 | SLO framework implemented. Incident process formalized. Missed: automated capacity planning (deferred). |

### Overall Q4 2025 Engineering Score: 0.74

VP Eng assessment: "Solid quarter. We shipped significant features (Sentinel GA, Replenishment Recs, SQL Playground, Mobile v2.0) while maintaining reliability. Areas to improve: better estimation of architectural work (Relay connector updates), and more aggressive tech debt reduction."

### Q3 2025 OKR Scores (Summary)

| Team | Objective | Score | Notes |
|------|----------|-------|-------|
| Platform | Complete observability migration | 0.8 | Prometheus + Grafana fully operational. Datadog decommissioned on schedule. Missed: OpenTelemetry auto-instrumentation for all services (80% coverage vs 100% target). |
| Nexus Core | Expand carrier coverage to 200 | 0.9 | Hit 200 carriers ahead of schedule. Carrier onboarding process streamlined. Top performer this quarter. |
| Relay | Ship connector SDK v2.0 | 0.7 | SDK shipped. Documentation lagged behind -- only 60% of API surface documented at launch. 3 external partners started building connectors. |
| Beacon | ClickHouse migration from PostgreSQL | 0.7 | Migration complete for 90% of analytics queries. Remaining 10% required custom ClickHouse query optimization. Dashboard performance improved 3x on average. |
| Sentinel | Complete beta program | 0.8 | Beta completed with 12 customers. NPS score: 72. Key feedback incorporated: bulk screening API, improved classification confidence scores. |
| Frontend | Design system (Forge) v1.0 launch | 0.9 | Forge launched to all teams. 74% component adoption by end of quarter. Storybook documentation rated "excellent" in developer survey. |
| SRE | Reduce incident volume by 30% | 0.6 | Reduced by 18%. Alert tuning helped but root causes (Kafka consumer lag, database connection pool exhaustion) required deeper fixes that spanned multiple teams. |

### Overall Q3 2025 Engineering Score: 0.77

---

## Engineering Guilds

Cross-team guilds were established in Q4 2025 to improve collaboration and knowledge sharing. Each guild meets monthly and maintains a Slack channel.

### Active Guilds

| Guild | Channel | Lead | Members | Focus |
|-------|---------|------|---------|-------|
| Go Guild | `#guild-go` | Marcus Webb | 45 | Go best practices, library reviews, language updates, performance optimization |
| Frontend Guild | `#guild-frontend` | Emma Torres | 22 | React patterns, Forge design system, accessibility, performance |
| Data Guild | `#guild-data` | Chen Wei | 18 | Data modeling, pipeline patterns, ML/AI, analytics best practices |
| Security Guild | `#guild-security` | Aisha Mohammed | 25 | Security awareness, threat modeling, secure coding practices |
| Platform Guild | `#guild-platform` | Tom Bradley | 30 | Kubernetes, observability, CI/CD, infrastructure patterns |
| API Guild | `#guild-api` | Priya Sharma | 20 | API design standards, versioning, documentation, developer experience |

**Guild meeting format:**
- Monthly, 45 minutes, open to anyone in engineering
- Lightning talk (15 min): one member presents a topic of interest
- Discussion (20 min): open floor for questions, challenges, proposals
- Action items (10 min): any standards or decisions that need follow-up

**Recent guild highlights:**
- Go Guild (February 2026): Marcus presented Go 1.22 generics improvements and how acme-go-kit v2.0 leverages them. Decided to standardize on slog for structured logging across all services.
- Frontend Guild (February 2026): Emma demonstrated the new Forge data table component. Discussion on React Server Components -- decision: not ready for ACME's architecture yet, revisit in Q3.
- Security Guild (January 2026): Aisha presented findings from the January 2026 penetration test. Live demo of the 3 medium findings and how they were remediated. Team committed to adding SAST rules to prevent similar issues.
- API Guild (January 2026): Priya proposed API versioning standard: URL-based versioning (/v1/, /v2/) for breaking changes, additive changes allowed within a version. Ratified by all team leads.

---

## Upcoming Planning Events

### Q2 2026 OKR Planning

- **Dates:** March 30 -- April 10, 2026
- **Theme (preliminary):** "Scale for the next 1,000 customers" -- focus on multi-tenancy improvements, performance at scale, and international expansion readiness
- **Key inputs:**
  - Q1 2026 OKR scores (available March 28)
  - Product roadmap priorities from VP Product (Lisa Nakamura)
  - Customer feedback themes from Q1 QBRs (from VP Customer Success, Robert Kim)
  - Technical risk register from SRE quarterly review (from Tom Bradley)
  - Hiring plan updates from People Ops (4 new engineers starting in Q2)

### Annual Strategy Offsite 2026

- **Dates:** December 1-3, 2026 (tentative)
- **Location:** Portland HQ (confirmed)
- **Key themes being discussed for 2027 planning:**
  - International expansion: EU customer base growing 40% YoY, may need dedicated EU engineering team
  - AI/ML strategy: expanding Claude API usage beyond Sentinel to other products (natural language queries in Beacon, intelligent alerts in Nexus)
  - Platform maturity: service mesh evaluation, event-driven architecture evolution, potential move from EKS to managed serverless for some workloads
  - Hiring plan: engineering headcount target of 150 by end of 2027 (from 120 today)

### VP Engineering Hire

Dana Chen currently holds a dual CTO/VP Eng role. The company is hiring a dedicated VP Engineering in Q2 2026 to allow Dana to focus on technology strategy.

- **Status:** Active search. Recruiter engaged (external: True Search).
- **Timeline:** Offer target by end of April 2026, start date target June/July 2026.
- **Scope:** The new VP Eng will own: team structure, hiring, performance management, sprint processes, tech debt strategy, and engineering culture. Dana retains: technology vision, architecture decisions, vendor/partner relationships, and external-facing technical leadership.
- **Impact on planning:** Q3 2026 OKR process will be the first led by the new VP Eng. Transition plan includes a 4-week overlap period where Dana and the new VP Eng co-lead the planning process.
