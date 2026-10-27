package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alyfarghali/spincycle/services/ingest/decoder"
	"github.com/alyfarghali/spincycle/services/ingest/registry"
)

type lookup map[string]registry.Device

func (l lookup) Lookup(id string) (registry.Device, bool) { d, ok := l[id]; return d, ok }

var (
	idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)                // contract A1
	tsPattern = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$`) // contract A4: UTC, ms, Z
	now       = time.Date(2026, 10, 27, 12, 0, 0, 123_000_000, time.UTC)
	simDevice = registry.Device{FleetID: "loadtest", TypeID: "loadtest-sim", Metrics: map[string]bool{"vibration_rms": true}}
)

func TestReadingPassesIngestValidation(t *testing.T) {
	for _, n := range []int{1, 42, 10000} {
		d := &device{id: deviceID("loadtest", n), phase: time.Duration(n) * time.Minute}
		reg := lookup{d.id: {ID: d.id, FleetID: simDevice.FleetID, TypeID: simDevice.TypeID, Metrics: simDevice.Metrics}}
		for i := range 3 {
			at := now.Add(time.Duration(i) * 5 * time.Second)
			r, reason, dropped := decoder.Decode(topic("loadtest", d.id, "telemetry"), d.reading(at, 1792000000), at, reg)
			if reason != decoder.OK || dropped != 0 {
				t.Fatalf("%s seq %d rejected: %q (dropped %d)", d.id, i, reason, dropped)
			}
			if r.DeviceID != d.id || r.FleetID != "loadtest" || r.Boot != 1792000000 || r.Seq != uint64(i) ||
				!r.Time.Equal(at.Truncate(time.Millisecond)) || len(r.Metrics) != 1 {
				t.Errorf("decoded %+v", r)
			}
		}
	}
}

func TestReadingEnvelope(t *testing.T) {
	d := &device{id: deviceID("loadtest", 7)}
	body := d.reading(now, 5)
	if len(body) > 1024 {
		t.Errorf("payload %d bytes, contract A4 limit is 1024", len(body))
	}
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"boot", "device_id", "fw", "metrics", "seq", "ts", "v"}) {
		t.Errorf("fields %v", keys)
	}
	if p["v"] != 1.0 || p["device_id"] != d.id || p["boot"] != 5.0 || p["seq"] != 0.0 {
		t.Errorf("envelope %v", p)
	}
	if fw, _ := p["fw"].(string); !strings.HasPrefix(fw, "loadgen-") {
		t.Errorf("fw %q: contract A4 says simulators send loadgen-<version>", fw)
	}
	if ts, _ := p["ts"].(string); !tsPattern.MatchString(ts) || ts != "2026-10-27T12:00:00.123Z" {
		t.Errorf("ts %q", ts)
	}
	m, _ := p["metrics"].(map[string]any)
	if v, ok := m["vibration_rms"].(float64); len(m) != 1 || !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		t.Errorf("metrics %v", m)
	}
}

func TestSeqCountsUpAndBootStays(t *testing.T) {
	d := &device{id: deviceID("loadtest", 1)}
	for i := range 5 {
		var p struct {
			Boot uint32 `json:"boot"`
			Seq  uint64 `json:"seq"`
		}
		_ = json.Unmarshal(d.reading(now.Add(time.Duration(i)*5*time.Second), 99), &p)
		if p.Seq != uint64(i) || p.Boot != 99 {
			t.Fatalf("reading %d: boot %d seq %d", i, p.Boot, p.Seq)
		}
	}
}

func TestWashCycleLevels(t *testing.T) {
	d := &device{id: "x"}
	start := time.Unix(0, 0).UTC() // phase 0: a cycle starts at every multiple of `cycle`
	rmsAt := func(off time.Duration) float64 {
		var p struct {
			Metrics map[string]float64 `json:"metrics"`
		}
		_ = json.Unmarshal(d.reading(start.Add(off), 1), &p)
		return p.Metrics["vibration_rms"]
	}
	washing, idling := 0, 0
	for off := time.Duration(0); off < cycle; off += 5 * time.Second {
		v := rmsAt(off)
		inWash := off < wash
		target := idleRMS
		if inWash {
			target, washing = washRMS, washing+1
		} else {
			idling++
		}
		if math.Abs(v-target) > 0.5*target { // noise is ±10 % (1σ); 5σ never happens in 720 draws
			t.Fatalf("at %v: rms %.4f, want about %.4f", off, v, target)
		}
	}
	if washing != int(wash/(5*time.Second)) || idling != int((cycle-wash)/(5*time.Second)) {
		t.Errorf("%d washing and %d idle readings per cycle", washing, idling)
	}
}

func TestDeviceIDsMatchContractAndSeed(t *testing.T) {
	seen := map[string]bool{}
	for n := 1; n <= 10000; n++ {
		id := deviceID("loadtest", n)
		if !idPattern.MatchString(id) || seen[id] {
			t.Fatalf("%q invalid or duplicate", id)
		}
		seen[id] = true
	}
	if deviceID("loadtest", 1) != "loadtest-00001" || deviceID("loadtest", 10000) != "loadtest-10000" {
		t.Error("IDs must match seed.sql")
	}
	seed, err := os.ReadFile("seed.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"'loadtest-' || lpad(i::text, 5, '0')", "generate_series(1, 10000)", "'loadtest-sim'"} {
		if !strings.Contains(string(seed), want) {
			t.Errorf("seed.sql no longer contains %q; loadgen IDs would be unknown to ingest", want)
		}
	}
}

func TestTopics(t *testing.T) {
	if got := topic("loadtest", "loadtest-00001", "telemetry"); got != "fleets/loadtest/devices/loadtest-00001/telemetry" {
		t.Errorf("telemetry topic %q (contract A3)", got)
	}
	if got := topic("loadtest", "loadtest-00001", "status"); got != "fleets/loadtest/devices/loadtest-00001/status" {
		t.Errorf("status topic %q (contract A3)", got)
	}
}

func TestTLSConfigAlwaysVerifies(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	os.WriteFile(caFile, selfSignedCA(t), 0o600)
	notPEM := filepath.Join(dir, "empty.pem")
	os.WriteFile(notPEM, []byte("not a certificate"), 0o600)

	system, err := tlsConfig("")
	if err != nil || system.RootCAs != nil {
		t.Errorf("empty CA should use system CAs: %v", err)
	}
	custom, err := tlsConfig(caFile)
	if err != nil || custom.RootCAs == nil {
		t.Errorf("CA file not loaded: %v", err)
	}
	for _, c := range []*tls.Config{system, custom} {
		if c != nil && (c.InsecureSkipVerify || c.MinVersion < tls.VersionTLS12) {
			t.Error("verification must never be disabled (CLAUDE.md §10)")
		}
	}
	if _, err := tlsConfig(filepath.Join(dir, "missing.pem")); err == nil {
		t.Error("missing CA file should fail")
	}
	if _, err := tlsConfig(notPEM); err == nil {
		t.Error("CA file without certificates should fail")
	}
}

func selfSignedCA(t *testing.T) []byte {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"},
		NotBefore: now, NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
