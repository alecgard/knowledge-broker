# ACME Nexus Platform API Reference

**Base URL:** `https://api.acme.dev`
**API Version:** v1
**Last Updated:** 2026-03-01

---

## Overview

The ACME Nexus API provides programmatic access to supply chain visibility, inventory management, shipment tracking, forecasting, compliance screening, and reporting capabilities. All endpoints follow REST conventions and return JSON responses.

### SDKs

Official SDKs are available for:

- **Go:** `go get github.com/acme-org/acme-go-sdk`
- **Python:** `pip install acme-nexus`
- **Node.js:** `npm install @acme-org/nexus-sdk`

All SDKs support automatic retries, pagination helpers, and typed request/response models. Source code is available on GitHub under `acme-org/`.

---

## Authentication

All API requests require a Bearer token in the Authorization header:

```
Authorization: Bearer <api_key>
```

API keys are generated in the Nexus web app under Settings > API Keys, or via the `/v1/api-keys` endpoint. Each key is scoped to a specific customer account and has configurable permissions:

| Scope | Description |
|-------|-------------|
| `read` | Read-only access to all resources |
| `read_write` | Read and write access to all resources |
| `admin` | Full access including user management and API key management |

API keys expire after 90 days by default. Enterprise customers can configure custom expiry up to 365 days.

---

## Error Response Format

All errors follow a standard envelope format:

```json
{
  "error": {
    "code": "invalid_request",
    "message": "The 'sku' parameter must be a non-empty string.",
    "details": [
      {
        "field": "sku",
        "reason": "required",
        "message": "SKU is required for this operation."
      }
    ],
    "request_id": "req_8f3a2b1c4d5e6f7a",
    "documentation_url": "https://docs.acme.dev/errors/invalid_request"
  }
}
```

### Error Codes

| HTTP Status | Error Code | Description |
|-------------|------------|-------------|
| 400 | `invalid_request` | Malformed request body or invalid parameters |
| 401 | `unauthorized` | Missing or invalid API key |
| 403 | `forbidden` | API key lacks required permissions |
| 404 | `not_found` | Resource does not exist |
| 409 | `conflict` | Resource already exists or version conflict |
| 422 | `unprocessable_entity` | Request is well-formed but semantically invalid |
| 429 | `rate_limit_exceeded` | Too many requests |
| 500 | `internal_error` | Server error (retry with exponential backoff) |
| 503 | `service_unavailable` | Temporary outage (retry after `Retry-After` header) |

---

## Pagination

All list endpoints use cursor-based pagination with page numbers:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | int | 1 | Page number (1-indexed) |
| `per_page` | int | 50 | Results per page (max 200) |

Response metadata is returned in the `meta` object:

```json
{
  "data": [...],
  "meta": {
    "page": 2,
    "per_page": 50,
    "total": 3420,
    "total_pages": 69
  }
}
```

---

## Filtering and Sorting

### Filtering

Most list endpoints support filtering via query parameters. Filters can be combined with `&`:

```
GET /v1/shipments?status=in_transit&carrier=ups&created_after=2026-03-01T00:00:00Z
```

String filters support wildcard matching with `*`:

```
GET /v1/inventory/positions?sku=SKU-100*
```

### Sorting

Use the `sort` parameter with a field name. Prefix with `-` for descending order:

```
GET /v1/shipments?sort=-created_at
GET /v1/inventory/positions?sort=quantity_available
```

Multiple sort fields are separated by commas:

```
GET /v1/shipments?sort=-status,created_at
```

---

## Rate Limiting

| Tier | Limit | Burst |
|------|-------|-------|
| Standard | 1,000 requests/minute | 50 requests/second |
| Enterprise | 5,000 requests/minute | 200 requests/second |

Rate limit headers are included in every response:

```
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 997
X-RateLimit-Reset: 1709251200
```

When rate limited, the API returns HTTP 429 with a `Retry-After` header indicating seconds until the limit resets. SDKs handle retry automatically with exponential backoff.

---

## Versioning Policy

The API uses URL-based versioning (`/v1/`). Breaking changes are introduced only in new major versions. The current version (`v1`) has been stable since GA in 2023 and will be supported for at least 24 months after `v2` is released.

Non-breaking changes (new fields in responses, new optional parameters, new endpoints) are added to the current version without a version bump. Deprecation notices are communicated via the `X-Deprecation-Notice` response header and email to account admins 90 days before removal.

---

## Inventory

### GET /v1/inventory/positions

Returns current inventory positions across all locations.

**Query parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `sku` | string | No | Filter by SKU. Supports wildcard: `SKU-100*` |
| `location_id` | string | No | Filter by location ID |
| `location_type` | string | No | `warehouse`, `store`, `in_transit` |
| `below_safety_stock` | bool | No | Only return positions below safety stock threshold |
| `page` | int | No | Page number (default 1) |
| `per_page` | int | No | Results per page (default 50, max 200) |

**Response:**

```json
{
  "data": [
    {
      "sku": "SKU-10042",
      "location_id": "WH-PDX-01",
      "location_type": "warehouse",
      "quantity_on_hand": 1250,
      "quantity_reserved": 200,
      "quantity_available": 1050,
      "safety_stock": 500,
      "last_updated": "2026-03-10T14:30:00Z"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 3420,
    "total_pages": 69
  }
}
```

### POST /v1/inventory/positions

Bulk update inventory positions. Accepts up to 500 position updates in a single request. Updates are processed atomically -- either all succeed or none are applied.

**Request body:**

```json
{
  "updates": [
    {
      "sku": "SKU-10042",
      "location_id": "WH-PDX-01",
      "quantity_on_hand": 1300,
      "quantity_reserved": 180,
      "safety_stock": 500,
      "reason": "cycle_count_adjustment",
      "reference": "CC-2026-0312"
    },
    {
      "sku": "SKU-10043",
      "location_id": "WH-PDX-01",
      "quantity_on_hand": 860,
      "quantity_reserved": 50,
      "safety_stock": 200,
      "reason": "receipt",
      "reference": "PO-2026-04821"
    }
  ],
  "idempotency_key": "idem_9a8b7c6d5e4f3210"
}
```

**Response (HTTP 200):**

```json
{
  "data": {
    "updated": 2,
    "failed": 0,
    "errors": [],
    "idempotency_key": "idem_9a8b7c6d5e4f3210",
    "processed_at": "2026-03-10T14:35:00Z"
  }
}
```

**Partial failure response (HTTP 207):**

```json
{
  "data": {
    "updated": 1,
    "failed": 1,
    "errors": [
      {
        "index": 1,
        "sku": "SKU-10043",
        "location_id": "WH-PDX-01",
        "code": "invalid_quantity",
        "message": "quantity_on_hand cannot be negative"
      }
    ],
    "idempotency_key": "idem_9a8b7c6d5e4f3210",
    "processed_at": "2026-03-10T14:35:00Z"
  }
}
```

Valid `reason` values: `cycle_count_adjustment`, `receipt`, `shipment`, `transfer`, `damage`, `return`, `manual_override`.

### GET /v1/inventory/transactions

Returns inventory transaction history. Each change to an inventory position generates a transaction record.

**Query parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `sku` | string | No | Filter by SKU |
| `location_id` | string | No | Filter by location ID |
| `reason` | string | No | Filter by transaction reason |
| `created_after` | ISO 8601 | No | Filter by transaction date |
| `created_before` | ISO 8601 | No | Filter by transaction date |
| `sort` | string | No | Sort field (default `-created_at`) |
| `page` | int | No | Page number (default 1) |
| `per_page` | int | No | Results per page (default 50, max 200) |

**Response:**

```json
{
  "data": [
    {
      "id": "TXN-2026-0048291",
      "sku": "SKU-10042",
      "location_id": "WH-PDX-01",
      "reason": "receipt",
      "reference": "PO-2026-04821",
      "quantity_change": 200,
      "quantity_before": 1050,
      "quantity_after": 1250,
      "created_at": "2026-03-10T14:30:00Z",
      "created_by": "relay-agent:SAP-connector"
    },
    {
      "id": "TXN-2026-0048290",
      "sku": "SKU-10042",
      "location_id": "WH-PDX-01",
      "reason": "shipment",
      "reference": "SHP-2026-0048100",
      "quantity_change": -100,
      "quantity_before": 1150,
      "quantity_after": 1050,
      "created_at": "2026-03-10T13:15:00Z",
      "created_by": "relay-agent:SAP-connector"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 14820,
    "total_pages": 297
  }
}
```

---

## Locations

### GET /v1/locations

Returns all locations configured for the account.

**Query parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `type` | string | No | Filter by type: `warehouse`, `store`, `distribution_center`, `supplier` |
| `country` | string | No | Filter by ISO 3166-1 alpha-2 country code |
| `active` | bool | No | Filter by active status (default: true) |
| `sort` | string | No | Sort field (default `name`) |
| `page` | int | No | Page number (default 1) |
| `per_page` | int | No | Results per page (default 50, max 200) |

**Response:**

```json
{
  "data": [
    {
      "id": "WH-PDX-01",
      "name": "Portland Distribution Center",
      "type": "warehouse",
      "address": {
        "line1": "1200 NW Industrial Way",
        "line2": "Building C",
        "city": "Portland",
        "state": "OR",
        "postal_code": "97209",
        "country": "US"
      },
      "coordinates": {
        "latitude": 45.5355,
        "longitude": -122.6855
      },
      "timezone": "America/Los_Angeles",
      "active": true,
      "metadata": {
        "square_footage": 125000,
        "dock_doors": 24
      },
      "created_at": "2024-06-15T10:00:00Z",
      "updated_at": "2026-01-20T08:30:00Z"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 87,
    "total_pages": 2
  }
}
```

### POST /v1/locations

Create a new location.

**Request body:**

```json
{
  "id": "ST-SEA-12",
  "name": "Seattle Capitol Hill Store",
  "type": "store",
  "address": {
    "line1": "456 Broadway E",
    "city": "Seattle",
    "state": "WA",
    "postal_code": "98102",
    "country": "US"
  },
  "coordinates": {
    "latitude": 47.6253,
    "longitude": -122.3222
  },
  "timezone": "America/Los_Angeles",
  "metadata": {
    "square_footage": 8500,
    "manager": "Pat Reynolds"
  }
}
```

**Response (HTTP 201):**

```json
{
  "data": {
    "id": "ST-SEA-12",
    "name": "Seattle Capitol Hill Store",
    "type": "store",
    "address": {
      "line1": "456 Broadway E",
      "city": "Seattle",
      "state": "WA",
      "postal_code": "98102",
      "country": "US"
    },
    "coordinates": {
      "latitude": 47.6253,
      "longitude": -122.3222
    },
    "timezone": "America/Los_Angeles",
    "active": true,
    "metadata": {
      "square_footage": 8500,
      "manager": "Pat Reynolds"
    },
    "created_at": "2026-03-10T15:00:00Z",
    "updated_at": "2026-03-10T15:00:00Z"
  }
}
```

### PUT /v1/locations/{id}

Update an existing location. Supports partial updates -- only include fields that should change.

**Request body:**

```json
{
  "name": "Seattle Capitol Hill Flagship",
  "metadata": {
    "square_footage": 12000,
    "manager": "Jordan Ellis",
    "remodel_date": "2026-02-15"
  }
}
```

**Response (HTTP 200):**

```json
{
  "data": {
    "id": "ST-SEA-12",
    "name": "Seattle Capitol Hill Flagship",
    "type": "store",
    "address": {
      "line1": "456 Broadway E",
      "city": "Seattle",
      "state": "WA",
      "postal_code": "98102",
      "country": "US"
    },
    "coordinates": {
      "latitude": 47.6253,
      "longitude": -122.3222
    },
    "timezone": "America/Los_Angeles",
    "active": true,
    "metadata": {
      "square_footage": 12000,
      "manager": "Jordan Ellis",
      "remodel_date": "2026-02-15"
    },
    "created_at": "2026-03-10T15:00:00Z",
    "updated_at": "2026-03-10T16:20:00Z"
  }
}
```

### DELETE /v1/locations/{id}

Soft-delete a location. Sets `active` to false. Locations with active inventory positions or open shipments cannot be deleted.

**Response (HTTP 200):**

```json
{
  "data": {
    "id": "ST-SEA-12",
    "deleted": true,
    "message": "Location deactivated. Inventory positions and historical data are preserved."
  }
}
```

**Error (HTTP 409):**

```json
{
  "error": {
    "code": "conflict",
    "message": "Cannot delete location ST-SEA-12: 34 active inventory positions exist. Transfer or zero-out inventory before deleting.",
    "request_id": "req_1a2b3c4d5e6f7890"
  }
}
```

---

## Shipments

### GET /v1/shipments

Returns shipments with tracking information.

**Query parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `status` | string | No | `booked`, `picked_up`, `in_transit`, `out_for_delivery`, `delivered`, `exception` |
| `carrier` | string | No | Carrier code: `fedex`, `ups`, `dhl`, `usps`, `maersk`, etc. |
| `origin` | string | No | Filter by origin location ID |
| `destination` | string | No | Filter by destination location ID |
| `created_after` | ISO 8601 | No | Filter by creation date |
| `created_before` | ISO 8601 | No | Filter by creation date |
| `sort` | string | No | Sort field (default `-created_at`) |
| `page` | int | No | Page number (default 1) |
| `per_page` | int | No | Results per page (default 50, max 200) |

**Response:**

```json
{
  "data": [
    {
      "id": "SHP-2026-0048291",
      "tracking_number": "1Z999AA10123456784",
      "carrier": "ups",
      "status": "in_transit",
      "origin": {"location_id": "WH-PDX-01", "city": "Portland", "state": "OR"},
      "destination": {"location_id": "ST-SEA-05", "city": "Seattle", "state": "WA"},
      "estimated_delivery": "2026-03-12T17:00:00Z",
      "items": [
        {"sku": "SKU-10042", "quantity": 100},
        {"sku": "SKU-10043", "quantity": 50}
      ],
      "events": [
        {"timestamp": "2026-03-10T08:00:00Z", "status": "picked_up", "location": "Portland, OR"},
        {"timestamp": "2026-03-10T14:00:00Z", "status": "in_transit", "location": "Tacoma, WA"}
      ],
      "created_at": "2026-03-09T16:00:00Z",
      "updated_at": "2026-03-10T14:00:00Z"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 891,
    "total_pages": 18
  }
}
```

### POST /v1/shipments

Create a new shipment record.

**Request body:**

```json
{
  "carrier": "fedex",
  "service_level": "ground",
  "origin": {
    "location_id": "WH-PDX-01"
  },
  "destination": {
    "location_id": "ST-CHI-03"
  },
  "items": [
    {"sku": "SKU-10042", "quantity": 200},
    {"sku": "SKU-10099", "quantity": 75}
  ],
  "expected_ship_date": "2026-03-11T08:00:00Z",
  "reference_number": "PO-2026-05100",
  "notes": "Fragile items -- require liftgate delivery."
}
```

**Response (HTTP 201):**

```json
{
  "data": {
    "id": "SHP-2026-0048500",
    "tracking_number": null,
    "carrier": "fedex",
    "service_level": "ground",
    "status": "booked",
    "origin": {"location_id": "WH-PDX-01", "city": "Portland", "state": "OR"},
    "destination": {"location_id": "ST-CHI-03", "city": "Chicago", "state": "IL"},
    "estimated_delivery": "2026-03-16T17:00:00Z",
    "items": [
      {"sku": "SKU-10042", "quantity": 200},
      {"sku": "SKU-10099", "quantity": 75}
    ],
    "events": [],
    "expected_ship_date": "2026-03-11T08:00:00Z",
    "reference_number": "PO-2026-05100",
    "notes": "Fragile items -- require liftgate delivery.",
    "created_at": "2026-03-10T15:45:00Z",
    "updated_at": "2026-03-10T15:45:00Z"
  }
}
```

### PUT /v1/shipments/{id}

Update a shipment. Only shipments in `booked` status can have items modified. Other fields (carrier, service_level, notes) can be updated until the shipment is `delivered`.

**Request body:**

```json
{
  "carrier": "ups",
  "service_level": "2day",
  "tracking_number": "1Z999AA10198765432",
  "notes": "Upgraded to 2-day at customer request."
}
```

**Response (HTTP 200):**

```json
{
  "data": {
    "id": "SHP-2026-0048500",
    "tracking_number": "1Z999AA10198765432",
    "carrier": "ups",
    "service_level": "2day",
    "status": "booked",
    "origin": {"location_id": "WH-PDX-01", "city": "Portland", "state": "OR"},
    "destination": {"location_id": "ST-CHI-03", "city": "Chicago", "state": "IL"},
    "estimated_delivery": "2026-03-13T17:00:00Z",
    "items": [
      {"sku": "SKU-10042", "quantity": 200},
      {"sku": "SKU-10099", "quantity": 75}
    ],
    "events": [],
    "expected_ship_date": "2026-03-11T08:00:00Z",
    "reference_number": "PO-2026-05100",
    "notes": "Upgraded to 2-day at customer request.",
    "created_at": "2026-03-10T15:45:00Z",
    "updated_at": "2026-03-10T16:00:00Z"
  }
}
```

### POST /v1/shipments/{id}/cancel

Cancel a shipment. Only shipments in `booked` or `picked_up` status can be cancelled.

**Request body:**

```json
{
  "reason": "customer_request",
  "notes": "Customer cancelled PO-2026-05100."
}
```

**Response (HTTP 200):**

```json
{
  "data": {
    "id": "SHP-2026-0048500",
    "status": "cancelled",
    "cancelled_at": "2026-03-10T16:30:00Z",
    "cancelled_reason": "customer_request",
    "cancelled_notes": "Customer cancelled PO-2026-05100.",
    "inventory_restored": true,
    "restored_items": [
      {"sku": "SKU-10042", "quantity": 200, "location_id": "WH-PDX-01"},
      {"sku": "SKU-10099", "quantity": 75, "location_id": "WH-PDX-01"}
    ]
  }
}
```

Valid `reason` values: `customer_request`, `inventory_unavailable`, `carrier_issue`, `address_invalid`, `duplicate`, `other`.

---

## Forecasting

### POST /v1/forecast/generate

Triggers a forecast generation for specified SKUs.

**Request body:**

```json
{
  "skus": ["SKU-10042", "SKU-10043"],
  "horizon_days": 90,
  "include_confidence_intervals": true
}
```

**Response (HTTP 202):**

```json
{
  "forecast_id": "FC-2026-00123",
  "status": "processing",
  "estimated_completion": "2026-03-10T15:30:00Z"
}
```

### GET /v1/forecast/{forecast_id}

Returns forecast results. Poll this endpoint until `status` is `complete` or `failed`.

**Response:**

```json
{
  "forecast_id": "FC-2026-00123",
  "status": "complete",
  "generated_at": "2026-03-10T15:28:00Z",
  "forecasts": [
    {
      "sku": "SKU-10042",
      "daily_forecast": [
        {
          "date": "2026-03-11",
          "predicted_demand": 42,
          "confidence_lower": 35,
          "confidence_upper": 51,
          "confidence_level": 0.90
        }
      ],
      "mape": 0.087,
      "model_version": "oracle-v3.2.1"
    }
  ]
}
```

---

## Screening (Sentinel)

### POST /v1/screening/check

Runs a denied party screening.

**Request body:**

```json
{
  "entity_name": "Global Trading Corp",
  "country": "CN",
  "address": "123 Commerce Rd, Shanghai",
  "entity_type": "organization",
  "lists": ["bis_entity", "ofac_sdn", "eu_consolidated"]
}
```

**Response (clear):**

```json
{
  "screening_id": "SCR-2026-99201",
  "result": "clear",
  "checked_at": "2026-03-10T14:35:00Z",
  "matches": [],
  "lists_checked": ["bis_entity", "ofac_sdn", "eu_consolidated"],
  "processing_time_ms": 45
}
```

**Response (potential match):**

```json
{
  "screening_id": "SCR-2026-99202",
  "result": "potential_match",
  "checked_at": "2026-03-10T14:36:00Z",
  "matches": [
    {
      "list": "ofac_sdn",
      "matched_name": "Global Trading Corporation Ltd",
      "similarity_score": 0.92,
      "entry_id": "OFAC-12345",
      "programs": ["SDGT"],
      "remarks": "Added 2024-06-15"
    }
  ],
  "lists_checked": ["bis_entity", "ofac_sdn", "eu_consolidated"],
  "processing_time_ms": 52
}
```

---

## Certificates (Sentinel)

### POST /v1/certificates

Upload a trade compliance certificate.

**Request body:**

```json
{
  "type": "certificate_of_origin",
  "issuing_country": "US",
  "destination_country": "CA",
  "issued_date": "2026-02-15",
  "expiry_date": "2027-02-15",
  "issuing_authority": "US Chamber of Commerce",
  "reference_number": "COO-2026-00451",
  "associated_skus": ["SKU-10042", "SKU-10043", "SKU-10044"],
  "document_url": "s3://acme-certs/customers/CUST-001/COO-2026-00451.pdf",
  "metadata": {
    "trade_agreement": "USMCA",
    "preference_criterion": "B"
  }
}
```

**Response (HTTP 201):**

```json
{
  "data": {
    "id": "CERT-2026-00891",
    "type": "certificate_of_origin",
    "status": "active",
    "issuing_country": "US",
    "destination_country": "CA",
    "issued_date": "2026-02-15",
    "expiry_date": "2027-02-15",
    "issuing_authority": "US Chamber of Commerce",
    "reference_number": "COO-2026-00451",
    "associated_skus": ["SKU-10042", "SKU-10043", "SKU-10044"],
    "document_url": "s3://acme-certs/customers/CUST-001/COO-2026-00451.pdf",
    "days_until_expiry": 342,
    "metadata": {
      "trade_agreement": "USMCA",
      "preference_criterion": "B"
    },
    "created_at": "2026-03-10T15:00:00Z",
    "updated_at": "2026-03-10T15:00:00Z"
  }
}
```

### GET /v1/certificates

List certificates for the account.

**Query parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `type` | string | No | `certificate_of_origin`, `phytosanitary`, `fda_prior_notice`, `dangerous_goods`, `fumigation`, `inspection` |
| `status` | string | No | `active`, `expired`, `expiring_soon` (within 30 days) |
| `sku` | string | No | Filter by associated SKU |
| `destination_country` | string | No | Filter by destination country |
| `sort` | string | No | Sort field (default `expiry_date`) |
| `page` | int | No | Page number (default 1) |
| `per_page` | int | No | Results per page (default 50, max 200) |

**Response:**

```json
{
  "data": [
    {
      "id": "CERT-2026-00891",
      "type": "certificate_of_origin",
      "status": "active",
      "issuing_country": "US",
      "destination_country": "CA",
      "issued_date": "2026-02-15",
      "expiry_date": "2027-02-15",
      "issuing_authority": "US Chamber of Commerce",
      "reference_number": "COO-2026-00451",
      "associated_skus": ["SKU-10042", "SKU-10043", "SKU-10044"],
      "days_until_expiry": 342,
      "created_at": "2026-03-10T15:00:00Z"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 156,
    "total_pages": 4
  }
}
```

### GET /v1/certificates/{id}

Returns a single certificate with full details.

**Response:**

```json
{
  "data": {
    "id": "CERT-2026-00891",
    "type": "certificate_of_origin",
    "status": "active",
    "issuing_country": "US",
    "destination_country": "CA",
    "issued_date": "2026-02-15",
    "expiry_date": "2027-02-15",
    "issuing_authority": "US Chamber of Commerce",
    "reference_number": "COO-2026-00451",
    "associated_skus": ["SKU-10042", "SKU-10043", "SKU-10044"],
    "document_url": "s3://acme-certs/customers/CUST-001/COO-2026-00451.pdf",
    "days_until_expiry": 342,
    "metadata": {
      "trade_agreement": "USMCA",
      "preference_criterion": "B"
    },
    "audit_trail": [
      {"action": "created", "timestamp": "2026-03-10T15:00:00Z", "user": "api-key:ak_prod_8f3a"},
      {"action": "sku_added", "timestamp": "2026-03-10T15:05:00Z", "user": "jane.smith@customer.com", "details": "Added SKU-10044"}
    ],
    "created_at": "2026-03-10T15:00:00Z",
    "updated_at": "2026-03-10T15:05:00Z"
  }
}
```

---

## Classifications (Sentinel)

### GET /v1/classifications

List HS code classification results.

**Query parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `sku` | string | No | Filter by SKU |
| `status` | string | No | `pending_review`, `approved`, `rejected`, `auto_classified` |
| `confidence_min` | float | No | Minimum confidence score (0.0 to 1.0) |
| `country` | string | No | Filter by destination country |
| `page` | int | No | Page number (default 1) |
| `per_page` | int | No | Results per page (default 50, max 200) |

**Response:**

```json
{
  "data": [
    {
      "id": "CLS-2026-04521",
      "sku": "SKU-10042",
      "product_description": "Stainless steel hex bolts, M8 x 30mm, grade A4-80",
      "hs_code": "7318.15.20",
      "hs_description": "Bolts, screws and nuts, of stainless steel",
      "confidence": 0.94,
      "status": "auto_classified",
      "destination_country": "CA",
      "classification_source": "ai",
      "model_version": "sentinel-cls-v2.1",
      "created_at": "2026-03-08T10:00:00Z"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 8420,
    "total_pages": 169
  }
}
```

### POST /v1/classifications/bulk

Submit multiple products for HS code classification. Returns immediately with a batch ID. Results are delivered via webhook (`classification.completed`) or can be polled.

**Request body:**

```json
{
  "items": [
    {
      "sku": "SKU-20001",
      "product_description": "Organic cotton t-shirt, men's, crew neck, size M",
      "destination_country": "GB",
      "origin_country": "BD",
      "material_composition": "100% organic cotton",
      "unit_value_usd": 12.50
    },
    {
      "sku": "SKU-20002",
      "product_description": "Leather belt, women's, genuine cowhide, brass buckle",
      "destination_country": "GB",
      "origin_country": "IT",
      "material_composition": "Cowhide leather, brass",
      "unit_value_usd": 35.00
    }
  ],
  "priority": "standard",
  "callback_url": "https://customer.example.com/webhooks/classifications"
}
```

**Response (HTTP 202):**

```json
{
  "data": {
    "batch_id": "BATCH-CLS-2026-00142",
    "items_submitted": 2,
    "status": "processing",
    "estimated_completion": "2026-03-10T16:00:00Z",
    "priority": "standard"
  }
}
```

Valid `priority` values: `standard` (results within 1 hour), `urgent` (results within 10 minutes, 3x cost).

---

## Webhooks

### GET /v1/webhooks

List all configured webhooks.

**Response:**

```json
{
  "data": [
    {
      "id": "WH-001",
      "url": "https://customer.example.com/webhooks/acme",
      "events": ["shipment.status_changed", "inventory.below_safety_stock"],
      "active": true,
      "secret": "whsec_****************************a1b2",
      "created_at": "2025-11-01T10:00:00Z",
      "last_triggered_at": "2026-03-10T14:22:00Z",
      "failure_count": 0
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 3,
    "total_pages": 1
  }
}
```

### POST /v1/webhooks

Register a new webhook endpoint.

**Request body:**

```json
{
  "url": "https://customer.example.com/webhooks/acme",
  "events": [
    "shipment.status_changed",
    "inventory.below_safety_stock",
    "forecast.completed",
    "screening.match_found"
  ],
  "secret": "my_webhook_secret_string"
}
```

**Response (HTTP 201):**

```json
{
  "data": {
    "id": "WH-004",
    "url": "https://customer.example.com/webhooks/acme",
    "events": [
      "shipment.status_changed",
      "inventory.below_safety_stock",
      "forecast.completed",
      "screening.match_found"
    ],
    "active": true,
    "secret": "whsec_****************************c3d4",
    "created_at": "2026-03-10T15:30:00Z",
    "last_triggered_at": null,
    "failure_count": 0
  }
}
```

**Available events:**

| Event | Description |
|-------|-------------|
| `shipment.status_changed` | Fired when a shipment transitions to a new status |
| `inventory.below_safety_stock` | Fired when inventory drops below safety stock threshold |
| `forecast.completed` | Fired when a forecast generation job completes |
| `screening.match_found` | Fired when a denied party screening finds a potential match |

Webhook payloads are signed with HMAC-SHA256 using the configured secret. The signature is sent in the `X-Acme-Signature-256` header. SDKs provide helper methods for signature verification.

**Retry policy:** Failed deliveries are retried up to 5 times with exponential backoff (1 min, 5 min, 30 min, 2 hours, 12 hours). After 5 consecutive failures, the webhook is automatically deactivated and an email notification is sent to account admins.

### DELETE /v1/webhooks/{id}

Delete a webhook endpoint.

**Response (HTTP 200):**

```json
{
  "data": {
    "id": "WH-004",
    "deleted": true
  }
}
```

### POST /v1/webhooks/{id}/test

Send a test event to the webhook endpoint. Returns the delivery result.

**Request body:**

```json
{
  "event": "shipment.status_changed"
}
```

**Response (HTTP 200):**

```json
{
  "data": {
    "webhook_id": "WH-004",
    "test_event": "shipment.status_changed",
    "delivery_status": "success",
    "response_code": 200,
    "response_time_ms": 145,
    "delivered_at": "2026-03-10T15:35:00Z"
  }
}
```

**Response (delivery failed):**

```json
{
  "data": {
    "webhook_id": "WH-004",
    "test_event": "shipment.status_changed",
    "delivery_status": "failed",
    "response_code": 502,
    "response_time_ms": 30012,
    "error": "Connection timed out after 30 seconds",
    "delivered_at": null
  }
}
```

---

## Users and API Keys

### GET /v1/users

List all users in the account. Requires `admin` scope.

**Query parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `role` | string | No | Filter by role: `viewer`, `editor`, `admin`, `super_admin` |
| `status` | string | No | `active`, `invited`, `deactivated` |
| `page` | int | No | Page number (default 1) |
| `per_page` | int | No | Results per page (default 50, max 200) |

**Response:**

```json
{
  "data": [
    {
      "id": "USR-001",
      "email": "jane.smith@customer.com",
      "name": "Jane Smith",
      "role": "admin",
      "status": "active",
      "last_login_at": "2026-03-10T09:00:00Z",
      "created_at": "2024-08-01T10:00:00Z",
      "sso_enabled": true,
      "mfa_enabled": true
    },
    {
      "id": "USR-002",
      "email": "bob.jones@customer.com",
      "name": "Bob Jones",
      "role": "viewer",
      "status": "active",
      "last_login_at": "2026-03-09T14:30:00Z",
      "created_at": "2025-01-15T12:00:00Z",
      "sso_enabled": true,
      "mfa_enabled": false
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 24,
    "total_pages": 1
  }
}
```

### POST /v1/users/invite

Invite a new user to the account. Sends an invitation email.

**Request body:**

```json
{
  "email": "alice.wong@customer.com",
  "name": "Alice Wong",
  "role": "editor",
  "teams": ["logistics", "warehouse-ops"],
  "message": "Welcome to ACME Nexus! You've been added to the logistics team."
}
```

**Response (HTTP 201):**

```json
{
  "data": {
    "id": "USR-025",
    "email": "alice.wong@customer.com",
    "name": "Alice Wong",
    "role": "editor",
    "status": "invited",
    "invitation_sent_at": "2026-03-10T15:00:00Z",
    "invitation_expires_at": "2026-03-17T15:00:00Z"
  }
}
```

### PUT /v1/users/{id}/role

Update a user's role. Requires `super_admin` scope to promote to `admin` or `super_admin`.

**Request body:**

```json
{
  "role": "admin"
}
```

**Response (HTTP 200):**

```json
{
  "data": {
    "id": "USR-002",
    "email": "bob.jones@customer.com",
    "name": "Bob Jones",
    "role": "admin",
    "previous_role": "viewer",
    "updated_at": "2026-03-10T16:00:00Z",
    "updated_by": "USR-001"
  }
}
```

### GET /v1/api-keys

List API keys for the account. Key values are partially masked.

**Response:**

```json
{
  "data": [
    {
      "id": "AK-001",
      "name": "Production Integration",
      "prefix": "ak_prod_8f3a",
      "scope": "read_write",
      "created_at": "2025-06-01T10:00:00Z",
      "expires_at": "2026-06-01T10:00:00Z",
      "last_used_at": "2026-03-10T14:55:00Z",
      "created_by": "USR-001"
    },
    {
      "id": "AK-002",
      "name": "Beacon Reports",
      "prefix": "ak_prod_2c1d",
      "scope": "read",
      "created_at": "2025-09-15T08:00:00Z",
      "expires_at": "2025-12-15T08:00:00Z",
      "last_used_at": null,
      "created_by": "USR-001",
      "status": "expired"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 5,
    "total_pages": 1
  }
}
```

### POST /v1/api-keys

Create a new API key. The full key value is returned only once in the response.

**Request body:**

```json
{
  "name": "Warehouse Scanner Integration",
  "scope": "read_write",
  "expires_in_days": 180,
  "allowed_ips": ["203.0.113.0/24", "198.51.100.0/24"]
}
```

**Response (HTTP 201):**

```json
{
  "data": {
    "id": "AK-006",
    "name": "Warehouse Scanner Integration",
    "key": "ak_prod_9x8y7z6w5v4u3t2s1r0q_FULL_KEY_SHOWN_ONCE",
    "prefix": "ak_prod_9x8y",
    "scope": "read_write",
    "created_at": "2026-03-10T16:00:00Z",
    "expires_at": "2026-09-06T16:00:00Z",
    "allowed_ips": ["203.0.113.0/24", "198.51.100.0/24"],
    "created_by": "USR-001"
  }
}
```

**Important:** The `key` field is returned only in the creation response. Store it securely. It cannot be retrieved again.

### DELETE /v1/api-keys/{id}

Revoke an API key immediately.

**Response (HTTP 200):**

```json
{
  "data": {
    "id": "AK-006",
    "revoked": true,
    "revoked_at": "2026-03-10T17:00:00Z",
    "revoked_by": "USR-001"
  }
}
```

---

## Relay (Integration Hub)

### GET /v1/relay/connectors

List all configured Relay connectors for the account.

**Response:**

```json
{
  "data": [
    {
      "id": "CONN-001",
      "name": "SAP S/4HANA Production",
      "type": "sap_s4hana",
      "status": "connected",
      "direction": "bidirectional",
      "last_sync_at": "2026-03-10T14:50:00Z",
      "sync_interval_minutes": 5,
      "records_synced_today": 14200,
      "error_count_today": 3,
      "agent_version": "3.1.4",
      "created_at": "2024-09-01T10:00:00Z"
    },
    {
      "id": "CONN-002",
      "name": "FedEx Tracking",
      "type": "fedex",
      "status": "connected",
      "direction": "inbound",
      "last_sync_at": "2026-03-10T14:55:00Z",
      "sync_interval_minutes": 5,
      "records_synced_today": 8450,
      "error_count_today": 0,
      "agent_version": "3.1.4",
      "created_at": "2024-09-15T10:00:00Z"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 4,
    "total_pages": 1
  }
}
```

### GET /v1/relay/connectors/{id}/status

Returns detailed health and status information for a specific connector.

**Response:**

```json
{
  "data": {
    "id": "CONN-001",
    "name": "SAP S/4HANA Production",
    "type": "sap_s4hana",
    "status": "connected",
    "agent": {
      "version": "3.1.4",
      "hostname": "relay-agent-prod-01.customer.internal",
      "uptime_hours": 720,
      "memory_usage_mb": 1240,
      "cpu_percent": 12.5
    },
    "sync_health": {
      "last_successful_sync": "2026-03-10T14:50:00Z",
      "last_failed_sync": "2026-03-09T03:15:00Z",
      "last_failure_reason": "SAP RFC timeout after 30s",
      "success_rate_24h": 0.998,
      "avg_sync_duration_ms": 4500,
      "records_synced_24h": 142000,
      "errors_24h": 3
    },
    "pipeline": {
      "kafka_consumer_lag": 42,
      "dead_letter_queue_size": 1,
      "throughput_records_per_sec": 45.2
    },
    "checked_at": "2026-03-10T15:00:00Z"
  }
}
```

### POST /v1/relay/connectors/{id}/sync

Trigger an immediate manual sync for a connector. Useful when the scheduled interval is too long or after resolving an issue.

**Request body (optional):**

```json
{
  "full_sync": false,
  "entity_types": ["inventory", "purchase_orders"],
  "since": "2026-03-10T00:00:00Z"
}
```

Set `full_sync` to `true` to re-sync all data from the source system. This is resource-intensive and should be used sparingly.

**Response (HTTP 202):**

```json
{
  "data": {
    "sync_id": "SYNC-2026-08841",
    "connector_id": "CONN-001",
    "type": "incremental",
    "entity_types": ["inventory", "purchase_orders"],
    "status": "started",
    "started_at": "2026-03-10T15:05:00Z",
    "estimated_duration_seconds": 120
  }
}
```

### GET /v1/relay/sync-logs

Returns sync execution history across all connectors.

**Query parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `connector_id` | string | No | Filter by connector |
| `status` | string | No | `success`, `partial_failure`, `failed` |
| `created_after` | ISO 8601 | No | Filter by date |
| `page` | int | No | Page number (default 1) |
| `per_page` | int | No | Results per page (default 50, max 200) |

**Response:**

```json
{
  "data": [
    {
      "id": "SYNC-2026-08841",
      "connector_id": "CONN-001",
      "connector_name": "SAP S/4HANA Production",
      "type": "incremental",
      "status": "success",
      "started_at": "2026-03-10T15:05:00Z",
      "completed_at": "2026-03-10T15:07:12Z",
      "duration_seconds": 132,
      "records_processed": 450,
      "records_created": 12,
      "records_updated": 438,
      "records_failed": 0,
      "errors": []
    },
    {
      "id": "SYNC-2026-08800",
      "connector_id": "CONN-001",
      "connector_name": "SAP S/4HANA Production",
      "type": "incremental",
      "status": "partial_failure",
      "started_at": "2026-03-09T03:15:00Z",
      "completed_at": "2026-03-09T03:15:32Z",
      "duration_seconds": 32,
      "records_processed": 210,
      "records_created": 5,
      "records_updated": 202,
      "records_failed": 3,
      "errors": [
        {
          "record_id": "SAP-MATDOC-992810",
          "entity_type": "inventory_movement",
          "error": "Invalid plant code 'ZZ99' -- not mapped in schema configuration",
          "retryable": false
        }
      ]
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 4320,
    "total_pages": 87
  }
}
```

---

## Beacon (Analytics and Reporting)

### GET /v1/reports

List generated reports.

**Query parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `status` | string | No | `queued`, `generating`, `complete`, `failed` |
| `type` | string | No | `inventory_summary`, `shipment_performance`, `forecast_accuracy`, `compliance_audit`, `custom` |
| `created_after` | ISO 8601 | No | Filter by date |
| `sort` | string | No | Sort field (default `-created_at`) |
| `page` | int | No | Page number (default 1) |
| `per_page` | int | No | Results per page (default 50, max 200) |

**Response:**

```json
{
  "data": [
    {
      "id": "RPT-2026-00341",
      "name": "Weekly Inventory Summary",
      "type": "inventory_summary",
      "format": "pdf",
      "status": "complete",
      "parameters": {
        "date_range": {"start": "2026-03-03", "end": "2026-03-09"},
        "locations": ["WH-PDX-01", "WH-CHI-02"],
        "include_charts": true
      },
      "file_size_bytes": 2450000,
      "generated_at": "2026-03-10T06:00:00Z",
      "download_expires_at": "2026-03-17T06:00:00Z",
      "created_by": "scheduled:weekly-inventory",
      "created_at": "2026-03-10T05:55:00Z"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 156,
    "total_pages": 4
  }
}
```

### POST /v1/reports

Generate a new report.

**Request body:**

```json
{
  "name": "March Carrier Performance",
  "type": "shipment_performance",
  "format": "excel",
  "parameters": {
    "date_range": {
      "start": "2026-03-01",
      "end": "2026-03-10"
    },
    "carriers": ["fedex", "ups"],
    "metrics": ["on_time_rate", "damage_rate", "avg_transit_days", "cost_per_shipment"],
    "group_by": "carrier"
  },
  "schedule": null,
  "recipients": ["jane.smith@customer.com", "logistics-team@customer.com"]
}
```

**Response (HTTP 202):**

```json
{
  "data": {
    "id": "RPT-2026-00355",
    "name": "March Carrier Performance",
    "type": "shipment_performance",
    "format": "excel",
    "status": "queued",
    "estimated_completion": "2026-03-10T15:10:00Z",
    "created_at": "2026-03-10T15:05:00Z"
  }
}
```

### GET /v1/reports/{id}/download

Returns a pre-signed download URL for a completed report.

**Response:**

```json
{
  "data": {
    "id": "RPT-2026-00341",
    "download_url": "https://acme-reports.s3.us-west-2.amazonaws.com/RPT-2026-00341.pdf?X-Amz-Signature=...",
    "expires_at": "2026-03-10T16:05:00Z",
    "file_size_bytes": 2450000,
    "format": "pdf"
  }
}
```

### GET /v1/dashboards

List all dashboards for the account.

**Response:**

```json
{
  "data": [
    {
      "id": "DASH-001",
      "name": "Operations Overview",
      "description": "Real-time view of inventory levels, shipment status, and forecast accuracy.",
      "widgets": 12,
      "visibility": "team",
      "owner": "USR-001",
      "last_viewed_at": "2026-03-10T14:00:00Z",
      "created_at": "2025-04-15T10:00:00Z",
      "updated_at": "2026-02-20T09:00:00Z"
    },
    {
      "id": "DASH-002",
      "name": "Carrier Scorecard",
      "description": "Performance metrics for all carriers including on-time delivery and cost analysis.",
      "widgets": 8,
      "visibility": "account",
      "owner": "USR-003",
      "last_viewed_at": "2026-03-09T16:30:00Z",
      "created_at": "2025-08-01T12:00:00Z",
      "updated_at": "2026-01-10T14:00:00Z"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 7,
    "total_pages": 1
  }
}
```

### POST /v1/dashboards

Create a new dashboard.

**Request body:**

```json
{
  "name": "Compliance Dashboard",
  "description": "Sentinel screening results, certificate status, and classification metrics.",
  "visibility": "account",
  "widgets": [
    {
      "type": "metric_card",
      "title": "Screenings Today",
      "data_source": "screening_checks",
      "metric": "count",
      "time_range": "today",
      "position": {"x": 0, "y": 0, "w": 3, "h": 2}
    },
    {
      "type": "pie_chart",
      "title": "Certificate Status",
      "data_source": "certificates",
      "group_by": "status",
      "position": {"x": 3, "y": 0, "w": 3, "h": 4}
    },
    {
      "type": "line_chart",
      "title": "Classification Confidence Trend",
      "data_source": "classifications",
      "metric": "avg_confidence",
      "time_range": "30d",
      "granularity": "day",
      "position": {"x": 0, "y": 2, "w": 6, "h": 4}
    }
  ]
}
```

**Response (HTTP 201):**

```json
{
  "data": {
    "id": "DASH-008",
    "name": "Compliance Dashboard",
    "description": "Sentinel screening results, certificate status, and classification metrics.",
    "widgets": 3,
    "visibility": "account",
    "owner": "USR-001",
    "embed_url": "https://app.acme.dev/beacon/dashboards/DASH-008/embed",
    "created_at": "2026-03-10T15:30:00Z",
    "updated_at": "2026-03-10T15:30:00Z"
  }
}
```

---

## Webhook Event Payloads

All webhook deliveries include the following headers:

```
Content-Type: application/json
X-Acme-Event: shipment.status_changed
X-Acme-Delivery-ID: del_a1b2c3d4e5f6
X-Acme-Signature-256: sha256=5d7f8a3b2c1e9f0d...
X-Acme-Timestamp: 1710082200
```

### shipment.status_changed

```json
{
  "event": "shipment.status_changed",
  "timestamp": "2026-03-10T14:30:00Z",
  "data": {
    "shipment_id": "SHP-2026-0048291",
    "tracking_number": "1Z999AA10123456784",
    "carrier": "ups",
    "previous_status": "picked_up",
    "new_status": "in_transit",
    "location": "Tacoma, WA",
    "estimated_delivery": "2026-03-12T17:00:00Z"
  }
}
```

### inventory.below_safety_stock

```json
{
  "event": "inventory.below_safety_stock",
  "timestamp": "2026-03-10T14:35:00Z",
  "data": {
    "sku": "SKU-10042",
    "location_id": "WH-PDX-01",
    "quantity_available": 480,
    "safety_stock": 500,
    "deficit": 20,
    "last_replenishment_date": "2026-03-01T10:00:00Z"
  }
}
```

### forecast.completed

```json
{
  "event": "forecast.completed",
  "timestamp": "2026-03-10T15:28:00Z",
  "data": {
    "forecast_id": "FC-2026-00123",
    "skus_forecasted": 42,
    "horizon_days": 90,
    "average_mape": 0.091,
    "model_version": "oracle-v3.2.1"
  }
}
```

### screening.match_found

```json
{
  "event": "screening.match_found",
  "timestamp": "2026-03-10T14:36:00Z",
  "data": {
    "screening_id": "SCR-2026-99202",
    "entity_name": "Global Trading Corp",
    "result": "potential_match",
    "match_count": 1,
    "highest_similarity": 0.92,
    "lists_matched": ["ofac_sdn"],
    "requires_review": true
  }
}
```

---

## Changelog

| Date | Change |
|------|--------|
| 2026-03-01 | Added bulk classification endpoint (`POST /v1/classifications/bulk`) |
| 2026-02-15 | Added `allowed_ips` field to API key creation |
| 2026-01-20 | Added `full_sync` option to manual sync trigger |
| 2025-12-01 | Added webhook test endpoint (`POST /v1/webhooks/{id}/test`) |
| 2025-11-01 | Sentinel endpoints (certificates, classifications, screening) GA |
| 2025-09-15 | Added `sort` parameter to all list endpoints |
| 2025-08-01 | Increased max `per_page` from 100 to 200 |
| 2025-06-01 | Initial v1 API release |
