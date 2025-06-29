package config

type RunConfig struct {
	Network         bool // true = pasta networking, false = no networking
	DeleteWhenDone  bool // DeleteWhenDone specifies whether the container's resources should be removed upon completion of its execution.
	MemoryLimit     int  // MemoryLimit in MB
	ConfigPath      string
	CopyMounts      string // host paths to copy into container
	Mounts          string // host paths to mount into container
	MountBool       bool   // MountBool indicates whether to use mount-based path handling in the container runtime.
	Language        string
	Script          string   // file path to script
	Command         string   // direct command to execute
	Args            []string // arguments to pass to the script/command
	ContainerConfig ContainerConfig
}

type ContainerConfig struct {
	ContainerID         string
	ContainerPath       string
	ContainerConfigPath string
}

// SetContainerConfig sets the container ID, container path, and configuration path in the ContainerConfig struct.
func (r *RunConfig) SetContainerConfig(id, conPath, configPath string) {
	r.ContainerConfig.ContainerID = id
	r.ContainerConfig.ContainerPath = conPath
	r.ContainerConfig.ContainerConfigPath = configPath
}
