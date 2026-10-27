package engine

import "time"

func OnReading(m Machine, rms float64, t time.Time, p Params) (Machine, []Transition) {
	if !t.After(m.LastReading) {
		return m, nil
	}
	var out []Transition
	if m.State == "" {
		m.State, m.Since = Unknown, t
	}
	if m.State == Offline {
		m, out = resume(m, t, p, out)
	}
	m, out = expire(m, t, p, out)
	m.LastReading = t

	switch {
	case rms >= p.OnThreshold:
		m.QuietSince = time.Time{}
		if m.ShakingSince.IsZero() {
			m.ShakingSince = t
		}
	case rms <= p.OffThreshold:
		m.ShakingSince = time.Time{}
		if m.QuietSince.IsZero() {
			m.QuietSince = t
		}
	}
	shakingFor := func(d time.Duration) bool { return !m.ShakingSince.IsZero() && t.Sub(m.ShakingSince) >= d }
	quietFor := func(d time.Duration) bool { return !m.QuietSince.IsZero() && t.Sub(m.QuietSince) >= d }

	switch m.State {
	case Free, Finished:
		if shakingFor(p.StartConfirm) {
			m, out = move(m, Running, t, out)
		}
	case Running:
		if quietFor(p.StopConfirm) {
			m, out = move(m, Finished, t, out)
		}
	case Unknown:
		if shakingFor(p.StartConfirm) {
			m, out = move(m, Running, t, out)
		} else if quietFor(p.StartConfirm) {
			// Never a wrong "free": if it was mid-cycle before the gap, the load may still be inside.
			to := Free
			if m.Prev == Running || m.Prev == Finished {
				to = Finished
			}
			m, out = move(m, to, t, out)
		}
	}
	return m, out
}

func OnTick(m Machine, now time.Time, p Params) (Machine, []Transition) {
	var out []Transition
	m, out = expire(m, now, p, out)
	if m.State != "" && m.State != Offline && now.Sub(m.LastReading) >= p.OfflineAfter {
		m, out = goOffline(m, now, out)
	}
	return m, out
}

func OnOffline(m Machine, at time.Time) (Machine, []Transition) {
	return goOffline(m, at, nil)
}

func goOffline(m Machine, at time.Time, out []Transition) (Machine, []Transition) {
	if m.State == "" || m.State == Offline {
		return m, out
	}
	m.Prev, m.PrevSince = m.State, m.Since
	return move(m, Offline, at, out)
}

func resume(m Machine, t time.Time, p Params, out []Transition) (Machine, []Transition) {
	m.ShakingSince, m.QuietSince = time.Time{}, time.Time{}
	if t.Sub(m.LastReading) <= p.ResumeWindow {
		out = append(out, Transition{From: Offline, To: m.Prev, At: t})
		m.State, m.Since = m.Prev, m.PrevSince // restore the state and its timer, e.g. clear_after keeps counting
		return m, out
	}
	return move(m, Unknown, t, out)
}

func expire(m Machine, now time.Time, p Params, out []Transition) (Machine, []Transition) {
	if m.State == Finished && now.Sub(m.Since) >= p.ClearAfter {
		return move(m, Free, m.Since.Add(p.ClearAfter), out)
	}
	return m, out
}

func move(m Machine, to State, at time.Time, out []Transition) (Machine, []Transition) {
	out = append(out, Transition{From: m.State, To: to, At: at})
	m.State, m.Since = to, at
	return m, out
}
