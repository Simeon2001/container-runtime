package isolator

import (
	"context"
	"fmt"
	"github.com/Simeon2001/AlpineCell/isolator/utils"
	"github.com/Simeon2001/AlpineCell/message"
	"github.com/Simeon2001/AlpineCell/security"
	"golang.org/x/sys/unix"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

// SpawnContainer initializes and configures a container environment with namespaces, mounts, and networking settings.
// It handles communication between the parent and child processes through pipes and manages container lifecycle signals.
// The function also sets up security configurations, mounts namespaces, and prepares the container environment for execution.
func SpawnContainer() {

	// log.Println("[✅] Spawning container...")

	// Setup signal handling
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// communication pipe between parent and child
	childReader := os.NewFile(3, "pipe parent→child (read)")
	childWriter := os.NewFile(4, "pipe child→parent (write)")

	if childReader == nil {
		log.Fatal("child: FD 4 (parent→child) not available")
	}
	if childWriter == nil {
		log.Fatal("child: FD 5 (child→parent) not available")
	}

	// start child init for communication through pipe
	childInit := message.ChildInitialization(childWriter, childReader)

	if ok, caperr := security.ProcessHasEffectiveCaps(); caperr != nil {
		must("effectivecap error ", caperr)
	} else if !ok {
		// log.Println("process does not have effective capabilities")
		// Step 1: wait for "ready"
		_, cerr := childInit.WaitForParentMsg()
		if cerr != nil {
			must("WaitForParentMsg error: ", cerr)
		}

		// Step 2: send "ok"
		if err := childInit.SendHelloToParent(); err != nil {
			must("SendHelloToParent", err)
		}

		// step 4; wait for usernamespace mapping
		_, err := childInit.WaitForIDMappingMsgFromParent()
		if err != nil {
			must("WaitForIDMappingMsgFromParent", err)
		}

		// gain capabilities to run as root, set up network, mounts and seccomp
		if err = security.GainCapabilitiesWithDefaults(); err != nil {
			must("GainCapabilitiesWithDefaults Error", err)
		}
	}

	// Step 5: send back messaging to parent about mapping
	if err := childInit.SendIDMappingMsgFromChild(); err != nil {
		must("SendIDMappingMsgToParent", err)
	}

	// wait for container configuration
	getconfig, err := childInit.WaitForConfigFromParent()
	if err != nil {
		must("WaitForConfigFromParent", err)
	}

	var dns string
	if getconfig.Network {
		// Step 6: wait for network config
		networkConfig, err := childInit.WaitForParentNetworkConfig()
		if err != nil {
			must("WaitForParentNetworkConfig", err)
		}
		dns = networkConfig.DNS
	} else {
		dns = ""
	}

	// Step 7: send security config
	securityConfig, err := childInit.WaitForParentSeccompConfig()
	if err != nil {
		must("WaitForParentSeccompConfig", err)
	}

	// Close communication
	must("child read pipe close", childReader.Close())
	must("child write pipe close", childWriter.Close())

	// end for getting pipe ----------------------------

	pathsToCreate := []string{
		securityConfig.UpperPath,
		securityConfig.WorkPath,
		securityConfig.MergedPath,
	}

	// Loop through paths and create directories
	for _, path := range pathsToCreate {
		// Check if directory already exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// Directory doesn't exist, create it
			err := os.MkdirAll(path, 0755)
			if err != nil {
				fmt.Printf("Error creating directory %s: %v\n", path, err)
				continue
			}
		}
	}

	rootfs := securityConfig.MergedPath

	// create pathname for mounted or copied directory
	var mountedProjectDir string
	if getconfig.ContainerConfig.ContainerID != "" {
		mountedProjectDir = "MDIR-" + getconfig.ContainerConfig.ContainerID
	} else {
		must("containerID is null error; ", fmt.Errorf("containerID is null"))
	}

	bindDest := filepath.Join(rootfs, mountedProjectDir) // rootfs + mountedProjectDir

	// Handle signals in a goroutine
	go func() {
		<-ctx.Done()
		fmt.Println("\n[!] Received signal, cleaning up inside of the container...")
		utils.CleanupMounts()
		os.Exit(0)
	}()

	// Make mount namespace private
	must("namespace private mount error: ", unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""))

	// Clear all environment variables and set only what's needed
	os.Clearenv()

	// Copy host's resolv.conf to container
	hostResolv, _ := os.ReadFile("/etc/resolv.conf")
	conEtcPath := filepath.Join(rootfs, "etc")
	containerResolv := filepath.Join(conEtcPath, "resolv.conf")

	// Construct the overlay options string
	options := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s,userxattr", securityConfig.RootfsPath, securityConfig.UpperPath, securityConfig.WorkPath)
	must("overlay mount failed", unix.Mount("overlay", rootfs, "overlay", 0, options))

	if getconfig.Network {
		// Copy host resolv.conf to container
		must("cant copy host resolv to container; ", os.WriteFile(containerResolv, hostResolv, 0644))
	}

	// Mount the rootfs as a bind mount (required for pivot_root)
	must("bind mount of rootfs error: ", unix.Mount(rootfs, rootfs, "", unix.MS_BIND|unix.MS_REC, ""))

	// mounted proc, dev, sys and devicesnode
	// mounter(rootfs, mountedProjectDir)
	mounter(rootfs, dns)

	// Create a directory to hold the old root (inside the new root)
	putOld := filepath.Join(rootfs, ".pivot_old") //rootfs + "/.pivot_old"
	must("creating dir for old root error: ", os.MkdirAll(putOld, 0700))

	// Bind mount host folder
	must("create Bind mount host folder error: ", os.MkdirAll(bindDest, 0700))

	if getconfig.MountBool {
		must("bind mount the source to the target dest error: ", unix.Mount(getconfig.Mounts, bindDest, "", unix.MS_BIND, ""))
	}
	if getconfig.CopyMounts != "" {
		must("copy mounts error: ", utils.CopyDirectoryContents(getconfig.CopyMounts, bindDest))
	}

	// change to the new rootfs
	must("chdir error: ", os.Chdir(rootfs))

	// do the pivot_root
	must("pivot_root failed", unix.PivotRoot(".", ".pivot_old"))
	must("changing root dir gone wrong: ", unix.Chdir("/"))
	must("masked path failed: ", utils.MaskPaths())
	// must(" Failed to remount: ", MakeFilesystemsReadOnly())

	// Unmount the old root and remove the directory
	must("unmount old root failed: ", unix.Unmount("/.pivot_old", unix.MNT_DETACH))
	must("remove pivot_old dir failed: ", os.RemoveAll("/.pivot_old"))
	// Move to our mounted project folder
	must("chdir to where cwd dir are failed: ", os.Chdir(mountedProjectDir))

	// Set PATH environment for LookPath to work
	os.Setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")

	// Check for dependency files and set execution commands based on language
	var execCommand string
	var execArgs []string
	var installScript string

	if getconfig.Language != "" {
		var depFile string
		var depFileName string
		var hasDependencies bool

		switch getconfig.Language {
		case "python":
			execCommand = "python3"
			execArgs = append([]string{getconfig.Script}, getconfig.Args...)

			depFileName = "requirements.txt"
			depFile = filepath.Join(bindDest, depFileName)

			if _, err := os.Stat(depFile); err == nil {
				log.Printf("Found %s in project directory", depFileName)
				hasDependencies = true
			} else if os.IsNotExist(err) {
				log.Printf("No %s found in project directory", depFileName)
			} else {
				must(fmt.Sprintf("Error checking for %s: ", depFileName), err)
			}

			if hasDependencies {
				installScript = "pip3 install -r requirements.txt"
			}

		case "js":
			execCommand = "node"
			execArgs = append([]string{getconfig.Script}, getconfig.Args...)

			// Check for package.json
			depFileName = "package.json"
			depFile = filepath.Join(bindDest, depFileName)

			if _, err := os.Stat(depFile); err == nil {
				log.Printf("Found %s in project directory", depFileName)
				hasDependencies = true

				// Check for lock files to determine package manager
				yarnLock := filepath.Join(bindDest, "yarn.lock")
				if _, err := os.Stat(yarnLock); err == nil {
					log.Printf("Found yarn.lock in project directory")
					installScript = "yarn install"
				} else {
					installScript = "npm install"
				}
			} else if os.IsNotExist(err) {
				log.Printf("No %s found in project directory", depFileName)
			} else {
				must(fmt.Sprintf("Error checking for %s: ", depFileName), err)
			}

		case "go":
			execCommand = "go"
			execArgs = append([]string{"run", getconfig.Script}, getconfig.Args...)

			// Check for go.mod
			depFileName = "go.mod"
			depFile = filepath.Join(bindDest, depFileName)

			if _, err := os.Stat(depFile); err == nil {
				log.Printf("Found %s in project directory", depFileName)
				hasDependencies = true
				installScript = "go mod download"

				// Also check for go.sum
				sumFile := filepath.Join(bindDest, "go.sum")
				if _, err := os.Stat(sumFile); err == nil {
					log.Printf("Found go.sum in project directory")
				}
			} else if os.IsNotExist(err) {
				log.Printf("No %s found in project directory", depFileName)
			} else {
				must(fmt.Sprintf("Error checking for %s: ", depFileName), err)
			}
		}

		// Install dependencies if they exist
		if hasDependencies && installScript != "" {
			log.Printf("Installing dependencies...")

			var installCmd string
			var installArgs []string

			switch getconfig.Language {
			case "python":
				installCmd = "pip3"
				installArgs = []string{"install", "-r", "requirements.txt"}
			case "js":
				if strings.Contains(installScript, "yarn") {
					installCmd = "yarn"
					installArgs = []string{"install"}
				} else {
					installCmd = "npm"
					installArgs = []string{"install"}
				}
			case "go":
				installCmd = "go"
				installArgs = []string{"mod", "download"}
			}

			// Look up install command path
			installCmdPath, err := exec.LookPath(installCmd)
			must("install command path error: ", err)

			// Build install argv
			installArgv := append([]string{installCmdPath}, installArgs...)

			// Set working directory and environment for installation
			env := append(os.Environ(),
				"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
				"TERM=xterm",
				"HOME=/root",
				"container=otala-runc",
				"OLDPWD=/",
				"HOSTNAME=otala-runc",
				"SHLVL=0",
				fmt.Sprintf("PWD=%s", bindDest),
			)

			// Change to project directory
			must("chdir error: ", os.Chdir(bindDest))

			// Execute installation command
			cmd := exec.Command(installCmdPath, installArgv...)
			cmd.Dir = bindDest
			cmd.Env = env
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			err = cmd.Run()
			must("dependency installation error: ", err)

			log.Printf("Dependencies installed successfully")
		}
	} else {

		// Use Command if Language and Script are empty
		if getconfig.Command != "" {
			execCommand = getconfig.Command
			execArgs = getconfig.Args
		}

	}

	// Look up the command path
	cmdPath, err := exec.LookPath(execCommand)
	must("cmdpath error: ", err)

	// Build argv with the command and its arguments
	argv := append([]string{cmdPath}, execArgs...)

	// Build the command with the full path and args
	env := append(os.Environ(),
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"TERM=xterm",
		"HOME=/root",
		"container=otala-runc",
		"OLDPWD=/",
		"HOSTNAME=otala-runc",
		"SHLVL=0",
		fmt.Sprintf("PWD=%s", mountedProjectDir),
	)

	must("capabilities error: ", security.ApplyCapabilities(securityConfig.Capabilities))
	must("seccomp error: ", security.ApplySeccomp(securityConfig.Seccomp))

	must("command Exec error: ", unix.Exec(cmdPath, argv, env))

	// clean after execution
	utils.CleanupMounts()

}
