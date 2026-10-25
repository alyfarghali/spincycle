# load-tests

Every load-test run: hardware, settings, results, and what it does and doesn't measure.

## Runs

| # | Date | What | Hardware | Ingest version | Result |
|---|---|---|---|---|---|
| 0 | 2026-10-14 | Correctness smoke test (not a load test) | MacBook M1, 8 GB; Docker Desktop; Mosquitto 2.0.20, TimescaleDB 2.30.2-pg16 | Bare: 1 worker, 1 transaction per message, ack after commit, ReceiveMaximum 1000 | 3,000 / 3,000 stored, 0 missing, 0 doubled across a `kill -9` |

## Run 0: crash safety of the bare ingest

**Measures:** whether a reading the broker accepted can be lost or stored twice when ingest crashes. **Doesn't measure:** throughput or latency (one device, one publisher, local Docker).

Setup: one publisher (`mosquitto_pub -l`, QoS 1, device `dev-sim-01`) sends 3,000 readings. A separate non-shared subscriber counts what the broker accepted. Ingest is killed with `kill -9` about 1 s in, then restarted after the publisher finishes.

| Check | Result |
|---|---|
| Broker accepted (independent subscriber) | 3,000 |
| Stored before the kill | 139 |
| Stored after restart | 2,861 |
| Redelivered duplicates dropped (unacked at the kill) | 10 |
| `readings_seen` rows, distinct `seq`, range | 3,000, 3,000, 1..3000 |
| Ingest stopped, 20 readings published, ingest started | 20 / 20 stored |

Pitfall found: `mosquitto_pub -l` exits as soon as stdin closes and discards messages still queued in the client (it sent 94 of 3,000 in one try). Keep stdin open until it drains, e.g. `(generate; sleep 8) | mosquitto_pub -l ...`, and always count broker-accepted messages independently instead of trusting the publisher.
