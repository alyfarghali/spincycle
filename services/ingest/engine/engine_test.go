package engine

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// Placeholder values until real recordings exist; production values come from device_types.config.
var testParams = Params{
	OnThreshold:  0.01,
	OffThreshold: 0.005,
	StartConfirm: 30 * time.Second,
	StopConfirm:  5 * time.Minute,
	ClearAfter:   20 * time.Minute,
	OfflineAfter: time.Minute,
	ResumeWindow: 10 * time.Minute,
}

const (
	idle  = 0.0015
	shake = 0.03
	mid   = 0.007
)

var t0 = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

type kind int

const (
	reading kind = iota
	tick
	lastWill
)

type step struct {
	kind kind
	at   time.Duration
	rms  float64
}

func every(from, to time.Duration, rms float64) []step {
	var s []step
	for at := from; at < to; at += 5 * time.Second {
		s = append(s, step{reading, at, rms})
	}
	return s
}

func at(k kind, d time.Duration) []step { return []step{{kind: k, at: d}} }

func play(steps ...[]step) (Machine, []string) {
	var m Machine
	var all []string
	for _, s := range slices.Concat(steps...) {
		var out []Transition
		switch s.kind {
		case reading:
			m, out = OnReading(m, s.rms, t0.Add(s.at), testParams)
		case tick:
			m, out = OnTick(m, t0.Add(s.at), testParams)
		case lastWill:
			m, out = OnOffline(m, t0.Add(s.at))
		}
		for _, tr := range out {
			all = append(all, fmt.Sprintf("%s>%s", tr.From, tr.To))
		}
	}
	return m, all
}

const (
	sec = time.Second
	min = time.Minute
)

func TestTransitions(t *testing.T) {
	cycle := slices.Concat(every(0, 60*sec, idle), every(60*sec, 10*min, shake))
	finished := slices.Concat(cycle, every(10*min, 15*min+5*sec, idle))

	cases := []struct {
		name  string
		steps [][]step
		state State
		want  []string
	}{
		{"new quiet device resolves to free",
			[][]step{every(0, 35*sec, idle)}, Free, []string{"unknown>free"}},
		{"a 15 s bump doesn't start a cycle",
			[][]step{every(0, 60*sec, idle), every(60*sec, 75*sec, shake), every(75*sec, 2*min, idle)}, Free, []string{"unknown>free"}},
		{"30 s of shaking starts a cycle",
			[][]step{cycle}, Running, []string{"unknown>free", "free>running"}},
		{"a 4 min soak pause doesn't finish it",
			[][]step{cycle, every(10*min, 14*min, idle), every(14*min, 16*min, shake)}, Running, []string{"unknown>free", "free>running"}},
		{"5 min quiet finishes it",
			[][]step{finished}, Finished, []string{"unknown>free", "free>running", "running>finished"}},
		{"finished clears to free after clear_after",
			[][]step{finished, every(15*min+5*sec, 35*min+5*sec, idle)}, Free, []string{"unknown>free", "free>running", "running>finished", "finished>free"}},
		{"values between thresholds don't reset a shaking run",
			[][]step{every(0, 60*sec, idle), every(60*sec, 75*sec, shake), every(75*sec, 85*sec, mid), every(85*sec, 95*sec, shake)},
			Running, []string{"unknown>free", "free>running"}},
		{"values between thresholds don't count as quiet",
			[][]step{cycle, every(10*min, 20*min, mid)}, Running, []string{"unknown>free", "free>running"}},
		{"silence marks it offline",
			[][]step{cycle, at(tick, 10*min+55*sec)}, Offline, []string{"unknown>free", "free>running", "running>offline"}},
		{"short Wi-Fi drop mid-cycle resumes running, never free",
			[][]step{cycle, at(lastWill, 10*min), every(12*min, 13*min, shake)},
			Running, []string{"unknown>free", "free>running", "running>offline", "offline>running"}},
		{"long drop mid-cycle then quiet: finished, not free",
			[][]step{cycle, at(lastWill, 10*min), every(30*min, 31*min, idle)},
			Finished, []string{"unknown>free", "free>running", "running>offline", "offline>unknown", "unknown>finished"}},
		{"long drop of an idle machine resolves to free",
			[][]step{every(0, 60*sec, idle), at(lastWill, 60*sec), every(20*min, 21*min, idle)},
			Free, []string{"unknown>free", "free>offline", "offline>unknown", "unknown>free"}},
		{"short drop while finished keeps the clear_after timer",
			[][]step{finished, at(lastWill, 16*min), every(17*min, 35*min+5*sec, idle)},
			Free, []string{"unknown>free", "free>running", "running>finished", "finished>offline", "offline>finished", "finished>free"}},
		{"a late reading doesn't rewind state",
			[][]step{cycle, {{reading, 2 * min, idle}}, every(10*min, 11*min, shake)}, Running, []string{"unknown>free", "free>running"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, got := play(c.steps...)
			if m.State != c.state || !slices.Equal(got, c.want) {
				t.Errorf("state %s, transitions %v\nwant  %s, transitions %v", m.State, got, c.state, c.want)
			}
		})
	}
}

func TestClearAfterTimestamp(t *testing.T) {
	m, _ := play(every(0, 60*sec, idle), every(60*sec, 10*min, shake), every(10*min, 15*min+5*sec, idle))
	_, out := OnReading(m, idle, m.Since.Add(25*time.Minute), testParams)
	if len(out) != 1 || !out[0].At.Equal(m.Since.Add(testParams.ClearAfter)) {
		t.Fatalf("finished>free should be stamped when clear_after ran out, got %v", out)
	}
}

func TestParseParams(t *testing.T) {
	typeCfg := []byte(`{"on_threshold":0.01,"off_threshold":0.005,"start_confirm":30,"stop_confirm":300,
		"clear_after":1200,"offline_after":60,"resume_window":600}`)

	p, err := ParseParams(typeCfg, []byte(`{"stop_confirm":420,"interval_s":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.StopConfirm != 7*time.Minute || p.StartConfirm != 30*time.Second {
		t.Errorf("device override not applied: %+v", p)
	}

	if _, err := ParseParams([]byte(`{"on_threshold":0.01}`), nil); err == nil {
		t.Error("missing keys should fail")
	}
	if _, err := ParseParams(typeCfg, []byte(`{"off_threshold":0.02}`)); err == nil {
		t.Error("off_threshold >= on_threshold should fail")
	}
}
