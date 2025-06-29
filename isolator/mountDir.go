package isolator

import (
	"fmt"
	"github.com/Simeon2001/AlpineCell/isolator/utils"
	"golang.org/x/sys/unix"
	"log"
	"os"
	"path/filepath"
	"syscall"
)

// mounter mounts necessary directories and filesystems inside the container
func mounter(rootfs, dns string) {
	// Create necessary directories
	dirs := []string{
		"/dev", "/dev/pts", "/dev/mqueue", "/dev/shm",
		"/sys", "/sys/fs/cgroup", "/run", "/proc",
		"/proc/acpi", "/proc/scsi", "/tmp",
	}
	for _, dir := range dirs {
		devDir := filepath.Join(rootfs, dir)
		if err := os.MkdirAll(devDir, 0755); err != nil {
			log.Printf("Warning: Failed to create directory %s: %v", dir, err)
		}
	}

	// Mount filesystems
	conProc := filepath.Join(rootfs, "/proc")
	conDev := filepath.Join(rootfs, "/dev")
	conSys := filepath.Join(rootfs, "/sys")
	cgroupPath := filepath.Join(conSys, "/fs/cgroup")
	// must("proc mount failed: ", unix.Mount("proc", conProc, "proc", 0, ""))
	must("proc mount failed: ", unix.Mount("proc", conProc, "proc",
		unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC|unix.MS_RELATIME, ""))

	// santize the filesystems and set the ownership
	must("Failed to sanitize ownership: ", sanitizeFileOwnership(rootfs))

	// Try to enable ping for all users (but don't fail if it doesn't work)
	pingGroupRangePath := filepath.Join(conProc, "sys", "net", "ipv4", "ping_group_range")
	if err := os.WriteFile(pingGroupRangePath, []byte("0 0"), 0644); err != nil {
		// Expected to fail in rootless containers - that's OK
		fmt.Printf("Note: Could not set ping_group_range (expected in rootless): %v\n", err)
	}

	// remount filesystem to be readonly
	must(" Failed to remount: ", makeFilesystemsReadOnly(rootfs))
	must("proc ensure rw: ", unix.Mount("", conProc, "",
		unix.MS_REMOUNT|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC|unix.MS_RELATIME, ""))

	// Try mounting sysfs (works in rootful), else bind-mount /sys
	must("mount sysfs", unix.Mount("sysfs", conSys, "sysfs", unix.MS_NOSUID|unix.MS_NOEXEC|unix.MS_NODEV|unix.MS_RDONLY, ""))
	must("mount /sys/fs/cgroup", unix.Mount("cgroup2", cgroupPath, "cgroup2", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC|unix.MS_RDONLY, "nsdelegate,memory_recursiveprot"))
	must("mount /dev as tmpfs", unix.Mount("tmpfs", conDev, "tmpfs", unix.MS_NOSUID|syscall.MS_STRICTATIME, "mode=755,size=65536k"))
	for _, dir := range []string{"/dev/pts", "/dev/mqueue", "/dev/shm"} {
		deviceDir := filepath.Join(rootfs, dir)
		must("Failed to create device directory %s after mounting tmpfs: ", os.MkdirAll(deviceDir, 0755))
	}

	// More mounts...
	conPts := filepath.Join(rootfs, "/dev/pts")
	conMqueue := filepath.Join(rootfs, "/dev/mqueue")
	conShm := filepath.Join(rootfs, "/dev/shm")
	mountOptions := fmt.Sprintf("newinstance,ptmxmode=0666,mode=0620,gid=5")
	must("mount /dev/pts", unix.Mount("devpts", conPts, "devpts", unix.MS_NOSUID|unix.MS_NOEXEC, mountOptions))
	// Create symlink for /dev/ptmx
	ptmxPath := filepath.Join(rootfs, "/dev/ptmx")
	_ = os.Remove(ptmxPath) // Remove if exists
	must("create ptmx symlink", os.Symlink("pts/ptmx", ptmxPath))
	must("mount /dev/mqueue", unix.Mount("mqueue", conMqueue, "mqueue", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""))
	must("mount /dev/shm", unix.Mount("tmpfs", conShm, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "size=64000k"))

	// Create device nodes
	must("device mount error: ", utils.CreateDeviceNodesAndMount(rootfs))
	setupEtcFiles(rootfs, dns)

}

// sanitizeFileOwnership sanitizes file ownership in the rootfs
func sanitizeFileOwnership(rootfsPath string) error {
	// In rootless containers, we might be mapped to different UIDs
	currentUID := os.Getuid()
	currentGID := os.Getgid()

	// Fix ownership of key system paths
	systemPaths := []string{"/usr/share/applications"}

	for _, path := range systemPaths {
		// Check if path exists first
		realPath := filepath.Join(rootfsPath, path)
		if _, err := os.Stat(realPath); os.IsNotExist(err) {
			continue // Skip non-existent paths
		}

		err := filepath.Walk(realPath, func(walkPath string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors, continue walking
			}

			// Fix directories and files owned by nobody
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				// In rootless containers, fix files owned by nobody (65534)
				// or files that should be owned by the container root
				if stat.Uid == 65534 || stat.Gid == 65534 {
					// Change to the current user (which is mapped to root inside container)
					if err := os.Chown(walkPath, currentUID, currentGID); err != nil {
						// Don't fail on individual file errors
						fmt.Printf("Warning: couldn't chown %s: %v\n", walkPath, err)
					}
				}
			}
			return nil
		})

		if err != nil {
			fmt.Printf("Warning: failed to walk %s: %v\n", path, err)
		}
	}
	return nil
}

// makeFilesystemsReadOnly mounts all filesystems as read-only
func makeFilesystemsReadOnly(rootfs string) error {
	// List of filesystems to remount as read-only
	readOnlyFs := []string{
		"/proc/sys",
		"/proc/sysrq-trigger",
		"/proc/irq",
		"/proc/bus",
		"/proc/asound",
		"/proc/fs",
	}

	for _, fs := range readOnlyFs {
		fsPath := filepath.Join(rootfs, fs)
		_, err := os.Stat(fsPath)
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist")
		}
		if err != nil {
			return fmt.Errorf("stat %q: %w", fs, err)
		}

		if err := unix.Mount(fsPath, fsPath, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
			return fmt.Errorf("failed to bindmount %s as read-only: %w", fs, err)
		} else {
			if err := unix.Mount("", fsPath, "", unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC|unix.MS_RELATIME, ""); err != nil {
				return fmt.Errorf("warning: Failed to remount %s as read-only: %w", fs, err)
			}

		}
	}
	return nil
}

// setupEtcFiles sets up /etc files inside the container and networking inside the container
func setupEtcFiles(rootfs, dns string) {

	// set hostname to otala-runc
	must("set hostname", unix.Sethostname([]byte("otala-runc")))

	// 1) helper tmpfs mount point under rootfs
	baseTmp := filepath.Join(rootfs, "tmp", "tmpfs-etc")
	must("mkdir tmpfs-etc", os.MkdirAll(baseTmp, 0700))
	must("mount tmpfs for /etc files", unix.Mount("tmpfs", baseTmp, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "size=64k,mode=700"))

	// file names we need
	etcFiles := []string{"hosts", "hostname"}
	if dns != "" {
		etcFiles = append(etcFiles, "resolv.conf")
	}
	for _, name := range etcFiles {
		// path inside tmpfs
		tmpFile := filepath.Join(baseTmp, name)
		must("create "+tmpFile, func() error {
			f, err := os.Create(tmpFile)
			if err != nil {
				return err
			}
			return f.Close()
		}())

		// target path under rootfs
		target := filepath.Join(rootfs, "etc", name)
		// ensure the parent dir exists
		must("mkdir parent of "+target, os.MkdirAll(filepath.Dir(target), 0755))

		// bind-mount the tmpfs file over the real etc file
		must("bind-mount "+name, unix.Mount(tmpFile, target, "", unix.MS_PRIVATE|unix.MS_BIND, ""))
	}

	// 2) set hostname inside container
	must("set hostname", unix.Sethostname([]byte("otala-runc")))

	// 3) write default hosts, resolv.conf, and hostname files
	hostsPath := filepath.Join(rootfs, "etc", "hosts")
	hostsData := []byte("127.0.0.1 localhost\n127.0.0.1 otala-runc\n")
	must("write hosts", os.WriteFile(hostsPath, hostsData, 0644))

	if dns != "" {
		resolvPath := filepath.Join(rootfs, "etc", "resolv.conf")
		resolvData := []byte(
			"nameserver " + dns + "\n" +
				"nameserver 8.8.8.8\n" +
				"nameserver 1.1.1.1\n" +
				"nameserver 2001:4860:4860::8888\n" +
				"nameserver 2001:4860:4860::8844\n")

		must("write resolv.conf", os.WriteFile(resolvPath, resolvData, 0644))

	}

	// Write hostname file
	hostnamePath := filepath.Join(rootfs, "etc", "hostname")
	hostnameData := []byte("otala-runc\n")
	must("write hostname", os.WriteFile(hostnamePath, hostnameData, 0644))

}

// must is a helper to exit the program if an error occurs
func must(reply string, err error) {
	if err != nil {
		log.Printf("[❌] %s: %v", reply, err)
		os.Exit(1)
	}
}

//func listDirSimple(dirPath string) {
//	fmt.Printf("=== Contents of %s (Simple) ===\n", dirPath)
//
//	entries, err := os.ReadDir(dirPath)
//	if err != nil {
//		fmt.Printf("Error reading directory: %v\n", err)
//		return
//	}
//
//	if len(entries) == 0 {
//		fmt.Println("Directory is empty")
//		return
//	}
//
//	for _, entry := range entries {
//		if entry.IsDir() {
//			fmt.Printf("DIR:  %s/\n", entry.Name())
//		} else {
//			fmt.Printf("FILE: %s\n", entry.Name())
//		}
//	}
//	fmt.Println()
//}
