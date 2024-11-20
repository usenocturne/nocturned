package ota

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/usenocturne/nocturned/utils"
	"github.com/usenocturne/nocturned/ws"
)

type OTAUpdater struct {
	wsHub *ws.WebSocketHub
}

func NewOTAUpdater(wsHub *ws.WebSocketHub) *OTAUpdater {
	return &OTAUpdater{wsHub: wsHub}
}

func (o *OTAUpdater) Download(url string) error {
	if err := os.Remove("/tmp/update.zip"); err != nil && !os.IsNotExist(err) {
		return err
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create("/tmp/update.zip")
	if err != nil {
		return err
	}
	defer out.Close()

	totalBytes := resp.ContentLength
	var downloadedBytes int64
	percentReported := int64(-1)

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			downloadedBytes += int64(n)
			out.Write(buf[:n])

			percentComplete := downloadedBytes * 100 / totalBytes
			if percentComplete > percentReported {
				percentReported = percentComplete
				o.wsHub.Broadcast(utils.WebSocketEvent{
					Type: "ota/download/progress",
					Payload: map[string]interface{}{
						"downloaded": downloadedBytes,
						"total":      totalBytes,
						"percent":    percentComplete,
					},
				})
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	return nil
}

func (o *OTAUpdater) Deploy() error {
	if err := os.RemoveAll("/tmp/update"); err != nil && !os.IsNotExist(err) {
		return err
	}

	if err := unzip("/tmp/update.zip", "/tmp/update"); err != nil {
		return err
	}

	setCmd := exec.Command("nix-env", "-p", "/nix/var/nix/profiles/system", "--set", "/tmp/update")
	setCmd.Stdout = os.Stdout
	setCmd.Stderr = os.Stderr
	if err := setCmd.Run(); err != nil {
		return err
	}

	switchCmd := exec.Command("/nix/var/nix/profiles/system/bin/switch-to-configuration", "switch")
	switchCmd.Stdout = os.Stdout
	switchCmd.Stderr = os.Stderr
	return switchCmd.Run()
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, rc)

		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}
