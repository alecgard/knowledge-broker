# ACME Org — Security & Compliance

## Certifications

- **SOC 2 Type II:** Achieved August 2024. Annual audit by Deloitte. Last audit: November 2025 (clean report).
- **ISO 27001:** Certification in progress, expected Q3 2026.
- **GDPR:** Compliant. DPA templates available. EU customer data processed only in eu-west-1.
- **CCPA:** Compliant. Privacy policy updated quarterly by Legal.
- **HIPAA:** Not compliant, not targeted. No healthcare customers.

---

## Security Practices

- **Encryption:** TLS 1.3 for all traffic. AES-256 encryption at rest for all databases and S3 buckets.
- **Authentication:** Okta-based SSO for internal tools. API key + JWT for customer API access. MFA required for all employees.
- **Authorization:** Role-based access control (RBAC) with 4 roles: viewer, editor, admin, super-admin. Resource-level permissions for multi-tenant isolation.
- **Vulnerability Management:** Dependabot for dependency scanning. Snyk for container scanning. Weekly vulnerability triage by Security team. SLA: critical vulnerabilities patched within 48 hours.
- **Penetration Testing:** Annual external pentest by NCC Group. Last test: January 2026 (3 medium findings, all remediated within 30 days). Bug bounty program on HackerOne (launched June 2025, 12 valid reports so far, highest payout $5,000).
- **Data Retention:** Customer data retained for duration of contract + 90 days. Screening audit logs retained for 7 years. Internal logs retained for 30 days (Loki) or 90 days (CloudWatch).

---

## Access Control

- **Production access:** Requires break-glass procedure. Logged and audited. Requires approval from team lead + SRE. Time-limited (max 4 hours). Only SRE and Platform team have standing access.
- **Customer data access:** Requires customer approval via CSM for support cases. All access logged in audit trail. No bulk data export without VP Eng approval.
- **Third-party access:** All vendors with data access must complete security questionnaire. Annual review. Current approved vendors: AWS, Stripe, Okta, PagerDuty, Checkly, HackerOne, Deloitte.
