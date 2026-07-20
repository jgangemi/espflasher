package espflasher

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestESP32H2PostConnectUSBJTAG(t *testing.T) {
	writeCount := 0
	readCount := 0
	writeAddrs := []uint32{}

	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			readCount++
			if addr == esp32h2UARTDevBufNo {
				return esp32h2UARTDevBufNoUSBJTAGSerial, nil
			}
			// Return 0 for SWD conf read
			return 0, nil
		},
		writeRegFunc: func(addr, value, mask, delayUS uint32) error {
			writeCount++
			writeAddrs = append(writeAddrs, addr)
			return nil
		},
	}
	f := &Flasher{
		conn: mc,
		opts: &FlasherOptions{},
	}

	err := esp32h2PostConnect(f)
	require.NoError(t, err)
	assert.True(t, f.usesUSB, "usesUSB should be true for USB-JTAG/Serial")
	assert.Greater(t, writeCount, 0, "should have written registers to disable watchdog")

	// Verify H2-specific register offsets were used
	assert.Contains(t, writeAddrs, esp32h2LPWDTWProtect, "should write to H2 LP_WDT wprotect")
	assert.Contains(t, writeAddrs, esp32h2LPWDTSWDWProtect, "should write to H2 LP_WDT SWD wprotect")
}

func TestESP32H2PostConnectUART(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			return 0, nil // Not USB, return UART value
		},
	}
	f := &Flasher{
		conn: mc,
		opts: &FlasherOptions{},
	}

	err := esp32h2PostConnect(f)
	require.NoError(t, err)
	assert.False(t, f.usesUSB, "usesUSB should be false for UART")
}

func TestESP32H2MAC(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			switch addr {
			case esp32h2EfuseBlock1Word0:
				return 0x60559ff7, nil
			case esp32h2EfuseBlock1Word0 + 4:
				return 0x00002ca2, nil
			}
			return 0, nil
		},
	}
	f := &Flasher{conn: mc, chip: defESP32H2}

	mac, err := f.MAC()
	require.NoError(t, err)
	assert.Equal(t, "2c:a2:60:55:9f:f7", mac.String())
}

func TestESP32H2ChipRevision(t *testing.T) {
	tests := []struct {
		name  string
		word3 uint32
		want  ChipRevision
	}{
		{"v0.0", 0x00000000, ChipRevision{0, 0}},
		{"v1.1", (1 << 21) | (1 << 18), ChipRevision{1, 1}},
		{"max", (3 << 21) | (0x7 << 18), ChipRevision{3, 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockConnection{
				readRegFunc: func(addr uint32) (uint32, error) {
					if addr == esp32h2EfuseBlock1Word3 {
						return tt.word3, nil
					}
					return 0, nil
				},
			}
			f := &Flasher{conn: mc, chip: defESP32H2}
			got, err := f.ChipRevision()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestESP32H2ChipFeatures(t *testing.T) {
	mc := &mockConnection{}
	f := &Flasher{conn: mc, chip: defESP32H2}
	got, err := f.ChipFeatures()
	require.NoError(t, err)
	assert.Equal(t, []string{"BT 5 (LE)", "IEEE802.15.4", "Single Core", "96MHz"}, got)
}

func TestESP32H2ReadErrorsPropagate(t *testing.T) {
	newF := func(readReg func(addr uint32) (uint32, error)) *Flasher {
		return &Flasher{conn: &mockConnection{readRegFunc: readReg}, chip: defESP32H2}
	}
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32h2EfuseBlock1Word0, esp32h2EfuseBlock1Word0 + 4},
		func(f *Flasher) error { _, err := f.MAC(); return err })
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32h2EfuseBlock1Word3},
		func(f *Flasher) error { _, err := f.ChipRevision(); return err })
}

func TestESP32H2PostConnectSecureMode(t *testing.T) {
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

	err := esp32h2PostConnect(f)
	require.NoError(t, err, "should gracefully handle read error")
	assert.False(t, f.usesUSB, "should default to non-USB on read error")
}
