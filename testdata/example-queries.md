# Example Queries for ACME Test Data

These queries exercise different aspects of the ACME knowledge base. Use them to test retrieval and synthesis quality.

```bash
DB="testdata/acme-test.db"
```

## Factual / Lookup

```bash
go run ./cmd/kb query --db $DB --human "What products does ACME sell?"
go run ./cmd/kb query --db $DB --human "Who leads the Platform team?"
go run ./cmd/kb query --db $DB --human "What port does the screening service run on?"
go run ./cmd/kb query --db $DB --human "What is ACME's current ARR?"
go run ./cmd/kb query --db $DB --human "What carriers does Relay support?"
```

## Cross-source (Confluence + Slack)

```bash
go run ./cmd/kb query --db $DB --human "What are the known issues with the Relay SAP connector?"
go run ./cmd/kb query --db $DB --human "How does ACME handle database failovers?"
go run ./cmd/kb query --db $DB --human "What is the on-call compensation policy?"
```

## Runbook / Operational

```bash
go run ./cmd/kb query --db $DB --human "The Nexus API is slow. What should I check?"
go run ./cmd/kb query --db $DB --human "A customer's Relay agent has been offline for 20 minutes. What do I do?"
go run ./cmd/kb query --db $DB --human "Forecast accuracy dropped below 15% MAPE. Walk me through diagnosis."
go run ./cmd/kb query --db $DB --human "We need to roll back a production deployment. What's the process?"
```

## Architecture / Technical

```bash
go run ./cmd/kb query --db $DB --human "What databases does ACME use and what are they for?"
go run ./cmd/kb query --db $DB --human "Explain the CI/CD pipeline"
go run ./cmd/kb query --db $DB --human "How does Sentinel do denied party screening?"
go run ./cmd/kb query --db $DB --human "What is the Forge design system?"
```

## Security & Compliance

```bash
go run ./cmd/kb query --db $DB --human "What security certifications does ACME have?"
go run ./cmd/kb query --db $DB --human "How is production access controlled?"
go run ./cmd/kb query --db $DB --human "What is the data retention policy?"
```

## Customer & Competitive

```bash
go run ./cmd/kb query --db $DB --human "Tell me about the GlobalMart account"
go run ./cmd/kb query --db $DB --human "How do we compare to ChainLink?"
go run ./cmd/kb query --db $DB --human "Which customers use Sentinel?"
```

## Synthesis / Reasoning

```bash
go run ./cmd/kb query --db $DB --human "If I'm a new engineer joining the Nexus Core team, what should I know?"
go run ./cmd/kb query --db $DB --human "What are the biggest technical risks in ACME's infrastructure?"
go run ./cmd/kb query --db $DB --human "A customer in the EU is asking about our data privacy practices. What should I tell them?"
go run ./cmd/kb query --db $DB --human "We're considering adding a new ERP integration. What's the process and who's involved?"
```

## Edge Cases (should return low confidence or partial answers)

```bash
go run ./cmd/kb query --db $DB --human "What is ACME's mobile app tech stack?"
go run ./cmd/kb query --db $DB --human "How many engineers work on the forecast engine?"
go run ./cmd/kb query --db $DB --human "What happened in the last post-mortem?"
```
