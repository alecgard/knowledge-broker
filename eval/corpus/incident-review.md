# Incident Review: Widget Service Outage — 2025-08-14

## Attendees

Present: Sarah Chen (SRE lead), Marcus Rivera (backend engineer), Priya Patel
(product manager), and James O'Brien (database administrator).

Sarah opened the meeting by walking through the timeline of the outage. She
explained that the first alert fired at 03:42 UTC when the error rate breached
the 5% critical threshold. Marcus was the on-call engineer that night. He
acknowledged the page within three minutes and started investigating the issue.

## Initial Investigation

He pulled up the Prometheus dashboard and noticed that the database connection
pool was completely exhausted. All 25 connections were in use and new requests
were queueing up behind them. He tried increasing the pool size via the
WIDGET_DB_MAX_CONNS environment variable, but that didn't help because the
underlying problem was something else entirely. The pool was full not because
of high traffic, but because every connection was stuck waiting. He checked
pg_stat_activity and saw that every active query was blocked in a lock wait
state. None of them were actually executing — they were all waiting for the
same table lock on the widgets table.

## Root Cause Analysis

It turned out to be a long-running migration that had been kicked off earlier
that evening by an automated job. The tool that ran it — the same one the team
had been using since the project started — executed an ALTER TABLE that required
a full table rewrite. The table had over 2 million rows, so the operation took
much longer than expected. While it held the exclusive lock, every write query
to that table blocked. The blocked writes held their database connections open,
which eventually starved the read path too. Within minutes, all 25 connections
in the pool were consumed by blocked queries, and the service started returning
503 errors to every request.

## Monitoring Gaps

She pointed out that their monitoring hadn't caught this early enough. The
database connection metric only had a warning threshold set at 20 connections.
By the time it hit 25, the service was already completely down. She also noted
that they had no alerting at all for long-running queries or lock waits. A
query could sit blocked for an hour and nobody would be paged about it. She
said this was the most critical gap to address — if they'd had a lock-wait
alert, the on-call engineer would have been notified 30 minutes before the
service actually went down.

## Resolution and Customer Impact

He resolved the immediate issue by killing the migration process using
pg_terminate_backend. Once he did that, the blocked connections drained within
seconds and the service recovered. The total downtime was 47 minutes.

She asked whether customers had been affected. He confirmed that approximately
12% of API requests during that window received 503 errors. She noted that this
was within the SLA for most customers but had triggered an automatic credit for
two enterprise accounts that have stricter uptime guarantees.

## Write Path Resilience

He proposed adding a circuit breaker to the write path. If the database starts
rejecting connections or queries start timing out, the service should return 503
immediately rather than queueing up requests that will inevitably fail. He
pointed out that they already have this pattern working for the cache layer —
when Redis is down, the service bypasses it rather than blocking. The same
principle should apply to the database write path. He estimated this would take
about a week to implement properly, including testing under simulated database
failures.

## Alerting Improvements

She wants to add a new Prometheus alert specifically for long-running queries.
She said anything over 30 seconds should trigger a warning, and anything over
5 minutes should page the on-call. She also plans to lower the connection pool
warning threshold from 20 to 15. Additionally, she's going to add a dedicated
alert for lock waits — any query blocked on a lock for more than 10 seconds
should generate a warning. These changes should ensure the team gets notified
well before connection exhaustion occurs.

## Migration Safety

He explained that the problematic migration could have been written differently.
Instead of a plain ALTER TABLE that locks the entire table, they could have used
pg_repack for an online table rewrite, or a shadow table strategy where a new
table is created with the desired schema, data is copied incrementally, and
then the tables are swapped atomically. He offered to document these safe
migration patterns and schedule a knowledge-sharing session for the team.

He is also going to implement an advisory lock check that prevents migrations
from running during peak traffic hours (06:00–22:00 UTC). The automated
migration job will check for this lock before proceeding, and if it's held,
the migration will be deferred to the next maintenance window. He estimated
this would take about two days to implement and test.

## Status Page Communication

She will update the status page communication template. During the incident,
it took 20 minutes to post the first status update because nobody could find
the template in the wiki. She wants to automate the initial status page post —
when a critical alert fires, a draft incident post should be created
automatically with the alert details pre-filled. The on-call engineer would
just need to review and publish it. She estimated this could cut the time to
first public communication from 20 minutes to under 5.

## Shutdown and Connection Handling

The team also discussed whether the 30-second graceful shutdown timeout is
sufficient. During the incident, some connections were stuck waiting for the
locked table for much longer than that. He suggested that the shutdown handler
should specifically kill any database connection that has been waiting for a
lock for more than 10 seconds, rather than relying on the global timeout. This
would ensure that lock-blocked connections don't prevent a clean shutdown. She
agreed and noted that this should be configurable via an environment variable
rather than hardcoded.

## Connection Pool Sizing

He observed that the connection pool size — currently at 25 — might be too
conservative for their current traffic levels. He'd seen it approach 20
connections during normal peak hours even before the incident. He suggested
bumping it to 50, which matches what the Kubernetes deployment already
configures via the WIDGET_DB_MAX_CONNS variable. She agreed that the default
in the codebase should match what they actually run in production, and filed
a ticket to update the default in config.go.
