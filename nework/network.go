package network

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"os/exec"
	"strconv"
)

// NetParams represents the network parameters for a container
type NetParams struct {
	Dev     string `json:"dev"`
	Address string `json:"address"`
	Netmask string `json:"netmask"`
	Gateway string `json:"gateway"`
	DNS     string `json:"dns"`
	MTU     string `json:"mtu"`
}

// Config configures the network for the child process
func Config(childPID int) (*NetParams, error) {
	pastaPath, err := exec.LookPath("pasta")
	if err != nil {
		return nil, fmt.Errorf("pasta path error: %w", err)
	}

	_, ipnet, err := net.ParseCIDR("10.0.2.0/24")
	if err != nil {
		return nil, fmt.Errorf("ParseCIDR error: ip address not in range: %w", err)
	}

	mtu := 65520
	ifname := "tap0"
	netmask, _ := ipnet.Mask.Size()

	address, err := addIPInt(ipnet.IP, 100)
	if err != nil {
		return nil, fmt.Errorf("cant generate resulting ip address:  %w", err)
	}

	gateway, err := addIPInt(ipnet.IP, 2)
	if err != nil {
		return nil, fmt.Errorf("cant generate resulting ip address:  %w", err)
	}

	dns, err := addIPInt(ipnet.IP, 3)
	if err != nil {
		return nil, fmt.Errorf("cant generate resulting ip address:  %w", err)
	}

	// Build pasta arguments
	opts := []string{
		"--stderr",
		"--ns-ifname=" + ifname,
		"--mtu=" + strconv.Itoa(mtu),
		"--config-net",
		"--address=" + address.String(),
		"--netmask=" + strconv.Itoa(netmask),
		"--gateway=" + gateway.String(),
		"--dns-forward=" + dns.String(),
		"--tcp-ports=none",
		"--udp-ports=none",
		"--host-lo-to-ns-lo",
		strconv.Itoa(childPID), // PID should be last argument
	}

	// Use the found pasta path
	cmd := exec.Command(pastaPath, opts...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		output := fmt.Sprintf("pasta error when executing: %s", out)
		return nil, fmt.Errorf("%s:  %w", output, err)
	}

	config := &NetParams{
		Dev:     ifname,
		Address: address.String(),
		Netmask: strconv.Itoa(netmask),
		Gateway: gateway.String(),
		DNS:     dns.String(),
		MTU:     strconv.Itoa(mtu),
	}

	return config, nil
}

func addIPInt(ip net.IP, i int) (net.IP, error) {
	ip = ip.To4() // Fixed: removed .IP since ip is already net.IP
	if ip == nil {
		return nil, fmt.Errorf("expected IPv4 address, got %v", ip)
	}
	ui32 := binary.BigEndian.Uint32(ip)
	resInt64 := int64(ui32) + int64(i)
	if resInt64 > int64(math.MaxUint32) {
		return nil, fmt.Errorf("%s + %d overflows", ip.String(), i)
	}
	res := make(net.IP, 4)
	binary.BigEndian.PutUint32(res, uint32(resInt64))
	return res, nil
}
