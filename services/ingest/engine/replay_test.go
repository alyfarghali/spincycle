package engine

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestReplay(t *testing.T) {
	var files []string
	for _, dir := range []string{"synthetic", "cycles"} {
		f, _ := filepath.Glob(filepath.Join("..", "..", "..", "testdata", dir, "*.csv"))
		files = append(files, f...)
	}
	if len(files) == 0 {
		t.Skip("no cycle CSVs")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) { replay(t, file) })
	}
}

func replay(t *testing.T, file string) {
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 || !slices.Equal(rows[0], []string{"ts", "vibration_rms", "event"}) {
		t.Fatalf("want header ts,vibration_rms,event, got %v", rows[0])
	}

	var m Machine
	var start, end time.Time
	var transitions []Transition
	for i, row := range rows[1:] {
		ts, err := time.Parse(time.RFC3339Nano, row[0])
		if err != nil {
			t.Fatalf("row %d: %v", i+2, err)
		}
		rms, err := strconv.ParseFloat(row[1], 64)
		if err != nil {
			t.Fatalf("row %d: %v", i+2, err)
		}
		switch row[2] {
		case "start":
			start = ts
		case "end":
			end = ts
		}
		var out []Transition
		m, out = OnReading(m, rms, ts, testParams)
		transitions = append(transitions, out...)
	}
	if start.IsZero() || end.IsZero() {
		t.Fatal("file needs one start and one end event")
	}

	firstTo := func(s State, after time.Time) (time.Time, bool) {
		for _, tr := range transitions {
			if tr.To == s && !tr.At.Before(after) {
				return tr.At, true
			}
		}
		return time.Time{}, false
	}
	if at, ok := firstTo(Running, start); !ok || at.Sub(start) > testParams.StartConfirm+time.Minute {
		t.Errorf("running %v after start (limit %v), found=%v", at.Sub(start), testParams.StartConfirm+time.Minute, ok)
	}
	if at, ok := firstTo(Finished, end); !ok || at.Sub(end) > testParams.StopConfirm+time.Minute {
		t.Errorf("finished %v after end (limit %v), found=%v", at.Sub(end), testParams.StopConfirm+time.Minute, ok)
	}
	for _, tr := range transitions {
		if tr.At.After(start) && tr.At.Before(end) && (tr.To == Finished || tr.To == Free) {
			t.Errorf("%s>%s at %v, mid-cycle", tr.From, tr.To, tr.At.Sub(start))
		}
	}
}
