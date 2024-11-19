package bluetooth

import (
	"fmt"
	"log"

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

func (a *Agent) RequestPinCode(device dbus.ObjectPath) (string, *dbus.Error) {
	log.Printf("RequestPinCode from %s", device)
	return "", dbus.NewError("org.bluez.Error.Rejected", nil)
}

func (a *Agent) DisplayPinCode(device dbus.ObjectPath, pincode string) *dbus.Error {
	log.Printf("DisplayPinCode (%s) from %s", pincode, device)

	a.manager.mu.Lock()
	a.manager.pairingInProgress = true
	a.manager.pairingKey = pincode
	a.manager.mu.Unlock()

	return nil
}

func (a *Agent) RequestPasskey(device dbus.ObjectPath) (uint32, *dbus.Error) {
	log.Printf("RequestPasskey from %s", device)
	return 0, dbus.NewError("org.bluez.Error.Rejected", nil)
}

func (a *Agent) DisplayPasskey(device dbus.ObjectPath, passkey uint32, entered uint16) *dbus.Error {
	log.Printf("DisplayPasskey (%d entered %d) from %s", passkey, entered, device)

	a.manager.mu.Lock()
	a.manager.pairingInProgress = true
	a.manager.pairingKey = fmt.Sprintf("%d", passkey)
	a.manager.mu.Unlock()

	return nil
}

func (a *Agent) RequestConfirmation(device dbus.ObjectPath, passkey uint32) *dbus.Error {
	log.Printf("RequestConfirmation (%d) from %s", passkey, device)

	a.current = &PairingRequest{
		Device:      string(device),
		Passkey:     fmt.Sprintf("%06d", passkey),
		RequestType: "confirmation",
	}

	a.manager.mu.Lock()
	a.manager.pairingInProgress = true
	a.manager.pairingKey = a.current.Passkey
	a.manager.mu.Unlock()

	return nil
}

func (a *Agent) RequestAuthorization(device dbus.ObjectPath) *dbus.Error {
	log.Printf("RequestAuthorization from %s", device)
	return nil
}

func (a *Agent) AuthorizeService(device dbus.ObjectPath, uuid string) *dbus.Error {
	log.Printf("AuthorizeService (%s) from %s", uuid, device)
	return nil
}

func (a *Agent) Cancel() *dbus.Error {
	log.Println("Request canceled")

	a.manager.mu.Lock()
	a.manager.pairingInProgress = false
	a.manager.pairingKey = ""
	a.manager.mu.Unlock()

	return nil
}

func (a *Agent) AcceptPairing() error {
	if a.current == nil {
		return fmt.Errorf("no pairing request in progress")
	}

	a.current = nil

	a.manager.mu.Lock()
	a.manager.pairingInProgress = false
	a.manager.pairingKey = ""
	a.manager.mu.Unlock()

	return nil
}

func (a *Agent) RejectPairing() error {
	if a.current == nil {
		return fmt.Errorf("no pairing request in progress")
	}

	a.current = nil

	a.manager.mu.Lock()
	a.manager.pairingInProgress = false
	a.manager.pairingKey = ""
	a.manager.mu.Unlock()

	return nil
}
