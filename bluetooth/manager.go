package bluetooth

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

type BluetoothManager struct {
	conn             *dbus.Conn
	adapter          dbus.ObjectPath
	agent            *Agent
	mu               sync.Mutex
	pairingRequests  chan PairingRequest
	pairingInProgress bool
	pairingKey        string
}

func NewBluetoothManager() (*BluetoothManager, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %v", err)
	}
	log.Println("Connected to system bus")

	adapter, err := findDefaultAdapter(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to find bluetooth adapter: %v", err)
	}
	log.Printf("Found adapter: %s", adapter)

	manager := &BluetoothManager{
		conn:            conn,
		adapter:         adapter,
		pairingRequests: make(chan PairingRequest, 1),
	}

	agent, err := NewAgent(conn, manager)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %v", err)
	}
	manager.agent = agent

	if err := manager.setPower(true); err != nil {
		return nil, fmt.Errorf("failed to power on adapter: %v", err)
	}

	manager.monitorDisconnects()

	return manager, nil
}

func findDefaultAdapter(conn *dbus.Conn) (dbus.ObjectPath, error) {
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/DBus"),
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, "org.bluez"),
	); err != nil {
		return "", fmt.Errorf("failed to add match: %v", err)
	}

	var owner string
	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	err := obj.Call("org.freedesktop.DBus.GetNameOwner", 0, "org.bluez").Store(&owner)
	if err != nil {
		return "", fmt.Errorf("failed to get bluez owner: %v", err)
	}

	if err := conn.AddMatchSignal(
		dbus.WithMatchSender("org.bluez"),
		dbus.WithMatchPathNamespace("/"),
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesAdded"),
	); err != nil {
		return "", fmt.Errorf("failed to add interfaces match: %v", err)
	}

	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	obj = conn.Object("org.bluez", "/")
	if err := obj.Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objects); err != nil {
		return "", fmt.Errorf("failed to get managed objects: %v", err)
	}

	for path, interfaces := range objects {
		_, hasAdapter := interfaces["org.bluez.Adapter1"]
		if hasAdapter {
			return path, nil
		}
	}

	return "", fmt.Errorf("no bluetooth adapter found")
}

func (m *BluetoothManager) monitorDisconnects() {
	if err := m.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchPathNamespace("/org/bluez"),
	); err != nil {
		log.Printf("Failed to add signal match: %v", err)
		return
	}

	signals := make(chan *dbus.Signal, 10)
	m.conn.Signal(signals)

	go func() {
		for signal := range signals {
			if signal.Name == "org.freedesktop.DBus.Properties.PropertiesChanged" {
				if len(signal.Body) < 3 {
					continue
				}

				iface := signal.Body[0].(string)
				if iface != BLUEZ_DEVICE_INTERFACE {
					continue
				}

				changes := signal.Body[1].(map[string]dbus.Variant)
				if connected, ok := changes["Connected"]; ok {
					if !connected.Value().(bool) {
						devicePath := string(signal.Path)
						log.Printf("Device disconnected: %s", devicePath)

						if m.agent != nil && m.agent.current != nil && m.agent.current.Device == devicePath {
							m.mu.Lock()
							m.pairingInProgress = false
							m.pairingKey = ""
							m.agent.current = nil
							m.mu.Unlock()
						}
					}
				}
			}
		}
	}()
}

func (m *BluetoothManager) setPower(enable bool) error {
	obj := m.conn.Object(BLUEZ_BUS_NAME, m.adapter)
	return obj.Call("org.freedesktop.DBus.Properties.Set", 0,
		BLUEZ_ADAPTER_INTERFACE, "Powered", dbus.MakeVariant(enable)).Err
}

func (m *BluetoothManager) SetDiscoverable(enable bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	obj := m.conn.Object(BLUEZ_BUS_NAME, m.adapter)

	if err := obj.Call("org.freedesktop.DBus.Properties.Set", 0,
		BLUEZ_ADAPTER_INTERFACE, "Discoverable", dbus.MakeVariant(enable)).Err; err != nil {
		return err
	}

	return obj.Call("org.freedesktop.DBus.Properties.Set", 0,
		BLUEZ_ADAPTER_INTERFACE, "Pairable", dbus.MakeVariant(enable)).Err
}

func formatDevicePath(adapter dbus.ObjectPath, address string) dbus.ObjectPath {
	formattedAddress := strings.ReplaceAll(address, ":", "_")
	return dbus.ObjectPath(fmt.Sprintf("%s/dev_%s", adapter, formattedAddress))
}

func (m *BluetoothManager) GetDeviceInfo(address string) (*BluetoothDeviceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	devicePath := formatDevicePath(m.adapter, address)
	obj := m.conn.Object(BLUEZ_BUS_NAME, devicePath)

	props := make(map[string]dbus.Variant)
	if err := obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, BLUEZ_DEVICE_INTERFACE).Store(&props); err != nil {
		return nil, err
	}

	info := &BluetoothDeviceInfo{
		Address: address,
	}

	if v, ok := props["Name"]; ok {
		info.Name = v.Value().(string)
	}
	if v, ok := props["Alias"]; ok {
		info.Alias = v.Value().(string)
	}
	if v, ok := props["Class"]; ok {
		info.Class = fmt.Sprintf("%d", v.Value().(uint32))
	}
	if v, ok := props["Icon"]; ok {
		info.Icon = v.Value().(string)
	}
	if v, ok := props["Paired"]; ok {
		info.Paired = v.Value().(bool)
	}
	if v, ok := props["Trusted"]; ok {
		info.Trusted = v.Value().(bool)
	}
	if v, ok := props["Blocked"]; ok {
		info.Blocked = v.Value().(bool)
	}
	if v, ok := props["Connected"]; ok {
		info.Connected = v.Value().(bool)
	}
	if v, ok := props["LegacyPairing"]; ok {
		info.LegacyPairing = v.Value().(bool)
	}

	batteryProps := make(map[string]dbus.Variant)
	if err := obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, BLUEZ_BATTERY_INTERFACE).Store(&batteryProps); err == nil {
		if v, ok := batteryProps["Percentage"]; ok {
			info.BatteryPercentage = int(v.Value().(uint8))
		}
	}

	return info, nil
}

func (m *BluetoothManager) RemoveDevice(address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	devicePath := formatDevicePath(m.adapter, address)
	obj := m.conn.Object(BLUEZ_BUS_NAME, m.adapter)

	return obj.Call(BLUEZ_ADAPTER_INTERFACE+".RemoveDevice", 0, devicePath).Err
}

func (m *BluetoothManager) IsPairingInProgress() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pairingInProgress
}

func (m *BluetoothManager) GetPairingKey() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pairingKey
}

func (m *BluetoothManager) AcceptPairing() error {
	return m.agent.AcceptPairing()
}

func (m *BluetoothManager) DenyPairing() error {
	return m.agent.RejectPairing()
}

func (m *BluetoothManager) GetCurrentPairingRequest() *PairingRequest {
	if m.agent == nil {
		return nil
	}
	return m.agent.current
}