package espflasher

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestESP32P4Rev1PostConnectUSBJTAG(t *testing.T) {
	writeCount := 0
	readCount := 0

	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			readCount++
			if addr == esp32p4Rev1UARTDevBufNo {
				return esp32p4UARTDevBufNoUSBJTAGSerial, nil
			}
			// Return 0 for SWD conf read
			return 0, nil
		},
		writeRegFunc: func(addr, value, mask, delayUS uint32) error {
			writeCount++
			return nil
		},
	}
	f := &Flasher{
		conn: mc,
		opts: &FlasherOptions{},
	}

	err := esp32p4Rev1PostConnect(f)
	require.NoError(t, err)
	assert.True(t, f.usesUSB, "usesUSB should be true for USB-JTAG/Serial")
	assert.Greater(t, writeCount, 0, "should have written registers to disable watchdog")
}

func TestESP32P4Rev1PostConnectUART(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			return 0, nil // Not USB, return UART value
		},
	}
	f := &Flasher{
		conn: mc,
		opts: &FlasherOptions{},
	}

	err := esp32p4Rev1PostConnect(f)
	require.NoError(t, err)
	assert.False(t, f.usesUSB, "usesUSB should be false for UART")
}

func TestESP32P4Rev1MAC(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			switch addr {
			case esp32p4EfuseBlock1Word0:
				return 0x60559ff7, nil
			case esp32p4EfuseBlock1Word0 + 4:
				return 0x00002ca2, nil
			}
			return 0, nil
		},
	}
	f := &Flasher{conn: mc, chip: defESP32P4Rev1}

	mac, err := f.MAC()
	require.NoError(t, err)
	assert.Equal(t, "2c:a2:60:55:9f:f7", mac.String())
}

func TestESP32P4Rev1ChipRevision(t *testing.T) {
	tests := []struct {
		name  string
		word2 uint32
		want  ChipRevision
	}{
		{"v0.0", 0x00000000, ChipRevision{0, 0}},
		{"v1.1", (1 << 4) | 1, ChipRevision{1, 1}},
		{"v5.15 (major bit2 set)", (1 << 23) | 1<<4 | 0xF, ChipRevision{5, 15}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockConnection{
				readRegFunc: func(addr uint32) (uint32, error) {
					if addr == esp32p4EfuseBlock1Word2 {
						return tt.word2, nil
					}
					return 0, nil
				},
			}
			f := &Flasher{conn: mc, chip: defESP32P4Rev1}
			got, err := f.ChipRevision()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestESP32P4Rev1ChipFeatures(t *testing.T) {
	mc := &mockConnection{}
	f := &Flasher{conn: mc, chip: defESP32P4Rev1}
	got, err := f.ChipFeatures()
	require.NoError(t, err)
	assert.Equal(t, []string{"Dual Core + LP Core", "400MHz"}, got)
}

func TestESP32P4Rev1ReadErrorsPropagate(t *testing.T) {
	newF := func(readReg func(addr uint32) (uint32, error)) *Flasher {
		return &Flasher{conn: &mockConnection{readRegFunc: readReg}, chip: defESP32P4Rev1}
	}
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32p4EfuseBlock1Word0, esp32p4EfuseBlock1Word0 + 4},
		func(f *Flasher) error { _, err := f.MAC(); return err })
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32p4EfuseBlock1Word2},
		func(f *Flasher) error { _, err := f.ChipRevision(); return err })
}

func TestESP32P4Rev1PostConnectSecureMode(t *testing.T) {
	// Simulate read error (secure download mode)
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			return 0, errors.New("register not readable") // Simulate unreadable register
		},
	}
	f := &Flasher{
		conn: mc,
		opts: &FlasherOptions{},
	}

	err := esp32p4Rev1PostConnect(f)
	require.NoError(t, err, "should gracefully handle read error")
	assert.False(t, f.usesUSB, "should default to non-USB on read error")
}
