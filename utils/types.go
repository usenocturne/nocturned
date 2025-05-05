package utils

// Bluetooth
type BluetoothDeviceInfo struct {
	Address           string `json:"address"`
	Name              string `json:"name"`
	Alias             string `json:"alias"`
	Class             string `json:"class"`
	Icon              string `json:"icon"`
	Paired            bool   `json:"paired"`
	Trusted           bool   `json:"trusted"`
	Blocked           bool   `json:"blocked"`
	Connected         bool   `json:"connected"`
	LegacyPairing     bool   `json:"legacyPairing"`
	BatteryPercentage int    `json:"batteryPercentage,omitempty"`
}

// Media Player
type MediaPlayerState string

const (
	Playing MediaPlayerState = "playing"
	Paused  MediaPlayerState = "paused"
	Stopped MediaPlayerState = "stopped"
)

type MediaPlayerInfo struct {
	Name     string          `json:"name"`
	Status   MediaPlayerState `json:"status"`
	Track    MediaTrackInfo  `json:"track"`
	Position uint32          `json:"position"`
	Address  string          `json:"address"`
}

type MediaTrackInfo struct {
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Duration uint32 `json:"duration"`
}

type PairingRequest struct {
	Device      string
	Passkey     string
	RequestType string
}

// WebSocket
type WebSocketEvent struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type PairingStartedPayload struct {
	Address    string `json:"address"`
	PairingKey string `json:"pairingKey"`
}

type DeviceConnectedPayload struct {
	Address string               `json:"address"`
	Device  *BluetoothDeviceInfo `json:"device,omitempty"`
}

type DeviceDisconnectedPayload struct {
	Address string `json:"address"`
}

type DevicePairedPayload struct {
	Device *BluetoothDeviceInfo `json:"device"`
}

type NetworkConnectedPayload struct {
	Address string `json:"address"`
}

type MediaPlayerUpdatePayload struct {
	Player MediaPlayerInfo `json:"player"`
}
