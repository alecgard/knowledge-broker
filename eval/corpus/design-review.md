# Design Review: Notification System — 2025-09-22

## Attendees

Present: Elena Vasquez (tech lead), Tom Park (frontend engineer), Dana Liu
(infrastructure), and Raj Mehta (product owner).

## Background

Elena opened by explaining why they needed a new notification system. The
existing one — built two years ago by a contractor who is no longer with the
company — was a monolithic cron job that polled the database every 60 seconds
and sent emails via SendGrid. It couldn't handle real-time notifications, had
no support for push notifications, and was tightly coupled to the email
provider.

## Architecture Proposal

She proposed replacing it with an event-driven architecture using NATS
JetStream as the message broker. Services would publish events to named
subjects, and the notification service would subscribe to relevant subjects
and fan out to delivery channels (email, push, in-app). She chose NATS over
Kafka because of its operational simplicity and lower resource footprint —
they only have three engineers on infrastructure and couldn't justify the
overhead of a Kafka cluster.

Tom asked whether the frontend would connect to NATS directly. She said no —
the frontend would receive real-time notifications through a WebSocket
connection to the API gateway, which would subscribe to NATS on behalf of
authenticated users. He was concerned about connection limits. She said the
API gateway already uses connection pooling and they'd tested it up to 10,000
concurrent WebSocket connections on a single instance.

## Delivery Channels

Raj asked about push notification support. She confirmed that the new system
would support three channels: email via SendGrid (keeping the existing
provider), push notifications via Firebase Cloud Messaging (FCM), and in-app
notifications stored in PostgreSQL. Each channel would have its own delivery
worker to isolate failures — if FCM goes down, email and in-app delivery
would continue unaffected.

Dana raised a concern about message ordering. She explained that NATS
JetStream provides at-least-once delivery with configurable acknowledgment.
For notification ordering, the system would use sequence numbers per user to
ensure in-app notifications display in the correct order, even if delivery
workers process them out of order. She noted that email and push are
inherently unordered, so ordering guarantees only apply to the in-app channel.

## Rate Limiting and Preferences

He asked about notification fatigue — users getting too many notifications.
She said the system would implement per-user rate limiting with configurable
thresholds. The default would be 50 notifications per hour per channel. Users
could also configure their preferences through the settings page: which
notification types they want to receive, through which channels, and during
what hours (a "quiet hours" feature). He suggested adding a digest mode where
low-priority notifications are batched and sent as a daily summary instead of
individually. She agreed and said they would implement that in the second
phase.

## Migration Strategy

Tom asked how they would migrate from the old system to the new one. She
outlined a three-phase approach: first, deploy the new system in shadow mode
where it processes events but doesn't actually deliver notifications — it
just logs what it would have sent. Second, run both systems in parallel for
two weeks, comparing output to verify the new system matches the old one.
Third, cut over by disabling the cron job and enabling delivery in the new
system.

Dana asked about rollback. She said the old cron job could be re-enabled
within minutes if something went wrong, since it's completely independent.
The new system's shadow mode logs would also provide an audit trail of
everything it processed during the parallel run.

## Storage and Retention

He asked about notification storage. She said in-app notifications would be
stored in PostgreSQL with a 90-day retention period. After 90 days, they
would be archived to S3 in Parquet format for compliance purposes. The
notification table would use partitioning by month to keep query performance
consistent as the table grows.

Dana noted that they should plan for the storage growth. She estimated
roughly 500,000 notifications per day at current scale, growing to 2 million
per day within the next year. At an average of 500 bytes per notification
row, that's about 30GB per month after the growth. She recommended using
TimescaleDB's compression feature to reduce storage by roughly 90%.

## Error Handling

Tom asked what happens when a delivery fails. She explained the retry
strategy: each channel worker would retry failed deliveries with exponential
backoff, starting at 1 second and capping at 5 minutes, with a maximum of 8
retries. After exhausting retries, the notification would be marked as
"failed" and an alert sent to the #notifications-oncall Slack channel. She
emphasised that failed deliveries should never block the pipeline — the
worker would move on to the next notification immediately after scheduling a
retry.

## Timeline

Raj asked about the timeline. She estimated eight weeks total: two weeks for
the core event pipeline and NATS integration, two weeks for the three
delivery workers, two weeks for the frontend WebSocket integration and
preferences UI, and two weeks for the shadow mode migration. He pushed back
on the timeline, saying the product team wanted it in six weeks. She said she
could compress the frontend work to one week if they deferred the preferences
UI to a follow-up release — users would initially get all notification types
through all channels, with the ability to unsubscribe only via email links.
He agreed to that compromise.

## Security Considerations

Dana asked about security. She said all notifications would go through the
existing authentication and authorisation layer. Push notification tokens
from FCM would be encrypted at rest using AES-256. The WebSocket connections
would require a valid JWT and would be terminated if the token expires
without renewal. She also noted that notification content would be sanitised
before storage to prevent XSS in the in-app notification display.

## Monitoring

Tom asked how they would monitor the new system. She listed the key metrics:
notification delivery latency (p50, p95, p99), delivery success rate per
channel, NATS consumer lag, WebSocket connection count, and retry queue
depth. She said all metrics would be exposed as Prometheus counters and
histograms, with Grafana dashboards. Alert thresholds would be: delivery
success rate below 99% triggers a warning, below 95% pages the on-call.
Consumer lag exceeding 10,000 messages triggers investigation.

## Decision

The team agreed to proceed with Elena's proposal. Raj signed off on the
six-week compressed timeline. Dana committed to provisioning the NATS cluster
by end of week. Tom said he would start prototyping the WebSocket integration
immediately.
