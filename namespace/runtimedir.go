package namespace

import (
	"archive/tar"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"syscall"
)

// getRuntimePaths returns the paths for the runtime data and config directories.
func getRuntimePaths() (string, string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}

	// Data directory (for rootfs, storage)
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		dataDir = filepath.Join(homeDir, ".local", "share")
	}
	runtimeDataDir := filepath.Join(dataDir, "otala-runc")

	// Config directory (for config.json)
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		configDir = filepath.Join(homeDir, ".config")
	}
	runtimeConfigDir := filepath.Join(configDir, "otala-runc")

	return runtimeDataDir, runtimeConfigDir, nil
}

// initializeruntimeDirs to crate container folders; rootfs, storage, metadata, containers
func initializeRuntimeDirs() (string, string, error) {
	dataDir, configDir, err := getRuntimePaths()
	if err != nil {
		return "", "", err
	}

	// Check if all required directories exist
	requiredDirs := []string{
		filepath.Join(dataDir, "rootfs"),
		filepath.Join(dataDir, "storage"),
		filepath.Join(dataDir, "metadata"),
		filepath.Join(configDir, "containers"),
	}

	allExist := true
	for _, dir := range requiredDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			allExist = false
			break
		} else if err != nil {
			return "", "", err
		}
	}

	// If all directories exist, return the paths
	if allExist {
		return dataDir, configDir, nil
	}

	// Create directories that don't exist
	if err := os.MkdirAll(filepath.Join(dataDir, "rootfs"), 0755); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "storage"), 0755); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "metadata"), 0755); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Join(configDir, "containers"), 0755); err != nil {
		return "", "", err
	}

	return dataDir, configDir, nil
}

// SetupContainerEnvironment sets up the container environment by creating necessary directories and files.
func SetupContainerEnvironment(containerID string, configData *[]byte, conExist bool, rootfs *embed.FS) (string, string, *[]byte, error) {
	// Initialize runtime directories
	dataDir, configDir, err := initializeRuntimeDirs()
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to initialize runtime directories: %v", err)
	}

	rootfsPath := filepath.Join(dataDir, "rootfs", "alpine")
	containerPath := filepath.Join(dataDir, "storage", containerID)
	upperPath := filepath.Join(containerPath, "upper")
	workPath := filepath.Join(containerPath, "work")
	mergedPath := filepath.Join(containerPath, "merged")
	configConPath := filepath.Join(configDir, containerID)
	configPath := filepath.Join(configConPath, "config.json")

	// Check if the container already exists then return the rootfs path
	if conExist {
		// Read the file as bytes
		dataFromConfigPath, err := os.ReadFile(configPath)
		if err != nil {
			return "", "", nil, fmt.Errorf("failed to parse config.json using the path given: %v", err)
		}
		return containerPath, configConPath, &dataFromConfigPath, nil
	}

	// Parse existing config data and add a rootfs path
	var config map[string]interface{}
	if err := json.Unmarshal(*configData, &config); err != nil {
		return "", "", nil, fmt.Errorf("failed to parse config.json: %v", err)
	}

	// Create container-specific directories
	if err := extractRootfs(rootfsPath, rootfs); err != nil {
		return "", "", nil, fmt.Errorf("failed to create rootfs directory: %v", err)
	}

	config["rootfs"] = rootfsPath
	config["merged"] = mergedPath
	config["upper"] = upperPath
	config["work"] = workPath

	updatedConfigData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to marshal updated config: %v", err)
	}

	// Write a config.json file
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return "", "", nil, fmt.Errorf("failed to create config directory: %v", err)
	}

	if err := os.WriteFile(configPath, updatedConfigData, 0644); err != nil {
		return "", "", nil, fmt.Errorf("failed to write config.json: %v", err)

	}

	return containerPath, configConPath, &updatedConfigData, nil
}

// extractRootfs extracts the Alpine Linux rootfs from the embedded filesystem into the target directory.
func extractRootfs(target string, alpineFS *embed.FS) error {
	f, err := alpineFS.Open("alpine-minirootfs.tar.gz")
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		path := filepath.Join(target, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			// Create directory with more permissive mode initially
			err := os.MkdirAll(path, 0755)
			if err != nil {
				return err
			}
			// Set proper mode after creation
			err = os.Chmod(path, os.FileMode(hdr.Mode))
			if err != nil {
				log.Printf("Warning: failed to set mode on %s: %v", path, err)
			}
		case tar.TypeReg:
			err := os.MkdirAll(filepath.Dir(path), 0755)
			if err != nil {
				return err
			}
			outFile, err := os.Create(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Chmod(os.FileMode(hdr.Mode))
			outFile.Close()
		case tar.TypeSymlink:
			// Ensure parent directory exists
			err := os.MkdirAll(filepath.Dir(path), 0755)
			if err != nil {
				return err
			}
			// Remove existing symlink if it exists
			if _, err := os.Lstat(path); err == nil {
				if err := os.Remove(path); err != nil {
					return fmt.Errorf("failed to remove existing symlink %s: %v", path, err)
				}
			}
			err = os.Symlink(hdr.Linkname, path)
			if err != nil {
				return err
			}
		case tar.TypeFifo:
			// Create named pipe (FIFO)
			err := os.MkdirAll(filepath.Dir(path), 0755)
			if err != nil {
				return err
			}
			err = syscall.Mkfifo(path, uint32(hdr.Mode))
			if err != nil && !os.IsExist(err) {
				return err
			}
		default:
			log.Printf("Skipping unsupported type: %s (%d)\n", hdr.Name, hdr.Typeflag)
		}
	}
	return nil
}
