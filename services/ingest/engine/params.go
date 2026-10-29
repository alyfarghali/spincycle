package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Params struct {
	OnThreshold  float64
	OffThreshold float64
	StartConfirm time.Duration
	StopConfirm  time.Duration
	ClearAfter   time.Duration
	OfflineAfter time.Duration
	ResumeWindow time.Duration
}

func ParseParams(typeConfig, deviceConfig []byte) (Params, error) {
	vals := map[string]float64{}
	for _, raw := range [][]byte{typeConfig, deviceConfig} {
		if len(raw) == 0 {
			continue
		}
		var layer map[string]json.RawMessage
		if err := json.Unmarshal(raw, &layer); err != nil {
			return Params{}, fmt.Errorf("engine config: %w", err)
		}
		// Only engine keys: devices.config also holds device settings such as interval_s.
		for _, k := range keys {
			if v, ok := layer[k]; ok {
				var f float64
				if err := json.Unmarshal(v, &f); err != nil {
					return Params{}, fmt.Errorf("engine config %s: %w", k, err)
				}
				vals[k] = f
			}
		}
	}
	for _, k := range keys {
		if _, ok := vals[k]; !ok {
			return Params{}, fmt.Errorf("engine config: missing %s", k)
		}
	}
	secs := func(k string) time.Duration { return time.Duration(vals[k] * float64(time.Second)) }
	p := Params{
		OnThreshold:  vals["on_threshold"],
		OffThreshold: vals["off_threshold"],
		StartConfirm: secs("start_confirm"),
		StopConfirm:  secs("stop_confirm"),
		ClearAfter:   secs("clear_after"),
		OfflineAfter: secs("offline_after"),
		ResumeWindow: secs("resume_window"),
	}
	return p, p.Validate()
}

var keys = []string{"on_threshold", "off_threshold", "start_confirm", "stop_confirm", "clear_after", "offline_after", "resume_window"}

func (p Params) Validate() error {
	switch {
	case p.OffThreshold < 0 || p.OffThreshold >= p.OnThreshold:
		return errors.New("engine config: need 0 <= off_threshold < on_threshold")
	case p.StartConfirm <= 0 || p.StopConfirm <= 0 || p.ClearAfter <= 0 || p.OfflineAfter <= 0:
		return errors.New("engine config: start_confirm, stop_confirm, clear_after, offline_after must be > 0")
	case p.ResumeWindow < 0:
		return errors.New("engine config: resume_window must be >= 0")
	}
	return nil
}
