package espflasher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestESP32C2MAC(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			switch addr {
			case esp32c2EfuseBlock2Word0:
				return 0x60559ff7, nil
			case esp32c2EfuseBlock2Word0 + 4:
				return 0x00002ca2, nil
			}
			return 0, nil
		},
	}
	f := &Flasher{conn: mc, chip: defESP32C2}

	mac, err := f.MAC()
	require.NoError(t, err)
	assert.Equal(t, "2c:a2:60:55:9f:f7", mac.String())
}

func TestESP32C2ChipRevision(t *testing.T) {
	tests := []struct {
		name  string
		word1 uint32
		want  ChipRevision
	}{
		{"v0.0", 0x00000000, ChipRevision{0, 0}},
		{"v1.1", (1 << 20) | (1 << 16), ChipRevision{1, 1}},
		{"max", (3 << 20) | (0xF << 16), ChipRevision{3, 15}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockConnection{
				readRegFunc: func(addr uint32) (uint32, error) {
					if addr == esp32c2EfuseBlock2Word1 {
						return tt.word1, nil
					}
					return 0, nil
				},
			}
			f := &Flasher{conn: mc, chip: defESP32C2}
			got, err := f.ChipRevision()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestESP32C2ReadErrorsPropagate(t *testing.T) {
	newF := func(readReg func(addr uint32) (uint32, error)) *Flasher {
		return &Flasher{conn: &mockConnection{readRegFunc: readReg}, chip: defESP32C2}
	}
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32c2EfuseBlock2Word0, esp32c2EfuseBlock2Word0 + 4},
		func(f *Flasher) error { _, err := f.MAC(); return err })
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32c2EfuseBlock2Word1},
		func(f *Flasher) error { _, err := f.ChipRevision(); return err })
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32c2EfuseBlock2Word7},
		func(f *Flasher) error { _, err := f.ChipFeatures(); return err })
}

func TestESP32C2ChipFeatures(t *testing.T) {
	tests := []struct {
		name  string
		word7 uint32
		want  []string
	}{
		{"no flash", 0, []string{"Wi-Fi", "BT 5 (LE)", "Single Core", "120MHz"}},
		{"4MB XMC", (1 << 29) | (1 << 24), []string{"Wi-Fi", "BT 5 (LE)", "Single Core", "120MHz", "Embedded Flash 4MB (XMC)"}},
		{"1MB unknown vendor", (3 << 29), []string{"Wi-Fi", "BT 5 (LE)", "Single Core", "120MHz", "Embedded Flash 1MB ()"}},
		{"unknown cap", (7 << 29), []string{"Wi-Fi", "BT 5 (LE)", "Single Core", "120MHz", "Unknown Embedded Flash ()"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockConnection{
				readRegFunc: func(addr uint32) (uint32, error) {
					if addr == esp32c2EfuseBlock2Word7 {
						return tt.word7, nil
					}
					return 0, nil
				},
			}
			f := &Flasher{conn: mc, chip: defESP32C2}
			got, err := f.ChipFeatures()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
