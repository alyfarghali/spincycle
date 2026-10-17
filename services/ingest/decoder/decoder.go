<<<<<<< HEAD
package decoder

import (
	"encoding/json"
	"github.com/alyfarghali/spincycle/services/ingest/registry"
	"strings"
	"time"
)

const (
	OK              Reason = ""
	PayloadTooLarge Reason = "payload_too_large"
	InvalidJSON     Reason = "invalid_json"
	UnknownVersion  Reason = "unknown_version"
	UnknownDevice   Reason = "unknown_device"
	FleetMismatch   Reason = "fleet_mismatch"
	DeviceMismatch  Reason = "device_mismatch"
	BadType         Reason = "bad_type"
	TSOutOfRange    Reason = "ts_out_of_range"
	NoMetrics       Reason = "no_metrics"
	maxPayload = 1024
	maxSkew    = time.Hour
)

type Reading struct {
	DeviceID string
	FleetID  string
	Boot     uint32
	Seq      uint64
	Time     time.Time
	Metrics  map[string]float64
	Received time.Time
}

type Reason string

type DeviceLookup interface {
	Lookup(id string) (registry.Device, bool)
}

func Decode(topic string, payload []byte, now time.Time, devs DeviceLookup) (Reading, Reason, int) {
	if len(payload) > maxPayload {
		return Reading{}, PayloadTooLarge, 0
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil || fields == nil {
		return Reading{}, InvalidJSON, 0
	}

	var v int
	if !field(fields, "v", &v) || v != 1 {
		return Reading{}, UnknownVersion, 0
	}

	parts := strings.Split(topic, "/")
	if len(parts) != 5 || parts[0] != "fleets" || parts[2] != "devices" || parts[4] != "telemetry" {
		return Reading{}, UnknownDevice, 0
	}
	topicFleet, topicDevice := parts[1], parts[3]

	dev, ok := devs.Lookup(topicDevice)
	if !ok {
		return Reading{}, UnknownDevice, 0
	}
	if dev.FleetID != topicFleet {
		return Reading{}, FleetMismatch, 0
	}

	var deviceID string
	if !field(fields, "device_id", &deviceID) {
		return Reading{}, BadType, 0
	}
	if deviceID != topicDevice {
		return Reading{}, DeviceMismatch, 0
	}

	var (
		fw      string
		boot    uint32
		seq     uint64
		ts      *string 
		metrics map[string]*float64
	)
	if !field(fields, "fw", &fw) ||
		!field(fields, "boot", &boot) ||
		!field(fields, "seq", &seq) ||
		!field(fields, "metrics", &metrics) ||
		len(metrics) == 0 {
		return Reading{}, BadType, 0
	}
	if raw, ok := fields["ts"]; !ok {
		return Reading{}, BadType, 0
	} else if string(raw) != "null" && json.Unmarshal(raw, &ts) != nil {
		return Reading{}, BadType, 0
	}

	r := Reading{
		DeviceID: topicDevice,
		FleetID:  topicFleet,
		Boot:     boot,
		Seq:      seq,
		Time:     now, 
		Received: now,
	}
	if ts != nil {
		t, err := time.Parse(time.RFC3339Nano, *ts)
		if err != nil || !strings.HasSuffix(*ts, "Z") {
			return Reading{}, BadType, 0
		}
		if d := t.Sub(now); d > maxSkew || d < -maxSkew {
			return Reading{}, TSOutOfRange, 0
		}
		r.Time = t
	}

	r.Metrics = make(map[string]float64, len(metrics))
	dropped := 0
	for name, value := range metrics {
		if value == nil {
			return Reading{}, BadType, 0
		}
		if dev.Metrics[name] {
			r.Metrics[name] = *value
		} else {
			dropped++
		}
	}
	if len(r.Metrics) == 0 {
		return Reading{}, NoMetrics, dropped
	}
	return r, OK, dropped
}

func field(fields map[string]json.RawMessage, name string, dst any) bool {
	raw, ok := fields[name]
	if !ok || string(raw) == "null" {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}
=======
// Parses and validates telemetry: known device, fleet matches topic, numeric metrics, ts window; counts rejects by reason.

package decoder
>>>>>>> main
