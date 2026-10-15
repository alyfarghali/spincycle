// Table tests for decoder validation rules (contract A9).

package decoder

import (
	"strings"
	"testing"
	"time"

	"github.com/alyfarghali/spincycle/services/ingest/registry"
)

var (
	now = time.Date(2026, 10, 20, 14, 3, 11, 250_000_000, time.UTC)

	devs = registry.New(registry.Device{
		ID: "c6-washer-03", FleetID: "laundry", TypeID: "laundry-washer",
		Metrics: map[string]bool{"vibration_rms": true},
	})
)

const topic = "fleets/laundry/devices/c6-washer-03/telemetry"

func payload(field, value string) string {
	f := map[string]string{
		"v":         `1`,
		"device_id": `"c6-washer-03"`,
		"fw":        `"0.1.0"`,
		"boot":      `12`,
		"seq":       `1842`,
		"ts":        `"2026-10-20T14:03:11.250Z"`,
		"metrics":   `{"vibration_rms": 0.0423}`,
	}
	if value == "DELETE" {
		delete(f, field)
	} else if field != "" {
		f[field] = value
	}
	var parts []string
	for k, v := range f {
		parts = append(parts, `"`+k+`":`+v)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name    string
		topic   string
		payload string
		want    Reason
		dropped int
	}{
		{"valid", topic, payload("", ""), OK, 0},
		{"unknown top-level field ignored", topic, strings.Replace(payload("", ""), "{", `{"extra":true,`, 1), OK, 0},

		{"too large", topic, `{"pad":"` + strings.Repeat("x", 1100) + `"}`, PayloadTooLarge, 0},
		{"not json", topic, `hello`, InvalidJSON, 0},
		{"json array", topic, `[1,2]`, InvalidJSON, 0},
		{"json null", topic, `null`, InvalidJSON, 0},

		{"version 2", topic, payload("v", `2`), UnknownVersion, 0},
		{"version missing", topic, payload("v", "DELETE"), UnknownVersion, 0},
		{"version as string", topic, payload("v", `"1"`), UnknownVersion, 0},

		{"unregistered device", "fleets/laundry/devices/c6-washer-99/telemetry", payload("device_id", `"c6-washer-99"`), UnknownDevice, 0},
		{"malformed topic", "fleets/laundry/c6-washer-03", payload("", ""), UnknownDevice, 0},
		{"wrong fleet in topic", "fleets/demo/devices/c6-washer-03/telemetry", payload("", ""), FleetMismatch, 0},
		{"payload device differs from topic", topic, payload("device_id", `"c6-dryer-01"`), DeviceMismatch, 0},

		{"device_id is a number", topic, payload("device_id", `3`), BadType, 0},
		{"fw missing", topic, payload("fw", "DELETE"), BadType, 0},
		{"boot null", topic, payload("boot", `null`), BadType, 0},
		{"boot negative", topic, payload("boot", `-1`), BadType, 0},
		{"boot too big for uint32", topic, payload("boot", `4294967296`), BadType, 0},
		{"seq is a float", topic, payload("seq", `1.5`), BadType, 0},
		{"seq as string", topic, payload("seq", `"1842"`), BadType, 0},
		{"ts missing", topic, payload("ts", "DELETE"), BadType, 0},
		{"ts not a time", topic, payload("ts", `"yesterday"`), BadType, 0},
		{"ts with offset instead of Z", topic, payload("ts", `"2026-10-20T10:03:11.250-04:00"`), BadType, 0},
		{"metrics empty", topic, payload("metrics", `{}`), BadType, 0},
		{"metrics null", topic, payload("metrics", `null`), BadType, 0},
		{"metric value is a string", topic, payload("metrics", `{"vibration_rms":"high"}`), BadType, 0},
		{"metric value null", topic, payload("metrics", `{"vibration_rms":null}`), BadType, 0},

		{"ts 59 min ahead", topic, payload("ts", `"2026-10-20T15:02:11.250Z"`), OK, 0},
		{"ts 61 min ahead", topic, payload("ts", `"2026-10-20T15:04:11.250Z"`), TSOutOfRange, 0},
		{"ts 61 min behind", topic, payload("ts", `"2026-10-20T13:02:11.250Z"`), TSOutOfRange, 0},

		{"undeclared metric dropped, declared kept", topic, payload("metrics", `{"vibration_rms":0.04,"temperature_c":21}`), OK, 1},
		{"only undeclared metrics", topic, payload("metrics", `{"temperature_c":21}`), NoMetrics, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got, dropped := Decode(tt.topic, []byte(tt.payload), now, devs)
			if got != tt.want {
				t.Errorf("reason = %q, want %q", got, tt.want)
			}
			if dropped != tt.dropped {
				t.Errorf("dropped = %d, want %d", dropped, tt.dropped)
			}
		})
	}
}

func TestDecodeFields(t *testing.T) {
	r, reason, _ := Decode(topic, []byte(payload("", "")), now, devs)
	if reason != OK {
		t.Fatalf("reason = %q", reason)
	}
	want := time.Date(2026, 10, 20, 14, 3, 11, 250_000_000, time.UTC)
	if r.DeviceID != "c6-washer-03" || r.FleetID != "laundry" || r.Boot != 12 || r.Seq != 1842 ||
		!r.Time.Equal(want) || r.Metrics["vibration_rms"] != 0.0423 || !r.Received.Equal(now) {
		t.Errorf("unexpected reading: %+v", r)
	}
}

func TestDecodeNullTS(t *testing.T) {
	r, reason, _ := Decode(topic, []byte(payload("ts", `null`)), now, devs)
	if reason != OK {
		t.Fatalf("reason = %q", reason)
	}
	if !r.Time.Equal(now) {
		t.Errorf("Time = %v, want receive time %v", r.Time, now)
	}
}
