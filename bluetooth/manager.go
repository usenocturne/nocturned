package bluetooth

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/godbus/dbus/v5"
	"github.com/vishvananda/netlink"

	"github.com/usenocturne/nocturned/utils"
	"github.com/usenocturne/nocturned/ws"
)

type BluetoothManager struct {
	conn            *dbus.Conn
	adapter         dbus.ObjectPath
	agent           *Agent
	mu              sync.Mutex
	pairingRequests chan utils.PairingRequest
	wsHub           *ws.WebSocketHub
}

func NewBluetoothManager(wsHub *ws.WebSocketHub) (*BluetoothManager, error) {
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
		pairingRequests: make(chan utils.PairingRequest, 1),
		wsHub:           wsHub,
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
	manager.monitorNetworkInterfaces()

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
						address := strings.TrimPrefix(devicePath, string(m.adapter)+"/dev_")
						address = strings.ReplaceAll(address, "_", ":")

						if m.wsHub != nil {
							m.wsHub.Broadcast(utils.WebSocketEvent{
								Type: "bluetooth/disconnect",
								Payload: utils.DeviceDisconnectedPayload{
									Address: address,
								},
							})
						}

						log.Printf("Device disconnected: %s", devicePath)

						if m.agent != nil && m.agent.current != nil && m.agent.current.Device == devicePath {
							m.mu.Lock()
							m.agent.current = nil
							m.mu.Unlock()
						}
					}
				}
			}
		}
	}()
}

func (m *BluetoothManager) monitorNetworkInterfaces() {
	linkUpdates := make(chan netlink.LinkUpdate)
	done := make(chan struct{})

	if err := netlink.LinkSubscribe(linkUpdates, done); err != nil {
		log.Printf("Failed to subscribe to link updates: %v", err)
		return
	}

	go func() {
		for update := range linkUpdates {
			if update.Header.Type == syscall.RTM_DELLINK && update.Link.Attrs().Name == "bnep0" {
				log.Println("bnep0 interface removed")

				// disconnect
				m.signalUdhcpc("bnep0", "USR2")

				if m.wsHub != nil {
					m.wsHub.Broadcast(utils.WebSocketEvent{
						Type: "bluetooth/network/disconnect",
					})
				}
			}

			if update.Header.Type == syscall.RTM_NEWLINK && update.Link.Attrs().Name == "bnep0" {
				if update.Link.Attrs().Flags&net.FlagUp != 0 {
					log.Println("bnep0 interface added and up")

					// connect
					m.ensureUdhcpcRunning("bnep0")
					m.signalUdhcpc("bnep0", "USR1")
				}
			}
		}
	}()
}

// signalUdhcpc sends a signal to the udhcpc process for the given interface
func (m *BluetoothManager) signalUdhcpc(iface, signal string) {
	pidFile := fmt.Sprintf("/var/run/udhcpc.%s.pid", iface)

	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		log.Printf("Failed to read udhcpc PID file for %s: %v", iface, err)

		if signal == "USR1" {
			m.ensureUdhcpcRunning(iface)
		}
		return
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		log.Printf("Invalid PID in udhcpc PID file for %s: %v", iface, err)
		return
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		log.Printf("Failed to find udhcpc process (PID %d) for %s: %v", pid, iface, err)
		return
	}

	var sig syscall.Signal
	switch signal {
	case "USR1":
		sig = syscall.SIGUSR1
	case "USR2":
		sig = syscall.SIGUSR2
	default:
		log.Printf("Invalid signal %s", signal)
		return
	}

	if err := process.Signal(sig); err != nil {
		log.Printf("Failed to send %s signal to udhcpc process (PID %d) for %s: %v", signal, pid, iface, err)

		if signal == "USR1" {
			m.ensureUdhcpcRunning(iface)
		}
		return
	}

	log.Printf("Sent %s signal to udhcpc process (PID %d) for %s", signal, pid, iface)
}

func (m *BluetoothManager) ensureUdhcpcRunning(iface string) {
	log.Printf("Ensuring udhcpc is running for %s", iface)

	link, err := netlink.LinkByName(iface)
	if err != nil {
		log.Printf("Interface %s not found: %v", iface, err)
		return
	}

	if link.Attrs().Flags&net.FlagUp == 0 {
		log.Printf("Interface %s is not up", iface)
		return
	}

	pidFile := fmt.Sprintf("/var/run/udhcpc.%s.pid", iface)

	if pidBytes, err := os.ReadFile(pidFile); err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
		if err == nil {
			process, err := os.FindProcess(pid)
			if err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					log.Printf("udhcpc is already running for %s with PID %d", iface, pid)
					return
				}
			}
		}
	}

	findCmd := exec.Command("pgrep", "-f", fmt.Sprintf("udhcpc -i %s", iface))
	findOutput, err := findCmd.Output()
	if err == nil && len(findOutput) > 0 {
		log.Printf("Found existing udhcpc process for %s: %s", iface, strings.TrimSpace(string(findOutput)))
		return
	}

	cmd := exec.Command("udhcpc", "-i", iface, "-p", pidFile, "-f", "-b", "-S")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to start udhcpc for %s: %v, output: %s", iface, err, output)
		return
	}

	log.Printf("Started udhcpc for %s: %s", iface, strings.TrimSpace(string(output)))
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

func (m *BluetoothManager) GetDeviceInfo(address string) (*utils.BluetoothDeviceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	devicePath := formatDevicePath(m.adapter, address)
	obj := m.conn.Object(BLUEZ_BUS_NAME, devicePath)

	props := make(map[string]dbus.Variant)
	if err := obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, BLUEZ_DEVICE_INTERFACE).Store(&props); err != nil {
		return nil, err
	}

	info := &utils.BluetoothDeviceInfo{
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

func (m *BluetoothManager) AcceptPairing() error {
	return m.agent.AcceptPairing()
}

func (m *BluetoothManager) DenyPairing() error {
	return m.agent.RejectPairing()
}

func (m *BluetoothManager) GetCurrentPairingRequest() *utils.PairingRequest {
	if m.agent == nil {
		return nil
	}
	return m.agent.current
}

func (m *BluetoothManager) ConnectNetwork(address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	devicePath := formatDevicePath(m.adapter, address)
	obj := m.conn.Object(BLUEZ_BUS_NAME, devicePath)

	if err := obj.Call("org.bluez.Network1.Connect", 0, "nap").Err; err != nil {
		return err
	}

	link, err := netlink.LinkByName("bnep0")
	if err != nil || link.Attrs().Flags&net.FlagUp == 0 {
		return fmt.Errorf("bnep0 interface is not up")
	}

	m.ensureUdhcpcRunning("bnep0")
	m.signalUdhcpc("bnep0", "USR1")

	if m.wsHub != nil {
		m.wsHub.Broadcast(utils.WebSocketEvent{
			Type: "bluetooth/network/connect",
			Payload: utils.NetworkConnectedPayload{
				Address: address,
			},
		})
	}

	return nil
}

func (m *BluetoothManager) GetDevices() ([]utils.BluetoothDeviceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var devices []utils.BluetoothDeviceInfo

	objects := make(map[dbus.ObjectPath]map[string]map[string]dbus.Variant)
	obj := m.conn.Object(BLUEZ_BUS_NAME, "/")
	if err := obj.Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objects); err != nil {
		return nil, fmt.Errorf("failed to get managed objects: %v", err)
	}

	for path, interfaces := range objects {
		if deviceProps, ok := interfaces[BLUEZ_DEVICE_INTERFACE]; ok {
			address := strings.TrimPrefix(string(path), string(m.adapter)+"/dev_")
			address = strings.ReplaceAll(address, "_", ":")

			deviceInfo := utils.BluetoothDeviceInfo{
				Address: address,
			}

			if v, ok := deviceProps["Name"]; ok {
				deviceInfo.Name = v.Value().(string)
			}
			if v, ok := deviceProps["Alias"]; ok {
				deviceInfo.Alias = v.Value().(string)
			}
			if v, ok := deviceProps["Class"]; ok {
				deviceInfo.Class = fmt.Sprintf("%d", v.Value().(uint32))
			}
			if v, ok := deviceProps["Icon"]; ok {
				deviceInfo.Icon = v.Value().(string)
			}
			if v, ok := deviceProps["Paired"]; ok {
				deviceInfo.Paired = v.Value().(bool)
			}
			if v, ok := deviceProps["Trusted"]; ok {
				deviceInfo.Trusted = v.Value().(bool)
			}
			if v, ok := deviceProps["Blocked"]; ok {
				deviceInfo.Blocked = v.Value().(bool)
			}
			if v, ok := deviceProps["Connected"]; ok {
				deviceInfo.Connected = v.Value().(bool)
			}
			if v, ok := deviceProps["LegacyPairing"]; ok {
				deviceInfo.LegacyPairing = v.Value().(bool)
			}

			if batteryProps, ok := interfaces[BLUEZ_BATTERY_INTERFACE]; ok {
				if v, ok := batteryProps["Percentage"]; ok {
					deviceInfo.BatteryPercentage = int(v.Value().(uint8))
				}
			}

			devices = append(devices, deviceInfo)
		}
	}

	return devices, nil
}

func (m *BluetoothManager) ConnectDevice(address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	devicePath := formatDevicePath(m.adapter, address)
	obj := m.conn.Object(BLUEZ_BUS_NAME, devicePath)

	props := make(map[string]dbus.Variant)
	if err := obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, BLUEZ_DEVICE_INTERFACE).Store(&props); err != nil {
		return fmt.Errorf("failed to get device properties: %v", err)
	}

	paired, ok := props["Paired"]
	if !ok || !paired.Value().(bool) {
		return fmt.Errorf("device is not paired")
	}

	if err := obj.Call("org.freedesktop.DBus.Properties.Set", 0,
		BLUEZ_DEVICE_INTERFACE, "Trusted", dbus.MakeVariant(true)).Err; err != nil {
		return fmt.Errorf("failed to set device as trusted: %v", err)
	}

	if err := obj.Call("org.bluez.Device1.Connect", 0).Err; err != nil {
		return fmt.Errorf("failed to connect to device: %v", err)
	}

	log.Printf("Device connected: %s", devicePath)

	if m.wsHub != nil {
		m.wsHub.Broadcast(utils.WebSocketEvent{
			Type: "bluetooth/connect",
			Payload: utils.DeviceConnectedPayload{
				Address: address,
			},
		})
	}

	return nil
}

func (m *BluetoothManager) DisconnectDevice(address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	devicePath := formatDevicePath(m.adapter, address)
	obj := m.conn.Object(BLUEZ_BUS_NAME, devicePath)

	if err := obj.Call("org.bluez.Device1.Disconnect", 0).Err; err != nil {
		return fmt.Errorf("failed to disconnect device: %v", err)
	}

	if m.wsHub != nil {
		m.wsHub.Broadcast(utils.WebSocketEvent{
			Type: "bluetooth/disconnect",
			Payload: utils.DeviceDisconnectedPayload{
				Address: address,
			},
		})
	}

	return nil
}
