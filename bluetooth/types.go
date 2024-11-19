package bluetooth

type BluetoothDeviceInfo struct {
	Address          string `json:"address"`
	Name             string `json:"name"`
	Alias            string `json:"alias"`
	Class            string `json:"class"`
	Icon             string `json:"icon"`
	Paired           bool   `json:"paired"`
	Bonded           bool   `json:"bonded"`
	Trusted          bool   `json:"trusted"`
	Blocked          bool   `json:"blocked"`
	Connected        bool   `json:"connected"`
	LegacyPairing    bool   `json:"legacyPairing"`
	BatteryPercentage int    `json:"batteryPercentage,omitempty"`
}

type PairingRequest struct {
	Device     string
	Passkey    string
	RequestType string
}
