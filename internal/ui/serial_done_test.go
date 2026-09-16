package ui

import (
	"errors"
	"testing"
)

func TestSerialDone_ExitsToHostList(t *testing.T) {
	m := Model{
		viewMode:   ViewSerial,
		serialForm: &serialFormModel{},
		serialOnly: false,
		styles:     NewStyles(80),
		width:      80,
		height:     24,
	}
	updated, _ := m.Update(serialDoneMsg{})
	um := updated.(Model)
	if um.viewMode != ViewList {
		t.Fatalf("Esc should return to ViewList, got %v", um.viewMode)
	}
	if um.serialForm != nil {
		t.Fatal("serialForm should be nil after Esc")
	}
}

func TestSerialConnectDone_NormalReturnsToSerial(t *testing.T) {
	m := Model{
		viewMode:   ViewSerial,
		serialForm: nil,
		serialOnly: false,
		styles:     NewStyles(80),
		width:      80,
		height:     24,
	}
	updated, _ := m.Update(serialConnectDoneMsg{err: nil})
	um := updated.(Model)
	if um.viewMode != ViewSerial {
		t.Fatalf("normal connect done should return to ViewSerial, got %v", um.viewMode)
	}
	if um.serialForm == nil {
		t.Fatal("serialForm should be recreated after connect done")
	}
	if um.serialForm.statusMessage != "Disconnected." {
		t.Fatalf("normal close should set Disconnected toast, got %q", um.serialForm.statusMessage)
	}
}

func TestSerialConnectDone_ErrorShowsToast(t *testing.T) {
	m := Model{
		viewMode:   ViewSerial,
		serialForm: nil,
		serialOnly: false,
		styles:     NewStyles(80),
		width:      80,
		height:     24,
	}
	err := errors.New("I/O error")
	updated, _ := m.Update(serialConnectDoneMsg{err: err})
	um := updated.(Model)
	if um.viewMode != ViewSerial {
		t.Fatalf("error close should still return to ViewSerial, got %v", um.viewMode)
	}
	if um.serialForm == nil || um.serialForm.statusMessage == "" {
		t.Fatalf("error close should set status toast, got %q", um.serialForm.statusMessage)
	}
	if len(um.serialForm.statusMessage) < 6 || um.serialForm.statusMessage[:6] != "Serial" {
		t.Fatalf("status should start with Serial, got %q", um.serialForm.statusMessage)
	}
}

func TestSerialDone_SerialOnlyQuits(t *testing.T) {
	m := Model{
		viewMode:   ViewSerial,
		serialForm: &serialFormModel{},
		serialOnly: true,
		styles:     NewStyles(80),
		width:      80,
		height:     24,
	}
	_, cmd := m.Update(serialDoneMsg{})
	if cmd == nil {
		t.Fatal("serialOnly should return tea.Quit cmd")
	}
	_, cmd = m.Update(serialConnectDoneMsg{err: nil})
	if cmd == nil {
		t.Fatal("serialOnly connect done should return tea.Quit cmd")
	}
}
