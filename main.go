package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/vishvananda/netlink"

	"github.com/usenocturne/nocturned/bluetooth"
	"github.com/usenocturne/nocturned/ota"
	"github.com/usenocturne/nocturned/utils"
	"github.com/usenocturne/nocturned/ws"
)

type InfoResponse struct {
	Version string `json:"version"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

func main() {
	wsHub := ws.NewWebSocketHub()

	btManager, err := bluetooth.NewBluetoothManager(wsHub)
	if err != nil {
		log.Fatal("Failed to initialize bluetooth manager:", err)
	}

	otaUpdater := ota.NewOTAUpdater(wsHub)

	// WebSockets
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Failed to upgrade connection: %v", err)
			return
		}
		wsHub.AddClient(conn)
	})

	// GET /info
	http.HandleFunc("/info", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		content, err := os.ReadFile("/etc/nocturne/version.txt")
		if err != nil {
			http.Error(w, "Error reading version file", http.StatusInternalServerError)
			return
		}

		response := InfoResponse{
			Version: string(content),
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Error encoding response", http.StatusInternalServerError)
			return
		}
	}))

	// POST /bluetooth/discover/on
	http.HandleFunc("/bluetooth/discover/on", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if err := btManager.SetDiscoverable(true); err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to enable discoverable mode: " + err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	}))

	// POST /bluetooth/discover/off
	http.HandleFunc("/bluetooth/discover/off", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := btManager.SetDiscoverable(false); err != nil {
			http.Error(w, "Failed to disable discoverable mode", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	// POST /bluetooth/pairing/accept
	http.HandleFunc("/bluetooth/pairing/accept", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := btManager.AcceptPairing(); err != nil {
			http.Error(w, "Failed to accept pairing", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	// POST /bluetooth/pairing/deny
	http.HandleFunc("/bluetooth/pairing/deny", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := btManager.DenyPairing(); err != nil {
			http.Error(w, "Failed to deny pairing", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	// GET /bluetooth/info/{address}
	http.HandleFunc("/bluetooth/info/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		address := strings.TrimPrefix(r.URL.Path, "/bluetooth/info/")
		if address == "" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Bluetooth address is required"})
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		info, err := btManager.GetDeviceInfo(address)
		if err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to get device info: " + err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if err := json.NewEncoder(w).Encode(info); err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Error encoding response: " + err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))

	// POST /bluetooth/remove/{address}
	http.HandleFunc("/bluetooth/remove/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		address := strings.TrimPrefix(r.URL.Path, "/bluetooth/remove/")
		if address == "" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Bluetooth address is required"})
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := btManager.RemoveDevice(address); err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to remove device: " + err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	// POST /bluetooth/connect/{address}
	http.HandleFunc("/bluetooth/connect/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		address := strings.TrimPrefix(r.URL.Path, "/bluetooth/connect/")
		if address == "" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Bluetooth address is required"})
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := btManager.ConnectDevice(address); err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to connect to device: " + err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		w.WriteHeader(http.StatusOK)
	}))

	// POST /bluetooth/disconnect/{address}
	http.HandleFunc("/bluetooth/disconnect/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		address := strings.TrimPrefix(r.URL.Path, "/bluetooth/disconnect/")
		if address == "" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Bluetooth address is required"})
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := btManager.DisconnectDevice(address); err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to disconnect device: " + err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		w.WriteHeader(http.StatusOK)
	}))

	// GET /bluetooth/network
	http.HandleFunc("/bluetooth/network", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		link, err := netlink.LinkByName("bnep0")
		if err != nil || link.Attrs().Flags&net.FlagUp == 0 {
			json.NewEncoder(w).Encode(map[string]string{"status": "down"})
			w.WriteHeader(http.StatusOK)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "up"})
		w.WriteHeader(http.StatusOK)
	}))

	// POST /bluetooth/network/{address}
	http.HandleFunc("/bluetooth/network/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		address := strings.TrimPrefix(r.URL.Path, "/bluetooth/network/")
		if address == "" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Bluetooth address is required"})
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := btManager.ConnectNetwork(address); err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to connect to Bluetooth network: " + err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	}))

	// GET /bluetooth/devices
	http.HandleFunc("/bluetooth/devices", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		devices, err := btManager.GetDevices()
		if err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to get devices: " + err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if devices == nil {
			devices = []utils.BluetoothDeviceInfo{}
		}

		if err := json.NewEncoder(w).Encode(devices); err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Error encoding response: " + err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))

	// POST /ota/download
	http.HandleFunc("/ota/download", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var requestData struct {
			URL string `json:"url"`
		}

		if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid request body"})
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		go func() {
			if err := otaUpdater.Download(requestData.URL); err != nil {
				wsHub.Broadcast(utils.WebSocketEvent{
					Type:    "ota/download/error",
					Payload: err.Error(),
				})
			}
		}()

		w.WriteHeader(http.StatusAccepted)
	}))

	// POST /ota/deploy
	http.HandleFunc("/ota/deploy", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed"})
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if err := otaUpdater.Deploy(); err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{Error: "OTA update failed: " + err.Error()})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}

	log.Printf("Server starting on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}