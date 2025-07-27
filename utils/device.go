package utils

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	brightnessPath     = "/sys/class/backlight/aml-bl/brightness"
	brightnessSavePath = "/data/etc/nocturne/brightness.txt"
	maxBrightness      = 255
	minBrightness      = 1
)

func SetBrightness(value int) error {
	if value < minBrightness || value > maxBrightness {
		return fmt.Errorf("brightness value must be between %d and %d", minBrightness, maxBrightness)
	}

	err := os.WriteFile(brightnessPath, []byte(fmt.Sprintf("%d", value)), 0644)
	if err != nil {
		return fmt.Errorf("failed to write brightness: %w", err)
	}

	err = os.WriteFile(brightnessSavePath, []byte(fmt.Sprintf("%d", value)), 0644)
	if err != nil {
		fmt.Printf("Warning: failed to save brightness value: %v\n", err)
	}

	return nil
}

func GetBrightness() (int, error) {
	data, err := os.ReadFile(brightnessPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read brightness: %w", err)
	}

	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("failed to parse brightness value: %w", err)
	}

	return value, nil
}

func InitBrightness() error {
	data, err := os.ReadFile(brightnessSavePath)
	if err != nil {
		return nil
	}

	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("failed to parse saved brightness value: %w", err)
	}

	return SetBrightness(value)
}

func ResetCounter() error {
	commands := []struct {
		name string
		args []string
	}{
		{"wingman", []string{"ab", "--boot-result", "1"}},
	}

	for _, cmd := range commands {
		if output, err := ExecuteCommand(cmd.name, cmd.args...); err != nil {
			return fmt.Errorf("failed to execute command '%s': %v (output: %s)", cmd.name, err, output)
		}
	}

	return nil
}

func FactoryReset() error {
	commands := []struct {
		name string
		args []string
	}{
		{"uenv", []string{"set", "firstboot", "1"}},
		{"sync", nil},
		{"shutdown", []string{"-r", "now"}},
	}

	for _, cmd := range commands {
		if output, err := ExecuteCommand(cmd.name, cmd.args...); err != nil {
			return fmt.Errorf("failed to execute command '%s': %v (output: %s)", cmd.name, err, output)
		}
	}

	return nil
}

func Shutdown() error {
	commands := []struct {
		name string
		args []string
	}{
		{"halt", nil},
	}

	for _, cmd := range commands {
		if output, err := ExecuteCommand(cmd.name, cmd.args...); err != nil {
			return fmt.Errorf("failed to execute command '%s': %v (output: %s)", cmd.name, err, output)
		}
	}

	return nil
}

func Reboot() error {
	commands := []struct {
		name string
		args []string
	}{
		{"reboot", nil},
	}

	for _, cmd := range commands {
		if output, err := ExecuteCommand(cmd.name, cmd.args...); err != nil {
			return fmt.Errorf("failed to execute command '%s': %v (output: %s)", cmd.name, err, output)
		}
	}

	return nil
}

func SetTimezone(timezone string) error {
	src := fmt.Sprintf("/usr/share/zoneinfo/%s", timezone)
	dst := "/var/local/etc/localtime"

	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing localtime: %w", err)
	}

	if err := os.Symlink(src, dst); err != nil {
		return fmt.Errorf("failed to create symlink for timezone: %w", err)
	}

	return nil
}
