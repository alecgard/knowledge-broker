# ACME Org — Competitive Intelligence

**Last updated:** 2026-03-10
**Maintained by:** Product Marketing (pmm@acme.dev)
**Classification:** Internal — Confidential — Do Not Share Externally

---

## Table of Contents

1. [Competitive Landscape Summary](#competitive-landscape-summary)
2. [Competitor Profiles](#competitor-profiles)
   - [ChainLink](#1-chainlink)
   - [Visibly](#2-visibly)
   - [SAP IBP](#3-sap-ibp)
   - [ShipVis](#4-shipvis)
   - [Kinaxis](#5-kinaxis)
   - [FourKites](#6-fourkites)
   - [project44](#7-project44)
3. [Overall Win/Loss Summary](#overall-winloss-summary)

---

## Competitive Landscape Summary

### Market Map

The supply chain software market is fragmenting into several overlapping segments. ACME competes across multiple categories, which is both a strength (breadth of platform) and a challenge (competing against best-of-breed in each segment).

```
                        PLANNING DEPTH
                    Low                 High
                 +---------------------------+
            High | FourKites    | Kinaxis     |
                 | project44    | SAP IBP     |
   VISIBILITY   | ShipVis      |             |
   BREADTH      |--------------|-------------|
            Low | Visibly      | (niche ERP  |
                 |              |  add-ons)   |
                 +---------------------------+

    ACME's position: Upper-middle on both axes
    ChainLink's position: Upper-left (strong visibility, weak planning)
```

### ACME's Competitive Position

**Where we win:**
- Mid-market retailers who need a unified platform (visibility + forecasting + compliance) without enterprise complexity or price
- Companies with cross-border compliance needs (Sentinel is differentiated — only ChainLink and project44 have anything comparable, and theirs are weaker)
- Customers who value modern UX and fast implementation over brand name recognition
- Deals where the buyer is operations-led rather than IT-led

**Where we struggle:**
- Enterprise deals where the incumbent ERP vendor (SAP, Oracle) bundles supply chain at a discount
- Deals that are primarily about S&OP/planning (Kinaxis, SAP IBP are far ahead)
- Deals where carrier network breadth is the primary buying criterion (FourKites, project44, ChainLink all have larger networks)
- Large 3PL/logistics companies that need deep ocean freight visibility (ShipVis)

**Market Trends Affecting Competitive Dynamics (2026):**
1. Consolidation: project44's acquisition of a compliance company signals convergence of visibility and compliance platforms
2. AI/ML becoming table stakes: every vendor now claims "AI-powered" forecasting and optimization
3. Mid-market is the fastest-growing segment: enterprise is saturated, SMB has high churn
4. Compliance regulations increasing globally: CBAM (EU), Uyghur Forced Labor Prevention Act, new tariff regimes — drives demand for Sentinel-like capabilities
5. Real-time visibility expectations rising: customers expect sub-minute updates, not hourly

---

## Competitor Profiles

### 1. ChainLink

**Company Overview:**
- **Founded:** 2017, San Francisco, CA
- **CEO:** David Huang
- **Funding:** Series C ($120M total raised). Last round: $65M in 2023, led by Insight Partners.
- **Headcount:** ~520 employees
- **ARR:** Estimated $70-80M (private, based on industry analysis)
- **Customers:** ~1,800 (primarily mid-market and enterprise)

**Product Comparison:**

| Feature | ACME | ChainLink | Notes |
|---------|------|-----------|-------|
| Real-time visibility | Yes | Yes | Comparable, ChainLink has slight edge in update frequency |
| Carrier integrations | 200+ | 300+ | ChainLink's largest advantage |
| Demand forecasting (ML) | Yes (Oracle engine) | Basic (ARIMA) | Significant ACME advantage |
| Replenishment recommendations | Yes | No | ACME only |
| Compliance/screening | Yes (Sentinel) | Limited | ChainLink launched basic screening in Q4 2025, very immature |
| Analytics/reporting | Yes (Beacon) | Basic dashboards | ACME has deeper analytics with SQL Playground and data export |
| ERP integrations | SAP, Oracle, Dynamics, Sage | SAP, Oracle, Dynamics | Comparable, ACME adds Sage Intacct |
| WMS integrations | Manhattan, Blue Yonder, Körber | Manhattan, Blue Yonder, Infor | Similar breadth |
| Self-serve onboarding | Yes (SMB) | Yes (all tiers) | ChainLink's self-serve is more mature |
| API quality | Good | Good | Both have well-documented REST APIs |
| Mobile app | Yes (React Native) | Yes (native iOS/Android) | ChainLink's mobile is more polished |
| Multi-tenant architecture | Yes | Yes | Comparable |

**Pricing:**
- ChainLink base: $3,000/month + $0.45 per tracked shipment
- Enterprise: custom pricing, typically $200K-$400K ACV
- Slightly cheaper per-shipment than ACME at lower volumes; more expensive at higher volumes due to higher base price

**Strengths:**
- Largest carrier network (300+ integrations) — this is their primary differentiator
- Strong brand recognition in enterprise, especially in traditional retail
- Self-serve onboarding is best-in-class — customers can get a working dashboard in under 1 hour
- Well-funded with strong go-to-market machine (large SDR team, heavy event presence)
- Mobile app is polished and frequently updated

**Weaknesses:**
- Monolithic architecture — they are midway through a multi-year re-platforming effort. Feature velocity has noticeably slowed (only 2 major features shipped in 2025 vs. 6 in 2024).
- Demand forecasting is basic statistical models (ARIMA, exponential smoothing) — no ML. Their forecast accuracy is consistently 25-30% worse than Oracle in head-to-head POCs.
- Compliance/screening product launched in Q4 2025 and is immature. Covers only OFAC and BIS Entity List — no EU lists, no certificate management, no audit trail.
- Customer success model is reactive — no proactive health monitoring. Customer complaints about responsiveness are common on G2.
- No replenishment recommendation engine.

**Common Sales Objections (when prospect is considering ChainLink):**
1. "ChainLink has more carrier integrations" — Response: "We cover 200+ carriers which handles 95%+ of shipment volume for mid-market retailers. The long tail of carriers ChainLink supports are niche regional carriers. More importantly, what differentiates us is what we do with the data — our ML forecasting, replenishment recommendations, and compliance screening."
2. "ChainLink is a more established brand" — Response: "Established, yes, but their architecture is showing its age. They are in the middle of a multi-year re-platforming. Meanwhile, we ship 6+ major features per year on our modern Go microservices stack."
3. "ChainLink's mobile app is better" — Response: "Fair point — their mobile app is more mature. Our mobile app covers core visibility workflows and we're investing heavily in mobile in 2026. That said, for mid-market operations teams, the web dashboard is where 90% of the work happens."

**Win/Loss Analysis:**
- **Overall win rate vs. ChainLink: 55%**
- We win when: buyer is operations-led, forecasting accuracy is a priority, compliance needs exist, implementation speed matters
- We lose when: buyer is IT-led and wants maximum carrier coverage, mobile is a key requirement, or the prospect already has ChainLink in a sister division

**Deal Example (Win):** Meridian Automotive Parts — Meridian evaluated both ACME and ChainLink in Q3 2023. ChainLink's larger LTL carrier network was appealing, but Meridian's VP Supply Chain prioritized demand forecasting accuracy for their seasonal business. Head-to-head POC showed Oracle forecast engine with 11% MAPE vs. ChainLink's 16% MAPE on Meridian's data. Closed at $155K ACV.

**Deal Example (Loss):** TerraVerde Outdoor — Lost when parent company (Apex Outdoor Group) mandated ChainLink standardization across portfolio. No amount of product superiority mattered against a corporate-level decision on volume pricing.

**Recommended Positioning:**
"ChainLink is a solid visibility platform, but ACME is a supply chain intelligence platform. Visibility tells you where things are — intelligence tells you where things should be. Our ML-powered forecasting, automated replenishment, and compliance screening go far beyond what ChainLink offers."

---

### 2. Visibly

**Company Overview:**
- **Founded:** 2020, Austin, TX
- **CEO:** Sarah Chen
- **Funding:** Series A ($18M total raised). Last round: $14M in 2022, led by Accel.
- **Headcount:** ~85 employees
- **ARR:** Estimated $8-12M (private)
- **Customers:** ~600 (primarily SMB and lower mid-market)

**Product Comparison:**

| Feature | ACME | Visibly | Notes |
|---------|------|---------|-------|
| Real-time visibility | Yes | Yes | Visibly is tracking-only |
| Carrier integrations | 200+ | 150+ | Visibly focuses on parcel carriers |
| Demand forecasting | Yes | No | N/A for Visibly |
| Replenishment recommendations | Yes | No | N/A for Visibly |
| Compliance/screening | Yes | No | N/A for Visibly |
| Analytics/reporting | Yes (Beacon) | Basic | Simple shipment analytics only |
| ERP integrations | 4 ERPs | None | Visibly doesn't integrate with ERPs |
| Self-serve onboarding | Yes (SMB) | Yes (all) | Visibly's 10-minute setup is exceptional |
| Pricing | $2,500+/mo | $500/mo flat | Visibly is 5x cheaper at base level |

**Pricing:**
- Flat $500/month for up to 50K shipments/year
- $1,000/month for up to 200K shipments/year
- No per-shipment fees, no Enterprise tier
- Extremely simple, transparent pricing

**Strengths:**
- Fastest time-to-value in the market — 10-minute self-serve onboarding with pre-built Shopify/WooCommerce integrations
- Simple, clean UI that non-technical users love
- Lowest price point by far — wins on budget-constrained deals
- Strong product-led growth motion (free trial converts at 25%)
- High NPS (estimated 60+) among their target segment

**Weaknesses:**
- Zero depth beyond shipment tracking — no inventory management, no forecasting, no compliance
- No ERP or WMS integrations — only connects to eCommerce platforms and carriers
- No analytics beyond basic shipment dashboards
- Customers outgrow it quickly — their own churn rate is estimated at 30%+ annually
- Engineering team is small (~25 engineers) and focused on scaling, not new features
- No enterprise features: no SSO, no audit trail, no custom SLAs

**Common Sales Objections:**
1. "Visibly is so much cheaper" — Response: "Visibly is a tracking widget, not a supply chain platform. If all you need is 'where is my package,' they're fine. But the moment you need inventory visibility, demand forecasting, or compliance, you'll be buying a second and third tool. We're one platform."
2. "Visibly was up and running in 10 minutes" — Response: "Speed to first dashboard is impressive, but what happens at month 6 when you need to understand why stockouts are happening? Visibly can't tell you that. ACME can."

**Win/Loss Analysis:**
- **Overall win rate vs. Visibly: 70%**
- We win when: prospect has any need beyond pure tracking, prospect is mid-market or above, procurement is involved (they evaluate total cost of ownership), or the prospect has ERP/WMS integration needs
- We lose when: prospect is SMB with very simple needs, price is the primary decision criterion, or the prospect wants to start simple and "grow into" a platform later

**Deal Example (Win):** UrbanThreads — Initially trialed Visibly (loved the simplicity) but realized they needed inventory visibility across stores + warehouse + in-transit. Visibly couldn't do this. Switched to ACME evaluation and signed within 3 weeks.

**Deal Example (Loss):** Several SMB prospects in the $15-20K ACV range choose Visibly because our base price is 5x higher. We've lost approximately 20 deals to Visibly in 2025 in the SMB segment.

**Recommended Positioning:**
"Visibly is great for businesses that only need package tracking. ACME is for businesses that want to optimize their entire supply chain. Think of it as Google Maps vs. an entire logistics operating system — both show you where things are, but only one helps you plan where they should go."

---

### 3. SAP IBP (Integrated Business Planning)

**Company Overview:**
- **Parent:** SAP SE (publicly traded, market cap ~$250B)
- **Product Lead:** Part of SAP's Digital Supply Chain portfolio
- **Headcount:** SAP overall ~107,000 employees; IBP team estimated at 800-1,000
- **Revenue:** Part of SAP's $35B+ total revenue. IBP standalone estimated $1.5-2B ARR.
- **Customers:** ~3,000 (overwhelmingly enterprise, Fortune 500-heavy)

**Product Comparison:**

| Feature | ACME | SAP IBP | Notes |
|---------|------|---------|-------|
| Real-time visibility | Yes | Limited | IBP is batch-oriented, not real-time |
| Carrier integrations | 200+ | Via SAP TM | Only works well in SAP ecosystem |
| Demand forecasting | Yes (ML) | Yes (ML + statistical) | IBP's forecasting is mature but complex |
| S&OP planning | No | Yes (core strength) | IBP far ahead — not our market |
| Replenishment | Yes (automated) | Yes (with SAP ERP) | IBP is deeper but requires SAP stack |
| Compliance/screening | Yes (Sentinel) | Via SAP GTS | GTS is powerful but extremely expensive |
| Analytics | Yes (Beacon) | SAP Analytics Cloud | SAC is enterprise-grade |
| Implementation time | 4-14 weeks | 12-24 months | Massive ACME advantage |
| Pricing | $30K-$460K ACV | $200K-$2M+ ACV | ACME is 5-10x cheaper |

**Pricing:**
- SAP IBP: starting at $200K+ ACV for mid-market deployments
- Typical enterprise deal: $500K-$2M ACV
- Requires SAP consultancy for implementation: additional $500K-$3M for implementation services
- Often bundled with other SAP products at a "discount" that still totals millions

**Strengths:**
- Deepest S&OP and supply planning capabilities in the market — decades of development
- Unmatched integration with SAP ERP ecosystem (S/4HANA, ECC, GTS, TM, EWM)
- Trusted brand for enterprise procurement — "nobody gets fired for buying SAP"
- Massive partner ecosystem (Deloitte, Accenture, PwC) for implementation and customization
- Global compliance capabilities via SAP GTS (Global Trade Services) — covers every country's regulations
- Financial planning integration (IBP connects to SAP BPC for financial impact analysis)

**Weaknesses:**
- Extremely expensive — total cost of ownership (license + implementation + consultancy + maintenance) can be 10-20x ACME for comparable visibility functionality
- Implementation takes 12-24 months minimum — some large deployments take 3+ years
- Terrible user experience — the UI is built for trained SAP consultants, not supply chain operators. Training costs are significant.
- Not real-time — IBP runs batch planning cycles (daily or weekly). For real-time visibility, SAP customers often buy a separate tool alongside IBP.
- Lock-in: once on SAP IBP, migrating away is extremely painful due to deep ERP integration
- Innovation pace is slow — SAP's release cycle is annual, with quarterly patches. Compared to ACME's weekly releases.

**Common Sales Objections:**
1. "We're a SAP shop, so IBP is the natural choice" — Response: "Being a SAP shop for ERP doesn't mean SAP is the best choice for supply chain visibility and intelligence. IBP is designed for long-range planning, not real-time operations. Many SAP customers use ACME for operational visibility and keep IBP for strategic planning. They complement each other."
2. "SAP is offering a big discount if we bundle IBP" — Response: "The bundle discount sounds attractive but look at total cost of ownership: 12+ months of implementation, consultancy fees, and training costs. With ACME, you'll be live in 8 weeks at a fraction of the cost. Calculate the value of having visibility 10 months sooner."
3. "We need S&OP capabilities" — Response: "We're transparent: ACME is not an S&OP platform. If S&OP is your primary need, IBP may be the right choice. But if your primary need is operational visibility with strong forecasting, ACME delivers that at a fraction of the cost and complexity. Many of our customers use ACME for operations and a separate planning tool for S&OP."

**Win/Loss Analysis:**
- **Overall win rate vs. SAP IBP: 45%**
- We win when: buyer is operations-led (not IT or procurement), speed of implementation matters, budget is under $300K, or the company is not heavily invested in the SAP ecosystem
- We lose when: buyer is IT-led and wants to consolidate on SAP, procurement is driving the decision and SAP offers bundle discounts, S&OP is a primary requirement, or the company has SAP consultants on staff

**Deal Example (Win):** Atlas Freight Corporation — Atlas runs Oracle NetSuite (not SAP). SAP pitched IBP as a greenfield opportunity. Atlas evaluated both. ACME won on implementation speed (8 weeks vs. SAP's quoted 14 months), price ($260K vs. SAP's $800K+), and modern API quality (Atlas needed white-label visibility for their clients).

**Deal Example (Loss):** A large US grocery chain (Fortune 100, cannot name due to NDA) evaluated ACME and SAP IBP in 2025. ACME won the POC on visibility and UX, but the CIO mandated SAP consolidation as part of a broader enterprise architecture initiative. Lost to corporate-level strategy, not product merit.

**Recommended Positioning:**
"SAP IBP is the 747 of supply chain planning — powerful, massive, and expensive. ACME is the private jet — gets you where you need to go faster, with a better experience, at a fraction of the cost. If you need to plan production schedules across 50 factories, IBP might be your tool. If you need real-time visibility, ML-powered forecasting, and compliance screening that's live in weeks instead of years, that's ACME."

---

### 4. ShipVis

**Company Overview:**
- **Founded:** 2018, Tel Aviv, Israel
- **CEO:** Noam Berkovich
- **Funding:** Series B ($55M total raised). Last round: $38M in 2024, led by Insight Partners and Vertex Ventures Israel.
- **Headcount:** ~180 employees (100 in Tel Aviv, 40 in New York, 40 in Singapore)
- **ARR:** Estimated $20-28M (private)
- **Customers:** ~400 (focused on importers, freight forwarders, and 3PLs with ocean freight exposure)

**Product Comparison:**

| Feature | ACME | ShipVis | Notes |
|---------|------|---------|-------|
| Real-time visibility | Yes (all modes) | Yes (ocean-first) | ShipVis is exceptional for ocean freight |
| Ocean freight tracking | Basic (via Maersk, CMA CGM APIs) | Deep (AIS data, port sensors, custom models) | Significant ShipVis advantage |
| ETA prediction (ocean) | Carrier-provided ETAs | Proprietary ML model (85% accuracy at 14-day horizon) | ShipVis far ahead for ocean |
| Air freight tracking | Yes | Basic | ACME is better for air |
| Parcel/ground tracking | Yes (200+ carriers) | Limited (50 carriers) | ACME far ahead for parcel |
| Port congestion analytics | No | Yes (real-time port dwell times, berth availability) | Unique ShipVis capability |
| Container tracking | Basic | Deep (container-level GPS, temperature, humidity sensors) | ShipVis advantage |
| Demand forecasting | Yes | No | ACME only |
| Compliance/screening | Yes | No | ACME only |
| Inventory management | Yes | No | ACME only |
| Analytics | Yes (Beacon) | Yes (ocean-focused dashboards) | Different focus areas |

**Pricing:**
- ShipVis Starter: $2,000/month (up to 500 containers/month tracked)
- ShipVis Professional: $5,000/month (up to 2,000 containers/month, includes port analytics)
- ShipVis Enterprise: custom pricing (typically $100K-$250K ACV)
- Per-container tracking fee: $1.50 for Starter, $1.00 for Professional, negotiable for Enterprise

**Strengths:**
- Best-in-class ocean freight visibility — AIS (Automatic Identification System) data combined with proprietary ML models for ETA prediction. Their ocean ETA accuracy is genuinely 15-20% better than any competitor.
- Port congestion analytics are unique — real-time dwell time data, berth availability predictions, demurrage risk alerts. No one else has this depth.
- Container-level IoT integration — ShipVis offers hardware (GPS trackers, temperature/humidity sensors) that attach to containers and feed data directly into their platform.
- Strong engineering culture (Israeli tech talent) — they ship features fast and their API is clean.
- Growing fast in Asia-Pacific and Europe, where ocean freight is dominant.

**Weaknesses:**
- Ocean-centric — weak in air freight, almost nonexistent in parcel/ground. Not a platform for domestic supply chain.
- No inventory management, no forecasting, no compliance — pure visibility play for international freight.
- Limited ERP/WMS integrations — they focus on freight forwarder and 3PL systems (CargoWise, Descartes).
- Small US presence — most customers are in Europe and Asia. US sales team is only 8 people.
- Hardware component (IoT sensors) adds complexity — some customers don't want to manage physical devices.
- Relatively small carrier network for non-ocean modes (50 carriers vs. ACME's 200+).

**Common Sales Objections:**
1. "ShipVis has much better ocean freight visibility" — Response: "Absolutely true — if ocean freight visibility is your primary and only need, ShipVis is excellent. But most mid-market retailers need visibility across all modes — ocean, air, ground, parcel — plus inventory management, forecasting, and compliance. ACME provides a single platform for all of that. With ShipVis, you'd still need 2-3 additional tools."
2. "ShipVis has IoT container tracking" — Response: "IoT container tracking is valuable for high-value or temperature-sensitive shipments. We're watching this space and evaluating partnerships. For most retail shipments, carrier-provided tracking is sufficient. If IoT container tracking is a must-have, ShipVis can complement ACME for that specific use case."

**Win/Loss Analysis:**
- **Overall win rate vs. ShipVis: 65%**
- We win when: customer needs domestic + international visibility (not just ocean), customer needs forecasting or compliance, customer is a retailer (not a freight forwarder or 3PL)
- We lose when: customer is a freight forwarder or 3PL with heavy ocean exposure, ocean ETA accuracy is the primary buying criterion, or customer has specific IoT container tracking requirements

**Deal Example (Win):** NordicHealth Supply — NordicHealth evaluated ShipVis for their ocean pharmaceutical shipments (cold chain from Asian manufacturers). ShipVis's container tracking was appealing, but NordicHealth needed compliance (Sentinel for EU MDR), inventory management, and domestic carrier tracking for Nordics distribution. ACME won as the unified platform. NordicHealth uses a simple carrier-provided temperature logging solution as a workaround for IoT.

**Deal Example (Loss):** A European freight forwarder (mid-size, ~$50K potential ACV) chose ShipVis for their ocean freight operations. They manage 3,000+ containers/month and ShipVis's port congestion analytics and ETA predictions were decisive. The deal was outside ACME's sweet spot.

**Recommended Positioning:**
"ShipVis is the best tool if your only challenge is tracking ocean containers. ACME is the right choice if you need to manage your entire supply chain — from purchase order to customer delivery, across all modes, with forecasting and compliance built in. If ocean freight is 80%+ of your supply chain, talk to ShipVis. If you need a platform, talk to ACME."

---

### 5. Kinaxis

**Company Overview:**
- **Founded:** 1984, Ottawa, Canada (originally Webplan, rebranded Kinaxis in 2005)
- **CEO:** John Sicard
- **Public Company:** TSX: KXS (market cap ~$5B CAD)
- **Headcount:** ~2,200 employees globally
- **ARR:** ~$350M USD (public filings)
- **Customers:** ~350 (large enterprise focus, average ACV ~$1M)

**Product Comparison:**

| Feature | ACME | Kinaxis RapidResponse | Notes |
|---------|------|-----------------------|-------|
| Real-time visibility | Yes | Limited | Not Kinaxis's focus |
| Carrier integrations | 200+ | Minimal (via partners) | Not Kinaxis's focus |
| Demand forecasting | Yes (ML, 90-day horizon) | Yes (ML + statistical, 18-month horizon) | Kinaxis is stronger for long-range |
| S&OP planning | No | Yes (core strength) | Massive Kinaxis advantage |
| Scenario modeling | No | Yes (concurrent planning engine) | Unique Kinaxis capability |
| Supply planning | No | Yes (deep) | Not our market |
| Multi-echelon inventory optimization | No | Yes | Kinaxis advantage |
| Compliance/screening | Yes (Sentinel) | No | ACME only |
| Real-time inventory positions | Yes | Batch (daily refresh) | ACME advantage |
| Analytics/reporting | Yes (Beacon) | Yes (embedded in RapidResponse) | Both capable |
| Implementation time | 4-14 weeks | 6-12 months | ACME advantage |
| Pricing | $30K-$460K ACV | $300K-$3M ACV | ACME is 5-10x cheaper |

**Pricing:**
- Kinaxis RapidResponse: starting at $300K ACV for mid-size deployments
- Typical enterprise deal: $800K-$3M ACV
- Implementation services (via Kinaxis or partners): $500K-$2M additional
- Annual maintenance/support: 20-22% of license fee

**Strengths:**
- Best-in-class S&OP and concurrent planning — their "concurrent planning" engine allows real-time scenario modeling that no competitor matches. Users can ask "what if our top supplier shuts down for 2 weeks?" and get an answer in seconds.
- Deep supply planning: multi-echelon inventory optimization, supply-demand matching, constraint-based planning across the full supply chain.
- Proven at enterprise scale — customers include Unilever, Ford, Merck, P&G. These are the largest and most complex supply chains in the world.
- Strong AI/ML capabilities for demand sensing and supply risk prediction.
- High customer retention (estimated NRR > 115%) — once embedded, customers rarely leave.
- Excellent professional services and customer success organization.

**Weaknesses:**
- Not a visibility platform — Kinaxis focuses on planning, not tracking. Customers still need a separate tool for shipment tracking and carrier integrations.
- Extremely expensive — the total cost of a Kinaxis deployment (license + implementation + training + change management) often exceeds $2M in the first year.
- Long implementation cycles — 6-12 months is standard, 18+ months for complex deployments.
- Target market is large enterprise (500+ employees in supply chain function) — not competitive in mid-market.
- No compliance/screening capabilities.
- UX is functional but not modern — designed for supply chain planners, not broad operations teams.

**Common Sales Objections:**
1. "We need S&OP and planning capabilities" — Response: "We're transparent that ACME is not an S&OP platform. If S&OP is your primary need, Kinaxis may be the right choice. However, many customers use ACME for operational visibility and real-time decision-making alongside a planning tool. ACME and Kinaxis are complementary — one for planning, one for execution."
2. "Kinaxis has scenario modeling" — Response: "Kinaxis's scenario engine is impressive for strategic planning. ACME's focus is on operational intelligence — real-time visibility, ML-driven demand forecasting at SKU level, and automated replenishment. These are different use cases. Many companies need both strategic planning and operational execution."
3. "Kinaxis is the standard for supply chain planning" — Response: "For companies with $500M+ in supply chain spend and dedicated planning teams, Kinaxis is a strong choice. For mid-market companies that need a single platform for visibility, forecasting, and compliance, ACME delivers 80% of the value at 20% of the cost and complexity."

**Win/Loss Analysis:**
- **Overall win rate vs. Kinaxis: 35%**
- We win when: customer is mid-market (Kinaxis is too expensive and complex), the primary need is visibility + forecasting rather than S&OP, compliance is a requirement, or implementation speed is critical
- We lose when: customer is large enterprise with dedicated planning team, S&OP or scenario modeling is the primary buying criterion, or customer has budget for both Kinaxis (planning) and a separate visibility tool

**Deal Example (Win):** SunCoast Beverages — SunCoast considered Kinaxis for demand planning (beverage industry seasonality is complex). After initial scoping, Kinaxis quoted $450K ACV with 9-month implementation. SunCoast's VP Operations, Carlos Vega, decided the ROI didn't justify the investment for a regional distributor. Chose ACME for $88K ACV with 6-week implementation, accepting that ACME's forecasting is less sophisticated than Kinaxis but "good enough for our scale."

**Deal Example (Loss):** Velocity Sports — Lost this customer to Kinaxis when they needed S&OP capabilities (scenario modeling, multi-echelon optimization) that ACME doesn't offer. Velocity churned from ACME in September 2025. See churned accounts documentation for full details.

**Recommended Positioning:**
"Kinaxis is the best planning tool for Fortune 500 supply chains. ACME is the best operational intelligence platform for mid-market businesses. If you have a 50-person supply chain planning team and $2M+ to invest, Kinaxis is the right tool. If you need real-time visibility, smart forecasting, and compliance in a platform that's live in weeks, ACME is the answer. And for companies that need both — ACME and Kinaxis work great side by side."

---

### 6. FourKites

**Company Overview:**
- **Founded:** 2014, Chicago, IL
- **CEO:** Mathew Elenjickal
- **Funding:** Series D ($200M+ total raised). Last round: $100M in 2021, led by Thomas H. Lee Partners.
- **Headcount:** ~1,100 employees
- **ARR:** Estimated $120-150M (private)
- **Customers:** ~1,200 (across enterprise and mid-market)

**Product Comparison:**

| Feature | ACME | FourKites | Notes |
|---------|------|-----------|-------|
| Real-time visibility | Yes | Yes (market-leading) | FourKites is slightly ahead on real-time capability |
| Carrier integrations | 200+ | 350+ | FourKites has largest carrier network (tied with project44) |
| Carrier network effects | N/A | Yes (carriers connect once, all customers benefit) | FourKites's carrier onboarding is frictionless |
| Demand forecasting | Yes (Oracle engine) | No | ACME only |
| Replenishment | Yes | No | ACME only |
| Compliance/screening | Yes (Sentinel) | No | ACME only |
| Yard management | No | Yes | FourKites acquired a yard management company in 2023 |
| Appointment scheduling | No | Yes | FourKites feature |
| Temperature tracking | No (custom for some) | Yes (native) | FourKites advantage for cold chain |
| Analytics | Yes (Beacon) | Yes (Dynamic Yard, Insights) | Both capable, different focus |
| ERP integrations | 4 ERPs | 2 ERPs (SAP, Oracle) | ACME is broader |
| Implementation time | 4-14 weeks | 6-16 weeks | Comparable |

**Pricing:**
- FourKites Core: $3,500/month base + per-shipment fees (varies by mode)
- Mid-Market: $50K-$150K ACV
- Enterprise: $150K-$500K+ ACV
- Yard management and appointment scheduling are premium add-ons ($25K-$75K each)

**Strengths:**
- One of the two largest carrier networks globally (350+ carriers). Carriers connect to FourKites once and all customers benefit from the network effect.
- Strong in multimodal visibility — road, rail, ocean, air, and parcel all tracked in one platform.
- Yard management and appointment scheduling capabilities (via acquisition) — gives them a broader footprint than pure visibility.
- Native temperature tracking for cold chain — pharmaceutical, food, and chemical companies love this.
- Large, well-funded go-to-market team — strong presence at industry events and in RFPs.
- Broader customer base than ShipVis — works with shippers, carriers, 3PLs, and brokers.

**Weaknesses:**
- No demand forecasting or replenishment — pure visibility/execution, not intelligence.
- No compliance or screening capabilities.
- Platform has grown through acquisitions — yard management, appointment scheduling, and some analytics feel bolted on rather than natively integrated. UX is inconsistent across modules.
- Pricing has increased significantly after Series D — some mid-market customers report 30-40% price increases at renewal.
- ERP integrations are narrower than ACME (only SAP and Oracle, no Dynamics or Sage).
- Customer success has scaled with headcount but quality has reportedly dipped (G2 reviews mention "rotating CSMs" and "slow response times").

**Common Sales Objections:**
1. "FourKites has a bigger carrier network" — Response: "True, FourKites has 350+ carriers vs. our 200+. But carrier count alone isn't the right metric. Our 200+ carriers cover 96% of tracked shipment volume for mid-market retailers. The question is: do you need just visibility, or do you need intelligence? FourKites can tell you where a shipment is. ACME can tell you where it is, predict when it'll arrive, forecast what you'll need next, and screen your trading partners for compliance."
2. "FourKites has yard management" — Response: "If yard management is a critical need, FourKites has an offering (via acquisition). However, it's a separate module with separate pricing and a different UX. If yard management isn't your top priority, consider the total value: ACME's forecasting, replenishment, and compliance capabilities provide more ROI for most mid-market retailers."
3. "FourKites has temperature tracking" — Response: "FourKites's native temperature tracking is strong. If cold chain monitoring is your primary buying criterion, they're worth considering. ACME supports cold chain tracking through custom integrations (we do this for NordicHealth and SunCoast Beverages) but it's not a native feature yet. It's on our 2026 roadmap."

**Win/Loss Analysis:**
- **Overall win rate vs. FourKites: 50%**
- We win when: customer values forecasting and compliance alongside visibility, ERP integration breadth matters (Dynamics, Sage), pricing is competitive, or customer is mid-market retailer
- We lose when: carrier network breadth is decisive, yard management or appointment scheduling is needed, cold chain is the primary use case, or customer is a large 3PL/carrier

**Deal Example (Win):** FreshDirect Europe — FreshDirect evaluated FourKites for their EU grocery operations. FourKites had strong temperature tracking (important for fresh food). However, FreshDirect needed compliance screening for cross-border shipments across 8 EU countries — FourKites had no answer for this. ACME won with Nexus + Sentinel. FreshDirect uses their existing carrier temperature logs rather than a platform-native solution.

**Deal Example (Loss):** A US cold chain 3PL (major account, ~$200K potential ACV) chose FourKites primarily for native temperature tracking across 250+ refrigerated carriers. ACME couldn't match this capability. The 3PL also valued yard management for their 12 distribution centers.

**Recommended Positioning:**
"FourKites is a strong visibility platform with the broadest carrier network. ACME is a supply chain intelligence platform that combines visibility with ML forecasting, automated replenishment, and compliance screening. If you only need to track shipments, FourKites is a good option. If you need to track, predict, plan, and comply — ACME does all of that in one platform."

---

### 7. project44

**Company Overview:**
- **Founded:** 2014, Chicago, IL
- **CEO:** Jett McCandless
- **Funding:** Series E ($400M+ total raised). Last round: $80M in 2023.
- **Headcount:** ~1,300 employees (reduced from peak of ~1,500 in 2022 — had layoffs)
- **ARR:** Estimated $150-180M (private, down from peak growth rate)
- **Customers:** ~1,000 (enterprise-heavy)
- **Recent acquisition:** Acquired ComplianceStream (trade compliance startup, $45M acquisition) in September 2025

**Product Comparison:**

| Feature | ACME | project44 | Notes |
|---------|------|-----------|-------|
| Real-time visibility | Yes | Yes (market-leading) | project44 is slightly ahead |
| Carrier integrations | 200+ | 350+ (tied with FourKites) | project44 advantage |
| Ocean visibility | Basic | Strong (acquired Convey in 2021) | project44 advantage |
| Demand forecasting | Yes (Oracle engine) | No | ACME only |
| Replenishment | Yes | No | ACME only |
| Compliance/screening | Yes (Sentinel, mature) | Yes (ComplianceStream, newly acquired) | ACME far ahead today |
| Analytics | Yes (Beacon) | Yes (Movement by project44) | Both capable |
| API quality | Good | Excellent (API-first architecture) | project44 has best-in-class API |
| ERP integrations | 4 ERPs | 3 ERPs (SAP, Oracle, Dynamics) | Comparable |
| Implementation time | 4-14 weeks | 4-12 weeks | Comparable |

**Pricing:**
- project44 base: varies widely by module and volume
- Mid-Market: $60K-$200K ACV
- Enterprise: $200K-$1M+ ACV
- ComplianceStream (compliance module): $30K-$100K add-on (pricing still being finalized post-acquisition)
- API access tiers with volume-based pricing

**Strengths:**
- API-first architecture — project44's API is considered the best in the industry. Developer experience is exceptional (great documentation, SDKs, sandbox environments).
- Massive carrier network (350+ carriers) with strong network effects.
- Strong ocean and intermodal visibility (acquired ocean visibility company Convey in 2021).
- Now has compliance capability via ComplianceStream acquisition — could become a more direct competitor to ACME's full platform story.
- Strong brand in enterprise — "the movement platform" positioning resonates with logistics executives.
- Heavy investment in data and analytics ("Movement" platform) for supply chain benchmarking.

**Weaknesses:**
- ComplianceStream acquisition is very recent (September 2025) and integration is ongoing. The compliance product is not yet fully integrated into the project44 platform — customers report a disjointed experience (separate login, separate UI, separate billing).
- ComplianceStream's capabilities are narrower than Sentinel: only covers US sanctions lists and EU consolidated list. No certificate management, no HS code classification, no audit trail.
- No demand forecasting or replenishment — like FourKites, project44 is visibility/execution, not intelligence.
- Had significant layoffs in 2022-2023, and employee turnover has impacted customer relationships. Multiple reviews on G2 mention "our champion at project44 left."
- Pricing is complex and has increased — customers report difficulty predicting costs due to volume-based pricing across multiple modules.
- Growth has slowed (industry estimates suggest ARR growth declined from 60% in 2021 to ~20% in 2025).

**Common Sales Objections:**
1. "project44 now has compliance with ComplianceStream" — Response: "project44 acquired ComplianceStream in September 2025. It's still a separate product with a separate login and UI. It covers basic US and EU sanctions screening. Compare that to ACME Sentinel, which has been in production since October 2025, with HS code classification, 17 sanctions lists, certificate management, and a complete audit trail. We built compliance natively, they bolted it on."
2. "project44 has the best API" — Response: "Their API is genuinely excellent and we respect their engineering. ACME's API is also well-documented and capable. The question is: what does the API connect you to? project44 gives you visibility. ACME gives you visibility, forecasting, replenishment, and compliance. A great API to a limited dataset is still limited."
3. "project44 has more carriers" — Response: "Same response as FourKites — carrier count is important but not the only factor. We cover the vast majority of shipment volume for mid-market retailers. The incremental carriers in project44's network are largely niche and regional."

**Win/Loss Analysis:**
- **Overall win rate vs. project44: 48%**
- We win when: compliance is a requirement (Sentinel beats ComplianceStream), forecasting is valued, customer is mid-market retailer, or customer is wary of project44's organizational instability (layoffs, turnover)
- We lose when: API quality is the top criterion, carrier network breadth is decisive, customer is enterprise and values the brand, or customer is a tech-forward company that values developer experience

**Deal Example (Win):** Pacific Rim Distributors — Pacific Rim needed visibility + compliance for dual-use electronics export controls. project44 (before ComplianceStream acquisition) had no compliance answer. Even after the acquisition, ComplianceStream doesn't support EAR compliance or Asian country-specific restricted party lists. ACME Sentinel was the decisive factor.

**Deal Example (Loss):** A large European fashion conglomerate (major account, ~$300K potential ACV) chose project44. The deciding factor was API quality — the conglomerate's engineering team wanted to build custom internal tools on top of a visibility API, and project44's developer experience was judged superior. They didn't need forecasting or compliance.

**Recommended Positioning:**
"project44 is a strong visibility API platform. ACME is a supply chain intelligence platform. project44 recently acquired a compliance startup, but it's a bolted-on experience that doesn't compare to ACME's natively-built Sentinel product. If you're an engineering-led organization that wants to build custom tools on top of a visibility API, project44 is worth considering. If you want a complete, natively-integrated platform for visibility, forecasting, compliance, and analytics — ACME is the answer."

---

## Overall Win/Loss Summary

### Win Rates by Competitor (Trailing 12 Months)

| Competitor | Win Rate | Deals Evaluated | Primary Win Factor | Primary Loss Factor |
|------------|----------|----------------|-------------------|-------------------|
| ChainLink | 55% | 82 | Forecasting accuracy (Oracle) | Carrier network breadth |
| Visibly | 70% | 45 | Platform depth | Price sensitivity (SMB) |
| SAP IBP | 45% | 28 | Speed + cost of implementation | SAP ecosystem lock-in |
| ShipVis | 65% | 18 | Full platform vs. ocean-only | Ocean freight specialization |
| Kinaxis | 35% | 22 | Cost + implementation speed | S&OP planning depth |
| FourKites | 50% | 55 | Forecasting + compliance | Carrier network + cold chain |
| project44 | 48% | 40 | Compliance (Sentinel) | API quality + brand |

### Win Rate by Deal Size

| Deal Size | Overall Win Rate | Notes |
|-----------|-----------------|-------|
| < $50K ACV | 58% | Strong in SMB/lower mid-market against Visibly |
| $50K - $150K ACV | 62% | Sweet spot — ACME's core mid-market strength |
| $150K - $300K ACV | 51% | Competitive with FourKites and project44 |
| > $300K ACV | 38% | Struggle against enterprise incumbents (SAP, Kinaxis) |

### Win Rate by Buyer Persona

| Buyer | Win Rate | Notes |
|-------|----------|-------|
| VP/Director of Operations | 65% | Best persona — they value operational intelligence |
| VP/Director of Supply Chain | 55% | Good, but sometimes these buyers lean toward planning tools |
| CIO/CTO | 40% | IT buyers often prefer established vendors or consolidation |
| CFO/Procurement | 42% | Cost-focused buyers sometimes choose cheaper (Visibly) or bundled (SAP) |

### Key Competitive Initiatives (Q2 2026)

1. **Carrier Network Expansion:** Add 50+ carriers by Q4 2026 to close the gap with ChainLink/FourKites/project44. Priority: LTL carriers (for Meridian Automotive and similar customers) and Asian regional carriers.
2. **Cold Chain Native Support:** Productize temperature tracking (currently custom for NordicHealth and SunCoast). Ship native cold chain module by Q3 2026. Directly addresses FourKites competitive weakness.
3. **Sentinel Differentiation Campaign:** Marketing campaign highlighting Sentinel's maturity vs. project44's ComplianceStream acquisition. Case studies from FreshDirect Europe and Pacific Rim.
4. **S&OP Partnership:** Explore partnership with Kinaxis or o9 Solutions for complementary S&OP + ACME operational intelligence positioning. Avoid losing deals to Kinaxis where we could instead be a complementary solution.
5. **Developer Experience Investment:** Improve API documentation, add SDKs (Python, Node.js, Java), launch sandbox environment. Close the gap with project44's developer experience.
6. **Enterprise Battle Cards:** Update all battle cards with Q1 2026 competitive intel. Monthly refresh cadence (currently quarterly).
