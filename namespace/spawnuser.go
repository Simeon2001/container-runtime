package namespace

import (
	"encoding/json"
	runConfig "github.com/Simeon2001/AlpineCell/config"
	"github.com/Simeon2001/AlpineCell/security"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/Simeon2001/AlpineCell/message"
	network "github.com/Simeon2001/AlpineCell/nework"
	"golang.org/x/sys/unix"
)

// Stage1UserNS sets up and initializes a new user namespace and associated namespaces for the container process.
// It creates inter-process communication pipes, handles namespace mappings, initializes networking, and seccomp settings.
// The function also manages lifecycle signals for cleanup and ensures cleanup is performed after process termination.
func Stage1UserNS(initConfig *runConfig.RunConfig, configData *[]byte) {

	var secconfig security.Config
	if err := json.Unmarshal(*configData, &secconfig); err != nil {
		must("unmarshal seccomp config err: ", err)
	}

	// Pipe #1: parent → child
	parentRead, parentWrite, err := os.Pipe()
	must("pipe parent→child", err)

	// Pipe #2: child → parent
	childRead, childWrite, err := os.Pipe()
	must("pipe child→parent", err)

	// Configure signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	var processID int

	cmd := exec.Command("/proc/self/exe", "child")

	// Pass the read-end of (parent→child) and the write-end of (child→parent) to the child
	// They will appear as FD 4 and FD 5 in the child process
	cmd.ExtraFiles = []*os.File{parentRead, childWrite}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set up syscall attributes for the new process with all namespaces at once
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// syscall.CLONE_NEWUTS which mean new hostname
		// syscall.CLONE_NEWPID which mean new processid
		// syscall.CLONE_NEWNET which mean new network stack
		// syscall.CLONE_NEWNS which mean new mount filesystem
		// for /sys to mount make sure you use NET namespace
		// for cgroup to mount make sure you use CGROUP namespace
		Cloneflags: unix.CLONE_NEWUSER | unix.CLONE_NEWNS | unix.CLONE_NEWUTS | unix.CLONE_NEWIPC | unix.CLONE_NEWPID | unix.CLONE_NEWCGROUP | unix.CLONE_NEWNET,
	}

	must("executing child process failed", cmd.Start())

	// close this pipe
	must("close parentRead (unused in parent)", parentRead.Close())
	must("close childWrite (unused in parent)", childWrite.Close())

	// -----starting parent-child messaging-----------------
	parentInfo := message.ParentInitialization(parentWrite, childRead)

	// Step 1: send "ready"
	if err = parentInfo.SendHelloToChild(); err != nil {
		must("SendHelloToChild", err)
	}

	// Step 2: wait for "ok"
	ok, err := parentInfo.WaitForChildMsg()
	if err != nil {
		must("WaitForChildMsg", err)
	}

	if ok {

		processID = cmd.Process.Pid

		// uidguid mapping for user namespace
		err = SetupUserNamespaceMapping(processID)
		if err != nil {
			must("SetupUserNamespaceMapping Error", err)
		}
		if err = parentInfo.SendIDMappingMsgAndConfig(initConfig); err != nil {
			must("SendIDMappingMsg", err)
		}

		if err = parentInfo.WaitForIDMappingMsg(); err != nil {
			must("WaitForIDMappingMsg", err)
		}

		if err = parentInfo.SendContainerConfig(*initConfig); err != nil {
			must("SendContainerConfig", err)
		}

		if initConfig.Network {
			// Start networking
			var netParams *network.NetParams
			netParams, err = network.Config(processID)
			if err != nil {
				must("network.Config", err)
			}

			if err = parentInfo.SendParentNetworkInit(*netParams); err != nil {
				must("SendParentNetworkInit", err)
			}

		}

		// send seccomp config to child
		if err = parentInfo.SendParentSeccompConfig(secconfig); err != nil {
			must("SendSeccompConfig", err)
		}
		// Close pipes after use
		must("close parentWrite", parentWrite.Close())
		must("close childRead", childRead.Close())
	}

	// Set up a goroutine to handle termination signals
	go func(pid int, sigChan chan os.Signal, initConfig *runConfig.RunConfig) {
		sig := <-sigChan
		log.Printf("[⚠️] Received signal %v. Shutting down container...", sig)
		clean(initConfig, pid)
		log.Println("[✅] Cleanup complete")
		// Wait a moment for cleanup to complete
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}(processID, sigChan, initConfig)

	// Wait for the child process to complete
	err = cmd.Wait()
	if err != nil {
		log.Printf("[❌] Child process exited with error: %v", err)
	} else {
		log.Println("[✅] Container exited successfully")
	}

	clean(initConfig, processID)
	log.Println("[✅] All resources cleaned up")

}

func must(reply string, err interface{}) {
	if err != nil {
		log.Printf("[❌] %s: %v", reply, err)
		os.Exit(1)
	}
}
