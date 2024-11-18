package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BluetoothManager struct {
	cmd              *exec.Cmd
	stdin            io.WriteCloser
	stdout           *bufio.Reader
	pairingInProgress bool
	pairingKey       string
	mu               sync.Mutex
}

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
	Modalias         string `json:"modalias,omitempty"`
	BatteryPercentage int   `json:"batteryPercentage,omitempty"`
}

var (
	btManager *BluetoothManager
	pairingKeyRegex = regexp.MustCompile(`\[agent\] Confirm passkey (\d+)`)
)

func InitBluetoothManager() (*BluetoothManager, error) {
	cmd := exec.Command("bluetoothctl")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	manager := &BluetoothManager{
		cmd:              cmd,
		stdin:            stdin,
		stdout:           bufio.NewReader(stdout),
		pairingInProgress: false,
		pairingKey:       "",
	}

	go manager.monitorOutput()

	manager.executeCommand("power on")

	return manager, nil
}

func (m *BluetoothManager) monitorOutput() {
	for {
		line, err := m.stdout.ReadString('\n')
		if err != nil {
			log.Printf("Error reading bluetooth output: %v", err)
			return
		}

		log.Printf("Bluetooth output: %s", line)

		m.mu.Lock()
		if strings.Contains(line, "Request confirmation") {
			m.pairingInProgress = true
		} else if strings.Contains(line, "Request canceled") {
			m.pairingInProgress = false
			m.pairingKey = ""
			log.Printf("Pairing request cancelled")
		} else if matches := pairingKeyRegex.FindStringSubmatch(line); len(matches) > 1 {
			m.pairingKey = matches[1]
			log.Printf("Found pairing key: %s", m.pairingKey)
		}
		m.mu.Unlock()
	}
}

func (m *BluetoothManager) executeCommand(cmd string) error {
	_, err := m.stdin.Write([]byte(cmd + "\n"))
	return err
}

func (m *BluetoothManager) SetDiscoverable(enable bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if enable {
		if err := m.executeCommand("discoverable on"); err != nil {
			return err
		}
		return m.executeCommand("pairable on")
	}

	if err := m.executeCommand("discoverable off"); err != nil {
		return err
	}
	return m.executeCommand("pairable off")
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pairingInProgress = false
	// m.pairingKey = ""
	return m.executeCommand("yes")
}

func (m *BluetoothManager) DenyPairing() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pairingInProgress = false
	m.pairingKey = ""
	return m.executeCommand("no")
}

func (m *BluetoothManager) GetDeviceInfo(address string) (*BluetoothDeviceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldStdout := m.stdout

	pr, pw := io.Pipe()
	m.stdout = bufio.NewReader(pr)

	resultChan := make(chan *BluetoothDeviceInfo, 1)
	errChan := make(chan error, 1)

	go func() {
		info := &BluetoothDeviceInfo{Address: address}
		scanner := bufio.NewScanner(pr)

		for scanner.Scan() {
			line := scanner.Text()

			if strings.Contains(line, "UUID:") {
				continue
			}

			if strings.HasPrefix(line, "Device") {
				continue
			}

			parts := strings.SplitN(strings.TrimSpace(line), ": ", 2)
			if len(parts) != 2 {
				continue
			}

			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])

			switch key {
			case "Name":
				info.Name = value
			case "Alias":
				info.Alias = value
			case "Class":
				info.Class = value
			case "Icon":
				info.Icon = value
			case "Paired":
				info.Paired = value == "yes"
			case "Bonded":
				info.Bonded = value == "yes"
			case "Trusted":
				info.Trusted = value == "yes"
			case "Blocked":
				info.Blocked = value == "yes"
			case "Connected":
				info.Connected = value == "yes"
			case "LegacyPairing":
				info.LegacyPairing = value == "yes"
			case "Modalias":
				info.Modalias = value
			case "Battery Percentage":
				if strings.Contains(value, "0x") {
					parts := strings.Split(value, "(")
					if len(parts) == 2 {
						percentage := strings.TrimRight(parts[1], ")")
						if p, err := strconv.Atoi(percentage); err == nil {
							info.BatteryPercentage = p
						}
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- err
			return
		}

		resultChan <- info
	}()

	if err := m.executeCommand("info " + address); err != nil {
		return nil, err
	}

	time.Sleep(100 * time.Millisecond)

	pw.Close()

	m.stdout = oldStdout

	select {
	case info := <-resultChan:
		return info, nil
	case err := <-errChan:
		return nil, err
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("timeout waiting for device info")
	}
}

func (m *BluetoothManager) RemoveDevice(address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pairingInProgress = false
	m.pairingKey = ""
	return m.executeCommand("remove " + address)
}
