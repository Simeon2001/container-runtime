package security

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"log"
	"os"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/syndtr/gocapability/capability"
)

// dropUnlistedBoundingCaps drops all capabilities not in the allowed list from the kernel bounding set.
func dropUnlistedBoundingCaps(allowed []string) error {
	allowedSet := make(map[capability.Cap]bool)
	for _, name := range allowed {
		if capVal, ok := capabilityMap[name]; ok {
			allowedSet[capVal] = true
		}
	}

	// Iterate from cap 0 to the highest supported cap
	for capNum := capability.Cap(0); capNum <= capability.CAP_LAST_CAP; capNum++ {
		// Drop if not in allowed set
		if !allowedSet[capNum] {
			err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(capNum), 0, 0, 0)
			// Ignore invalid cap errors
			if err != nil && !errors.Is(err, unix.EINVAL) {
				return fmt.Errorf("failed to drop cap %d from bounding set: %v", capNum, err)
			}
		}
	}
	return nil
}

func ApplyCapabilities(caps Capabilities) error {

	// First, drop all unallowed capabilities from the kernel bounding set
	if err := dropUnlistedBoundingCaps(caps.Bounding); err != nil {
		return err
	}

	// Get current capabilities
	c, err := capability.NewPid2(0)
	if err != nil {
		return fmt.Errorf("failed to get capabilities: %v", err)
	}

	// Load current capabilities
	if err := c.Load(); err != nil {
		return fmt.Errorf("failed to load capabilities: %v", err)
	}

	// Clear all capabilities first
	c.Clear(capability.CAPS)

	// NOTE: Do NOT set bounding capabilities here - they've already been handled
	// by dropUnlistedBoundingCaps() above. The bounding set can only be reduced,
	// never expanded, so trying to set them here will be ignored.

	// Set effective capabilities
	for _, capName := range caps.Effective {
		if capEffect, ok := capabilityMap[capName]; ok {
			c.Set(capability.EFFECTIVE, capEffect)
		}
	}

	// Set permitted capabilities
	for _, capName := range caps.Permitted {
		if capPermit, ok := capabilityMap[capName]; ok {
			c.Set(capability.PERMITTED, capPermit)
		}
	}

	// Set ambient capabilities (if supported)
	for _, capName := range caps.Ambient {
		if capAmbt, ok := capabilityMap[capName]; ok {
			c.Set(capability.AMBIENT, capAmbt)
		}
	}

	// Apply the capabilities
	if err := c.Apply(capability.CAPS); err != nil {
		return fmt.Errorf("failed to apply capabilities: %v", err)
	}

	// fmt.Printf("Applied capabilities - Effective: %v\n", caps.Effective)
	return nil
}

// ProcessHasEffectiveCaps checks if the current process has any effective Linux capabilities.
func ProcessHasEffectiveCaps() (bool, error) {
	if runtime.GOOS != "linux" {
		return false, fmt.Errorf("capability checks are only supported on Linux")
	}

	// Prepare the header for the capget system call.
	header := unix.CapUserHeader{
		Version: unix.LINUX_CAPABILITY_VERSION_3,
		Pid:     int32(os.Getpid()),
	}

	var data unix.CapUserData

	if err := unix.Capget(&header, &data); err != nil {
		return false, fmt.Errorf("failed to get process capabilities: %w", err)
	}

	// It returns true if any effective capabilities are found, otherwise false.
	// log.Printf("Capabilities: %+v", data)
	return data.Effective != 0, nil
}

// ----------------------gain capabilities -----------------

type CapabilityManager struct {
	maxRetries    int
	backoffDelay  time.Duration
	backoffFactor int
}

// NewCapabilityManager creates a new capability manager with default settings
func NewCapabilityManager() *CapabilityManager {
	return &CapabilityManager{
		maxRetries:    10,
		backoffDelay:  10 * time.Millisecond,
		backoffFactor: 5, // Start backoff after 5 retries
	}
}

// ProcessInfo holds information about the current process and its namespace
type ProcessInfo struct {
	PID        int
	Namespace  uint64
	EnvVar     string
	RetryCount int
}

// GetProcessInfo retrieves current process information including namespace
func (cm *CapabilityManager) GetProcessInfo() (*ProcessInfo, error) {
	pid := os.Getpid()

	// Get PID namespace inode (unique identifier for the namespace)
	namespace, err := cm.getPIDNamespaceInode(pid)
	if err != nil {
		// log.Println("Failed to get PID namespace (negligible when unsharing pidns)")
		namespace = 0
	}

	// Create unique environment variable name
	envVar := fmt.Sprintf("_OTALARUNC_REXEC-COUNT_%d_%d", namespace, pid)

	// Get current retry count
	retryCount, err := cm.getRetryCount(envVar)
	if err != nil {
		return nil, fmt.Errorf("failed to parse retry count: %w", err)
	}

	return &ProcessInfo{
		PID:        pid,
		Namespace:  namespace,
		EnvVar:     envVar,
		RetryCount: retryCount,
	}, nil
}

// getPIDNamespaceInode extracts the inode number of a process's PID namespace
func (cm *CapabilityManager) getPIDNamespaceInode(pid int) (uint64, error) {
	namespacePath := fmt.Sprintf("/proc/%d/ns/pid", pid)

	stat, err := os.Stat(namespacePath)
	if err != nil {
		return 0, fmt.Errorf("failed to stat namespace file %s: %w", namespacePath, err)
	}

	// Extract system-specific stat information
	statSys, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("unexpected stat type %T, expected *syscall.Stat_t", stat.Sys())
	}

	return statSys.Ino, nil
}

// getRetryCount retrieves and parses the current retry count from environment
func (cm *CapabilityManager) getRetryCount(envVar string) (int, error) {
	envValue := os.Getenv(envVar)
	if envValue == "" {
		return 0, nil
	}

	count, err := strconv.Atoi(envValue)
	if err != nil {
		return 0, fmt.Errorf("invalid retry count %q in %s: %w", envValue, envVar, err)
	}

	return count, nil
}

// shouldApplyBackoff determines if backoff delay should be applied
func (cm *CapabilityManager) shouldApplyBackoff(retryCount int) bool {
	return retryCount > cm.backoffFactor
}

// calculateBackoffDelay calculates the backoff delay based on retry count
func (cm *CapabilityManager) calculateBackoffDelay(retryCount int) time.Duration {
	if !cm.shouldApplyBackoff(retryCount) {
		return 0
	}

	// Exponential backoff: delay * retryCount
	return cm.backoffDelay * time.Duration(retryCount)
}

// validateRetryCount checks if we've exceeded maximum retries
func (cm *CapabilityManager) validateRetryCount(info *ProcessInfo) error {
	if info.RetryCount > cm.maxRetries {
		return fmt.Errorf("failed to gain capabilities after %d retries (%s=%d)",
			cm.maxRetries, info.EnvVar, info.RetryCount)
	}
	return nil
}

// prepareReexecution sets up the environment for the next re-execution
func (cm *CapabilityManager) prepareReexecution(info *ProcessInfo) {
	newCount := info.RetryCount + 1

	// log.Printf("Preparing re-execution: %s: %d->%d\n",
	//info.EnvVar, info.RetryCount, newCount)

	_ = os.Setenv(info.EnvVar, strconv.Itoa(newCount))

}

// executeProcess replaces the current process with a new instance
func (cm *CapabilityManager) executeProcess() error {
	// Re-execute the current binary with the same arguments and environment
	err := syscall.Exec("/proc/self/exe", os.Args, os.Environ())
	if err != nil {
		return fmt.Errorf("failed to re-execute process: %w", err)
	}

	// This line should never be reached after successful exec
	panic("syscall.Exec returned unexpectedly - this should not happen")
}

// GainCapabilities is the main function that handles the complete re-execution process
func (cm *CapabilityManager) GainCapabilities() error {
	// Step 1: Get current process information
	info, err := cm.GetProcessInfo()
	if err != nil {
		return fmt.Errorf("failed to get process info: %w", err)
	}

	// log.Printf("Re-executing otala-runc child process (PID=%d, Namespace=%d) to gain capabilities",
	//	info.PID, info.Namespace)

	// Step 2: Validate retry count
	if err := cm.validateRetryCount(info); err != nil {
		return err
	}

	// Step 3: Apply backoff delay if needed
	if delay := cm.calculateBackoffDelay(info.RetryCount); delay > 0 {
		log.Printf("Applying backoff delay of %v (retry %d)", delay, info.RetryCount)
		time.Sleep(delay)
	}

	// Step 4: Prepare for re-execution
	cm.prepareReexecution(info)

	// Step 5: Re-execute the process
	return cm.executeProcess()
}

// GainCapabilitiesWithDefaults provides a simple interface using default settings
func GainCapabilitiesWithDefaults() error {
	manager := NewCapabilityManager()
	return manager.GainCapabilities()
}
