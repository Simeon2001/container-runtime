package message

import (
	"encoding/json"
	"fmt"
	runConfig "github.com/Simeon2001/AlpineCell/config"
	"github.com/Simeon2001/AlpineCell/security"
	"os"

	network "github.com/Simeon2001/AlpineCell/nework"
)

// Message represents a communication message between parent and child processes
type Message struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// ParentPipe handles communication from parent to child process
type ParentPipe struct {
	Writer *json.Encoder
	Reader *json.Decoder
}

// SendHelloToChild sends a "ready" message to the child process
func (p *ParentPipe) SendHelloToChild() error {
	if err := p.Writer.Encode(Message{Type: "ready"}); err != nil {
		return fmt.Errorf("error encoding message to child: %w", err)
	}
	return nil
}

// WaitForChildMsg waits for and validates a response from the child process
func (p *ParentPipe) WaitForChildMsg() (bool, error) {
	var msg Message
	if err := p.Reader.Decode(&msg); err != nil {
		return false, fmt.Errorf("error decoding message from child: %w", err)
	}
	if msg.Type != "ok" {
		return false, fmt.Errorf("unexpected response from child: got %q, expected %q", msg.Type, "ok")
	}
	return true, nil
}

// SendIDMappingMsgAndConfig sends a "mapping" message and container configuration to the child process
func (p *ParentPipe) SendIDMappingMsgAndConfig(conConfig *runConfig.RunConfig) error {
	if err := p.Writer.Encode(Message{Type: "mapping", Value: conConfig}); err != nil {
		return fmt.Errorf("error encoding message to child: %w", err)
	}
	return nil
}

// WaitForIDMappingMsg wait for message from a child process
func (p *ParentPipe) WaitForIDMappingMsg() error {
	var msg Message
	if err := p.Reader.Decode(&msg); err != nil {
		return fmt.Errorf("error decoding message from child: %w", err)
	}
	if msg.Type != "mapping-ok" {
		return fmt.Errorf("unexpected response from child: got %q, expected %q", msg.Type, "ok")
	}
	return nil
}

// SendParentNetworkInit initializes parent network communication
func (p *ParentPipe) SendParentNetworkInit(netparams network.NetParams) error {
	if err := p.Writer.Encode(Message{Type: "network", Value: netparams}); err != nil {
		return fmt.Errorf("error encoding message to child: %w", err)
	}
	return nil
}

// SendParentSeccompConfig sends seccomp configuration to the parent process
func (p *ParentPipe) SendParentSeccompConfig(secconfig security.Config) error {
	if err := p.Writer.Encode(Message{Type: "security", Value: secconfig}); err != nil {
		return fmt.Errorf("error encoding message to child: %w", err)
	}
	return nil
}

// SendContainerConfig sends container configuration to the parent process
func (p *ParentPipe) SendContainerConfig(config runConfig.RunConfig) error {
	if err := p.Writer.Encode(Message{Type: "configuration", Value: config}); err != nil {
		return fmt.Errorf("error encoding message to child: %w", err)
	}
	return nil
}

// ParentInitialization creates a new ParentPipe with the provided file handles
func ParentInitialization(writer, reader *os.File) *ParentPipe {
	return &ParentPipe{
		Writer: json.NewEncoder(writer),
		Reader: json.NewDecoder(reader),
	}
}

// ChildPipe handles communication from child to parent process
type ChildPipe struct {
	Writer *json.Encoder
	Reader *json.Decoder
}

// WaitForParentMsg waits for and validates a message from the parent process
func (c *ChildPipe) WaitForParentMsg() (bool, error) {
	var msg Message
	if err := c.Reader.Decode(&msg); err != nil {
		return false, fmt.Errorf("error decoding message from parent: %w", err)
	}
	if msg.Type != "ready" {
		return false, fmt.Errorf("unexpected response from parent: got %q, expected %q", msg.Type, "ready")
	}
	return true, nil
}

// SendHelloToParent sends an "ok" message to the parent process
func (c *ChildPipe) SendHelloToParent() error {
	if err := c.Writer.Encode(Message{Type: "ok"}); err != nil {
		return fmt.Errorf("error encoding message to parent: %w", err)
	}
	return nil
}

// WaitForParentNetworkConfig get networkconfig from parent
func (c *ChildPipe) WaitForParentNetworkConfig() (*network.NetParams, error) {
	var rawMsg Message
	if err := c.Reader.Decode(&rawMsg); err != nil {
		return nil, fmt.Errorf("error decoding message from parent: %w", err)
	}
	if rawMsg.Type != "network" {
		return nil, fmt.Errorf("unexpected response from parent: got %q, expected %q", rawMsg.Type, "network")
	}

	// Marshal and re-unmarshal the raw value into network.NetParams
	rawBytes, err := json.Marshal(rawMsg.Value)
	if err != nil {
		return nil, fmt.Errorf("error marshaling value back to JSON: %w", err)
	}
	var cfg network.NetParams
	if err := json.Unmarshal(rawBytes, &cfg); err != nil {
		return nil, fmt.Errorf("error decoding value into NetParams: %w", err)
	}
	return &cfg, nil
}

// WaitForParentSeccompConfig get seccomp config from parent
func (c *ChildPipe) WaitForParentSeccompConfig() (*security.Config, error) {
	var rawMsg Message
	if err := c.Reader.Decode(&rawMsg); err != nil {
		return nil, fmt.Errorf("error decoding message from parent: %w", err)
	}
	if rawMsg.Type != "security" {
		return nil, fmt.Errorf("unexpected response from parent: got %q, expected %q", rawMsg.Type, "security")
	}

	// Marshal and re-unmarshal the raw value into network.NetParams
	rawBytes, err := json.Marshal(rawMsg.Value)
	if err != nil {
		return nil, fmt.Errorf("error marshaling value back to JSON: %w", err)
	}
	var cfg security.Config
	if err := json.Unmarshal(rawBytes, &cfg); err != nil {
		return nil, fmt.Errorf("error decoding value into NetParams: %w", err)
	}
	return &cfg, nil
}

// SendIDMappingMsgFromChild sends a "mapping-ok" message to the parent process
func (c *ChildPipe) SendIDMappingMsgFromChild() error {
	if err := c.Writer.Encode(Message{Type: "mapping-ok"}); err != nil {
		return fmt.Errorf("error encoding message to child: %w", err)
	}
	return nil
}

// WaitForIDMappingMsgFromParent waits for message from parent process
func (c *ChildPipe) WaitForIDMappingMsgFromParent() (bool, error) {
	var msg Message
	if err := c.Reader.Decode(&msg); err != nil {
		return false, fmt.Errorf("error decoding message from parent: %w", err)
	}
	if msg.Type != "mapping" {
		return false, fmt.Errorf("unexpected response from parent: got %q, expected %q", msg.Type, "mapping")
	}
	return true, nil
}

// WaitForConfigFromParent  waits for message from parent process, and returns the config
func (c *ChildPipe) WaitForConfigFromParent() (runConfig.RunConfig, error) {
	var msg Message
	if err := c.Reader.Decode(&msg); err != nil {
		return runConfig.RunConfig{}, fmt.Errorf("error decoding message from parent: %w", err)
	}
	if msg.Type != "configuration" {
		return runConfig.RunConfig{}, fmt.Errorf("unexpected response from parent: got %q, expected %q", msg.Type, "configuration")
	}
	marshalledMap, err := json.Marshal(msg.Value)
	if err != nil {
		return runConfig.RunConfig{}, fmt.Errorf("error marshaling value back to JSON: %w", err)
	}

	// Step 2: unmarshal JSON into your config struct
	var getconfig runConfig.RunConfig
	if err = json.Unmarshal(marshalledMap, &getconfig); err != nil {
		return runConfig.RunConfig{}, fmt.Errorf("error decoding value into RunConfig: %w", err)
	}
	return getconfig, nil
}

// ChildInitialization creates a new ChildPipe with the provided file handles
func ChildInitialization(writer, reader *os.File) *ChildPipe {
	return &ChildPipe{
		Writer: json.NewEncoder(writer),
		Reader: json.NewDecoder(reader),
	}
}
