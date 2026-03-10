# ACME Org — Customer Profiles & Account Intelligence

**Last updated:** 2026-03-10
**Maintained by:** Revenue Operations (rev-ops@acme.dev)
**Classification:** Internal — Confidential

---

## Table of Contents

1. [Active Enterprise Accounts](#active-enterprise-accounts)
2. [Active Mid-Market Accounts](#active-mid-market-accounts)
3. [Active SMB Accounts](#active-smb-accounts)
4. [Churned Accounts](#churned-accounts)
5. [Customer Segments & Metrics](#customer-segments--metrics)

---

## Active Enterprise Accounts

### 1. GlobalMart

**Industry:** Big-box retail
**Size:** 2,500 stores, 85,000 employees across US and Canada
**Headquarters:** Bentonville, Arkansas

**Products & Revenue:**
- Nexus (Enterprise tier): $280,000/year
- Relay: $95,000/year
- Beacon (Enterprise tier): $75,000/year
- **Total ACV: $450,000**

**Integration Details:**
- ERP: SAP S/4HANA (on-premise, version 2023)
- WMS: Manhattan Associates (WMOS)
- Carriers: FedEx, UPS, USPS, GlobalMart Private Fleet (custom TMS integration)
- eCommerce: Custom platform (headless commerce on Salesforce Commerce Cloud)
- 12M shipments/year tracked through Nexus
- Relay agent deployed on dedicated VM in GlobalMart data center (on-premise)

**CSM:** Jennifer Walsh (Enterprise CSM, 8 years industry experience)

**Key Contacts:**
- **Executive Sponsor:** Martin Reeves, SVP Supply Chain Operations (martin.reeves@globalmart.com)
- **Day-to-Day:** Sandra Kowalski, Director of Logistics Technology (s.kowalski@globalmart.com)
- **Technical Lead:** Bryan Park, Senior Systems Architect (b.park@globalmart.com)
- **Procurement:** Diane Flores, VP Vendor Management (d.flores@globalmart.com)

**Account History:**
- Signed: January 2022 (initial ACV $180K — Nexus + Relay only)
- Renewal 1: January 2023 — expanded to Beacon, ACV grew to $310K
- Renewal 2: January 2024 — upgraded to Enterprise tier, ACV grew to $390K
- Renewal 3: January 2025 — added custom Private Fleet TMS connector, ACV grew to $450K
- Next renewal: January 2027 (3-year agreement signed in 2025)

**Health Score:** 82/100 (trend: stable)

**Open Issues & Risks:**
- RISK: GlobalMart is evaluating Kinaxis for S&OP planning. If they adopt Kinaxis, there may be pressure to consolidate supply chain tools. Jennifer Walsh has a meeting with Martin Reeves on 2026-03-20 to discuss ACME's planning roadmap.
- ISSUE: SAP IDoc processing performance — GlobalMart generates 50MB+ IDoc batches that trigger the known Relay memory leak (REL-4521). They are using the split-batch workaround but it adds 20 minutes to their nightly sync cycle. Fix planned for Relay 3.2.
- ISSUE: Private Fleet TMS connector has intermittent data gaps. Relay team investigating — suspected issue with TMS API pagination when fleet exceeds 500 active vehicles.

**Notes & Special Arrangements:**
- Dedicated Slack channel: #acme-globalmart (ACME side: Jennifer Walsh, James Okafor, Priya Sharma)
- Quarterly executive sponsor meetings with ACME CEO Alex Rivera
- Custom SLA: 99.99% uptime for Nexus API (stricter than standard 99.95%). Penalty clause: 5% credit for each hour below SLA in a given month.
- GlobalMart has "most favored nation" pricing clause — they are guaranteed pricing no worse than any other customer at equivalent volume.
- Annual on-site planning session at GlobalMart HQ in September

---

### 2. FreshDirect Europe

**Industry:** Online grocery delivery
**Size:** 8 countries (UK, Germany, France, Netherlands, Belgium, Spain, Italy, Portugal), 12,000 employees
**Headquarters:** Amsterdam, Netherlands

**Products & Revenue:**
- Nexus: $165,000/year
- Relay: $70,000/year
- Sentinel: $95,000/year (heavy screening volume — 500K screenings/month)
- **Total ACV: $330,000**

**Integration Details:**
- ERP: Oracle NetSuite (cloud)
- WMS: Custom proprietary WMS ("FreshTrack")
- Carriers: DHL, DPD, PostNL, Correos, GLS
- eCommerce: Custom platform (React frontend, Node.js backend)
- EU data residency requirement — all data processed in eu-west-1 cluster
- Custom WMS connector built by Relay team (6-week engagement, completed August 2024)

**CSM:** Thomas Eriksson (Enterprise CSM, based in London office)

**Key Contacts:**
- **Executive Sponsor:** Pieter van den Berg, CTO (pieter.vdb@freshdirect.eu)
- **Day-to-Day:** Claudia Richter, Head of Supply Chain Systems (c.richter@freshdirect.eu)
- **Compliance Lead:** Isabelle Moreau, Director of Trade Compliance (i.moreau@freshdirect.eu)
- **Technical Lead:** Andrei Popescu, Platform Engineer (a.popescu@freshdirect.eu)

**Account History:**
- Signed: June 2023 (initial ACV $140K — Nexus + Relay)
- Expansion: October 2025 — first Sentinel customer, ACV grew to $330K
- Next renewal: June 2026

**Health Score:** 91/100 (trend: improving)

**Open Issues & Risks:**
- RISK: Renewal coming up June 2026. Pieter van den Berg has asked for multi-year pricing. Thomas Eriksson is preparing a 3-year proposal with 8% annual discount in exchange for case study participation and beta program commitment.
- ISSUE: NetSuite SuiteTalk rate limiting during initial catalog sync. FreshDirect has 180K+ SKUs (grocery catalog is massive). Workaround in place but initial syncs after schema changes take 6+ hours.
- OPPORTUNITY: FreshDirect expanding to Poland and Czech Republic in Q3 2026. Will need additional Sentinel screening capacity and new carrier integrations (InPost, PPL).

**Notes & Special Arrangements:**
- First Sentinel customer — provided extensive product feedback during beta. Isabelle Moreau is on the Sentinel Advisory Board.
- All data processing agreements reviewed by ACME Legal and FreshDirect DPO quarterly.
- GDPR-specific audit trail retention: 5 years for standard data, 7 years for screening results.
- FreshDirect logo and case study rights granted in contract.

---

### 3. Pacific Rim Distributors

**Industry:** Electronics distribution, Asia-Pacific
**Size:** Operations in 14 countries, 6,200 employees
**Headquarters:** Singapore

**Products & Revenue:**
- Nexus: $140,000/year
- Relay: $85,000/year
- Beacon: $55,000/year
- Sentinel: $80,000/year
- **Total ACV: $360,000**

**Integration Details:**
- ERP: Microsoft Dynamics 365 (cloud)
- WMS: Blue Yonder (on-premise, version 9.x)
- Carriers: Maersk, SF Express, DHL, Nippon Express, Kerry Logistics, CMA CGM
- eCommerce: N/A (B2B distribution, EDI-based ordering)
- Complex multi-leg shipments: factory -> regional DC -> country DC -> retailer
- 6 Relay connectors running simultaneously (most complex Relay deployment)

**CSM:** David Yamamoto (Enterprise CSM, based in Singapore office)

**Key Contacts:**
- **Executive Sponsor:** Henry Lau, COO (henry.lau@pacrim-dist.com)
- **Day-to-Day:** Mei Lin Tan, VP Supply Chain Technology (ml.tan@pacrim-dist.com)
- **Compliance Lead:** Kenji Watanabe, Director Export Controls (k.watanabe@pacrim-dist.com)
- **Technical Lead:** Rajesh Iyer, IT Director (r.iyer@pacrim-dist.com)

**Account History:**
- Signed: March 2023 (initial ACV $120K — Nexus + Relay)
- Expansion 1: September 2023 — added Beacon, ACV grew to $175K
- Expansion 2: March 2024 — expanded Relay connectors, ACV grew to $240K
- Expansion 3: November 2025 — added Sentinel for EAR compliance, ACV grew to $360K
- Next renewal: March 2026 (renewal in progress — David Yamamoto leading)

**Health Score:** 76/100 (trend: declining)

**Open Issues & Risks:**
- RISK: Health score declining due to Relay stability issues. With 6 connectors, any one going down creates a cascade of stale data alerts. Pacific Rim escalated twice in February 2026.
- ISSUE: Blue Yonder WMS connector (on-premise) has connectivity issues due to Pacific Rim's network infrastructure in their Vietnam DC. Frequent disconnections — see RB-002. James Okafor and team working on a more resilient reconnection strategy.
- ISSUE: EDI X12 856 parser doesn't handle Pacific Rim's ASN structures (5 levels of hierarchical nesting, we support 4). Blocking their Indonesia distribution expansion. Fix prioritized for Relay 3.2.
- RISK: Renewal is in March 2026 — Henry Lau has expressed concern about Relay reliability. David Yamamoto is proposing a dedicated Relay support engineer and 15% discount on Relay pricing to secure renewal.

**Notes & Special Arrangements:**
- Custom screening rules for EAR (Export Administration Regulations) on dual-use electronics components
- Sentinel configured with enhanced screening for 12 additional country-specific restricted party lists (Japan, South Korea, Taiwan, etc.)
- Time zone coverage: David Yamamoto provides SGT business hours coverage; Jennifer Walsh (US-based) provides PT coverage as backup
- Quarterly compliance review meetings with ACME Sentinel team and Pacific Rim export controls team

---

### 4. NordicHealth Supply

**Industry:** Medical device and pharmaceutical distribution
**Size:** 4 Nordic countries (Sweden, Norway, Denmark, Finland), 3,800 employees
**Headquarters:** Stockholm, Sweden

**Products & Revenue:**
- Nexus: $125,000/year
- Relay: $60,000/year
- Sentinel: $70,000/year
- Beacon: $45,000/year
- **Total ACV: $300,000**

**Integration Details:**
- ERP: SAP S/4HANA (cloud edition)
- WMS: Körber (HighJump) v2024.1
- Carriers: PostNord, DHL, DB Schenker, Bring
- eCommerce: N/A (B2B, EDI X12 850/856/810)
- GDP (Good Distribution Practice) compliance requirements for pharma chain of custody
- Cold chain temperature tracking integration (custom sensor data feed into Nexus)

**CSM:** Thomas Eriksson (also handles FreshDirect Europe — both EU-based Enterprise accounts)

**Key Contacts:**
- **Executive Sponsor:** Lars Johansson, VP Supply Chain (lars.j@nordichealthsupply.se)
- **Day-to-Day:** Katarina Lindqvist, Head of IT (k.lindqvist@nordichealthsupply.se)
- **Regulatory Lead:** Erik Andersen, Quality & Compliance Director (e.andersen@nordichealthsupply.se)
- **Technical Lead:** Mikko Virtanen, Systems Integration Manager (m.virtanen@nordichealthsupply.se)

**Account History:**
- Signed: August 2024 (initial ACV $185K — Nexus + Relay + Sentinel)
- Expansion: February 2025 — added Beacon for regulatory reporting, ACV grew to $300K
- Next renewal: August 2027 (3-year term)

**Health Score:** 88/100 (trend: stable)

**Open Issues & Risks:**
- ISSUE: Cold chain temperature data integration is a custom solution built by Relay team. Not fully productized — requires manual updates when NordicHealth adds new sensor hardware.
- OPPORTUNITY: NordicHealth exploring expansion into Germany and Poland. Would significantly increase shipment volume and screening requirements.
- RISK: Erik Andersen has asked about HIPAA compliance for potential US market entry. We are not HIPAA compliant and this is not on the roadmap.

**Notes & Special Arrangements:**
- Custom Sentinel configuration for EU MDR (Medical Device Regulation) compliance tracking
- Certificate management module used for GDP certificates, CE marking documentation, and regulatory approvals
- Monthly compliance reporting via Beacon scheduled reports (automated PDF delivery to Erik Andersen)
- EU data residency — runs on eu-west-1 cluster alongside FreshDirect Europe

---

### 5. Atlas Freight Corporation

**Industry:** Third-party logistics (3PL) and freight forwarding
**Size:** 25 offices globally, 5,500 employees
**Headquarters:** Chicago, Illinois

**Products & Revenue:**
- Nexus (Enterprise tier): $200,000/year
- Relay: $110,000/year
- Beacon (Enterprise tier): $65,000/year
- Sentinel: $85,000/year
- **Total ACV: $460,000**

**Integration Details:**
- ERP: Oracle NetSuite
- WMS: Manhattan Associates (multi-site deployment across 8 DCs)
- TMS: MercuryGate (custom connector)
- Carriers: 50+ carrier integrations (Atlas manages shipments for their clients)
- eCommerce: N/A (B2B logistics provider)
- Unique deployment: Atlas uses ACME as their client-facing visibility platform — white-labeled Nexus dashboards for Atlas's own customers

**CSM:** Jennifer Walsh (co-manages with a dedicated implementation engineer)

**Key Contacts:**
- **Executive Sponsor:** Greg Hanson, CEO (greg.hanson@atlasfreight.com)
- **Day-to-Day:** Rebecca Torres, VP Technology (r.torres@atlasfreight.com)
- **Operations Lead:** Mike Donnelly, SVP Operations (m.donnelly@atlasfreight.com)
- **Technical Lead:** Sanjay Mehta, Director of Engineering (s.mehta@atlasfreight.com)

**Account History:**
- Signed: May 2024 (initial ACV $260K — Nexus + Relay + Beacon)
- Expansion: January 2025 — added Sentinel for customs brokerage compliance, ACV grew to $345K
- Expansion: October 2025 — upgraded to Enterprise tier, white-label agreement, ACV grew to $460K
- Next renewal: May 2027 (3-year term)

**Health Score:** 85/100 (trend: improving)

**Open Issues & Risks:**
- ISSUE: White-label Nexus dashboards have limited customization options. Atlas wants deeper branding control (custom color schemes, logo placement, domain mapping). Frontend team has this on Q3 2026 roadmap.
- OPPORTUNITY: Atlas onboarding 3 new enterprise clients in Q2 2026. Each client will add ~2M shipments/year to their Nexus volume. Potential ACV increase of $80-120K based on per-shipment pricing.
- RISK: MercuryGate TMS connector is a custom build. MercuryGate releases major updates annually and connector compatibility has broken twice during upgrades.

**Notes & Special Arrangements:**
- White-label agreement allows Atlas to present Nexus dashboards under their "AtlasVision" brand
- Custom pricing: per-shipment fee reduced to $0.35 (from standard $0.50) due to volume commitment of 15M+ shipments/year
- Dedicated implementation engineer (from ACME Implementation Team) for ongoing Atlas client onboarding
- Quarterly product roadmap preview sessions — Atlas gets early access to beta features

---

### 6. Meridian Automotive Parts

**Industry:** Automotive aftermarket parts distribution
**Size:** 340 distribution centers across North America, 9,200 employees
**Headquarters:** Detroit, Michigan

**Products & Revenue:**
- Nexus: $180,000/year
- Relay: $75,000/year
- Beacon: $50,000/year
- **Total ACV: $305,000**

**Integration Details:**
- ERP: SAP S/4HANA (on-premise, heavily customized)
- WMS: Blue Yonder (cloud, v2024.2)
- Carriers: FedEx, UPS, XPO Logistics, Old Dominion, Estes Express (LTL focus)
- eCommerce: N/A (B2B, proprietary ordering portal)
- EDI X12 for all supplier and customer communications
- 8M shipments/year, heavy LTL and partial truckload

**CSM:** Marcus Green (Enterprise CSM)

**Key Contacts:**
- **Executive Sponsor:** Patricia Mendez, SVP Supply Chain (p.mendez@meridianauto.com)
- **Day-to-Day:** Steve Kowalczyk, Director Supply Chain Systems (s.kowalczyk@meridianauto.com)
- **Technical Lead:** Pradeep Nair, Senior IT Manager (p.nair@meridianauto.com)

**Account History:**
- Signed: November 2023 (initial ACV $155K — Nexus + Relay)
- Expansion: June 2024 — added Beacon, ACV grew to $205K
- Expansion: March 2025 — increased Relay connectors for LTL carriers, ACV grew to $305K
- Next renewal: November 2026

**Health Score:** 79/100 (trend: stable)

**Open Issues & Risks:**
- ISSUE: Forecast engine accuracy is below target for automotive parts. Demand patterns are highly seasonal and event-driven (e.g., winter weather spikes demand for batteries, wipers). Intelligence Squad working on automotive-specific seasonality model.
- ISSUE: LTL carrier tracking is less granular than parcel — many LTL carriers only provide pickup/delivery scans, no in-transit updates. Customers complain about "black hole" between pickup and delivery.
- RISK: Meridian evaluating ChainLink for their carrier network breadth (especially LTL). Patricia Mendez mentioned this in January QBR.

**Notes & Special Arrangements:**
- Custom demand forecast model tuning session scheduled quarterly with Intelligence Squad
- LTL carrier integration priority: ACME committed to adding 10 additional LTL carriers by Q4 2026
- Meridian participates in ACME's customer advisory board (annual event, last held October 2025)

---

## Active Mid-Market Accounts

### 7. UrbanThreads

**Industry:** Direct-to-consumer fashion brand
**Size:** 15 retail stores + eCommerce, 450 employees
**Headquarters:** Brooklyn, New York

**Products & Revenue:**
- Nexus: $42,000/year
- Relay: $18,000/year
- **Total ACV: $60,000**

**Integration Details:**
- ERP: N/A (Shopify serves as system of record)
- WMS: ShipStation
- Carriers: FedEx, UPS, USPS
- eCommerce: Shopify Plus
- 200K shipments/year

**CSM:** Maria Chen (Mid-Market CSM)

**Key Contacts:**
- **Executive Sponsor:** Rachel Kim, COO (rachel@urbanthreads.com)
- **Day-to-Day:** Devon Brooks, Head of Operations (devon@urbanthreads.com)
- **Technical:** N/A (no dedicated IT team — Devon handles technology decisions)

**Account History:**
- Signed: September 2022 (initial ACV $36K — Nexus only)
- Expansion: March 2023 — added Relay for Shopify integration, ACV grew to $48K
- Renewal: September 2024 — price increase to $60K based on shipment volume growth
- Next renewal: September 2026

**Health Score:** 95/100 (trend: stable)

**Open Issues & Risks:**
- No material risks. UrbanThreads is the most satisfied customer in the portfolio.
- OPPORTUNITY: UrbanThreads opening 5 new stores in 2026 and launching wholesale channel. May need Beacon for advanced reporting and potentially Sentinel if they start importing directly from overseas manufacturers.

**Notes & Special Arrangements:**
- Published case study: "How UrbanThreads Reduced Stockouts by 34% with ACME Nexus" (blog.acme.dev/urbanthreads)
- Reference customer — available for prospect calls (Rachel Kim has done 8 reference calls in the past year)
- Very active product feedback contributor: ~5 feature requests/month via in-app widget
- Invited to speak at ACME's annual customer summit (ChainConnect 2026)

---

### 8. HomeBase Co-op

**Industry:** Home improvement cooperative
**Size:** 120 member stores, 2,800 employees (central organization)
**Headquarters:** Des Moines, Iowa

**Products & Revenue:**
- Nexus: $55,000/year
- Beacon: $25,000/year
- **Total ACV: $80,000**

**Integration Details:**
- ERP: Sage Intacct (central organization)
- WMS: N/A (individual stores manage own warehousing)
- Carriers: FedEx, UPS, various regional carriers
- eCommerce: BigCommerce (shared online storefront)
- Custom multi-tenant Nexus configuration: each co-op member sees own inventory + shared purchasing pool

**CSM:** Sarah Liu (Mid-Market CSM)

**Key Contacts:**
- **Executive Sponsor:** Don Mitchell, Executive Director (d.mitchell@homebasecoop.org)
- **Day-to-Day:** Karen Anderson, Director of Technology (k.anderson@homebasecoop.org)
- **Purchasing Lead:** Jim Frazier, VP Purchasing (j.frazier@homebasecoop.org)

**Account History:**
- Signed: January 2024 (initial ACV $55K — Nexus only)
- Expansion: July 2024 — added Beacon for member reporting, ACV grew to $80K
- Next renewal: January 2027 (3-year term)

**Health Score:** 72/100 (trend: declining)

**Open Issues & Risks:**
- ISSUE: Multi-tenant configuration requires manual setup for each new co-op member. 12 new members joined in 2025 and onboarding each one took 2 weeks of implementation team time. Karen Anderson frustrated with turnaround time.
- ISSUE: Individual co-op member stores have diverse IT setups. Some still use spreadsheets for inventory. Data quality varies significantly across members.
- RISK: Don Mitchell asked about pricing restructuring — current per-member pricing model becomes expensive as co-op grows. Sarah Liu escalated to Rev Ops for a volume-based pricing proposal.

**Notes & Special Arrangements:**
- Unique multi-tenant architecture: central co-op org has admin view of all members; each member has isolated view of their own data + read-only view of shared purchasing pool
- Custom Beacon dashboards for cooperative purchasing analysis (which members are ordering what, shared volume leverage with suppliers)
- Annual on-site training session for new co-op members (ACME Implementation Team travels to Des Moines)

---

### 9. SunCoast Beverages

**Industry:** Specialty beverage distribution (craft beer, wine, spirits)
**Size:** Southeast US (8 states), 1,200 employees
**Headquarters:** Tampa, Florida

**Products & Revenue:**
- Nexus: $48,000/year
- Relay: $22,000/year
- Sentinel: $18,000/year
- **Total ACV: $88,000**

**Integration Details:**
- ERP: Microsoft Dynamics 365 Business Central
- WMS: Körber (HighJump)
- Carriers: FedEx, UPS, various LTL carriers, internal fleet
- eCommerce: N/A (B2B distribution)
- Sentinel used for TTB (Alcohol and Tobacco Tax and Trade Bureau) compliance and state liquor authority reporting

**CSM:** Maria Chen (Mid-Market CSM)

**Key Contacts:**
- **Executive Sponsor:** Carlos Vega, VP Operations (c.vega@suncoastbev.com)
- **Day-to-Day:** Tammy Rodriguez, Supply Chain Manager (t.rodriguez@suncoastbev.com)
- **Compliance:** Frank DeLuca, Compliance Director (f.deluca@suncoastbev.com)

**Account History:**
- Signed: April 2024 (initial ACV $48K — Nexus only)
- Expansion: September 2024 — added Relay, ACV grew to $70K
- Expansion: March 2025 — added Sentinel for compliance, ACV grew to $88K
- Next renewal: April 2027 (3-year term)

**Health Score:** 84/100 (trend: improving)

**Open Issues & Risks:**
- ISSUE: Sentinel's certificate management module doesn't natively support state-level liquor licenses and permits. SunCoast is using it with custom fields as a workaround but Frank DeLuca has requested native support.
- OPPORTUNITY: SunCoast exploring expansion to Mid-Atlantic states. Would add 3 new DCs and increase shipment volume by ~40%.

**Notes & Special Arrangements:**
- Custom Sentinel compliance configuration for alcohol distribution regulations (TTB, state-level reporting)
- Temperature-controlled shipment tracking for wine (leveraging similar approach to NordicHealth cold chain but less complex)
- Frank DeLuca provides regular feedback on Sentinel compliance features — invited to Sentinel Advisory Board alongside FreshDirect's Isabelle Moreau

---

### 10. Greenfield Organics

**Industry:** Organic food distribution
**Size:** West Coast US (CA, OR, WA), 800 employees
**Headquarters:** Sacramento, California

**Products & Revenue:**
- Nexus: $35,000/year
- Relay: $15,000/year
- **Total ACV: $50,000**

**Integration Details:**
- ERP: NetSuite
- WMS: N/A (custom inventory system built on Airtable, integrated via Relay REST API connector)
- Carriers: FedEx, OnTrac, internal delivery fleet
- eCommerce: Shopify (B2B wholesale portal)
- Cold chain requirements for perishable goods

**CSM:** Sarah Liu (Mid-Market CSM)

**Key Contacts:**
- **Executive Sponsor:** Amy Nakamura, CEO & Founder (amy@greenfieldorganics.com)
- **Day-to-Day:** Jake Sullivan, Operations Manager (jake@greenfieldorganics.com)

**Account History:**
- Signed: June 2024 (initial ACV $35K — Nexus only)
- Expansion: December 2024 — added Relay for NetSuite integration, ACV grew to $50K
- Next renewal: June 2026

**Health Score:** 68/100 (trend: declining)

**Open Issues & Risks:**
- ISSUE: Airtable-based inventory system is a poor fit for Relay integration. REST API connector works but data freshness is limited to 15-minute polling intervals (Airtable API limitations). Jake Sullivan frequently reports stale inventory data.
- ISSUE: Cold chain tracking gap — Greenfield ships perishables with temperature monitors but there is no automated way to ingest temperature data into Nexus (unlike NordicHealth, which has a custom integration).
- RISK: Amy Nakamura considering switching to a proper WMS (evaluating ShipBob and ShipHero). During transition, ACME integration will need to be rebuilt. Risk of churn if transition is painful.
- RISK: At current ACV, Greenfield is at the low end of Mid-Market. If they don't expand, may not justify CSM coverage.

**Notes & Special Arrangements:**
- Startup-friendly payment terms: quarterly billing (most customers are annual)
- Jake Sullivan is in ACME's beta program and tests new Relay connectors
- Greenfield featured in ACME blog post about sustainable supply chains

---

### 11. CopperRange Mining Supply

**Industry:** Mining equipment and supplies distribution
**Size:** Western US and Canada, 2,100 employees
**Headquarters:** Salt Lake City, Utah

**Products & Revenue:**
- Nexus: $58,000/year
- Relay: $27,000/year
- Beacon: $15,000/year
- **Total ACV: $100,000**

**Integration Details:**
- ERP: SAP Business One
- WMS: N/A (SAP WM module)
- Carriers: FedEx Freight, XPO Logistics, Old Dominion, Estes Express, flatbed/specialized carriers
- eCommerce: N/A (B2B, phone/email ordering transitioning to EDI)
- Heavy/oversized shipments — many items exceed standard parcel dimensions

**CSM:** Marcus Green (Enterprise CSM — CopperRange is at the Enterprise/Mid-Market boundary)

**Key Contacts:**
- **Executive Sponsor:** Bill Thornton, CFO (b.thornton@copperrange.com)
- **Day-to-Day:** Lisa Huang, Director of Logistics (l.huang@copperrange.com)
- **Technical Lead:** Derek Olsen, IT Manager (d.olsen@copperrange.com)

**Account History:**
- Signed: February 2025 (initial ACV $85K — Nexus + Relay)
- Expansion: November 2025 — added Beacon, ACV grew to $100K
- Next renewal: February 2028 (3-year term)

**Health Score:** 77/100 (trend: stable)

**Open Issues & Risks:**
- ISSUE: Oversized/heavy shipment tracking is incomplete. Many specialized carriers (flatbed, crane services) don't have API integrations. Manual tracking entry is required for ~15% of CopperRange's shipments.
- ISSUE: SAP Business One connector is less mature than S/4HANA connector. Missing some inventory valuation fields that CopperRange's finance team needs.
- OPPORTUNITY: CopperRange acquiring a competitor (RockSolid Supply) in Q2 2026. If acquisition closes, combined entity would nearly double in size. Potential ACV increase to $180K+.

**Notes & Special Arrangements:**
- SAP Business One connector improvements prioritized by Relay team as part of mid-market connector initiative
- CopperRange willing to be a design partner for heavy/oversized shipment tracking features
- Bill Thornton is cost-conscious — pushed hard on pricing during initial negotiation. 3-year term got them 12% discount.

---

## Active SMB Accounts

### 12. PedalWorks Cycling

**Industry:** Cycling equipment and accessories retail
**Size:** 4 stores + eCommerce, 65 employees
**Headquarters:** Boulder, Colorado

**Products & Revenue:**
- Nexus: $18,000/year
- Relay: $6,000/year
- **Total ACV: $24,000**

**Integration Details:**
- ERP: N/A (Shopify POS)
- WMS: ShipStation
- Carriers: FedEx, UPS, USPS
- eCommerce: Shopify

**CSM:** Pooled (automated engagement, no dedicated CSM)

**Key Contacts:**
- **Primary:** Mark Jensen, Owner (mark@pedalworks.com)

**Account History:**
- Signed: March 2025 (self-serve onboarding, ACV $18K)
- Expansion: August 2025 — added Relay, ACV grew to $24K
- Next renewal: March 2026

**Health Score:** 61/100 (trend: declining)

**Open Issues & Risks:**
- RISK: Renewal coming up March 2026. Mark Jensen has not engaged with product in 3 weeks (no logins). Automated re-engagement email sent.
- ISSUE: PedalWorks is seasonal — 70% of revenue is April-September. Off-season shipment volume doesn't justify the monthly cost. Mark has asked about seasonal pricing.

**Notes & Special Arrangements:**
- Self-serve onboarding — no implementation team involvement
- Considering seasonal pricing model: Rev Ops evaluating feasibility for SMB segment

---

## Churned Accounts

### 13. Velocity Sports (CHURNED — Product Gaps)

**Industry:** Sporting goods wholesale distribution
**Size:** National (US), 1,800 employees
**Headquarters:** Denver, Colorado
**Status:** CHURNED — contract ended September 2025
**Final ACV:** $125,000

**Products Purchased (at time of churn):**
- Nexus: $75,000/year
- Relay: $35,000/year
- Beacon: $15,000/year

**What Happened:**
Velocity Sports signed in March 2023 with strong enthusiasm. Initial deployment went well. However, starting in Q1 2024, Velocity began requesting advanced S&OP (Sales & Operations Planning) capabilities that ACME does not offer:

1. **Integrated demand and supply planning** — Velocity wanted a single platform where demand forecasts fed directly into production scheduling and supplier capacity planning. ACME's Oracle forecast engine produces demand forecasts but has no supply-side planning.
2. **Multi-echelon inventory optimization (MEIO)** — Velocity has 4 tiers of inventory (factory, regional DC, local DC, retail partner). They wanted automated optimization of safety stock levels across all tiers simultaneously. ACME only optimizes at single-location level.
3. **Scenario modeling** — Velocity wanted to run "what-if" scenarios (e.g., "what happens if our Vietnam supplier lead time increases by 2 weeks?"). ACME has no scenario planning capability.

Velocity's CSM (Marcus Green) escalated these requirements to Product in Q2 2024. Lisa Nakamura (VP Product) confirmed that S&OP planning was on the long-term roadmap but not prioritized for 2025. Velocity's operations director, Craig Palmer, began evaluating Kinaxis in August 2024.

**Churn Timeline:**
- March 2024: Craig Palmer formally requests S&OP features, escalated to Product
- June 2024: Product confirms S&OP not on 2025 roadmap
- August 2024: Velocity begins Kinaxis evaluation
- December 2024: Velocity signs Kinaxis contract ($320K ACV)
- March 2025: Velocity gives 6-month notice of non-renewal
- September 2025: Contract ends, data exported, account deactivated

**Churned To:** Kinaxis

**Lessons Learned:**
- Need a clearer "what we are and aren't" positioning for prospects with S&OP needs — should have qualified this out earlier or set expectations
- Product gap in scenario planning/what-if analysis is a recurring theme — also mentioned by GlobalMart and Meridian Automotive
- Consider partnerships with S&OP vendors rather than trying to build everything in-house
- Rev Ops added S&OP requirements as a qualification criterion in the sales process to avoid similar situations

**Win-Back Potential:** Low. Kinaxis contract is 3 years. Velocity is satisfied with Kinaxis for planning but uses FourKites for visibility (lost both the planning and visibility pieces).

---

### 14. BrightStar Electronics (CHURNED — Poor Implementation)

**Industry:** Consumer electronics retail
**Size:** 85 stores + eCommerce, 3,200 employees
**Headquarters:** Atlanta, Georgia
**Status:** CHURNED — contract terminated February 2025 (early termination)
**Final ACV:** $180,000

**Products Purchased (at time of churn):**
- Nexus: $110,000/year
- Relay: $50,000/year
- Beacon: $20,000/year

**What Happened:**
BrightStar signed in June 2023 with an expected 8-week implementation timeline. The implementation became a disaster:

1. **Week 1-4:** Discovery phase went well. Implementation team mapped BrightStar's SAP ECC (legacy, not S/4HANA) integration requirements. This was the first SAP ECC deployment for ACME — the Relay connector was only certified for S/4HANA.
2. **Week 5-8:** SAP ECC connector development began. The Relay team had to build custom IDoc mapping for ECC-specific formats. Original 8-week timeline extended to 16 weeks.
3. **Week 9-16:** ECC connector delivered but with significant bugs. IDoc processing errors caused inventory position discrepancies of up to 30%. BrightStar's operations team lost trust in the data.
4. **Week 17-24:** Bug fixes deployed iteratively. Each fix resolved one issue but introduced another. BrightStar escalated to their executive sponsor (VP Supply Chain, Sharon Lee), who demanded a meeting with ACME CEO.
5. **Month 7:** Alex Rivera (ACME CEO) met with Sharon Lee. Offered 6 months of free service and dedicated engineering support. Sharon agreed to continue but set a firm deadline: full functionality by month 10 or early termination.
6. **Month 10:** System was 85% functional but BrightStar's finance team found discrepancies between Nexus inventory valuations and SAP ECC GL entries. BrightStar invoked early termination clause.

**Churn Timeline:**
- June 2023: Contract signed, implementation begins
- October 2023: Implementation timeline doubles (8 weeks -> 16 weeks)
- February 2024: Severe data quality issues, executive escalation
- March 2024: CEO meeting, 6-month remediation agreement
- September 2024: Partial functionality, continuing issues
- November 2024: BrightStar notifies of early termination
- February 2025: Contract terminated, data exported

**Churned To:** ChainLink (which already had a certified SAP ECC connector)

**Lessons Learned:**
- Never sell an integration we haven't certified. The SAP ECC connector was sold as "coming soon" when it was actually 6+ months from production readiness.
- Implementation team should have stronger authority to push back on sales commitments that require uncertified connectors.
- Added a mandatory "integration certification check" to the sales handoff process. If the required integration isn't certified, deal must be approved by VP Engineering.
- James Okafor (Relay Lead) implemented a connector readiness scoring system: Green (certified), Yellow (beta), Red (not started). Sales can only commit Green connectors without VP Eng approval.
- SAP ECC connector was eventually completed and certified (May 2025) — 4 other customers now use it successfully. The BrightStar experience, while painful, funded the ECC connector development.

**Win-Back Potential:** Medium. BrightStar's ChainLink contract is 2 years (ends June 2025 — already expired). Sharon Lee was contacted by Jennifer Walsh in January 2026 — BrightStar is "cautiously interested" in re-evaluating. SAP ECC connector is now stable.

---

### 15. TerraVerde Outdoor (CHURNED — Acquisition)

**Industry:** Outdoor recreation gear retail
**Size:** 45 stores + eCommerce, 1,500 employees
**Headquarters:** Portland, Oregon (ACME's hometown)
**Status:** CHURNED — contract ended July 2025
**Final ACV:** $72,000

**Products Purchased (at time of churn):**
- Nexus: $45,000/year
- Relay: $18,000/year
- Beacon: $9,000/year

**What Happened:**
TerraVerde was an early ACME customer (signed January 2022) and had been a happy, referenceable account. In Q4 2024, Apex Outdoor Group (a PE-backed outdoor retail holding company) acquired TerraVerde. Apex's portfolio of 6 outdoor brands was standardized on ChainLink for supply chain visibility.

Despite ACME's efforts to retain the account:
1. Jennifer Walsh (CSM) offered significant discount (30% reduction, ACV would have been $50K)
2. ACME Sales Engineering did a technical comparison showing ACME superiority in forecasting and compliance
3. Alex Rivera personally called Apex's COO, but the decision was made at portfolio level

Apex mandated ChainLink standardization across all portfolio companies for volume pricing leverage. TerraVerde's operations team was disappointed — several team members privately told Jennifer they preferred ACME.

**Churn Timeline:**
- November 2024: TerraVerde acquired by Apex Outdoor Group
- December 2024: Apex IT team begins supply chain platform audit
- February 2025: Apex decides to standardize on ChainLink
- April 2025: TerraVerde gives 3-month notice
- July 2025: Contract ends, data exported

**Churned To:** ChainLink (mandated by parent company Apex Outdoor Group)

**Lessons Learned:**
- M&A risk is largely uncontrollable but should be monitored. Added "M&A risk" as a health score factor — CSMs now track customer M&A news via Google Alerts and Crunchbase.
- Consider offering PE/holding company level agreements that span portfolio companies — could have positioned to win the Apex standardization decision.
- Local (Portland) customer relationships don't protect against corporate-level decisions.
- TerraVerde's operations team members are advocates — they may influence future vendor selection at other companies. Jennifer Walsh maintains LinkedIn relationships.

**Win-Back Potential:** Low while under Apex ownership. However, if Apex divests TerraVerde or any portfolio company, those would be warm leads. Monitoring via CRM.

---

## Customer Segments & Metrics

### Segment Definitions

| Segment | ACV Range | CSM Model | QBR Frequency | Implementation Time |
|---------|-----------|-----------|---------------|---------------------|
| Enterprise | > $100K | Dedicated CSM (8-12 accounts each) | Quarterly | 8-14 weeks |
| Mid-Market | $25K - $100K | Dedicated CSM (25-40 accounts each) | Bi-annual | 4-8 weeks |
| SMB | < $25K | Pooled / automated | Annual (automated) | Self-serve or 1-2 weeks |

### Current Segment Distribution (as of Q1 2026)

| Segment | Customer Count | % of Total | Total ARR | % of ARR | Avg ACV |
|---------|---------------|------------|-----------|----------|---------|
| Enterprise | 142 | 11.8% | $28.4M | 59.2% | $200K |
| Mid-Market | 485 | 40.4% | $15.5M | 32.3% | $32K |
| SMB | 573 | 47.8% | $4.1M | 8.5% | $7.2K |
| **Total** | **1,200** | **100%** | **$48.0M** | **100%** | **$40K** |

### Revenue Retention & Churn Metrics (Trailing 12 Months)

| Metric | Enterprise | Mid-Market | SMB | Overall |
|--------|-----------|------------|-----|---------|
| Gross Revenue Retention | 95% | 90% | 78% | 91% |
| Net Revenue Retention (NRR) | 118% | 108% | 92% | 110% |
| Logo Churn Rate | 5% | 10% | 22% | 14% |
| Expansion Rate | 23% | 18% | 14% | 19% |

**Commentary on Metrics:**
- Enterprise NRR of 118% is driven by upsell into Sentinel (new product) and volume-based expansion. Target is 120%+.
- SMB logo churn of 22% is a concern. Primary drivers: customers outgrowing SMB tier (positive churn — upgrades to Mid-Market), seasonal businesses not seeing year-round value, and price sensitivity.
- Overall NRR of 110% is healthy for B2B SaaS but below best-in-class peers (115-125%). Sentinel adoption is the primary growth lever.
- Mid-Market segment is the fastest-growing by customer count (35% YoY growth vs. 20% for Enterprise and 15% for SMB).

### Average Implementation Time by Segment

| Segment | Nexus Only | Nexus + Relay | Full Suite (Nexus + Relay + Beacon + Sentinel) |
|---------|-----------|---------------|----------------------------------------------|
| Enterprise | 4 weeks | 8 weeks | 14 weeks |
| Mid-Market | 2 weeks | 4 weeks | 8 weeks |
| SMB | Self-serve (< 1 day) | 1 week | N/A (SMB rarely purchases full suite) |

**Implementation Bottlenecks:**
- Relay connector certification is the longest pole in the tent. If a customer needs a connector that isn't certified, add 6-12 weeks for development and certification.
- Sentinel implementation requires compliance configuration unique to each customer's regulatory requirements. Average 3 weeks of compliance setup with customer's legal/compliance team.
- Enterprise implementations often delayed by customer IT resource availability (change management, firewall rules, VPN setup for on-premise connectors).

### NPS Scores by Segment (Last Survey: January 2026)

| Segment | NPS | Response Rate | Promoters | Passives | Detractors |
|---------|-----|---------------|-----------|----------|------------|
| Enterprise | 52 | 78% | 62% | 28% | 10% |
| Mid-Market | 45 | 55% | 54% | 36% | 10% |
| SMB | 28 | 32% | 40% | 48% | 12% |
| **Overall** | **42** | **48%** | **50%** | **38%** | **12%** |

**NPS Commentary:**
- Enterprise NPS of 52 is strong, driven by dedicated CSM relationships and quarterly business reviews.
- Mid-Market NPS of 45 is adequate but has room for improvement. Common feedback: "love the product, wish onboarding was faster."
- SMB NPS of 28 is below target (35). Low response rate may skew results. Primary detractor theme: "too expensive for what we use" and "hard to set up without help."
- Top promoter theme across all segments: "Nexus gives us visibility we never had before — it's a game changer for our operations team."
- Top detractor theme across all segments: "Integration setup was more complex than expected" and "would like more self-serve configuration options."

### Key Customer Success Initiatives (Q2 2026)

1. **SMB Self-Serve Improvement:** Reduce time-to-value for SMB customers. Goal: first meaningful dashboard within 30 minutes of sign-up. Currently takes 2-3 hours on average.
2. **Mid-Market Health Score Automation:** Implement predictive health scoring using product usage signals (login frequency, feature adoption, support ticket volume). Currently health scores are manually updated by CSMs.
3. **Enterprise Expansion Playbook:** Formalize the Sentinel cross-sell motion. 40% of Enterprise customers have not yet adopted Sentinel. Goal: 60% Sentinel penetration by end of 2026.
4. **Churn Prevention Program:** Proactive outreach to accounts with declining health scores. Pilot with 50 at-risk Mid-Market accounts in Q2 2026.
5. **Implementation Time Reduction:** Target 25% reduction in average implementation time through better tooling (automated connector configuration, self-serve Relay agent deployment).
