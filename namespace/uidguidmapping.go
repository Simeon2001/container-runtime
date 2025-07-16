package namespace

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
)

type SubIDRange struct {
	Start  int
	Length int
}

func getUIDGUID() ([]string, []string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return nil, nil, fmt.Errorf("user.Current: %v", err)
	}
	uid, err := strconv.Atoi(currentUser.Uid)
	if err != nil {
		return nil, nil, fmt.Errorf("strconv.Atoi: %v", err)
	}

	getSubIDsExecutable := "getsubids"
	if v := os.Getenv("GETSUBIDS"); v != "" {
		getSubIDsExecutable = v
	}
	getSubIDsPath, err := exec.LookPath(getSubIDsExecutable)
	if err != nil {
		return nil, nil, fmt.Errorf("getsubids not found; %v", err)
	}

	// Get UID and GID ranges separately
	uidRanges, gidRanges, err := getAllSubIDRanges(getSubIDsPath, currentUser.Username, strconv.Itoa(uid))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get SubID ranges: %v", err)
	}

	uidMap, gidMap, err := generateUIDGIDMaps(currentUser, uidRanges, gidRanges)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate UID/GID maps: %v", err)
	}
	return uidMap, gidMap, nil
}

// extractSubIDs executes getsubids and returns SubIDRange
func extractSubIDs(getSubIDsPath string, useGIDFlag bool, userID string) (SubIDRange, error) {
	var cmd *exec.Cmd

	if useGIDFlag {
		cmd = exec.Command(getSubIDsPath, "-g", userID)
	} else {
		cmd = exec.Command(getSubIDsPath, userID)
	}

	output, err := cmd.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return SubIDRange{}, fmt.Errorf("failed to execute getsubids: %v, stderr: %s", err, string(exitError.Stderr))
		}
		return SubIDRange{}, fmt.Errorf("failed to execute getsubids: %v", err)
	}

	return parseSubIDOutput(string(output))
}

// parseSubIDOutput parses the getsubids output format: "0: username 100000 65536"
func parseSubIDOutput(output string) (SubIDRange, error) {
	outputStr := strings.TrimSpace(output)
	fields := strings.Fields(outputStr)

	if len(fields) < 4 {
		return SubIDRange{}, fmt.Errorf("unexpected getsubids output format: %s (got %d fields, expected at least 4)", outputStr, len(fields))
	}

	// Extract start ID and count from fields[2] and fields[3]
	startID, err := strconv.Atoi(fields[2])
	if err != nil {
		return SubIDRange{}, fmt.Errorf("failed to parse start ID '%s': %v", fields[2], err)
	}

	length, err := strconv.Atoi(fields[3])
	if err != nil {
		return SubIDRange{}, fmt.Errorf("failed to parse length '%s': %v", fields[3], err)
	}

	return SubIDRange{Start: startID, Length: length}, nil
}

// removeDuplicateRanges removes duplicate SubIDRange entries
func removeDuplicateRanges(ranges []SubIDRange) []SubIDRange {
	seen := make(map[SubIDRange]bool)
	var uniqueRanges []SubIDRange

	for _, item := range ranges {
		if !seen[item] {
			seen[item] = true
			uniqueRanges = append(uniqueRanges, item)
		}
	}
	return uniqueRanges
}

// getAllSubIDRanges gets UID and GID ranges separately for both username and userid
func getAllSubIDRanges(getSubIDsPath, username, userid string) ([]SubIDRange, []SubIDRange, error) {
	var uidByUsername, uidByUserID, gidByUsername, gidByUserID []SubIDRange

	// Get UID ranges (useGIDFlag = false)
	if uidRange, err := extractSubIDs(getSubIDsPath, false, username); err == nil {
		uidByUsername = append(uidByUsername, uidRange)
	} else {
		// fmt.Printf("Warning: failed to get UID by username: %v\n", err)
	}

	if uidRange, err := extractSubIDs(getSubIDsPath, false, userid); err == nil {
		uidByUserID = append(uidByUserID, uidRange)
	} else {
		// fmt.Printf("Warning: failed to get UID by userid: %v\n", err)
	}

	// Get GID ranges (useGIDFlag = true)
	if gidRange, err := extractSubIDs(getSubIDsPath, true, username); err == nil {
		gidByUsername = append(gidByUsername, gidRange)
	} else {
		// fmt.Printf("Warning: failed to get GID by username: %v\n", err)
	}

	if gidRange, err := extractSubIDs(getSubIDsPath, true, userid); err == nil {
		gidByUserID = append(gidByUserID, gidRange)
	} else {
		// fmt.Printf("Warning: failed to get GID by userid: %v\n", err)
	}

	// Combine and deduplicate separately
	allUIDRanges := removeDuplicateRanges(append(uidByUsername, uidByUserID...))
	allGIDRanges := removeDuplicateRanges(append(gidByUsername, gidByUserID...))

	return allUIDRanges, allGIDRanges, nil
}

// GenerateUIDGIDMaps returns the new UID and GID maps for the container
func generateUIDGIDMaps(currentUser *user.User, subUIDRanges, subGIDRanges []SubIDRange) ([]string, []string, error) {
	uidMap := []string{"0", currentUser.Uid, "1"}
	gidMap := []string{"0", currentUser.Gid, "1"}

	uidMap = appendRangeMappings(uidMap, subUIDRanges, 1)
	gidMap = appendRangeMappings(gidMap, subGIDRanges, 1)

	return uidMap, gidMap, nil
}

// appendRangeMappings appends range mappings to the provided map slice
func appendRangeMappings(idMap []string, ranges []SubIDRange, startOffset int) []string {
	currentOffset := startOffset

	for _, idRange := range ranges {
		idMap = append(idMap,
			strconv.Itoa(currentOffset),
			strconv.Itoa(idRange.Start),
			strconv.Itoa(idRange.Length),
		)
		currentOffset += idRange.Length
	}

	return idMap
}

func SetupUserNamespaceMapping(pid int) error {
	uArgs, gArgs, err := getUIDGUID()
	if err != nil {
		return fmt.Errorf("failed to get UID/GID maps: %v", err)
	}
	pidStr := strconv.Itoa(pid)

	if err = executeMapping("newuidmap", pidStr, uArgs); err != nil {
		return fmt.Errorf("failed to execute newuidmap: %v", err)
	}

	if err = executeMapping("newgidmap", pidStr, gArgs); err != nil {
		return fmt.Errorf("failed to execute newgidmap: %v", err)
	}

	return nil
}

func executeMapping(command, pidStr string, args []string) error {
	cmdArgs := append([]string{pidStr}, args...)
	cmd := exec.Command(command, cmdArgs...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s %v failed: %s: %w",
			command, pidStr, args, string(output), err)
	}

	return nil
}
