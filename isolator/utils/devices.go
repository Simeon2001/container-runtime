package utils

import (
	"fmt"
	"golang.org/x/sys/unix"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func CreateDeviceNodesAndMount(rootfs string) error {

	deviceList := [7]string{"null", "zero", "full", "random", "urandom", "tty", "console"}

	// Ensure /dev directory exists in container
	devDir := filepath.Join(rootfs, "dev")
	if err := os.MkdirAll(devDir, 0755); err != nil {
		return fmt.Errorf("failed to create dev directory: %w", err)
	}

	// Create each device node
	for _, device := range deviceList {
		hostDevicepath := filepath.Join("/dev", device)
		containerDevicepath := filepath.Join(rootfs, "dev", device)

		// Check if host device exists
		hostStat, err := os.Stat(hostDevicepath)
		if err != nil {
			log.Printf("Warning: host device %s doesn't exist, skipping: %v", hostDevicepath, err)
			continue
		}

		// Check if container device path already exists and remove it
		if _, err := os.Stat(containerDevicepath); err == nil {
			if err := os.Remove(containerDevicepath); err != nil {
				return fmt.Errorf("failed to remove existing %s: %w", containerDevicepath, err)
			}
		}

		// Create the target file with same type as source
		if hostStat.Mode().IsRegular() || hostStat.Mode()&os.ModeCharDevice != 0 {
			file, err := os.Create(containerDevicepath)
			if err != nil {
				return fmt.Errorf("failed to create target file %s: %w", containerDevicepath, err)
			}
			file.Close()
		} else if hostStat.IsDir() {
			if err := os.MkdirAll(containerDevicepath, hostStat.Mode().Perm()); err != nil {
				return fmt.Errorf("failed to create target directory %s: %w", containerDevicepath, err)
			}
		}

		// Perform bind mount
		if err := unix.Mount(hostDevicepath, containerDevicepath, "", unix.MS_BIND, ""); err != nil {
			return fmt.Errorf("bind mount failed: %v -> %v: %w (errno: %d)",
				hostDevicepath, containerDevicepath, err, err.(syscall.Errno))
		}
	}

	if err := createDevSymlinks(rootfs); err != nil {
		return err
	}

	return nil
}

// symlink for stdin, stdout, stderr in /dev
func createDevSymlinks(rootfs string) error {
	dev := filepath.Join(rootfs, "dev")

	links := []struct {
		linkName string
		target   string
	}{
		{"stdin", "/proc/self/fd/0"},
		{"stdout", "/proc/self/fd/1"},
		{"stderr", "/proc/self/fd/2"},
		{"core", "/proc/kcore"},
		{"fd", "/proc/self/fd/"},
	}

	for _, l := range links {
		fullLinkPath := filepath.Join(dev, l.linkName)

		// Remove existing symlink/file if any (but NOT the directory)
		if info, err := os.Lstat(fullLinkPath); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || info.Mode().IsRegular() {
				if err := os.Remove(fullLinkPath); err != nil {
					log.Printf("Warning: failed to remove existing %s: %v", fullLinkPath, err)
					continue
				}
			}
		}

		// Create the symlink
		if err := os.Symlink(l.target, fullLinkPath); err != nil {
			return fmt.Errorf("failed to create symlink %s -> %s: %w", fullLinkPath, l.target, err)
		}
	}

	return nil
}

// MaskPaths masks paths that are not allowed to be accessed by the container
func MaskPaths() error {
	var maskedPaths = []string{
		"/proc/acpi",
		"/proc/kcore",
		"/proc/keys",
		"/proc/latency_stats",
		"/proc/sched_debug",
		"/proc/scsi",
		"/proc/timer_list",
		"/proc/timer_stats",
		"/sys/devices/virtual/powercap",
		"/sys/firmware",
		"/sys/fs/selinux",
		"/proc/interrupts",
	}

	// Dynamically add CPU thermal throttle paths
	cpuPaths, err := getCPUThermalThrottlePaths()
	if err != nil {
		return fmt.Errorf("failed to get CPU thermal throttle paths: %w", err)
	}
	maskedPaths = append(maskedPaths, cpuPaths...)

	for _, p := range maskedPaths {
		fi, err := os.Stat(p)
		if os.IsNotExist(err) {
			continue // only mask existing paths
		}
		if err != nil {
			return fmt.Errorf("stat %q: %w", p, err)
		}

		if fi.IsDir() {
			if err := unix.Mount(
				"tmpfs", p, "tmpfs",
				unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC,
				"mode=755,size=0",
			); err != nil {
				return fmt.Errorf("mount tmpfs on %q: %w", p, err)
			}
		} else {
			if err := unix.Mount(
				"/dev/null", p, "",
				unix.MS_BIND|unix.MS_REC, "",
			); err != nil {
				return fmt.Errorf("bind-mount /dev/null on %q: %w", p, err)
			}

		}
	}
	return nil
}

// getCPUThermalThrottlePaths dynamically discovers CPU thermal throttle paths
func getCPUThermalThrottlePaths() ([]string, error) {
	var paths []string

	// Look for CPU directories in /sys/devices/system/cpu/
	cpuDir := "/sys/devices/system/cpu"
	entries, err := os.ReadDir(cpuDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read CPU directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if directory name matches cpu[0-9]+ pattern
		name := entry.Name()
		if strings.HasPrefix(name, "cpu") && len(name) > 3 {
			// Extract CPU number
			cpuNumStr := name[3:]
			if _, err := strconv.Atoi(cpuNumStr); err == nil {
				// Valid CPU number, check if thermal_throttle exists
				thermalPath := filepath.Join(cpuDir, name, "thermal_throttle")
				if _, err := os.Stat(thermalPath); err == nil {
					paths = append(paths, thermalPath)
				}
			}
		}
	}

	return paths, nil
}

// SetupConsole mounts the console device and sets up the console options
//func SetupConsole(rootfs string) error {
//	consolePath := filepath.Join(rootfs, "/dev/console")
//
//	// Create console file if it doesn't exist
//	if _, err := os.Stat(consolePath); os.IsNotExist(err) {
//		file, err := os.Create(consolePath)
//		if err != nil {
//			return fmt.Errorf("failed to create console: %w", err)
//		}
//		file.Close()
//	}
//
//	// Mount console as devpts with special options
//	// gid=100004 might be your tty group ID, adjust as needed
//	consoleOpts := fmt.Sprintf("ptmxmode=0666,mode=0620,gid=100004")
//	// consoleOpts := "gid=100004,mode=620,ptmxmode=666"
//
//	if err := unix.Mount("devpts", consolePath, "devpts",
//		unix.MS_RELATIME, consoleOpts); err != nil {
//		// Fallback to bind mount if devpts fails
//		log.Printf("devpts mount failed for console, falling back to bind mount: %v", err)
//		hostConsolePath := "/dev/console"
//		if err := unix.Mount(hostConsolePath, consolePath, "", unix.MS_BIND, ""); err != nil {
//			return fmt.Errorf("console bind mount failed: %w", err)
//		}
//		// Add noexec flag
//		if err := unix.Mount("", consolePath, "",
//			unix.MS_REMOUNT|unix.MS_NOSUID|unix.MS_NOEXEC, ""); err != nil {
//			return fmt.Errorf("console remount failed: %w", err)
//		}
//	}
//
//	log.Printf("[+] Setup console device")
//	return nil
//}
