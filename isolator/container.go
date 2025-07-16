package isolator

import (
	"fmt"
	"github.com/Simeon2001/AlpineCell/isolator/utils"
	"github.com/Simeon2001/AlpineCell/message"
	"github.com/Simeon2001/AlpineCell/security"
	"golang.org/x/sys/unix"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SpawnContainer initializes and configures a container environment with namespaces, mounts, and networking settings.
// It handles communication between the parent and child processes through pipes and manages container lifecycle signals.
// The function also sets up security configurations, mounts namespaces, and prepares the container environment for execution.
func SpawnContainer() {

	// log.Println("[✅] Spawning container...")

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

	// Set working directory and environment for installation
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

	// Check for dependency files and set execution commands based on language
	var execCommand string
	var execArgs []string
	var installScript string
	cwd, _ := os.Getwd()

	if getconfig.Language != "" {
		var depFile string
		var depFileName string
		var hasDependencies bool

		switch getconfig.Language {
		case "python":
			execCommand = "python3"
			execArgs = append([]string{getconfig.Script}, getconfig.Args...)

			depFileName = "requirements.txt"
			depFile = filepath.Join(cwd, depFileName)

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

		case "javascript":
			execCommand = "node"
			execArgs = append([]string{getconfig.Script}, getconfig.Args...)

			// Check for package.json
			depFileName = "package.json"
			depFile = filepath.Join(cwd, depFileName)

			if _, err := os.Stat(depFile); err == nil {
				log.Printf("Found %s in project directory", depFileName)
				hasDependencies = true

				// Check for lock files to determine package manager
				yarnLock := filepath.Join(cwd, "yarn.lock")
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

		case "golang":
			execCommand = "go"
			execArgs = append([]string{"run", getconfig.Script}, getconfig.Args...)

			// Check for go.mod
			depFileName = "go.mod"
			depFile = filepath.Join(cwd, depFileName)

			if _, err := os.Stat(depFile); err == nil {
				log.Printf("Found %s in project directory", depFileName)
				hasDependencies = true
				installScript = "go mod download"

				// Also check for go.sum
				sumFile := filepath.Join(cwd, "go.sum")
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
				// Create virtual environment first
				venvPath := "/opt/venv"
				log.Printf("Creating virtual environment at %s...", venvPath)

				venvCmd := exec.Command("python", "-m", "venv", venvPath)
				venvCmd.Dir = cwd
				venvCmd.Env = env
				venvCmd.Stdout = os.Stdout
				venvCmd.Stderr = os.Stderr

				err := venvCmd.Run()
				must("virtual environment creation error: ", err)

				log.Printf("Virtual environment created successfully")

				// Use the virtual environment's pip for installation
				installCmd = filepath.Join(venvPath, "bin", "pip")
				installArgs = []string{"install", "--no-cache-dir", "-r", "requirements.txt"}

				// Update PATH to include virtual environment at the beginning
				for i, envVar := range env {
					if strings.HasPrefix(envVar, "PATH=") {
						currentPath := envVar[5:] // Remove "PATH=" prefix
						env[i] = fmt.Sprintf("PATH=%s/bin:%s", venvPath, currentPath)
						break
					}
				}

				os.Setenv("PATH", fmt.Sprintf("%s/bin:%s", venvPath, os.Getenv("PATH")))
				env = append(env, fmt.Sprintf("VIRTUAL_ENV=%s", venvPath))
				env = append(env, "PYTHONHOME=")

			case "javascript":
				if strings.Contains(installScript, "yarn") {
					installCmd = "yarn"
					installArgs = []string{"install"}
				} else {
					installCmd = "npm"
					installArgs = []string{"install"}
				}
			case "golang":
				installCmd = "go"
				installArgs = []string{"mod", "download"}
			}

			// Look up install command path
			installCmdPath, err := exec.LookPath(installCmd)
			must("install command path error: ", err)

			// Execute installation command
			cmd := exec.Command(installCmdPath, installArgs...)
			cmd.Dir = cwd
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

	var finalCmdPath string
	var finalArgv []string

	// Look up the command path
	cmdPath, err := exec.LookPath(execCommand)
	must("cmdpath error: ", err)

	// Build argv with the command and its arguments
	argv := append([]string{cmdPath}, execArgs...)

	fullCommandString := cmdPath + " " + strings.Join(execArgs, " ")

	// Define the set of characters that require a shell for interpretation.
	specialChars := []string{"|", ">", "<", "&&", "||", ";"}
	needsShell := false

	// Check if any part of the command string contains special shell characters.
	for _, char := range specialChars {
		if strings.Contains(fullCommandString, char) {
			needsShell = true
			break
		}
	}

	if execCommand == "sh" && len(execArgs) > 0 && strings.HasPrefix(execArgs[0], "-c ") {
		fmt.Println("💡 Detected 'sh -c' pattern. Preparing command for shell execution.")
		commandToRun := strings.TrimPrefix(execArgs[0], "-c ")

		// If there are more '-a' flags, append them to the command string.
		if len(execArgs) > 1 {
			commandToRun += " " + strings.Join(execArgs[1:], " ")
		}

		finalCmdPath, err = exec.LookPath("sh")
		must("Could not find 'sh' in PATH", err)

		// The final argument vector for exec must be: ["sh", "-c", "your full command"]
		finalArgv = []string{"sh", "-c", commandToRun}

	} else if needsShell {
		fmt.Println("💡 Detected shell operators (e.g., '|', '>'). Wrapping command in 'sh -c'.")

		finalCmdPath, err = exec.LookPath("sh")
		must("Could not find 'sh' in PATH", err)

		finalArgv = []string{"sh", "-c", fullCommandString}
	} else {
		fmt.Println("✔️ No shell wrapping needed. Preparing for direct execution.")
		finalCmdPath = cmdPath
		finalArgv = argv
	}

	must("capabilities error: ", security.ApplyCapabilities(securityConfig.Capabilities))
	must("seccomp error: ", security.ApplySeccomp(securityConfig.Seccomp))

	must("command Exec error: ", unix.Exec(finalCmdPath, finalArgv, env))

}
