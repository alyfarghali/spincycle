// Acks a message only on the connection it arrived on. The only place ingest sends a PUBACK.

package subscriber

import "errors"

// ErrStaleConnection means the message arrived on a connection that has since
// closed. Don't treat it as a failure: the broker resends unacked messages
// when the session resumes, and dedup drops the copy we already stored.
var ErrStaleConnection = errors.New("connection closed before ack; broker will redeliver")

// Ack sends the PUBACK for m. Call it only after m is committed, a duplicate,
// or rejected for good; that ordering is what makes QoS 1 survive a crash.
//
// Acking on a closed connection is unsafe (paho.golang issue #160): packet IDs
// are reused per connection, so a late ack could acknowledge a different
// message on the new connection. So messages from an old connection are never
// acked; the broker redelivers them instead.
//
// paho sends acks in the order messages arrived even if Ack is called out of
// order, as MQTT requires.
func Ack(m Message) error {
	select {
	case <-m.client.Done():
		return ErrStaleConnection
	default:
	}
	return m.client.Ack(m.Packet)
}
