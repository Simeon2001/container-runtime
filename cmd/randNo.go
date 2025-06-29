package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

//
//func randNowithDash() string {
//
//	randomInt := strconv.Itoa(mrand.Intn(100))
//	randomBigInt := strconv.Itoa(mrand.Intn(1000))
//	randomInfInt := strconv.Itoa(mrand.Intn(5000))
//
//	joinDashNo := "/" + randomInt + "-" + randomBigInt + "-" + randomInfInt
//	return joinDashNo
//}

func generateUniqueID() (string, error) {
	// Generate 16 bytes (128 bits) of random data
	randBytes := make([]byte, 16)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %v", err)
	}

	// Get current timestamp in seconds and microseconds
	now := time.Now()
	sec := now.Unix()
	usec := now.Nanosecond() / 1000 // Convert nanoseconds to microseconds

	// Get process ID
	pid := os.Getpid()

	// Get machine ID
	machineID, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("failed to get hostname: %v", err)
	}

	// Combine all data into a single byte slice
	combinedData := make([]byte, 0, len(randBytes)+8+4+len(machineID))

	// Convert values to byte slices and append them
	combinedData = append(combinedData, randBytes...)

	// Convert seconds to bytes
	secBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(secBytes, uint64(sec))
	combinedData = append(combinedData, secBytes...)

	// Convert microseconds to bytes
	usecBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(usecBytes, uint32(usec))
	combinedData = append(combinedData, usecBytes...)

	// Convert process ID to bytes
	pidBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(pidBytes, uint32(pid))
	combinedData = append(combinedData, pidBytes...)

	// Append machine ID
	combinedData = append(combinedData, []byte(machineID)...)

	// Hash the combined data using SHA-256
	hash := sha256.Sum256(combinedData)

	// Encode the hash as a hexadecimal string
	return hex.EncodeToString(hash[:]), nil
}
