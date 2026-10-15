// Loads ingest settings from environment variables (see .env.example).

package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	BrokerURL    string // INGEST_BROKER_URL, e.g. mqtts://localhost:8883
	CAFile       string // INGEST_CA_FILE: CA that signed the broker certificate
	ClientID     string // INGEST_CLIENT_ID: fixed, so the broker keeps our session
	MQTTUser     string // INGEST_MQTT_USER
	MQTTPassword string // MQTT_INGEST_PASSWORD
	DatabaseURL  string // DATABASE_URL
}

// Load reads every setting and reports all missing ones at once, so a bad
// .env is fixed in one go instead of one variable per restart.
func Load() (Config, error) {
	var missing []string
	get := func(name string) string {
		v := os.Getenv(name)
		if v == "" {
			missing = append(missing, name)
		}
		return v
	}

	cfg := Config{
		BrokerURL:    get("INGEST_BROKER_URL"),
		CAFile:       get("INGEST_CA_FILE"),
		ClientID:     get("INGEST_CLIENT_ID"),
		MQTTUser:     get("INGEST_MQTT_USER"),
		MQTTPassword: get("MQTT_INGEST_PASSWORD"),
		DatabaseURL:  get("DATABASE_URL"),
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}
