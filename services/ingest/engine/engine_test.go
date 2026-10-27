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

func bursts(from, to, on, off time.Duration) []step {
	var s []step
	for t := from; t < to; t += on + off {
		s = append(s, every(t, t+on, shake)...)
		s = append(s, every(t+on, t+on+off, idle)...)
	}
	return s
}

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
		{"neighbour shaking in 20 s bursts never starts a cycle (CLAUDE.md §8)",
			[][]step{every(0, 60*sec, idle), bursts(60*sec, 10*min, 20*sec, 10*sec)}, Free, []string{"unknown>free"}},
		{"values between thresholds don't start a cycle",
			[][]step{every(0, 60*sec, idle), every(60*sec, 10*min, mid)}, Free, []string{"unknown>free"}},
		{"reboot mid-cycle without a Last Will: gap, then shaking resumes running (§8)",
			[][]step{cycle, every(10*min+40*sec, 12*min, shake)}, Running, []string{"unknown>free", "free>running"}},
		{"reboot mid-cycle seen by the sweeper: offline, then resumes running",
			[][]step{cycle, at(tick, 11*min), every(11*min+30*sec, 12*min, shake)},
			Running, []string{"unknown>free", "free>running", "running>offline", "offline>running"}},
		{"long drop then shaking resolves unknown to running",
			[][]step{every(0, 60*sec, idle), at(lastWill, 60*sec), every(20*min, 21*min, shake)},
			Running, []string{"unknown>free", "free>offline", "offline>unknown", "unknown>running"}},
		{"a new cycle can start before clear_after",
			[][]step{finished, every(15*min+5*sec, 16*min, shake)},
			Running, []string{"unknown>free", "free>running", "running>finished", "finished>running"}},
		{"a quiet run interrupted by shaking restarts stop_confirm",
			[][]step{cycle, every(10*min, 14*min, idle), every(14*min, 14*min+10*sec, shake), every(14*min+10*sec, 18*min, idle)},
			Running, []string{"unknown>free", "free>running"}},
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

func TestTransitionTimes(t *testing.T) {
	var m Machine
	var got []Transition
	steps := slices.Concat(every(0, 60*sec, idle), every(60*sec, 10*min, shake), every(10*min, 15*min+5*sec, idle))
	for _, s := range steps {
		var out []Transition
		m, out = OnReading(m, s.rms, t0.Add(s.at), testParams)
		got = append(got, out...)
	}
	m, out := OnTick(m, t0.Add(16*min+5*sec), testParams)
	got = append(got, out...)

	want := []Transition{
		{Unknown, Free, t0.Add(30 * sec)},           // quiet for start_confirm
		{Free, Running, t0.Add(90 * sec)},           // shaking from 60 s, confirmed at 90 s
		{Running, Finished, t0.Add(15 * min)},       // quiet from 10 min, confirmed at 15 min
		{Finished, Offline, t0.Add(16*min + 5*sec)}, // the tick that noticed the silence
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	if m.Prev != Finished || !m.PrevSince.Equal(t0.Add(15*min)) {
		t.Errorf("offline should save the previous state and its start, got %s %v", m.Prev, m.PrevSince)
	}
}

func TestOfflineEdgeCases(t *testing.T) {
	var never Machine
	if m, out := OnOffline(never, t0); len(out) != 0 || m.State != "" {
		t.Errorf("Last Will for a never-seen device should do nothing, got %v", out)
	}
	if _, out := OnTick(never, t0.Add(time.Hour), testParams); len(out) != 0 {
		t.Errorf("tick for a never-seen device should do nothing, got %v", out)
	}

	m, _ := play(every(0, 60*sec, shake))
	m, first := OnOffline(m, t0.Add(60*sec))
	m, second := OnOffline(m, t0.Add(61*sec))
	_, third := OnTick(m, t0.Add(5*min), testParams)
	if len(first) != 1 || len(second) != 0 || len(third) != 0 {
		t.Errorf("one offline transition expected, got %v %v %v", first, second, third)
	}
}

func TestResumeWindowBoundary(t *testing.T) {
	base := slices.Concat(every(0, 60*sec, idle), every(60*sec, 2*min, shake), at(lastWill, 2*min))
	last := 2*min - 5*sec

	m, _ := play(base, []step{{reading, last + testParams.ResumeWindow, shake}})
	if m.State != Running {
		t.Errorf("back exactly at resume_window should resume, got %s", m.State)
	}
	m, _ = play(base, []step{{reading, last + testParams.ResumeWindow + 5*sec, shake}})
	if m.State != Unknown {
		t.Errorf("back after resume_window should be unknown, got %s", m.State)
	}
}

func TestDuplicateReadingIgnored(t *testing.T) {
	m, _ := play(every(0, 60*sec, idle))
	again, out := OnReading(m, shake, m.LastReading, testParams)
	if len(out) != 0 || again != m {
		t.Errorf("a reading with the same timestamp must change nothing")
	}
}

func TestParseParams(t *testing.T) {
	typeCfg := []byte(`{"on_threshold":0.01,"off_threshold":0.005,"start_confirm":30,"stop_confirm":300,
		"clear_after":1200,"offline_after":60,"resume_window":600}`)

	p, err := ParseParams(typeCfg, []byte(`{"stop_confirm":420,"interval_s":5}`))
	if err != nil {
		t.Fatal(err)
	}
	want := testParams
	want.StopConfirm = 7 * time.Minute
	if p != want {
		t.Errorf("got  %+v\nwant %+v", p, want)
	}
	if p, err := ParseParams(typeCfg, nil); err != nil || p != testParams {
		t.Errorf("type config alone: %+v, %v", p, err)
	}

	bad := []struct{ name, device string }{
		{"off equal to on", `{"off_threshold":0.01}`},
		{"off above on", `{"off_threshold":0.02}`},
		{"negative off", `{"off_threshold":-0.001}`},
		{"zero start_confirm", `{"start_confirm":0}`},
		{"zero stop_confirm", `{"stop_confirm":0}`},
		{"zero clear_after", `{"clear_after":0}`},
		{"zero offline_after", `{"offline_after":0}`},
		{"negative resume_window", `{"resume_window":-1}`},
		{"string value", `{"stop_confirm":"300"}`},
		{"not JSON", `{`},
	}
	for _, c := range bad {
		if _, err := ParseParams(typeCfg, []byte(c.device)); err == nil {
			t.Errorf("%s: should fail", c.name)
		}
	}
	if _, err := ParseParams([]byte(`{"on_threshold":0.01}`), nil); err == nil {
		t.Error("missing keys should fail: there are no defaults in code")
	}
	if _, err := ParseParams(nil, nil); err == nil {
		t.Error("empty config should fail")
	}
	if p, err := ParseParams(typeCfg, []byte(`{"resume_window":0}`)); err != nil || p.ResumeWindow != 0 {
		t.Errorf("resume_window 0 (always unknown after offline) should be allowed: %v", err)
	}
}
