package tests

import (
	"fmt"
	"sync"
)

// MockTextprotoCommander provides a mock implementation of the TextprotoCommander interface for testing.
type MockTextprotoCommander struct {
	CmdFunc         func(format string, args ...interface{}) (uint, error)
	ReadLineFunc    func() (string, error)
	StartResponseMu sync.Mutex
	EndResponseMu   sync.Mutex
	CloseCalled     bool
	Commands        []string
}

func (m *MockTextprotoCommander) Cmd(format string, args ...interface{}) (uint, error) {
	m.Commands = append(m.Commands, fmt.Sprintf(format, args...))
	if m.CmdFunc != nil {
		return m.CmdFunc(format, args...)
	}
	return 1, nil // Default command ID
}

func (m *MockTextprotoCommander) StartResponse(_ uint) {
	m.StartResponseMu.Lock()
	defer m.StartResponseMu.Unlock()
	// No-op for mock
}

func (m *MockTextprotoCommander) EndResponse(_ uint) {
	m.EndResponseMu.Lock()
	defer m.EndResponseMu.Unlock()
	// No-op for mock
}

func (m *MockTextprotoCommander) ReadLine() (string, error) {
	if m.ReadLineFunc != nil {
		return m.ReadLineFunc()
	}
	return "", nil // Default empty line
}

// Close method for MockTextprotoCommander to satisfy the *textproto.Conn type assertion in api/update.go
func (m *MockTextprotoCommander) Close() error {
	m.CloseCalled = true
	return nil
}
