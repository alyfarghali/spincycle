package decoder

import "strings"

const BadStatus Reason = "bad_status"

func DecodeStatus(topic string, payload []byte, devs DeviceLookup) (deviceID string, offline bool, reason Reason) {
	parts := strings.Split(topic, "/")
	if len(parts) != 5 || parts[0] != "fleets" || parts[2] != "devices" || parts[4] != "status" {
		return "", false, UnknownDevice
	}
	dev, ok := devs.Lookup(parts[3])
	if !ok {
		return "", false, UnknownDevice
	}
	if dev.FleetID != parts[1] {
		return "", false, FleetMismatch
	}
	switch string(payload) {
	case "offline":
		return parts[3], true, OK
	case "online":
		return parts[3], false, OK
	}
	return "", false, BadStatus
}
