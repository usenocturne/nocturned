package bluetooth

import (
	"fmt"
	"log"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

type Agent struct {
	conn    *dbus.Conn
	manager *BluetoothManager
	path    dbus.ObjectPath
	current *PairingRequest
}

func NewAgent(conn *dbus.Conn, manager *BluetoothManager) (*Agent, error) {
	agent := &Agent{
		conn:    conn,
		manager: manager,
		path:    dbus.ObjectPath(BLUEZ_AGENT_PATH),
	}

	if err := conn.Export(agent, agent.path, BLUEZ_AGENT_INTERFACE); err != nil {
		return nil, err
	}

	node := &introspect.Node{
		Name: BLUEZ_AGENT_PATH,
		Interfaces: []introspect.Interface{
			{
				Name:    BLUEZ_AGENT_INTERFACE,
				Methods: introspect.Methods(agent),
			},
		},
	}

	if err := conn.Export(introspect.NewIntrospectable(node), agent.path,
		"org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, err
	}

	obj := conn.Object(BLUEZ_BUS_NAME, dbus.ObjectPath(BLUEZ_OBJECT_PATH))
	if err := obj.Call(BLUEZ_AGENT_MANAGER+".RegisterAgent", 0, agent.path, "").Err; err != nil {
		return nil, err
	}

	return agent, nil
}

func (a *Agent) Release() *dbus.Error {
	log.Println("Agent released")
	return nil
}

func (a *Agent) RequestConfirmation(device dbus.ObjectPath, passkey uint32) *dbus.Error {
	log.Printf("RequestConfirmation (%d) from %s", passkey, device)

	passkeyStr := fmt.Sprintf("%06d", passkey)
	a.current = &PairingRequest{
		Device:      string(device),
		Passkey:     passkeyStr,
		RequestType: "confirmation",
	}

	if a.manager.wsClients != nil {
		address := strings.TrimPrefix(string(device), string(a.manager.adapter)+"/dev_")
		address = strings.ReplaceAll(address, "_", ":")

		a.manager.wsClients.Broadcast(WebSocketEvent{
			Type: "bluetooth/pairing",
			Payload: PairingStartedPayload{
				Address:    address,
				PairingKey: passkeyStr,
			},
		})
	}

	return nil
}

func (a *Agent) RequestAuthorization(device dbus.ObjectPath) *dbus.Error {
	log.Printf("RequestAuthorization from %s", device)
	return nil
}

func (a *Agent) AcceptPairing() error {
	if a.current == nil {
		return fmt.Errorf("no pairing request in progress")
	}

	address := strings.TrimPrefix(a.current.Device, string(a.manager.adapter)+"/dev_")
	address = strings.ReplaceAll(address, "_", ":")

	deviceInfo, err := a.manager.GetDeviceInfo(address)
	if err != nil {
		log.Printf("Error getting device info after pairing: %v", err)
		deviceInfo = &BluetoothDeviceInfo{
			Address: address,
			Paired:  true,
		}
	}

	if a.manager.wsClients != nil {
		a.manager.wsClients.Broadcast(WebSocketEvent{
			Type: "bluetooth/paired",
			Payload: DevicePairedPayload{
				Device: deviceInfo,
			},
		})
	}

	a.current = nil
	return nil
}

func (a *Agent) RejectPairing() error {
	if a.current == nil {
		return fmt.Errorf("no pairing request in progress")
	}

	a.current = nil
	return nil
}

func (a *Agent) Cancel() *dbus.Error {
	log.Println("Pairing cancelled")

	if a.current != nil && a.manager.wsClients != nil {
		address := strings.TrimPrefix(a.current.Device, string(a.manager.adapter)+"/dev_")
		address = strings.ReplaceAll(address, "_", ":")

		a.manager.wsClients.Broadcast(WebSocketEvent{
			Type: "bluetooth/pairing/cancelled",
			Payload: DeviceDisconnectedPayload{
				Address: address,
			},
		})
	}

	a.current = nil
	return nil
}