# ACME Org — Internal Policies

**Last updated:** 2026-02-15
**Owner:** People Ops, Legal, and Engineering Leadership
**Questions?** Slack: #policy-questions or email policy@acme.dev

This document contains ACME's internal policies relevant to engineering and general operations. All employees are expected to read and follow these policies. Violations should be reported per the procedures described in each section.

---

## Table of Contents

1. [Remote Work Policy](#remote-work-policy)
2. [On-Call Compensation Policy](#on-call-compensation-policy)
3. [Code of Conduct](#code-of-conduct)
4. [Expense Policy](#expense-policy)
5. [Travel Policy](#travel-policy)
6. [Open Source Contribution Policy](#open-source-contribution-policy)
7. [Data Classification Policy](#data-classification-policy)
8. [AI Tools Policy](#ai-tools-policy)
9. [Vendor Security Review Process](#vendor-security-review-process)
10. [Incident Communication Policy](#incident-communication-policy)

---

## Remote Work Policy

**Effective date:** 2024-01-01
**Applies to:** All employees
**Policy owner:** People Ops

### Overview

ACME is a remote-friendly company. Employees may work fully remote, from the Portland headquarters, or any combination. We believe great work happens when people have flexibility over where and how they work.

### Core Hours

All employees are expected to be available during **core hours: 10:00 AM to 3:00 PM Pacific Time**, Monday through Friday. Core hours exist so that teams can schedule meetings, pair on problems, and collaborate synchronously. Outside of core hours, work when you are most productive.

If you are in a timezone where core hours are unreasonable (e.g., Singapore, which is UTC+8 making Pacific core hours 1:00 AM - 6:00 AM), work with your manager to establish an alternative overlap window. The expectation is at least 3 hours of overlap with your team's primary timezone.

### Home Office Stipend

All remote employees receive a one-time **$1,500 home office stipend** to set up a productive workspace. This covers furniture (desk, chair), lighting, and other ergonomic needs.

**How to claim:**
1. Purchase items within your first 90 days
2. Submit receipts via Expensify tagged as "Home Office Setup"
3. Reimbursement processes within 2 weeks

Additional equipment needs beyond the stipend should be discussed with your manager. Ergonomic assessments are available upon request through People Ops.

### Internet Stipend

Remote employees receive a **$75/month internet stipend** to offset the cost of home internet. This is added to your paycheck automatically — no expense report needed.

### Quarterly In-Person Gatherings

Each team holds a **quarterly in-person gathering** at the Portland headquarters (or another location chosen by the team lead with VP approval). These are typically 3-4 days and include:

- Strategic planning and roadmap review
- Hackathon or collaborative project work
- Team social activities
- Cross-team networking events

**Travel and accommodation are fully covered** by the company. See the Travel Policy for booking details.

Attendance at quarterly gatherings is expected but not mandatory. If you cannot attend, let your manager know at least 2 weeks in advance.

### Workspace Requirements

Regardless of where you work, you must:
- Have a reliable internet connection (minimum 25 Mbps down / 5 Mbps up)
- Use your company-issued laptop with Jamf MDM profile active
- Work from a location where you can take video calls with reasonable privacy
- Not work from public networks (coffee shops, airports) for extended periods without using the company device trust profile. Short-duration work is acceptable.
- Comply with data handling policies regardless of location

### International Remote Work

Working from outside your country of employment for more than 14 consecutive days requires advance approval from People Ops and Legal due to tax and employment law implications. Submit requests at least 30 days in advance via the #people-ops Slack channel.

---

## On-Call Compensation Policy

**Effective date:** 2025-01-01
**Applies to:** All engineers on on-call rotations
**Policy owner:** VP of Engineering

### Compensation Structure

| Component | Amount | Notes |
|-----------|--------|-------|
| Weekly on-call stipend | $500 | Paid for each week you carry the pager |
| After-hours page | $200 per page | Pages received between 6:00 PM and 9:00 AM PT, or on weekends/holidays |
| Comp day | 1 day | Earned after each on-call week, use within 60 days |

### Rotation Rules

- No engineer shall be on-call more than **1 week out of every 4 weeks**
- Minimum 2 weeks between on-call rotations for the same engineer
- On-call weeks run Monday 9:00 AM PT to Monday 9:00 AM PT
- Swaps are allowed and encouraged — coordinate via PagerDuty and notify your team lead
- If you are on-call during a company holiday, you receive the after-hours page rate for any pages during the holiday, plus an additional comp day

### Expectations During On-Call

- **Response time:** Acknowledge PagerDuty alerts within 5 minutes
- **Availability:** Must be reachable and able to get to a laptop within 15 minutes during on-call hours
- **Escalation:** If you cannot resolve an issue within 30 minutes, escalate to secondary on-call (Platform/SRE)
- **Handoff:** At the end of your on-call week, write a handoff note in #on-call summarizing any ongoing issues, things to watch, and open action items
- **Sobriety:** While on-call, you should be in a state where you can effectively debug production systems. Use your judgment.

### Opting Out

If you have a personal situation that makes on-call difficult for a period (medical, family, etc.), talk to your manager. We will accommodate without requiring details. No one should feel pressured to be on-call when they cannot do it safely and effectively.

### New Hire Ramp

New engineers shadow on-call during their first month (no pager responsibility) and join the rotation in their second or third month, at the team lead's discretion. See the Onboarding Guide for details.

---

## Code of Conduct

**Effective date:** 2023-06-01
**Applies to:** All employees, contractors, and vendors
**Policy owner:** People Ops and Legal

### Our Commitment

ACME is committed to providing a welcoming, inclusive, and harassment-free environment for everyone, regardless of age, body size, disability, ethnicity, sex characteristics, gender identity and expression, level of experience, education, socio-economic status, nationality, personal appearance, race, religion, or sexual identity and orientation.

### Expected Behavior

All ACME team members are expected to:

- **Be respectful.** Treat everyone with dignity. Disagree constructively and assume good intent.
- **Be inclusive.** Actively seek out and include diverse perspectives. Use inclusive language. Respect people's pronouns.
- **Be professional.** Maintain a standard of conduct appropriate for a professional workplace, whether in person, on video, or in written communication (Slack, email, code reviews, etc.).
- **Be collaborative.** Share knowledge freely. Help others succeed. Celebrate wins together.
- **Be accountable.** Own your mistakes. Learn from them. Do not blame others.

### Unacceptable Behavior

The following behaviors are not tolerated at ACME:

- Harassment of any kind, including but not limited to: unwelcome sexual attention, offensive comments related to personal characteristics, deliberate intimidation, stalking, or sustained disruption of discussions
- Discrimination based on any protected characteristic
- Bullying, including repeated unreasonable behavior directed at an individual or group that creates a risk to health and safety
- Retaliation against anyone who reports a concern or participates in an investigation
- Sharing others' private information (doxxing) without explicit permission
- Violent threats or language directed against another person
- Sexualized language or imagery in work contexts (Slack, presentations, code comments, etc.)

### In Code Review

Code review is a particularly important context for professional conduct. We expect:

- Critique the code, not the person. "This function could be simplified" not "Why did you write it this way?"
- Explain the "why" behind suggestions, not just the "what"
- Acknowledge good work. A simple "Nice approach!" goes a long way.
- Be timely. Review PRs within 1 business day. If you cannot, let the author know.
- Use conventional comments (e.g., `nit:`, `suggestion:`, `question:`, `blocking:`) to clarify the weight of your feedback

### Reporting Process

If you experience or witness behavior that violates this Code of Conduct:

1. **Direct resolution (optional):** If you feel safe doing so, address the behavior directly with the person. Many issues are unintentional and can be resolved through conversation.
2. **Report to your manager:** Your manager can help mediate and determine appropriate next steps.
3. **Report to People Ops:** Email conduct@acme.dev or use the anonymous reporting form (link in Okta under "Report a Concern"). All reports are treated confidentially.
4. **Report to Legal:** For serious concerns or if you are not comfortable with other channels, contact General Counsel Patricia Nguyen directly.

### Consequences

Violations of this Code of Conduct may result in:
- Verbal warning and coaching
- Written warning
- Required training or counseling
- Temporary suspension
- Termination of employment

The severity of consequences depends on the nature, frequency, and impact of the violation. All investigations are conducted by People Ops with Legal oversight.

---

## Expense Policy

**Effective date:** 2025-06-01
**Applies to:** All employees
**Policy owner:** Finance

### General Principles

Spend company money as if it were your own. Be reasonable. When in doubt, ask your manager before making the purchase.

### Approval Thresholds

| Amount | Approval Required |
|--------|-------------------|
| Up to $500 | No pre-approval needed (submit receipt after) |
| $500 - $2,000 | Manager approval (Slack message or email is fine) |
| $2,000 - $10,000 | VP approval |
| Over $10,000 | CFO approval |

### Meals & Entertainment

| Category | Limit | Notes |
|----------|-------|-------|
| Meals while traveling | $75/day | Includes all meals and tips. Alcohol limit: 2 drinks, reasonable price. |
| Team meals (local) | $50/person | For team events, celebrations, offsites. Manager approval for groups > 5. |
| Client meals | $100/person | Must include a business purpose on the receipt |
| Coffee / snacks | $15/day | While traveling or working from a coworking space |

### Software & Subscriptions

- **Under $50/month:** Purchase and expense. No approval needed.
- **$50-$200/month:** Manager approval.
- **Over $200/month:** Must go through Vendor Security Review (see below) and procurement.

### Professional Development

Each engineer has a **$2,000/year professional development budget** for:
- Books and ebooks
- Online courses (Udemy, Coursera, O'Reilly, etc.)
- Conference tickets (separate from company-sponsored travel — see Travel Policy)
- Certification exams

No pre-approval needed for individual purchases under $500. Just submit the receipt with "Professional Development" as the category.

### How to Submit Expenses

1. Submit all expenses via **Expensify** (access via Okta)
2. Attach receipt photos (required for all expenses over $25)
3. Include business purpose and attendees (for meals)
4. Submit within 30 days of the expense
5. Reimbursement processes in the next payroll cycle (bi-monthly)

### Corporate Card

Engineers at L5 (Senior) and above may request a corporate Amex card for frequent business expenses. Request via #finance Slack channel. Spending limits match the approval thresholds above.

---

## Travel Policy

**Effective date:** 2025-06-01
**Applies to:** All employees
**Policy owner:** Finance

### Booking

All travel must be booked through **TripActions** (access via Okta). This ensures:
- Negotiated corporate rates
- Duty of care and traveler tracking
- Automatic expense reporting (flights and hotels flow directly to Expensify)
- Carbon offset tracking

If TripActions does not have availability, you may book directly and submit for reimbursement, but notify your manager first.

### Flights

- **Domestic flights:** Economy class for flights under 4 hours. Premium economy for flights 4+ hours.
- **International flights:** Premium economy. Business class for flights over 8 hours with VP approval.
- **Book at least 14 days in advance** when possible for best rates. Last-minute bookings require manager justification.
- **Loyalty programs:** You may keep personal miles and points earned on business travel.

### Hotels

- **Nightly rate limit:** $250/night in most cities, $350/night in high-cost cities (SF, NYC, London, Singapore, Tokyo)
- **Extended stays (5+ nights):** Consider Airbnb or corporate housing. Finance can help arrange.
- **Room type:** Standard room. Suites require VP approval.

### Ground Transportation

- **Rideshare (Uber/Lyft):** Preferred over rental cars for short distances. Use the ACME business profile for automatic expensing.
- **Rental cars:** Book via TripActions when needed. Insurance is covered through the corporate policy — decline the rental company's insurance.
- **Public transit:** Always reimbursable. No receipt required under $25.

### Company-Sponsored Travel

The following travel is fully covered by ACME:

| Type | Frequency | Details |
|------|-----------|---------|
| Quarterly team gatherings | 4x/year | Portland HQ or team-chosen location. 3-4 days. |
| Conferences | 2/year per engineer | Pre-approved list: KubeCon, GopherCon, QCon, re:Invent, etc. Others with manager approval. |
| Customer visits | As needed | Sales Engineering and CSM-coordinated. VP approval for international. |
| Company all-hands | 2x/year | All-company event in Portland. 2 days. |

### Conference Attendance

Engineers are encouraged to attend up to 2 conferences per year:

1. Discuss with your manager which conferences align with your role and growth goals
2. Register via TripActions (conference tickets) and book travel
3. If you are speaking at a conference, the company covers all costs regardless of the 2/year limit
4. Write a brief trip report (1 page) and share in #engineering within a week of returning
5. If the conference has relevant sessions, consider sharing key takeaways in a tech talk

---

## Open Source Contribution Policy

**Effective date:** 2025-03-01
**Applies to:** All engineers
**Policy owner:** VP of Engineering and Legal

### Our Philosophy

ACME benefits enormously from open source software. We encourage contributing back to the communities we depend on. At the same time, we need to protect our intellectual property and ensure contributions do not create legal or security risks.

### Types of Contributions

#### 1. Bug Fixes & Small Contributions to Existing Projects

**Approval:** Team lead verbal approval
**Time budget:** Up to 40 hours total per quarter
**Requirements:**
- Must not include ACME proprietary code or business logic
- Must not disclose ACME infrastructure details, security configurations, or customer information
- Use your @acme.dev email for commits
- Contributions must be under the project's existing license (or Apache 2.0 if the project accepts multiple)

**Examples:** Fixing a bug in a Go library we use, improving documentation for a Kubernetes operator, adding a feature to an open source tool we depend on.

#### 2. Significant Contributions to Existing Projects

**Approval:** VP of Engineering
**Time budget:** Agreed upon with manager (may be part of your quarterly goals)
**Requirements:**
- Same as above, plus:
- Written plan describing the contribution, estimated time, and benefit to ACME
- Legal review if the project has a CLA (Contributor License Agreement)

**Examples:** Adding a major feature to an open source project, becoming a maintainer of a project we depend on.

#### 3. New Open Source Projects (Publishing ACME Code)

**Approval:** VP of Engineering + Legal (Patricia Nguyen)
**Requirements:**
- Written proposal including: what the project is, why it should be open source, competitive risk assessment, maintenance plan
- Security review by Security team to ensure no proprietary code, secrets, or internal references
- All new ACME-originated projects must be published under **Apache 2.0** license
- ACME retains copyright: `Copyright 2026 ACME Org, Inc.`
- README must include contribution guidelines and code of conduct
- Must have CI/CD pipeline for tests before publishing
- Ongoing maintenance commitment from the publishing team (minimum 1 year)

### During Work Hours

Contributing to open source during work hours is permitted and encouraged, provided:
- It does not interfere with your sprint commitments
- Your manager is aware
- The contribution aligns with ACME's technology stack or business interests
- Time is tracked in Jira under the "Open Source" epic

### Intellectual Property

- Code you write as part of your employment at ACME is owned by ACME (per your employment agreement)
- Open source contributions made during work hours or using company resources are ACME property and must follow this policy
- Personal open source projects on your own time, using your own equipment, on topics unrelated to ACME's business, are yours. If you are unsure whether a personal project overlaps, ask Legal.

---

## Data Classification Policy

**Effective date:** 2025-01-01
**Applies to:** All employees
**Policy owner:** Security Team (Aisha Mohammed)

### Classification Levels

All data at ACME falls into one of four classification levels. When in doubt, classify higher.

### Level 1: Public

**Definition:** Information that is intentionally shared with the public and whose disclosure poses no risk.

**Examples:**
- Published blog posts, case studies, and whitepapers
- Public-facing API documentation (docs.acme.dev)
- Open source code published by ACME
- Press releases and marketing materials
- Job postings

**Handling requirements:**
- No special handling required
- Can be shared freely via any channel
- No encryption requirements beyond standard TLS for web

### Level 2: Internal

**Definition:** Information intended for ACME employees that is not sensitive but should not be shared externally.

**Examples:**
- Internal wiki pages and documentation
- Engineering architecture diagrams and design docs
- Team meeting notes and sprint plans
- Internal Slack conversations
- Org charts and employee directories
- Non-sensitive source code (application logic, tests)

**Handling requirements:**
- Share only within ACME (Slack, Google Drive, GitHub org)
- Do not post on public forums, social media, or personal blogs
- Okay to discuss in general terms externally ("we use Kafka") but not specific implementation details
- Standard encryption at rest and in transit (this is default for all our systems)

### Level 3: Confidential

**Definition:** Sensitive information whose unauthorized disclosure could harm ACME, our customers, or our business partners.

**Examples:**
- Customer data (names, contacts, business data, shipment data)
- Financial data (revenue, ARR, burn rate, pricing details)
- Customer contracts and agreements
- Security configurations, vulnerability reports, pentest results
- Unreleased product plans and roadmap details
- Employee compensation and performance data
- Competitive intelligence and battle cards
- API keys, tokens, and credentials
- Source code for security-critical systems (auth-service, screening-service)

**Handling requirements:**
- Access restricted to employees who need it for their job function
- Must be stored in approved systems (Google Drive with restricted sharing, 1Password for credentials, AWS Secrets Manager for service credentials)
- Must not be stored on personal devices, personal cloud storage, or USB drives
- Must not be shared via email to external parties without encryption (use Google Workspace confidential mode or password-protected attachments)
- Must not be pasted into external tools or websites (including AI tools — see AI Tools Policy)
- Must be encrypted at rest (AES-256) and in transit (TLS 1.3)
- Access is logged and auditable

### Level 4: Restricted

**Definition:** Highly sensitive information whose unauthorized disclosure could cause severe harm, legal liability, or regulatory violations.

**Examples:**
- Customer PII (personally identifiable information): names, addresses, emails, phone numbers
- Sanctions screening results and denied party match details
- SOC 2 audit reports and detailed compliance findings
- Database backups containing customer data
- Encryption keys and root certificates
- Production database credentials
- Customer authentication tokens
- GDPR-regulated data (EU customer personal data)
- Trade secret algorithms (Oracle forecast model weights, screening matching algorithms)

**Handling requirements:**
- All Confidential handling requirements, plus:
- Access requires explicit approval from data owner (typically a team lead or VP)
- Access is time-limited where possible (break-glass procedure for production access)
- Must be stored in systems with row-level access controls or dedicated secure storage
- Must never be copied to development or staging environments without anonymization/pseudonymization
- Must never be included in logs, error messages, or monitoring data
- Retention policies must be followed (see Data Retention section in Security & Compliance)
- Any breach or suspected breach of Restricted data requires immediate notification to Security team (security@acme.dev or #security-incidents Slack)
- Annual access review conducted by Security team

### Data Classification Decision Tree

When you encounter data and are unsure how to classify it:

1. Does it contain customer PII, credentials, or trade secrets? **Restricted**
2. Does it contain customer business data, financial info, or security details? **Confidential**
3. Is it intended for internal use only? **Internal**
4. Is it already public or intended to be public? **Public**
5. Still unsure? Ask in #security-questions or classify as Confidential until reviewed.

---

## AI Tools Policy

**Effective date:** 2025-09-01
**Applies to:** All employees, with specific sections for engineers
**Policy owner:** VP of Engineering and Security Team

### Approved AI Tools

| Tool | Status | Use Case | Notes |
|------|--------|----------|-------|
| Claude (Anthropic) | Approved | Code assistance, writing, analysis | Enterprise plan with data retention off. Use via API or claude.ai with ACME workspace. |
| GitHub Copilot | Approved | Code completion, in-editor assistance | Business plan. Configured to not retain code snippets. Telemetry disabled. |
| Claude Code | Approved | CLI-based coding assistance | Enterprise plan. Same data policies as Claude. |

### Not Approved

| Tool | Status | Reason |
|------|--------|--------|
| ChatGPT (OpenAI) | Not approved | Data retention concerns. OpenAI's enterprise terms do not meet our data handling requirements as of last review (2025-08). |
| Google Gemini | Under review | Awaiting completion of vendor security review. Expected decision Q2 2026. |
| Cursor | Under review | Vendor security review in progress. |
| Local/self-hosted LLMs | Case-by-case | Must be approved by Security team. No customer data processing without full security review. |

### Rules for All AI Tool Usage

**What you CAN do:**
- Use approved AI tools for code generation, debugging, writing documentation, and learning
- Paste Internal-classified source code into approved tools (they have enterprise agreements with data protection)
- Use AI tools to help with code review, test writing, and refactoring
- Use AI for drafting internal communications, design docs, and RFCs
- Use AI for analyzing public data, open source code, and publicly available documentation

**What you CANNOT do:**
- Enter **any customer data** into any AI tool, approved or not. This includes customer names, shipment data, inventory data, PII, and any data classified as Confidential or Restricted.
- Enter **credentials, API keys, tokens, or secrets** into any AI tool
- Use AI tools to generate code that handles authentication, authorization, or cryptographic operations without thorough manual review by the Security team
- Use non-approved AI tools for any work-related purpose
- Share AI-generated output externally without human review and approval
- Rely on AI-generated code without testing. All AI-generated code must pass the same review, testing, and CI/CD standards as human-written code.
- Use AI to make decisions about customer accounts, access, or data without human oversight

### Code Review Requirements for AI-Generated Code

AI-generated code is subject to the same code review standards as all other code. Additionally:

- **Label AI-assisted PRs** with the `ai-assisted` GitHub label so reviewers know to pay extra attention
- **Review AI-generated code line by line.** Do not assume it is correct because it "looks right."
- **Test AI-generated code thoroughly.** AI models can produce code that passes superficial review but has subtle bugs, security issues, or performance problems.
- **Verify licensing.** AI tools may generate code similar to open source projects. If you suspect the generated code is substantially similar to a specific open source project, check the license compatibility.

### Reporting Concerns

If you believe someone is using AI tools in violation of this policy, or if you accidentally entered sensitive data into an AI tool:
1. Immediately notify the Security team: security@acme.dev or #security-incidents Slack
2. Do not try to "undo" it yourself — the Security team will guide remediation
3. No punitive action for accidental violations reported promptly and in good faith

---

## Vendor Security Review Process

**Effective date:** 2025-01-01
**Applies to:** Any vendor, tool, or service that will access, process, or store ACME or customer data
**Policy owner:** Security Team (Aisha Mohammed)

### When Is a Review Required?

A vendor security review is required when:
- A new vendor will have access to ACME systems or data
- A new SaaS tool will be used to process Internal, Confidential, or Restricted data
- An existing vendor changes their data handling practices, terms of service, or undergoes a security incident
- Annual re-review of existing vendors with data access (triggered automatically by Security team)

### Review Process

#### Step 1: Submit Request

Submit a vendor review request in #security-reviews Slack channel using the `/vendor-review` workflow. Provide:
- Vendor name and website
- What data they will access (classification level)
- Business justification
- Urgency (standard: 2 weeks, expedited: 1 week with VP approval)
- Link to vendor's security documentation (SOC 2 report, privacy policy, etc.)

#### Step 2: Security Questionnaire

The Security team sends the vendor our **ACME Vendor Security Questionnaire** (based on SIG Lite with ACME-specific additions). Key areas:

| Category | What We Assess |
|----------|---------------|
| Data Protection | Encryption at rest and in transit, data residency, retention policies, deletion procedures |
| Access Control | Authentication mechanisms, RBAC, audit logging, privileged access management |
| Incident Response | Breach notification timeline, incident management process, communication procedures |
| Compliance | SOC 2, ISO 27001, GDPR compliance, CCPA compliance, relevant industry certifications |
| Infrastructure | Cloud provider, data center security, backup and disaster recovery |
| Personnel | Background checks, security training, access provisioning/deprovisioning |
| Third Parties | Sub-processors, data sharing with third parties, supply chain security |
| Business Continuity | Uptime SLA, RTO/RPO, disaster recovery testing frequency |

#### Step 3: Review & Decision

The Security team reviews the questionnaire and any supporting documentation (SOC 2 Type II reports, pentest summaries, etc.). Possible outcomes:

- **Approved:** Vendor meets all requirements. Approved for use with specified data classification level.
- **Approved with conditions:** Vendor meets most requirements but has gaps that can be mitigated. Conditions documented and tracked.
- **Denied:** Vendor does not meet minimum security requirements. Engineering must find an alternative.

#### Step 4: Ongoing Monitoring

- **Annual re-review:** All approved vendors with data access are reviewed annually
- **Incident notification:** If a vendor experiences a security breach, their approval is suspended pending review
- **Contract requirements:** All vendor contracts must include our standard security addendum (available from Legal)

### Currently Approved Vendors

For a list of currently approved vendors, see the Vendor Registry in 1Password (shared vault: "Security — Vendor Approvals").

### Timeline

| Request Type | SLA |
|-------------|-----|
| Standard review | 2 weeks |
| Expedited review (VP approval required) | 1 week |
| Annual re-review | 4 weeks (initiated by Security team) |
| Emergency review (security incident) | 48 hours |

---

## Incident Communication Policy

**Effective date:** 2025-04-01
**Applies to:** All employees involved in incident response
**Policy owner:** VP of Engineering and VP of Customer Success

### Purpose

When things go wrong, clear and timely communication protects our customers' trust and our reputation. This policy defines who communicates, when, how, and what during customer-impacting incidents.

### Roles

| Role | Who | Responsibility |
|------|-----|---------------|
| Incident Commander (IC) | Rotating weekly (SRE + team leads) | Owns the incident end-to-end. Coordinates all response activities. |
| Technical Lead | Assigned by IC from relevant team | Drives diagnosis and remediation. |
| Communications Lead | Assigned by IC (typically CSM or Support) | Owns all external and internal communication. |
| Executive Sponsor | VP Eng (SEV-1/2) or Team Lead (SEV-3) | Decision-making authority, customer escalation contact. |

### Communication Timeline

#### SEV-1 (Critical — Complete Outage)

| Time | Action | Owner |
|------|--------|-------|
| T+0 | Declare incident in #incidents Slack. Page on-call. | First responder |
| T+5 min | IC acknowledged. War room (Zoom) opened. | IC |
| T+10 min | Internal status update in #incidents. | IC |
| T+15 min | Status page updated: "Investigating" | Comms Lead |
| T+15 min | Enterprise CSMs notified via #csm-alerts to prepare customer comms | Comms Lead |
| T+30 min | Customer notification: email to affected accounts + status page update | Comms Lead |
| T+30 min | VP Eng and CEO notified | IC |
| Every 30 min | Status page update with current state | Comms Lead |
| Resolution | Status page: "Resolved" with summary | Comms Lead |
| Resolution +2 hrs | Customer email: incident resolved, brief summary, next steps | Comms Lead |
| Resolution +48 hrs | Post-mortem published internally. Customer-facing summary sent to affected Enterprise accounts. | IC + Comms Lead |

#### SEV-2 (Major — Significant Degradation)

| Time | Action | Owner |
|------|--------|-------|
| T+0 | Declare incident in #incidents | First responder |
| T+10 min | IC acknowledged. Investigation begins. | IC |
| T+20 min | Status page updated if customer-facing impact confirmed | Comms Lead |
| T+30 min | Enterprise CSMs notified for affected accounts | Comms Lead |
| Every 60 min | Status page update | Comms Lead |
| Resolution | Status page: "Resolved" | Comms Lead |
| Resolution +24 hrs | Internal post-mortem | IC |

#### SEV-3 (Minor — Limited Impact)

| Time | Action | Owner |
|------|--------|-------|
| T+0 | Noted in #incidents | Team on-call |
| T+30 min | Status page updated if customer-visible | Comms Lead |
| Resolution | Status page: "Resolved" | Comms Lead |
| Resolution +1 week | Post-mortem (lightweight format) | Team lead |

#### SEV-4 (Low — Minimal Impact)

No external communication required. Tracked in Jira and resolved in normal sprint workflow.

### Status Page Updates

Our status page is hosted at **status.acme.dev** (managed via Statuspage by Atlassian). The Communications Lead is responsible for all status page updates.

**Components on status page:**
- Nexus Platform (API, Dashboard)
- Relay (Data Sync)
- Beacon (Analytics & Reporting)
- Sentinel (Compliance & Screening)

**Status levels:**
- **Operational:** All systems functioning normally
- **Degraded Performance:** Slower than usual, but functional
- **Partial Outage:** Some functionality unavailable
- **Major Outage:** Service is down

### Status Page Update Template

```
Title: [Component] - [Issue Type]

Body:
We are currently investigating [brief description of the issue].

Impact: [What customers are experiencing]

Current status: [What we know and what we are doing]

Next update: [Time of next planned update]
```

### Customer Email Template (SEV-1/2)

```
Subject: [ACME Service Update] [Component] - [Issue Summary]

Dear [Customer Name],

We want to let you know about a service issue affecting [component/feature].

What happened:
[Brief, non-technical description of the issue]

Impact to your account:
[Specific impact — e.g., "Shipment tracking data may be delayed by up to 15 minutes"]

What we are doing:
[Current remediation steps]

Current status:
[Resolved / In progress / Monitoring]

Next steps:
[What happens next — e.g., "We will send a follow-up with a full incident report within 48 hours"]

We apologize for the inconvenience and appreciate your patience.

Best regards,
[Name]
ACME Support Team
support@acme.dev
```

### Internal Communication

During active incidents:
- All updates go to #incidents Slack channel
- Do not discuss incidents in other public Slack channels to avoid confusion
- If customers or sales team ask for updates, direct them to the Communications Lead
- Do not share technical details externally without Communications Lead approval
- Social media inquiries go to Marketing (Hiroshi Tanaka)

### Post-Mortem Communication

After every SEV-1 and SEV-2 incident:
1. **Internal post-mortem** published within 48 hours (SEV-1) or 1 week (SEV-2) in Confluence under `Engineering > Post-Mortems`
2. **Customer-facing summary** sent to affected Enterprise accounts within 1 week
3. **Action items** tracked in Jira with SLAs: SEV-1 items within 1 week, SEV-2 items within 2 weeks
4. Post-mortem review in the next engineering all-hands

### Key Principles

- **Default to transparency.** Customers trust us more when we communicate openly about issues. Silence is worse than bad news.
- **Be specific about impact.** "Some users may experience issues" is not helpful. "Shipment tracking updates are delayed by approximately 10 minutes for customers in the US-West region" is.
- **No blame in external communications.** Never point to a specific vendor, employee, or code change in customer-facing messages.
- **Underpromise on timelines.** If you think it will take 1 hour to fix, say 2 hours. Better to resolve early than miss a commitment.
- **One voice.** Only the Communications Lead sends external updates during an incident. This prevents conflicting messages.

---

*This document is maintained by People Ops, Legal, and Engineering Leadership. Changes require review from at least two of these three groups. For questions, suggestions, or to report a policy violation, contact policy@acme.dev or post in #policy-questions.*
