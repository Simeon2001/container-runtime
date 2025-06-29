package utils

import (
	"bufio"
	"golang.org/x/sys/unix"
	"log"
	"os"
	"sort"
	"strings"
)

// CleanupMounts unmounts all filesystems that were mounted inside the container
func CleanupMounts() {
	mnts, err := listMounts()
	if err != nil {
		log.Printf("failed listMounts: %v", err)
		return
	}

	// collect mounts under "/" ignoring root itself
	var subs []string
	for _, m := range mnts {
		if m != "/" && strings.HasPrefix(m, "/") {
			subs = append(subs, m)
		}
	}
	sort.Slice(subs, func(i, j int) bool {
		return len(subs[i]) > len(subs[j])
	})
	for _, path := range subs {
		if err := unix.Unmount(path, unix.MNT_DETACH); err != nil {
			log.Printf("warn unmount %s: %v", path, err)
		} else {
			log.Printf("unmounted %s", path)
		}
	}
}

// listMounts returns all mount points from /proc/self/mountinfo
func listMounts() ([]string, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var mounts []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 5 {
			mounts = append(mounts, fields[4])
		}
	}
	return mounts, scanner.Err()
}
