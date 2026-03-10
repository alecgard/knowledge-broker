# ACME Org — Internal Services & Infrastructure

## Service Catalog

| Service | Owner | Language | Repo | Port | Dependencies |
|---------|-------|---------|------|------|-------------|
| `nexus-api` | Nexus Core | Go | acme/nexus-api | 8080 | PostgreSQL, Redis, Kafka |
| `inventory-service` | Inventory Squad | Go | acme/inventory-service | 8081 | PostgreSQL, Kafka, Redis |
| `shipment-service` | Logistics Squad | Go | acme/shipment-service | 8082 | PostgreSQL, Kafka, Redis |
| `forecast-engine` | Intelligence Squad | Python | acme/forecast-engine | 8083 | PostgreSQL, Redis, S3 |
| `alert-service` | Intelligence Squad | Go | acme/alert-service | 8084 | PostgreSQL, Kafka, Redis, PagerDuty API |
| `replenishment-engine` | Intelligence Squad | Go | acme/replenishment-engine | 8085 | PostgreSQL, forecast-engine |
| `relay-agent` | Relay Team | Go | acme/relay-agent | 8090 | Kafka, customer systems |
| `relay-control-plane` | Relay Team | Go | acme/relay-control-plane | 8091 | PostgreSQL, Redis |
| `beacon-api` | Beacon Team | Go | acme/beacon-api | 8100 | ClickHouse, PostgreSQL |
| `report-generator` | Beacon Team | Go | acme/report-generator | 8101 | ClickHouse, S3, chromedp |
| `sentinel-api` | Sentinel Team | Go | acme/sentinel-api | 8110 | PostgreSQL, Elasticsearch |
| `screening-service` | Sentinel Team | Go | acme/screening-service | 8111 | Elasticsearch, Redis |
| `classification-service` | Sentinel Team | Go | acme/classification-service | 8112 | PostgreSQL, Claude API |
| `auth-service` | Platform Team | Go | acme/auth-service | 8000 | PostgreSQL, Redis, Okta |
| `gateway` | Platform Team | Go | acme/gateway | 443 | auth-service, all backend services |
| `forge-ui` | Frontend Team | TypeScript | acme/forge-ui | 3000 | nexus-api, beacon-api, sentinel-api |

---

## Infrastructure

**Cloud:** AWS (us-west-2 primary, eu-west-1 secondary for EU customers)

**Kubernetes:** EKS 1.29, managed node groups. 3 clusters:
- `acme-prod-usw2`: Production US (120 nodes, m6i.2xlarge)
- `acme-prod-euw1`: Production EU (40 nodes, m6i.2xlarge)
- `acme-staging`: Staging (20 nodes, m6i.xlarge)

**Databases:**
- PostgreSQL 16 on RDS (Multi-AZ, r6g.2xlarge). Separate instances per service. Total: 8 production DB instances.
- ClickHouse Cloud for Beacon analytics. 3-node cluster, 2TB storage.
- Redis 7 on ElastiCache (r7g.xlarge, 3-node cluster). Used for caching, session store, feature flags, rate limiting.
- Elasticsearch 8.12 on AWS OpenSearch for Sentinel screening index. 3 data nodes, 500GB.

**Message Queue:** Amazon MSK (Kafka 3.6). 6-broker cluster, 30 topics, ~50K messages/second peak.

**Object Storage:** S3 for report artifacts, data exports, ML model artifacts, relay agent configs. ~15TB total.

**CDN:** CloudFront for static assets and API acceleration.

**DNS:** Route 53. Primary domains: acme.dev, acmelogistics.com, api.acme.dev

---

## Monitoring & Observability

- **Metrics:** Prometheus + Grafana (self-hosted on EKS). 2M active time series.
- **Logging:** Loki + Grafana. 30-day retention. ~500GB/day ingestion.
- **Tracing:** Jaeger with OpenTelemetry SDK. 1% sampling in production, 100% in staging.
- **Alerting:** Grafana Alerting → PagerDuty (P1/P2) or Slack (P3/P4).
- **Uptime:** Checkly for external synthetic monitoring. Checks every 60 seconds from 5 regions.

---

## CI/CD

- GitHub Actions for CI (lint, test, build, security scan)
- ArgoCD for GitOps deployments to Kubernetes
- Docker images pushed to ECR
- Terraform for infrastructure (modules in acme/terraform-modules repo)
- Atlantis for Terraform plan/apply on PR

---

## Secrets Management

AWS Secrets Manager. Rotated quarterly for all service accounts. HashiCorp Vault for dynamic database credentials.

---

## Internal Tools

- **Launchpad** (internal): Admin portal for customer success team. View customer configs, trigger re-syncs, manage feature flags per customer. Built with React + nexus-api.
- **Relay Studio** (internal): Visual connector builder for Relay team. Schema mapping UI, test harness for connectors. Built with React + relay-control-plane.
- **Sentinel Review Queue** (internal): Human review interface for low-confidence HS code classifications. Reviewers are trained customs brokers (3 contractors, managed by Sentinel team).
