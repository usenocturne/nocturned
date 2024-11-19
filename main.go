package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/usenocturne/nocturned/bluetooth"
)

type InfoResponse struct {
	Version string `json:"version"`
}

type ErrorResponse struct {
	Error string `json:"error"`
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
	var (
		btManager *bluetooth.BluetoothManager
		err error
	)

	btManager, err = bluetooth.NewBluetoothManager()
	if err != nil {
		log.Fatal("Failed to initialize bluetooth manager:", err)
	}

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

	// GET /bluetooth/pairing/inProgress
	http.HandleFunc("/bluetooth/pairing/inProgress", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		json.NewEncoder(w).Encode(map[string]bool{"inProgress": btManager.IsPairingInProgress()})
	}))

	// GET /bluetooth/pairing/pairingKey
	http.HandleFunc("/bluetooth/pairing/pairingKey", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"key": btManager.GetPairingKey()})
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
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		address := strings.TrimPrefix(r.URL.Path, "/bluetooth/remove/")
		if address == "" {
			http.Error(w, "Bluetooth address is required", http.StatusBadRequest)
			return
		}

		if err := btManager.RemoveDevice(address); err != nil {
			http.Error(w, "Failed to remove device", http.StatusInternalServerError)
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