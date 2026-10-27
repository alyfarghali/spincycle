// Buffers readings and flushes them in one transaction: record IDs in readings_seen, then COPY only newly seen rows into telemetry.

package batchwriter
