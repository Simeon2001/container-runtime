package namespace

import (
	runConfig "github.com/Simeon2001/AlpineCell/config"
	"log"
	"os"
	"syscall"
	"time"

	"github.com/Simeon2001/AlpineCell/systemd"
	"golang.org/x/sys/unix"
)

// clean is responsible for cleaning up resources and processes related to container execution.
// It removes container-related directories, reaps zombie processes, stops associated systemd units, and kills the main process.
// config specifies container runtime configuration, including paths to remove and cleanup behavior.
// pid represents the process ID of the container runtime process to terminate.
func clean(config *runConfig.RunConfig, pid int) {

	// Remove bind mount directory
	// Paths to delete
	if config.DeleteWhenDone {
		pathsToDelete := []string{config.ContainerConfig.ContainerConfigPath, config.ContainerConfig.ContainerPath}

		for _, path := range pathsToDelete {
			if err := os.RemoveAll(path); err != nil {
				log.Printf("[❌] Failed to delete %s: %v", path, err)
			} else {
				log.Printf("[✅] Successfully deleted %s", path)
			}
		}
	}

	log.Println("[✅] now reaping zombies...")

	// Small delay just to allow child exit cleanup if needed
	time.Sleep(2 * time.Second)

	// Reap zombies
	zombieCount := 0
	for {
		var ws unix.WaitStatus
		pid, err := unix.Wait4(-1, &ws, 0, nil)
		if err != nil {
			log.Printf("[❌] Reaper finished, collected %d zombies", zombieCount)
			break
		}
		zombieCount++
		log.Printf("[✅] Reaped zombie process with pid %d", pid)
	}

	containerName := "otalacon-" + config.ContainerConfig.ContainerID
	systemd.CleanSystemd(containerName)
	killer(pid)

}

// killer terminates a process by its process ID (pid) using the SIGKILL signal.
func killer(pid int) {
	// Find process
	process, err := os.FindProcess(pid)
	if err != nil {
		log.Printf("[❌] Error finding process: %v\n", err)
		return
	}
	// Send signal
	err = process.Signal(syscall.SIGKILL)
	if err != nil {
		log.Printf("[❌] Failed to send signal: %v\n", err)
		return
	}

	log.Printf("Sent %v to process %d\n", "SIGKILL", pid)

}
