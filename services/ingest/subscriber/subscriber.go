// paho.golang MQTT 5 client: shared subscription, persistent session, manual ack; hands messages to a channel without blocking.

package subscriber

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"

	"github.com/alyfarghali/spincycle/services/ingest/config"
)

const (
	TelemetryTopic = "$share/ingest/fleets/+/devices/+/telemetry"
	ReceiveMaximum = 1000
	InboxSize      = 2 * ReceiveMaximum
)

type Message struct {
	Packet *paho.Publish
	client *paho.Client
}

func Start(ctx context.Context, cfg config.Config, inbox chan<- Message, quit <-chan struct{}) (*autopaho.ConnectionManager, error) {
	serverURL, err := url.Parse(cfg.BrokerURL)
	if err != nil {
		return nil, fmt.Errorf("parse broker URL: %w", err)
	}
	tlsCfg, err := tlsConfig(cfg.CAFile)
	if err != nil {
		return nil, err
	}

	receiveMax := uint16(ReceiveMaximum)
	cliCfg := autopaho.ClientConfig{
		ServerUrls: []*url.URL{serverURL},
		TlsCfg:     tlsCfg,
		KeepAlive:  30,

		CleanStartOnInitialConnection: false,
		SessionExpiryInterval:         3600,

		ConnectUsername: cfg.MQTTUser,
		ConnectPassword: []byte(cfg.MQTTPassword),

		ConnectPacketBuilder: func(cp *paho.Connect, _ *url.URL) (*paho.Connect, error) {
			if cp.Properties == nil {
				cp.Properties = &paho.ConnectProperties{}
			}
			cp.Properties.ReceiveMaximum = &receiveMax
			return cp, nil
		},

		OnConnectionUp: func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
			slog.Info("broker connected", "url", serverURL.Redacted())
			go func() {
				if _, err := cm.Subscribe(ctx, &paho.Subscribe{
					Subscriptions: []paho.SubscribeOptions{{Topic: TelemetryTopic, QoS: 1}},
				}); err != nil {
					slog.Error("subscribe failed", "topic", TelemetryTopic, "err", err)
				}
			}()
		},
		OnConnectionDown: func() bool {
			slog.Warn("broker connection lost; reconnecting")
			return true
		},
		OnConnectError: func(err error) {
			slog.Error("broker connect failed", "err", err)
		},

		ClientConfig: paho.ClientConfig{
			ClientID:                   cfg.ClientID,
			EnableManualAcknowledgment: true,
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				func(pr paho.PublishReceived) (bool, error) {
					select {
					case inbox <- Message{Packet: pr.Packet, client: pr.Client}:
					case <-quit:
					}
					return true, nil
				},
			},
		},
	}

	return autopaho.NewConnection(ctx, cliCfg)
}

func tlsConfig(caFile string) (*tls.Config, error) {
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
