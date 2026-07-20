package espflasher

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestESP32S2PostConnectUSBOTG(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			if addr == esp32s2UARTDevBufNo {
				return esp32s2UARTDevBufNoUSBOTG, nil
			}
			return 0, nil
		},
	}
	f := &Flasher{
		conn: mc,
		opts: &FlasherOptions{},
	}

	err := esp32s2PostConnect(f)
	require.NoError(t, err)
	assert.True(t, f.usesUSB, "usesUSB should be true for USB-OTG")
}

func TestESP32S2PostConnectUART(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			return 0, nil // Not USB, return UART value
		},
	}
	f := &Flasher{
		conn: mc,
		opts: &FlasherOptions{},
	}

	err := esp32s2PostConnect(f)
	require.NoError(t, err)
	assert.False(t, f.usesUSB, "usesUSB should be false for UART")
}

func TestESP32S2PostConnectSecureMode(t *testing.T) {
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

	err := esp32s2PostConnect(f)
	require.NoError(t, err, "should gracefully handle read error")
	assert.False(t, f.usesUSB, "should default to non-USB on read error")
}

func TestESP32S2MAC(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			switch addr {
			case esp32s2EfuseBlock1Word0:
				return 0x60559ff7, nil
			case esp32s2EfuseBlock1Word0 + 4:
				return 0x00002ca2, nil
			}
			return 0, nil
		},
	}
	f := &Flasher{conn: mc, chip: defESP32S2}

	mac, err := f.MAC()
	require.NoError(t, err)
	assert.Equal(t, "2c:a2:60:55:9f:f7", mac.String())
}

func TestESP32S2ChipRevision(t *testing.T) {
	tests := []struct {
		name  string
		word3 uint32
		word4 uint32
		want  ChipRevision
	}{
		{"v0.0", 0x00000000, 0x00000000, ChipRevision{0, 0}},
		{"v1.1", 1 << 18, 1 << 4, ChipRevision{1, 1}},
		{"max", (3 << 18) | (1 << 20), 0x7 << 4, ChipRevision{3, 15}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockConnection{
				readRegFunc: func(addr uint32) (uint32, error) {
					switch addr {
					case esp32s2EfuseBlock1Word3:
						return tt.word3, nil
					case esp32s2EfuseBlock1Word4:
						return tt.word4, nil
					}
					return 0, nil
				},
			}
			f := &Flasher{conn: mc, chip: defESP32S2}
			got, err := f.ChipRevision()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestESP32S2ChipFeatures(t *testing.T) {
	tests := []struct {
		name        string
		word3       uint32
		block2word4 uint32
		want        []string
	}{
		{
			"defaults",
			0, 0,
			[]string{"Wi-Fi", "Single Core", "240MHz", "No Embedded Flash", "No Embedded PSRAM", "No calibration in BLK2 of efuse"},
		},
		{
			"4MB flash, 4MB psram, V2 calib",
			(2 << 21) | (2 << 28), 2 << 4,
			[]string{"Wi-Fi", "Single Core", "240MHz", "Embedded Flash 4MB", "Embedded PSRAM 4MB", "ADC and temperature sensor calibration in BLK2 of eFuse V2"},
		},
		{
			"unknown values",
			(0xF << 21) | (0xF << 28), 0x7 << 4,
			[]string{"Wi-Fi", "Single Core", "240MHz", "Unknown Embedded Flash", "Unknown Embedded PSRAM", "Unknown calibration in BLK2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockConnection{
				readRegFunc: func(addr uint32) (uint32, error) {
					switch addr {
					case esp32s2EfuseBlock1Word3:
						return tt.word3, nil
					case esp32s2EfuseBlock2Word4:
						return tt.block2word4, nil
					}
					return 0, nil
				},
			}
			f := &Flasher{conn: mc, chip: defESP32S2}
			got, err := f.ChipFeatures()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestESP32S2ReadErrorsPropagate(t *testing.T) {
	newF := func(readReg func(addr uint32) (uint32, error)) *Flasher {
		return &Flasher{conn: &mockConnection{readRegFunc: readReg}, chip: defESP32S2}
	}
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32s2EfuseBlock1Word0, esp32s2EfuseBlock1Word0 + 4},
		func(f *Flasher) error { _, err := f.MAC(); return err })
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32s2EfuseBlock1Word3, esp32s2EfuseBlock1Word4},
		func(f *Flasher) error { _, err := f.ChipRevision(); return err })
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32s2EfuseBlock1Word3, esp32s2EfuseBlock2Word4},
		func(f *Flasher) error { _, err := f.ChipFeatures(); return err })
}

// TestESP32S2HardResetWatchdogSequence verifies that, when USB-OTG was
// detected and the strap/force-download gate is clear, esp32s2HardReset
// issues the exact unlock->config1->config0->relock RTC watchdog sequence
// esptool's ESP32S2ROM.watchdog_reset() uses.
func TestESP32S2HardResetWatchdogSequence(t *testing.T) {
	var writes []struct {
		addr, value uint32
	}
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			switch addr {
			case esp32s2GPIOStrapReg, esp32s2RTCCntlOption1Reg:
				return 0, nil // strap clear, force-download not set
			}
			return 0, nil
		},
		writeRegFunc: func(addr, value, mask, delayUS uint32) error {
			writes = append(writes, struct{ addr, value uint32 }{addr, value})
			return nil
		},
	}
	f := &Flasher{conn: mc, opts: &FlasherOptions{}, usesUSB: true}

	ok := esp32s2HardReset(f)
	require.True(t, ok, "watchdog reset should be taken when gate is clear")

	require.Len(t, writes, 4, "expected unlock, config1, config0, relock writes")
	assert.Equal(t, esp32s2RTCCntlWDTWProtect, writes[0].addr, "write 1: unlock WDT")
	assert.Equal(t, esp32s2RTCCntlWDTWKey, writes[0].value)
	assert.Equal(t, esp32s2RTCCntlWDTConfig1, writes[1].addr, "write 2: set WDT timeout")
	assert.Equal(t, esp32s2WDTConfig1TimeoutTicks, writes[1].value)
	assert.Equal(t, esp32s2RTCCntlWDTConfig0, writes[2].addr, "write 3: enable WDT")
	assert.Equal(t, esp32s2WDTConfig0EnableValue, writes[2].value)
	assert.Equal(t, esp32s2RTCCntlWDTWProtect, writes[3].addr, "write 4: relock WDT")
	assert.Equal(t, uint32(0), writes[3].value)
}

// TestESP32S2HardResetNotUSB verifies esp32s2HardReset falls back
// (returns false, issues no writes) when USB-OTG was not detected.
func TestESP32S2HardResetNotUSB(t *testing.T) {
	mc := &mockConnection{
		writeRegFunc: func(addr, value, mask, delayUS uint32) error {
			t.Fatalf("unexpected write to 0x%08X", addr)
			return nil
		},
	}
	f := &Flasher{conn: mc, opts: &FlasherOptions{}, usesUSB: false}

	assert.False(t, esp32s2HardReset(f), "should fall back when not USB-OTG")
}

// TestESP32S2HardResetStrapGateBlocksWatchdog verifies that when the strap
// register indicates the chip is strapped for download boot, esp32s2HardReset
// falls back to the DTR/RTS path instead of arming the watchdog.
func TestESP32S2HardResetStrapGateBlocksWatchdog(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			if addr == esp32s2GPIOStrapReg {
				return esp32s2GPIOStrapSPIBootMask, nil
			}
			return 0, nil
		},
		writeRegFunc: func(addr, value, mask, delayUS uint32) error {
			t.Fatalf("unexpected write to 0x%08X", addr)
			return nil
		},
	}
	f := &Flasher{conn: mc, opts: &FlasherOptions{}, usesUSB: true}

	assert.False(t, esp32s2HardReset(f), "should fall back when strapped for download boot")
}

// TestESP32S2HardResetForceDownloadGateBlocksWatchdog verifies that when
// RTC_CNTL_OPTION1 indicates download mode is force-enabled,
// esp32s2HardReset falls back to the DTR/RTS path instead of arming the
// watchdog.
func TestESP32S2HardResetForceDownloadGateBlocksWatchdog(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			if addr == esp32s2RTCCntlOption1Reg {
				return esp32s2RTCCntlForceDownloadBoot, nil
			}
			return 0, nil
		},
		writeRegFunc: func(addr, value, mask, delayUS uint32) error {
			t.Fatalf("unexpected write to 0x%08X", addr)
			return nil
		},
	}
	f := &Flasher{conn: mc, opts: &FlasherOptions{}, usesUSB: true}

	assert.False(t, esp32s2HardReset(f), "should fall back when download boot is force-enabled")
}

// TestESP32S2HardResetGateReadError verifies esp32s2HardReset falls back
// when the strap/force-download registers cannot be read (e.g. secure
// download mode).
func TestESP32S2HardResetGateReadError(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			return 0, errors.New("register not readable")
		},
	}
	f := &Flasher{conn: mc, opts: &FlasherOptions{}, usesUSB: true}

	assert.False(t, esp32s2HardReset(f), "should fall back when gate registers are unreadable")
}

// TestESP32S2HardResetOption1ReadError verifies esp32s2HardReset falls back
// when the strap register read succeeds but the RTC_CNTL_OPTION1 read fails.
func TestESP32S2HardResetOption1ReadError(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			if addr == esp32s2RTCCntlOption1Reg {
				return 0, errors.New("register not readable")
			}
			return 0, nil // strap read succeeds
		},
	}
	f := &Flasher{conn: mc, opts: &FlasherOptions{}, usesUSB: true}

	assert.False(t, esp32s2HardReset(f), "should fall back when OPTION1 register is unreadable")
}

// TestESP32S2HardResetWriteError verifies esp32s2HardReset returns false when
// a WriteRegister call inside esp32s2WatchdogReset fails (e.g. the write to
// WDTConfig0), so Reset() falls through to the DTR/RTS fallback path. This is
// the feature's core safety contract: a failed watchdog arm must not be
// silently treated as success.
func TestESP32S2HardResetWriteError(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			return 0, nil // strap clear, force-download not set
		},
		writeRegFunc: func(addr, value, mask, delayUS uint32) error {
			if addr == esp32s2RTCCntlWDTConfig0 {
				return errors.New("write failed")
			}
			return nil
		},
	}
	f := &Flasher{conn: mc, opts: &FlasherOptions{}, usesUSB: true}

	assert.False(t, esp32s2HardReset(f), "should fall back when a watchdog register write fails")
}
