# ACME Org — New Engineer Onboarding Guide

**Last updated:** 2026-03-01
**Owner:** People Ops (HR) & Platform Team
**Questions?** Slack: #onboarding-help

Welcome to ACME Org! This guide walks you through everything from your first day through your first quarter. Our goal is to get you shipping code with confidence as quickly as possible while making sure you have the context and relationships you need to be successful long-term.

---

## Table of Contents

1. [Before Day 1](#before-day-1)
2. [Day 1 Schedule](#day-1-schedule)
3. [First Week](#first-week)
4. [First Month](#first-month)
5. [First Quarter](#first-quarter)
6. [Mentorship Program](#mentorship-program)
7. [Recommended Reading](#recommended-reading)
8. [FAQ](#faq)

---

## Before Day 1

### What to Expect

About two weeks before your start date, you will receive a "Welcome to ACME" email from People Ops with the following:

- **Start date confirmation** and your Day 1 schedule
- **Hardware selection form** — choose your laptop (MacBook Pro 14" M3 Pro or MacBook Pro 16" M3 Max) and any peripherals (monitor, keyboard, headset). If you are remote, hardware ships to your home address and should arrive 3-5 business days before your start date.
- **Home office stipend info** — $1,500 one-time stipend for home office setup (desk, chair, lighting, etc.). Reimbursed via Expensify within your first 30 days.
- **Pre-reading packet** — a short PDF with company overview, org chart, product descriptions, and our core values. Not required reading, but helpful context.

### Accounts Provisioned Before Day 1

IT will provision the following accounts before your start date. You will receive login instructions via your personal email:

| Account | Purpose | How to Access |
|---------|---------|---------------|
| Google Workspace | Email, Calendar, Drive | Your @acme.dev email |
| Okta | Single sign-on for all internal tools | okta.acme.dev |
| Slack Enterprise Grid | Team communication | acme-org.slack.com |
| GitHub (ACME org) | Source code, code review | github.com/acme |
| 1Password Teams | Shared secrets and credentials | Okta SSO |

Additional tool access (AWS, PagerDuty, Grafana, etc.) will be provisioned during your first week based on your team assignment. See the [Access Requests Checklist](#access-requests-checklist) below.

### Before You Arrive Checklist

- [ ] Complete the background check via Checkr (link in your offer email)
- [ ] Fill out tax forms and direct deposit info in Gusto
- [ ] Select your hardware in the equipment form
- [ ] Join the #new-hires Slack channel (invite sent to personal email)
- [ ] Review the pre-reading packet
- [ ] Set up your @acme.dev Google account and configure MFA

---

## Day 1 Schedule

All Day 1 activities happen on your start date. If you are remote, everything is over Zoom. If you are in Portland, meet in the main conference room (3rd floor, "Cascade Room").

### 9:00 AM PT — Welcome Session

- Meet the People Ops team
- Overview of ACME: history, mission, products, and where we are headed
- Company values deep dive: "Ship with confidence. Default to transparency. Customers are partners."
- Walkthrough of benefits, PTO policy, and employee handbook
- Q&A

### 10:00 AM PT — IT Setup & Security

- Laptop setup with IT team (remote folks: guided Zoom session)
- Install Jamf MDM profile (required for all company devices)
- Configure Okta MFA (hardware key provided, or use Okta Verify app)
- Verify access to Google Workspace, Slack, GitHub, 1Password
- Security awareness overview: password policy, phishing awareness, acceptable use
- Sign the Acceptable Use Policy and Data Handling Agreement

### 11:00 AM PT — HR & Compliance

- Benefits enrollment walkthrough (health, dental, vision, 401k)
- Review of key policies: remote work, PTO, expense reporting
- Emergency contacts and safety procedures
- Sign remaining employment documents

### 12:00 PM PT — Team Lunch

- Lunch with your immediate team (remote: DoorDash credit sent to your email, eat together over Zoom)
- Casual introductions, team norms, what the team is working on
- Your manager will share the team's current quarterly goals

### 2:00 PM PT — Buddy Introduction

- Meet your onboarding buddy (a peer on your team who has been at ACME for 6+ months)
- Your buddy is your go-to person for questions during your first 90 days
- They will help you navigate tools, processes, and team culture
- Buddy expectations:
  - Daily check-in for the first week (15 minutes)
  - Weekly check-in for the first month
  - Available on Slack for ad-hoc questions anytime

### 3:00 PM PT — Dev Environment Setup (Part 1)

- Start setting up your development environment with your buddy
- See the [Dev Environment Setup Guide](#dev-environment-setup-guide) below
- Do not worry about finishing today. You will have dedicated time during your first week.

### 4:30 PM PT — Wrap Up

- Check in with your manager: how was Day 1? Any questions or concerns?
- Review your first week schedule
- Go home and rest. Onboarding is a marathon, not a sprint.

---

## First Week

Your first week is about getting your local environment running, understanding the codebase, and meeting key people. Your calendar will have light meeting load this week by design.

### Dev Environment Setup Guide

#### Prerequisites

Make sure you have admin access on your laptop (IT configures this by default).

#### Step 1: Package Manager & Core Tools

```bash
# Install Homebrew (if not already installed by IT)
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Core development tools
brew install git
brew install gh           # GitHub CLI
brew install go           # Go 1.22+ required (verify: go version)
brew install node         # Node.js 20 LTS (for frontend work)
brew install python@3.12  # Python 3.12 (for forecast-engine)
brew install jq           # JSON processing
brew install yq           # YAML processing
brew install make         # Build tooling
brew install pre-commit   # Git hooks framework
```

#### Step 2: Docker & Kubernetes

```bash
# Docker Desktop (required for local services)
brew install --cask docker

# Kubernetes tools
brew install kubectl
brew install kubectx       # Easy context/namespace switching
brew install helm          # Kubernetes package manager
brew install argocd        # ArgoCD CLI for deployments
brew install k9s           # Terminal-based K8s dashboard

# AWS CLI (for accessing staging/prod resources)
brew install awscli
```

After installing Docker Desktop, open it and allocate at least 8GB of memory and 4 CPUs in Settings > Resources. The local development stack is resource-intensive.

#### Step 3: IDE Setup

We do not mandate a specific IDE, but most engineers use one of:

- **GoLand** (JetBrains) — license provided via IT, request in #it-help
- **VS Code** — free, recommended extensions listed below
- **Neovim** — if that is your thing, several engineers maintain a shared config

**Recommended VS Code Extensions:**
- Go (golang.go)
- ESLint
- Prettier
- Docker
- Kubernetes
- GitLens
- Tailwind CSS IntelliSense (for frontend)
- Thunder Client (API testing)

#### Step 4: Go Development Setup

```bash
# Verify Go version (must be 1.22+)
go version

# Set up Go environment
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin

# Install common Go tools
go install golang.org/x/tools/gopls@latest          # Language server
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/air-verse/air@latest            # Live reload for Go services

# Configure Git for ACME private modules
git config --global url."git@github.com:acme/".insteadOf "https://github.com/acme/"
export GOPRIVATE=github.com/acme/*
```

#### Step 5: Local Infrastructure

We use Docker Compose to run shared dependencies locally. Clone the dev-environment repo first:

```bash
# Clone the dev-environment repo
cd ~/code
git clone git@github.com:acme/dev-environment.git
cd dev-environment

# Start local infrastructure
docker-compose up -d

# This starts:
#   - PostgreSQL 16 (port 5432, user: acme, password: acme_local)
#   - Apache Kafka + Zookeeper (port 9092)
#   - Redis 7 (port 6379)
#   - ClickHouse (port 8123, for Beacon development)
#   - Elasticsearch 8 (port 9200, for Sentinel development)
#   - Jaeger (port 16686, tracing UI)
#   - Grafana (port 3001, local dashboards)

# Verify everything is running
docker-compose ps
```

### Repos to Clone

Clone these repos into your workspace directory (we recommend `~/code/acme/`):

**Core shared libraries (everyone should have these):**

```bash
mkdir -p ~/code/acme && cd ~/code/acme

# Shared Go libraries
git clone git@github.com:acme/acme-go-kit.git       # Logging, tracing, auth middleware
git clone git@github.com:acme/acme-kafka.git         # Kafka consumer/producer wrappers
git clone git@github.com:acme/acme-pgx.git           # PostgreSQL helpers

# Dev tools and configs
git clone git@github.com:acme/dev-environment.git    # Docker Compose for local infra
git clone git@github.com:acme/terraform-modules.git  # Shared Terraform modules (read-only for most)
```

**Clone repos for your team:**

| Team | Repos |
|------|-------|
| Platform | `acme/auth-service`, `acme/gateway` |
| Nexus Core — Inventory | `acme/nexus-api`, `acme/inventory-service` |
| Nexus Core — Logistics | `acme/nexus-api`, `acme/shipment-service` |
| Nexus Core — Intelligence | `acme/nexus-api`, `acme/forecast-engine`, `acme/alert-service`, `acme/replenishment-engine` |
| Relay | `acme/relay-agent`, `acme/relay-control-plane` |
| Beacon | `acme/beacon-api`, `acme/report-generator` |
| Sentinel | `acme/sentinel-api`, `acme/screening-service`, `acme/classification-service` |
| Frontend | `acme/forge-ui` |
| QA & Release Eng | All service repos + `acme/e2e-tests`, `acme/release-pipeline` |
| SRE | `acme/terraform-infra`, `acme/monitoring-configs`, `acme/runbooks` |

### Running Services Locally

Each service repo includes a Makefile with standard targets. Here is the typical workflow:

```bash
cd ~/code/acme/nexus-api  # or whichever service

# Install dependencies
make deps

# Run database migrations (requires local PostgreSQL from docker-compose)
make migrate

# Seed test data
make seed

# Run the service
make run
# Service starts on its configured port (see .env.local)

# Run tests
make test

# Run linter
make lint

# Build Docker image
make docker-build
```

**Standard Make targets across all Go services:**

| Target | Description |
|--------|-------------|
| `make deps` | Install/update Go dependencies |
| `make build` | Compile the binary |
| `make run` | Run the service locally (with hot-reload via air) |
| `make test` | Run unit tests |
| `make test-integration` | Run integration tests (requires Docker) |
| `make lint` | Run golangci-lint |
| `make migrate` | Run database migrations |
| `make seed` | Seed local database with test data |
| `make docker-build` | Build Docker image |
| `make proto` | Regenerate protobuf code (if applicable) |

**Environment variables:** Each service has a `.env.example` file. Copy it to `.env.local`:

```bash
cp .env.example .env.local
# Edit .env.local with your local values (most defaults work with docker-compose)
```

**Common local port mappings:**

| Service | Port |
|---------|------|
| `gateway` | 443 (local: 8443) |
| `auth-service` | 8000 |
| `nexus-api` | 8080 |
| `inventory-service` | 8081 |
| `shipment-service` | 8082 |
| `forecast-engine` | 8083 |
| `alert-service` | 8084 |
| `replenishment-engine` | 8085 |
| `relay-agent` | 8090 |
| `relay-control-plane` | 8091 |
| `beacon-api` | 8100 |
| `report-generator` | 8101 |
| `sentinel-api` | 8110 |
| `screening-service` | 8111 |
| `classification-service` | 8112 |
| `forge-ui` | 3000 |

### Access Requests Checklist

Submit access requests during your first week via the #it-help Slack channel. Use the `/access-request` Slack workflow and specify the tool and permission level.

| Tool | Permission Level | Approval |
|------|-----------------|----------|
| GitHub (acme org) | Write access to your team's repos | Auto-approved |
| AWS SSO | ReadOnly for staging, no prod access initially | Manager approval |
| Okta Apps | Apps relevant to your team | Auto-approved |
| PagerDuty | Your team's schedule (observer initially) | Team lead approval |
| Grafana | Editor for your team's dashboards | Auto-approved |
| ArgoCD | View access to your team's applications | Auto-approved |
| Jaeger | Full access | Auto-approved |
| 1Password | Your team's vault | Team lead approval |
| Sentry | Your team's projects | Auto-approved |
| Slack Channels | See list below | Self-serve |

**Slack channels to join:**

- `#engineering` — all-engineering announcements and discussions
- `#your-team-name` — your team's channel (e.g., #platform, #nexus-core, #relay, #beacon, #sentinel, #frontend)
- `#deployments` — deployment notifications from ArgoCD
- `#incidents` — incident declarations and updates
- `#on-call` — on-call handoff and discussion
- `#code-review` — PR review requests
- `#dev-help` — ask any engineering question
- `#onboarding-help` — questions specifically about onboarding
- `#random` — off-topic, memes, social
- `#pets` — exactly what you think it is
- `#book-club` — monthly engineering book club

### First Week Schedule

| Day | Morning | Afternoon |
|-----|---------|-----------|
| Monday (Day 1) | Orientation, IT setup, HR | Team lunch, buddy intro, dev setup |
| Tuesday | Finish dev environment setup | Clone repos, run services locally |
| Wednesday | Codebase walkthrough with buddy | Architecture overview session with team lead |
| Thursday | Pick up your first "good-first-issue" | Pair programming with buddy |
| Friday | Continue first issue, ask questions | First week retro with manager (30 min) |

---

## First Month

### Ship Your First PR

Every repo has issues labeled `good-first-issue` in GitHub. These are small, well-scoped tasks designed for new engineers:

- Typically 1-3 days of work
- Clear acceptance criteria
- Existing tests to use as examples
- A team member assigned as a reviewer who expects your PR

**How to find starter tasks:**
1. Go to your team's repo(s) on GitHub
2. Filter issues by the `good-first-issue` label
3. Pick one that interests you and assign it to yourself
4. If none are available, ask your team lead — they will create one

**PR workflow at ACME:**
1. Create a branch: `feat/NEX-1234-add-bulk-inventory-api` (type/ticket-short-description)
2. Write code, including tests (80% coverage target)
3. Run `make lint` and `make test` locally before pushing
4. Open a PR with a clear description: what changed, why, how to test
5. Request review from your buddy + one other team member
6. Address feedback, get at least 1 approval
7. Merge via "Squash and merge" (our default merge strategy)
8. ArgoCD deploys to staging automatically. Production deploys happen Tuesdays.

### Shadow On-Call

During your first month, you will shadow on-call for your team for one week:

- **You will NOT carry the pager.** You are observing only.
- Shadow the primary on-call engineer for your team
- Join any incident response calls as an observer
- Review incoming alerts and how they are triaged
- Read the runbooks relevant to your team (see the Runbooks section in the internal wiki)
- Ask questions! Understanding our operational posture is critical.

**After shadowing, write a short summary** (1 page) of what you observed and any questions you have. Share it with your team lead. This is not graded — it helps us improve our runbooks and onboarding.

### Key Meetings in Your First Month

| Meeting | With Whom | When | Purpose |
|---------|-----------|------|---------|
| Weekly 1:1 | Your manager | Every week, 30 min | Performance, goals, questions, feedback |
| Skip-level | VP of Engineering (Dana Chen) | Once in first month, 30 min | Get to know you, answer big-picture questions |
| Team standup | Your team | Daily, 15 min | What you did, what you are doing, blockers |
| Team sprint planning | Your team | Biweekly, 1 hour | Plan next sprint's work |
| Engineering all-hands | All of engineering | Monthly, 1 hour | Company updates, tech talks, demos |

### Complete Security Training

All engineers must complete security awareness training within their first 30 days. This is an annual requirement.

- **Platform:** Curricula (access via Okta)
- **Duration:** ~2 hours of interactive modules
- **Topics covered:**
  - Phishing identification and reporting
  - Password hygiene and credential management
  - Data classification and handling (Public, Internal, Confidential, Restricted)
  - Secure coding practices
  - Incident reporting procedures
  - Physical security (for Portland office employees)
- **Completion tracked** by People Ops. You will get reminder emails if not completed within 30 days.
- **Annual refresher** is shorter (~45 minutes) and due each year on your hire anniversary.

---

## First Quarter

### Join the On-Call Rotation

After shadowing and completing at least one month on the team, you will join the on-call rotation:

- Your team lead will add you to the PagerDuty schedule
- You start as primary on-call for your team (typically one week every 4-6 weeks depending on team size)
- Platform team + SRE serve as secondary/escalation, so you are never alone
- Review the on-call expectations:
  - Acknowledge pages within 5 minutes
  - Follow the relevant runbook
  - Escalate if you are stuck (never sit on an unresolved alert)
  - Write a brief handoff note at the end of your on-call week
- **Compensation:** $500/week on-call stipend, $200 per after-hours page, plus a comp day after your on-call week

### Lead a Small Project

In your first quarter, your manager will assign you a small project to lead:

- Typically a feature or improvement scoped to 2-4 weeks of work
- You own the design doc, implementation, testing, and rollout
- Present the design to your team for feedback before starting implementation
- This is how we assess readiness for more complex work — it is about the process, not perfection

**Design doc template** is in Google Drive: `Engineering > Templates > Design Doc Template`. Key sections:
- Problem statement
- Proposed solution
- Alternatives considered
- Rollout plan (feature flag, canary, etc.)
- Metrics for success

### Give a Tech Talk

ACME has a monthly engineering all-hands with a 15-minute tech talk slot. New engineers are encouraged (not required) to give a talk in their first quarter.

**Popular first talk topics:**
- "What surprised me about our codebase"
- A deep dive into something you learned during onboarding
- A technology or pattern from your previous job that could help us
- A walkthrough of the project you led

**How to sign up:** Post in #engineering with your topic. The engineering all-hands organizer (currently Viktor Nowak) will slot you in.

---

## Mentorship Program

### Overview

Every new hire at ACME is paired with a mentor from a **different team**. This is intentional — your buddy helps you navigate your immediate team, while your mentor gives you broader perspective across the organization.

### How It Works

- **Duration:** 6 months from your start date
- **Cadence:** Monthly 1-hour check-ins (you schedule them)
- **Format:** Can be video call, coffee chat (Portland office), or async Slack thread — whatever works for both of you
- **Matching:** People Ops matches mentors and mentees based on interests, career goals, and experience level. You will be introduced via email in your first week.

### For Mentees

Your mentor is there to help you with:
- Understanding how ACME works beyond your immediate team
- Career development and growth path questions
- Navigating ambiguity and organizational dynamics
- Technical guidance on topics outside your team's scope
- Building your internal network

**Come to each session with at least one question or topic.** A little preparation goes a long way.

### For Mentors

If you are reading this as a mentor, thank you! Here is what we ask:

- **Commit to 6 months** of monthly check-ins (1 hour each)
- **Be proactive** — if your mentee does not schedule, nudge them. New hires sometimes feel awkward about "taking up your time."
- **Share context** that is hard to find in docs. The unwritten rules, the history behind decisions, the people to know.
- **Introduce your mentee** to 2-3 people outside their team during the mentorship period.
- **Flag concerns** to People Ops if you notice your mentee struggling. Early intervention helps.

### Mentorship Topics by Month

| Month | Suggested Focus |
|-------|----------------|
| 1 | Getting to know each other, mentee's goals, how ACME works |
| 2 | Codebase and architecture questions, cross-team dependencies |
| 3 | Career growth at ACME, IC vs. management track |
| 4 | Technical depth: discuss a challenging problem from mentor's experience |
| 5 | Giving and receiving feedback, navigating disagreements |
| 6 | Retrospective: what went well, what to focus on next, transition to informal relationship |

---

## Recommended Reading

### Internal Documentation

Read these in roughly this order during your first few weeks:

1. **ACME Org Knowledge Base** (`acme-org.md`) — Company overview, products, org structure, infrastructure, and runbooks. This is the single source of truth for how ACME works.
2. **Architecture Decision Records (ADRs)** — in each service repo under `/docs/adr/`. Start with your team's repos.
3. **API Documentation** — `docs.acme.dev` for public-facing API docs. Internal API docs are in each repo's `/docs/` folder.
4. **Incident Post-Mortems** — in Confluence under `Engineering > Post-Mortems`. Read the last 5-10 to understand common failure modes and how we respond.
5. **Product Roadmap** — in Productboard (access via Okta). Understand what is coming in the next 2 quarters.
6. **Team-specific onboarding docs** — each team maintains their own supplementary onboarding notes in their repo's CONTRIBUTING.md.

### External Books & Resources

These are books our engineering team frequently references. Not required reading, but highly recommended:

**Systems & Architecture:**
- *Designing Data-Intensive Applications* by Martin Kleppmann — our unofficial engineering bible. Especially relevant for understanding our Kafka, PostgreSQL, and ClickHouse usage.
- *The Site Reliability Engineering Book* (Google SRE Book) — free online. Read chapters on monitoring, incident management, and on-call.
- *Building Microservices* by Sam Newman — relevant to our service architecture.

**Go Programming:**
- *The Go Programming Language* by Donovan & Kernighan — the definitive Go reference.
- *Concurrency in Go* by Katherine Cox-Buday — important for understanding our concurrent data pipelines.
- *100 Go Mistakes and How to Avoid Them* by Teiva Harsanyi — practical patterns we use daily.

**Supply Chain & Domain:**
- *Supply Chain Management: Strategy, Planning, and Operation* by Chopra & Meindl — understand our customers' world.
- *The Goal* by Eliyahu Goldratt — classic operations management novel. Great for building empathy with our users.

**Engineering Culture:**
- *An Elegant Puzzle: Systems of Engineering Management* by Will Larson
- *Accelerate* by Forsgren, Humble, and Kim — the research behind our deployment practices.
- *Team Topologies* by Skelton and Pais — influenced how we structure our teams.

### Engineering Book Club

We have a monthly engineering book club (Slack: #book-club). Currently reading: *Staff Engineer: Leadership Beyond the Management Track* by Will Larson. New members always welcome.

---

## FAQ

### General

**Q: What are the working hours?**
A: We are flexible. Core hours are 10am-3pm PT for meetings and collaboration. Outside of that, work when you are most productive. Most engineers work roughly 9am-6pm PT but this varies.

**Q: What is the PTO policy?**
A: Flexible PTO — take what you need, minimum 15 days/year. No accrual, no payout. Just coordinate with your manager and team. We mean it. Please take time off.

**Q: How do I get help if I am stuck?**
A: In order of preference:
1. Check the docs (internal wiki, repo READMEs, ADRs)
2. Ask your buddy
3. Post in #dev-help on Slack
4. Ask your manager
Never sit stuck for more than 30 minutes without reaching out.

**Q: When do I get production access?**
A: Engineers do not get standing production access. We use a break-glass procedure for debugging — request via your team lead with an incident ticket or justification. SRE and Platform team have standing access for operational needs.

### Development

**Q: What Go version do we use?**
A: Go 1.22+. All services use Go modules. The minimum version is enforced in each repo's `go.mod`.

**Q: How do I run all services locally at once?**
A: You usually do not need to. Run only the service you are working on plus its direct dependencies. The `dev-environment` repo's Docker Compose handles shared infrastructure (databases, Kafka, Redis). If you need to run multiple services, use the `make run` target in each repo in separate terminals.

**Q: What is the CI pipeline?**
A: GitHub Actions runs on every push: lint (golangci-lint) > unit tests > integration tests > build Docker image > security scan (Snyk). PRs must pass all checks before merging. E2E tests run against staging after merge.

**Q: How do deployments work?**
A: GitOps via ArgoCD. When your PR merges to main, CI builds a Docker image and pushes to ECR. ArgoCD detects the new image tag in the GitOps repo and deploys to staging automatically. Production deploys happen on Tuesdays — Release Engineering manages the promotion from staging to prod.

**Q: What is the branch strategy?**
A: Trunk-based development. All work happens on short-lived feature branches off of main. Branch naming: `{type}/{ticket}-{description}`. No long-lived branches. Feature flags for incomplete features.

### Tools & Access

**Q: I cannot access [tool]. What do I do?**
A: Post in #it-help with the tool name and the error you are seeing. IT SLA is 4 hours for access requests during business hours.

**Q: How do I get added to the on-call rotation?**
A: Your team lead will add you to PagerDuty after you complete the on-call shadow week and they confirm you are ready. Typically happens in your second month.

**Q: What AI tools am I allowed to use?**
A: Approved tools: Claude (Anthropic) and GitHub Copilot. Not approved: ChatGPT (data retention concerns). Rules: no customer data in prompts, no confidential code in public AI tools, all AI-generated code must pass standard code review. See the full AI Tools Policy for details.

**Q: Where do I find the VPN?**
A: We do not use a traditional VPN. All internal tools are behind Okta SSO with device trust (Jamf). AWS access is via SSO with short-lived credentials. If you need to access a customer's Relay agent, use the bastion host (details in the Relay team's runbook).

### Culture & Career

**Q: What does the engineering career ladder look like?**
A: We have two tracks — Individual Contributor (IC) and Management. IC levels: L3 (Junior) > L4 (Mid) > L5 (Senior) > L6 (Staff) > L7 (Principal). Management: M4 (Team Lead) > M5 (Engineering Manager) > M6 (Director) > M7 (VP). Detailed rubric is in Google Drive under `People Ops > Career Ladder`.

**Q: How often are performance reviews?**
A: Twice a year — April and October. Continuous feedback is encouraged through regular 1:1s. Peer feedback is collected via Culture Amp before each review cycle.

**Q: Can I transfer to another team?**
A: Yes, after 6 months in your current role. Talk to your manager about your interests. Internal transfers are encouraged — cross-pollination makes the whole org stronger.

**Q: How do I propose a new tool or technology?**
A: Write a brief RFC (Request for Comments) using the template in Google Drive (`Engineering > Templates > RFC Template`). Share in #engineering for feedback. Major changes (new languages, databases, cloud services) require VP Eng approval. Minor tools (linters, test frameworks) just need team lead sign-off.

**Q: What if I have feedback about this onboarding process?**
A: Please share it! Post in #onboarding-help or tell your manager. We iterate on this guide every quarter based on new hire feedback. Your fresh perspective is valuable.

---

**Welcome to the team. We are glad you are here.**

*If anything in this guide is wrong or outdated, please open a PR against the `acme/handbook` repo or post in #onboarding-help. This is a living document.*
