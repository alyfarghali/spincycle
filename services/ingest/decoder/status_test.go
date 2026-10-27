package decoder

import (
	"testing"

	"github.com/alyfarghali/spincycle/services/ingest/registry"
)

func TestDecodeStatus(t *testing.T) {
	devs := registry.New(registry.Device{ID: "c6-washer-03", FleetID: "laundry"})
	const topic = "fleets/laundry/devices/c6-washer-03/status"

	cases := []struct {
		name, topic, payload string
		offline              bool
		reason               Reason
	}{
		{"Last Will or clean disconnect", topic, "offline", true, OK},
		{"online after connect", topic, "online", false, OK},
		{"trailing newline is not the contract's plain text", topic, "offline\n", false, BadStatus},
		{"wrong case", topic, "OFFLINE", false, BadStatus},
		{"JSON instead of plain text", topic, `{"status":"offline"}`, false, BadStatus},
		{"empty payload", topic, "", false, BadStatus},
		{"unregistered device", "fleets/laundry/devices/ghost/status", "offline", false, UnknownDevice},
		{"wrong fleet for the device", "fleets/demo/devices/c6-washer-03/status", "offline", false, FleetMismatch},
		{"telemetry topic", "fleets/laundry/devices/c6-washer-03/telemetry", "offline", false, UnknownDevice},
		{"malformed topic", "fleets/laundry/c6-washer-03/status", "offline", false, UnknownDevice},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, offline, reason := DecodeStatus(c.topic, []byte(c.payload), devs)
			if reason != c.reason || offline != c.offline || (reason == OK && id != "c6-washer-03") {
				t.Errorf("got (%q, %v, %q), want offline %v reason %q", id, offline, reason, c.offline, c.reason)
			}
		})
	}
}
