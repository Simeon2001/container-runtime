package main

import (
	"embed"
	"fmt"
	runConfig "github.com/Simeon2001/AlpineCell/config"
	"github.com/Simeon2001/AlpineCell/namespace"
	"github.com/Simeon2001/AlpineCell/systemd"
	"os"
	"path/filepath"
	"strings"
)

// mbToBytes converts a value in megabytes (mb) to bytes and returns it as a string.
func mbToBytes(mb int) string {
	bytes := mb * 1024 * 1024
	return fmt.Sprintf("%d", bytes)
}

func InitProcess(file, rootfs *embed.FS, config *runConfig.RunConfig) {

	var cwd string
	if config.MountBool {
		cwd = config.Mounts
	} else {
		cwd = config.CopyMounts
	}

	uniqueID, exist, err := getOrCreateUniqueID(cwd)
	if err != nil {
		must("Generating uniqueID err: ", err)
	}
	// log.Printf("here the path you want to copy, uniqueID, exist: %s, %v, %v\n", cwd, uniqueID, exist)
	containerName := "otalacon-" + uniqueID
	memoryAllocoy := mbToBytes(config.MemoryLimit)

	err, boolValue, _ := systemd.Manager(containerName, memoryAllocoy)
	if err != nil {
		if boolValue == true {
			InitProcess(file, rootfs, config)
		} else {
			must("systemd error", err)
		}
	}
	// log.Printf("your cgroup Path: %s\n", cgroupPath)
	configData, err := loadConfig(file)
	if err != nil {
		must("Loading config err: ", err)
	}

	containerPath, containerConfigPath, configJSONData, err := namespace.SetupContainerEnvironment(containerName, configData, exist, rootfs)
	if err != nil {
		must("Setting up container environment err: ", err)
	}
	config.SetContainerConfig(uniqueID, containerPath, containerConfigPath)
	namespace.Stage1UserNS(config, configJSONData)

}

// loadConfig loads the config file from the filesystem and returns the data as a byte slice.
func loadConfig(configFile *embed.FS) (*[]byte, error) {
	data, err := configFile.ReadFile("config.json")
	if err != nil {
		return nil, err
	}

	return &data, nil
}

// getOrCreateUniqueID checks for .otalarunc-config file in the given directory
func getOrCreateUniqueID(cwd string) (string, bool, error) {
	configFilePath := filepath.Join(cwd, ".otalarunc-config")

	// Check if the file exists
	if _, err := os.Stat(configFilePath); err == nil {
		// File exists, read the unique ID from it
		data, err := os.ReadFile(configFilePath)
		if err != nil {
			return "", false, err
		}

		// Return the unique ID, trimming any whitespace
		uniqueID := strings.TrimSpace(string(data))
		return uniqueID, true, nil
	} else if os.IsNotExist(err) {
		// File doesn't exist, generate new unique ID
		uniqueID, err := generateUniqueID()
		if err != nil {
			return "", false, err
		}

		// Write the unique ID to the file
		err = os.WriteFile(configFilePath, []byte(uniqueID), 0644)
		if err != nil {
			return "", false, err
		}

		return uniqueID, false, nil
	} else {
		// Some other error occurred
		return "", false, err
	}
}
