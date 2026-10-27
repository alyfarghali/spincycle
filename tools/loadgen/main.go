package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
)

type options struct {
	serverURL *url.URL
	tlsCfg    *tls.Config
	password  string
	fleet     string
}

var connected, published, failed atomic.Int64

func main() {
	broker := flag.String("broker", "mqtts://localhost:8883", "broker URL")
	caFile := flag.String("ca", "broker/certs/dev/ca.pem", "CA that signed the broker certificate (empty: system CAs)")
	fleet := flag.String("fleet", "loadtest", "fleet ID; the loadgen account may only publish to loadtest")
	n := flag.Int("devices", 1, "number of virtual devices")
	interval := flag.Duration("interval", 5*time.Second, "reading period per device")
	ramp := flag.Duration("ramp", 60*time.Second, "spread device start-up over this long")
	shared := flag.Bool("shared", false, "all devices on one connection (tests message rate, not connection count)")
	duration := flag.Duration("duration", 0, "stop after this long, counted from the end of the ramp (0: until Ctrl+C)")
	flag.Parse()

	password := os.Getenv("MQTT_LOADGEN_PASSWORD")
	if password == "" {
		log.Fatal("MQTT_LOADGEN_PASSWORD is not set; run: set -a; source .env; set +a")
	}
	serverURL, err := url.Parse(*broker)
	if err != nil {
		log.Fatalf("broker URL: %v", err)
	}
	tlsCfg, err := tlsConfig(*caFile)
	if err != nil {
		log.Fatal(err)
	}
	opts := options{serverURL: serverURL, tlsCfg: tlsCfg, password: password, fleet: *fleet}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *ramp+*duration)
		defer cancel()
	}

	var sharedCM *autopaho.ConnectionManager
	if *shared {
		if sharedCM, err = connect(ctx, opts, fmt.Sprintf("loadgen-%d", os.Getpid()), nil, nil); err != nil {
			log.Fatal(err)
		}
		if err := sharedCM.AwaitConnection(ctx); err != nil {
			log.Fatalf("no broker connection: %v", err)
		}
	}
	mode := "one connection per device"
	if *shared {
		mode = "one shared connection"
	}
	log.Printf("%s: %d devices every %s, ramp %s, %s", serverURL.Redacted(), *n, *interval, *ramp, mode)

	// A new boot per run: ingest drops repeated (device, boot, seq), so a fixed boot would discard every rerun.
	boot := uint32(time.Now().Unix())

	var wg sync.WaitGroup
	for i := range *n {
		d := &device{id: fmt.Sprintf("%s-%05d", *fleet, i+1), phase: time.Duration(rand.Int64N(int64(cycle)))}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Ramp: thousands of simultaneous TLS handshakes would measure a startup spike, not steady state.
			select {
			case <-time.After(*ramp * time.Duration(i) / time.Duration(*n)):
			case <-ctx.Done():
				return
			}
			cm := sharedCM
			if cm == nil {
				var err error
				// Own context: cancelling the run context would drop the connection before "offline" goes out.
				connCtx, closeConn := context.WithCancel(context.Background())
				defer closeConn()
				var pending sync.WaitGroup
				if cm, err = connect(connCtx, opts, d.id, d, &pending); err != nil {
					log.Printf("%s: %v", d.id, err)
					return
				}
				defer goOffline(cm, opts.fleet, d.id, &pending)
				if cm.AwaitConnection(ctx) != nil {
					return
				}
			}
			run(ctx, cm, d, opts.fleet, boot, *interval)
		}()
	}

	report := time.NewTicker(5 * time.Second)
	defer report.Stop()
	start := time.Now()
	var last int64
	for {
		select {
		case <-report.C:
			p := published.Load()
			log.Printf("t=%3.0fs connected %5d  published %7d (%4.0f/s)  failed %d",
				time.Since(start).Seconds(), connected.Load(), p, float64(p-last)/5, failed.Load())
			last = p
		case <-ctx.Done():
			wg.Wait()
			if sharedCM != nil {
				_ = sharedCM.Disconnect(context.Background())
			}
			log.Printf("done: published %d, failed %d", published.Load(), failed.Load())
			return
		}
	}
}

// connect opens one MQTT connection. With a device, it behaves like the real board (contract A2):
// client ID = device ID, LWT "offline" and retained "online" on its status topic.
func connect(ctx context.Context, o options, clientID string, d *device, pending *sync.WaitGroup) (*autopaho.ConnectionManager, error) {
	cfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{o.serverURL},
		TlsCfg:                        o.tlsCfg,
		KeepAlive:                     30,
		CleanStartOnInitialConnection: true,
		ConnectUsername:               "loadgen",
		ConnectPassword:               []byte(o.password),
		ReconnectBackoff:              autopaho.NewExponentialBackoff(time.Second, time.Minute, 2*time.Second, 2), // contract A2: 1 s to 60 s
		OnConnectError:                func(err error) { failed.Add(1) },
		OnConnectionDown:              func() bool { connected.Add(-1); return true },
		ClientConfig:                  paho.ClientConfig{ClientID: clientID},
	}
	if d == nil {
		cfg.OnConnectionUp = func(*autopaho.ConnectionManager, *paho.Connack) { connected.Add(1) }
		return autopaho.NewConnection(ctx, cfg)
	}

	status := fmt.Sprintf("fleets/%s/devices/%s/status", o.fleet, d.id)
	cfg.WillMessage = &paho.WillMessage{Topic: status, Payload: []byte("offline"), QoS: 1, Retain: true}
	cfg.OnConnectionUp = func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
		connected.Add(1)
		pending.Add(1)
		go func() {
			defer pending.Done()
			cm.Publish(ctx, &paho.Publish{Topic: status, Payload: []byte("online"), QoS: 1, Retain: true})
		}()
	}
	return autopaho.NewConnection(ctx, cfg)
}

// A clean disconnect doesn't fire the Last Will, so a device says "offline" itself first (contract A6).
func goOffline(cm *autopaho.ConnectionManager, fleet, id string, pending *sync.WaitGroup) {
	pending.Wait() // an "online" still in flight would otherwise land after "offline" and overwrite it
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status := fmt.Sprintf("fleets/%s/devices/%s/status", fleet, id)
	_, _ = cm.Publish(ctx, &paho.Publish{Topic: status, Payload: []byte("offline"), QoS: 1, Retain: true})
	_ = cm.Disconnect(ctx)
}

func run(ctx context.Context, cm *autopaho.ConnectionManager, d *device, fleet string, boot uint32, interval time.Duration) {
	topic := fmt.Sprintf("fleets/%s/devices/%s/telemetry", fleet, d.id)
	// Random offset within one interval, so devices that connected together don't publish together.
	select {
	case <-time.After(rand.N(interval)):
	case <-ctx.Done():
		return
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		_, err := cm.Publish(ctx, &paho.Publish{Topic: topic, QoS: 1, Payload: d.reading(time.Now(), boot)})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			failed.Add(1)
		} else {
			published.Add(1)
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
	}
}

// Same rules as ingest's subscriber: certificate and hostname are always verified.
func tlsConfig(caFile string) (*tls.Config, error) {
	if caFile == "" {
		return &tls.Config{MinVersion: tls.VersionTLS12}, nil
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no certificates in %s", caFile)
	}
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}
