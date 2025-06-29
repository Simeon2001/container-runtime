package systemd

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/godbus/dbus/v5"
)

// SystemdManagerPath is the path to the systemd manager object.
const (
	UserService   = "org.freedesktop.systemd1"
	UserPath      = "/org/freedesktop/systemd1"
	UserInterface = "org.freedesktop.systemd1.Manager"
)

func Manager(containerName string, memoryBytes string) (error, bool, string) {

	memBytes, err := strconv.ParseUint(memoryBytes, 10, 64)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Memory limit must be a number: %v\n", err)
		os.Exit(1)
	}

	// Connect to the user's session bus
	conn, err := dbus.ConnectSessionBus()
	defer func(conn *dbus.Conn) {
		_ = conn.Close()
	}(conn)
	if err != nil {
		return fmt.Errorf("failed to connect to session bus: %v", err), false, ""
	}

	// Get systemd manager object
	systemd := conn.Object(UserService, dbus.ObjectPath(UserPath))

	// Create unit name - scope units end with .scope
	unitName := fmt.Sprintf("%s.scope", containerName)

	var unitPath dbus.ObjectPath

	// Try to get the unit — if it doesn't exist, we skip stopping
	err = systemd.Call("org.freedesktop.systemd1.Manager.GetUnit", 0, unitName).Store(&unitPath)
	if err != nil {
		// fmt.Printf("Unit %s does not exist, continuing...\n", unitName)
	} else {
		return fmt.Errorf("containerName existed already in cgroup UserService"), true, ""
	}

	// The correct D-Bus signature is a(sv)
	properties := []struct {
		Name  string
		Value dbus.Variant
	}{
		// {
		// 	Name:  "Description",
		// 	Value: dbus.MakeVariant(fmt.Sprintf("Scope for %s", containerName)),
		// },
		{
			Name:  "MemoryMax",
			Value: dbus.MakeVariant(memBytes), // uint64
		},
		{
			Name:  "MemorySwapMax",
			Value: dbus.MakeVariant(memBytes), // uint64
		},
		{
			Name:  "PIDs",
			Value: dbus.MakeVariant([]uint32{uint32(os.Getpid())}),
		},
	}

	// For a(sa(sv)) — no auxiliary units
	var aux []struct {
		Name       string
		Properties []struct {
			Name  string
			Value dbus.Variant
		}
	}

	// Call StartTransientUnit
	var jobPath dbus.ObjectPath

	err = systemd.Call(
		UserInterface+".StartTransientUnit",
		0,
		unitName,   // unit name
		"replace",  // mode - "replace" is usually what you want
		properties, // properties - matches a(sv)
		aux,        // aux (unused, pass empty) - matches a(sa(sv))
	).Store(&jobPath)

	if err != nil {
		return fmt.Errorf("Failed to start transient unit: %v\n", err), false, ""
	}

	// Give systemd a moment to set up the cgroup
	time.Sleep(500 * time.Millisecond)

	// Find and verify the cgroup path
	cgroupPath, err := findCgroupPath(containerName)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to find cgroup path: %v\n", err)
	} else {
		// Check memory.max
		// log.Printf("%s is the cgroup path \n", cgroupPath)
		_, err := readMemoryMax(cgroupPath)
		if err != nil {
			return fmt.Errorf("Failed to read memory.max: %v\n", err), false, ""
		} else {
			// log.Printf("Verified memory.max: %s\n", memoryMax)
		}
	}

	return nil, false, cgroupPath

}

func CleanSystemd(pathName string) {

	// Connect to the user's session bus
	conn, err := dbus.ConnectSessionBus()
	defer func(conn *dbus.Conn) {
		_ = conn.Close()
	}(conn)
	if err != nil {
		log.Fatal("dbus session connection failed")
	}

	// Get systemd manager object
	systemd := conn.Object(UserService, dbus.ObjectPath(UserPath))

	// Create unit name - scope units end with .scope
	unitName := fmt.Sprintf("%s.scope", pathName)
	// Stop the existing unit
	stopCall := systemd.Call("org.freedesktop.systemd1.Manager.StopUnit", 0, unitName, "replace")
	if stopCall.Err != nil {
		log.Fatalf("Failed to stop unit: %v\n", stopCall.Err)
	}

	fmt.Println("Existing scope stopped.")
	if err := systemd.Call("org.freedesktop.systemd1.Manager.ResetFailedUnit", 0, unitName); err != nil {
		log.Fatalf("Unit %s resetting didnt work...\n", unitName)
	}

}
