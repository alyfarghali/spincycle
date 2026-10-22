<<<<<<< HEAD
// Buffers readings and flushes them in one transaction: record IDs in readings_seen, then COPY only newly seen rows into telemetry.
=======
// Buffers readings and flushes via COPY into telemetry at N rows or the flush interval.
>>>>>>> main

package batchwriter
