<<<<<<< HEAD
package subscriber

import "errors"

var ErrStaleConnection = errors.New("connection closed before ack; broker will redeliver")

func Ack(m Message) error {
	select {
	case <-m.client.Done():
		return ErrStaleConnection
	default:
	}
	return m.client.Ack(m.Packet)
}
=======
// Acks messages in order, only after the batch containing them is committed.

package subscriber
>>>>>>> main
