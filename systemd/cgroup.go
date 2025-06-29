package systemd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// findCgroupPath tries to locate the cgroup path for our scope unit
func findCgroupPath(containerName string) (string, error) {
	// First try the unified hierarchy (cgroup v2)
	// In systemd environments, user scopes may be under /sys/fs/cgroup/user.slice/
	possiblePaths := []string{
		// cgroup v2 paths
		fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service/%s.scope", os.Getuid(), os.Getuid(), containerName),
		fmt.Sprintf("/sys/fs/cgroup/%s.scope", containerName),
		// If using session scope
		fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/session-*.scope/%s.scope", os.Getuid(), containerName),
	}

	for _, path := range possiblePaths {
		// Handle wildcards in path
		if strings.Contains(path, "*") {
			matches, err := filepath.Glob(path)
			if err == nil && len(matches) > 0 {
				// Use the first match
				return matches[0], nil
			}
		} else if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// If we couldn't find it in the common places, try searching
	return findCgroupPathBySearching(containerName)
}

// findCgroupPathBySearching is a fallback that searches for the cgroup
func findCgroupPathBySearching(containerName string) (string, error) {
	cmd := exec.Command("find", "/sys/fs/cgroup", "-name", containerName+".scope", "-type", "d")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to search for cgroup: %v", err)
	}

	if len(output) == 0 {
		return "", fmt.Errorf("cgroup not found")
	}

	// Use the first result
	path := strings.TrimSpace(string(output))
	parts := strings.Split(path, "\n")
	return parts[0], nil
}

// readMemoryMax reads the memory.max file in the given cgroup path
func readMemoryMax(cgroupPath string) (string, error) {
	content, err := os.ReadFile(filepath.Join(cgroupPath, "memory.max"))
	if err != nil {
		// Try legacy cgroup v1 path
		content, err = os.ReadFile(filepath.Join(cgroupPath, "memory.limit_in_bytes"))
		if err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(string(content)), nil
}
