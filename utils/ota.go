package utils

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

type UpdateRequest struct {
	ImageURL string `json:"image_url"`
	SumURL   string `json:"sum_url"`
}

type UpdateStatus struct {
	InProgress bool   `json:"in_progress"`
	Stage      string `json:"stage,omitempty"`
	Error      string `json:"error,omitempty"`
}

type ProgressMessage struct {
	Type          string  `json:"type"`
	Stage         string  `json:"stage"`
	BytesComplete int64   `json:"bytes_complete"`
	BytesTotal    int64   `json:"bytes_total"`
	Speed         float64 `json:"speed"`
	Percent       float64 `json:"percent"`
}

type CompletionMessage struct {
	Type    string `json:"type"`
	Stage   string `json:"stage"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type OKResponse struct {
	Status string `json:"status"`
}

var (
	updateStatusMutex sync.Mutex
	currentStatus     UpdateStatus
	rootPartitionA    = "/dev/system_a"
	rootPartitionB    = "/dev/system_b"
)

type progressReader struct {
	reader       io.Reader
	total        int64
	read         int64
	lastUpdate   time.Time
	lastBytes    int64
	onProgress   func(int64, int64, float64)
	updatePeriod time.Duration
}

func NewProgressReader(reader io.Reader, total int64, onProgress func(int64, int64, float64)) *progressReader {
	return &progressReader{
		reader:       reader,
		total:        total,
		onProgress:   onProgress,
		lastUpdate:   time.Now(),
		updatePeriod: time.Second / 4,
	}
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.read += int64(n)
		now := time.Now()
		if now.Sub(pr.lastUpdate) >= pr.updatePeriod {
			elapsed := now.Sub(pr.lastUpdate).Seconds()
			speed := float64(pr.read-pr.lastBytes) / elapsed / 1024 / 1024
			pr.onProgress(pr.read, pr.total, speed)
			pr.lastUpdate = now
			pr.lastBytes = pr.read
		}
	}
	return n, err
}

func ExecuteCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	o, err := cmd.CombinedOutput()
	return o, err
}

func SetUpdateStatus(inProgress bool, stage string, errorMsg string) {
	updateStatusMutex.Lock()
	defer updateStatusMutex.Unlock()

	currentStatus = UpdateStatus{
		InProgress: inProgress,
		Stage:      stage,
		Error:      errorMsg,
	}
}

func GetUpdateStatus() UpdateStatus {
	updateStatusMutex.Lock()
	defer updateStatusMutex.Unlock()

	return currentStatus
}

func UpdateSystem(image string, sum string, onProgress func(ProgressMessage)) error {
	imgFile, err := os.Open(image)
	if err != nil {
		return fmt.Errorf("failed to open image: %w", err)
	}
	defer imgFile.Close()

	imgSha := sha256.New()
	if _, err := io.Copy(imgSha, imgFile); err != nil {
		return fmt.Errorf("failed to get sha256sum of image: %w", err)
	}

	s := fmt.Sprintf("%x", imgSha.Sum(nil))
	if s != sum {
		return fmt.Errorf("provided sum does not match: %s", s)
	}

	if _, err := imgFile.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek image file: %w", err)
	}

	output, err := ExecuteCommand("wingman", "ab", "--json")
	if err != nil {
		return fmt.Errorf("failed to execute wingman: %w", err)
	}

	type JSONOutput struct {
		ActiveSlot       int         `json:"active_slot"`
		ActiveSlotLetter string      `json:"active_slot_letter"`
		VersionMajor     uint8       `json:"version_major"`
		VersionMinor     uint8       `json:"version_minor"`
		Slots            [2]struct{} `json:"slots"`
		CRC32            uint32      `json:"crc32"`
	}

	var abInfo JSONOutput
	if err := json.Unmarshal(output, &abInfo); err != nil {
		return fmt.Errorf("failed to parse wingman ab output: %w", err)
	}

	// A=0, B=1
	active := abInfo.ActiveSlot
	rootPart := rootPartitionA
	if active == 1 {
		rootPart = rootPartitionB
	}

	inDecompress, err := gzip.NewReader(imgFile)
	if err != nil {
		return fmt.Errorf("failed to decompress image file: %w", err)
	}
	defer inDecompress.Close()

	tempFile, err := os.CreateTemp("", "uncompressed-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, inDecompress); err != nil {
		return fmt.Errorf("failed to decompress image: %w", err)
	}

	uncompressedSize, err := tempFile.Seek(0, 2)
	if err != nil {
		return fmt.Errorf("failed to get uncompressed size: %w", err)
	}

	if _, err := imgFile.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek image file: %w", err)
	}

	inDecompress, err = gzip.NewReader(imgFile)
	if err != nil {
		return fmt.Errorf("failed to decompress image file: %w", err)
	}
	defer inDecompress.Close()

	out, err := os.OpenFile(rootPart, os.O_WRONLY|os.O_TRUNC|os.O_SYNC, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to open flash device: %w", err)
	}
	defer out.Close()

	progressReader := NewProgressReader(inDecompress, uncompressedSize, func(complete, total int64, speed float64) {
		percent := float64(complete) / float64(total) * 100
		onProgress(ProgressMessage{
			Type:          "progress",
			Stage:         "flash",
			BytesComplete: complete,
			BytesTotal:    total,
			Speed:         float64(int(speed*10)) / 10,
			Percent:       float64(int(percent*10)) / 10,
		})
	})

	_, err = io.Copy(out, progressReader)
	if err != nil {
		return fmt.Errorf("failed to copy image: %w", err)
	}

	if active == 0 {
		_, err := ExecuteCommand("wingman", "ab", "--slot", "1")
		return err
	} else {
		_, err := ExecuteCommand("wingman", "ab", "--slot", "0")
		return err
	}
}
