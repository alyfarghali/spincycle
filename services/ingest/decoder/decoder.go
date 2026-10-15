// Parses and validates telemetry per contract A9: known device, fleet matches topic, field types, ts window, declared metrics.
// Pure function: no I/O, and the clock is passed in, so every rule is table-testable.

package decoder

import (
	"encoding/json"
	"github.com/alyfarghali/spincycle/services/ingest/registry"
	"strings"
	"time"
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
)

const (
	maxPayload = 1024
	maxSkew    = time.Hour
)

type DeviceLookup interface {
	Lookup(id string) (registry.Device, bool)
}

// Decode checks one telemetry message in the order contract A9 lists.
// It also returns the number of undeclared metrics it dropped, so the caller
// can count them without rejecting the reading.
func Decode(topic string, payload []byte, now time.Time, devs DeviceLookup) (Reading, Reason, int) {
	if len(payload) > maxPayload {
		return Reading{}, PayloadTooLarge, 0
	}

	// Decode to raw fields first and each field separately afterwards. A single
	// json.Unmarshal into a struct can't tell "not JSON" from "wrong type",
	// and A9 counts those as different reasons.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil || fields == nil {
		return Reading{}, InvalidJSON, 0
	}

	var v int
	if !field(fields, "v", &v) || v != 1 {
		return Reading{}, UnknownVersion, 0
	}

	// The topic is authoritative (A3): identity comes from it, not the payload.
	// Topic shape: fleets/{fleet}/devices/{device}/telemetry
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
		ts      *string // stays nil when the payload has "ts": null
		metrics map[string]*float64
	)
	if !field(fields, "fw", &fw) ||
		!field(fields, "boot", &boot) ||
		!field(fields, "seq", &seq) ||
		!field(fields, "metrics", &metrics) ||
		len(metrics) == 0 {
		return Reading{}, BadType, 0
	}
	// ts is the one field allowed to be null, so it can't go through field().
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
		Time:     now, // until the device clock syncs, use our receive time (A5)
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

	// Keep only metrics the device's type declares; drop the rest and count them.
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

// field decodes one required field into dst. False if it's missing, null, or
// the wrong JSON type (e.g. a string where a number belongs, or -1 for a uint).
// Null needs its own check: Go decodes null into a plain int or string by
// leaving the zero value, so "boot": null would silently become boot 0.
func field(fields map[string]json.RawMessage, name string, dst any) bool {
	raw, ok := fields[name]
	if !ok || string(raw) == "null" {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}
