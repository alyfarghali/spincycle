package main

import (
	"encoding/json"
	"math/rand/v2"
	"time"
)

const (
	cycle   = 60 * time.Minute
	wash    = 35 * time.Minute
	idleRMS = 0.0015 // bench noise floor, hardware/sensor.md
	washRMS = 0.027  // speaker test level, hardware/sensor.md
)

type device struct {
	id    string
	seq   uint64
	phase time.Duration
}

type payload struct {
	V        int                `json:"v"`
	DeviceID string             `json:"device_id"`
	FW       string             `json:"fw"`
	Boot     uint32             `json:"boot"`
	Seq      uint64             `json:"seq"`
	TS       string             `json:"ts"`
	Metrics  map[string]float64 `json:"metrics"`
}

func (d *device) reading(now time.Time, boot uint32) []byte {
	rms := idleRMS
	if (time.Duration(now.UnixNano())+d.phase)%cycle < wash {
		rms = washRMS
	}
	rms *= 1 + 0.1*rand.NormFloat64()

	body, _ := json.Marshal(payload{
		V:        1,
		DeviceID: d.id,
		FW:       "loadgen-0.1.0",
		Boot:     boot,
		Seq:      d.seq,
		TS:       now.UTC().Format("2006-01-02T15:04:05.000Z"),
		Metrics:  map[string]float64{"vibration_rms": rms},
	})
	d.seq++
	return body
}
