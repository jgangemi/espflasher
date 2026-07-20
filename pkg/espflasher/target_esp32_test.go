package espflasher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestESP32MAC(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			switch addr {
			case esp32EfuseWord1:
				return 0x60559ff7, nil
			case esp32EfuseWord2:
				return 0x00002ca2, nil
			}
			return 0, nil
		},
	}
	f := &Flasher{conn: mc, chip: defESP32}

	mac, err := f.MAC()
	require.NoError(t, err)
	assert.Equal(t, "2c:a2:60:55:9f:f7", mac.String())
}

// TestESP32ChipRevision covers every entry (and one unlisted gap value) of
// esptool's combine-value -> major-revision lookup table, plus the minor
// version bitfield.
func TestESP32ChipRevision(t *testing.T) {
	tests := []struct {
		name       string
		word3      uint32
		word5      uint32
		apbCtlDate uint32
		want       ChipRevision
	}{
		{"combine=0 -> major 0", 0, 0, 0, ChipRevision{0, 0}},
		{"combine=1 (bit0) -> major 1", 1 << 15, 0, 0, ChipRevision{1, 0}},
		{"combine=3 (bit0+bit1) -> major 2", 1 << 15, 1 << 20, 0, ChipRevision{2, 0}},
		{"combine=7 (all bits) -> major 3", 1 << 15, 1 << 20, 1 << 31, ChipRevision{3, 0}},
		{"combine=2 (bit1 only, unlisted) -> major 0", 0, 1 << 20, 0, ChipRevision{0, 0}},
		{"combine=4 (bit2 only, unlisted) -> major 0", 0, 0, 1 << 31, ChipRevision{0, 0}},
		{"minor=3", 0, 3 << 24, 0, ChipRevision{0, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockConnection{
				readRegFunc: func(addr uint32) (uint32, error) {
					switch addr {
					case esp32EfuseWord3:
						return tt.word3, nil
					case esp32EfuseWord5:
						return tt.word5, nil
					case esp32APBCtlDateReg:
						return tt.apbCtlDate, nil
					}
					return 0, nil
				},
			}
			f := &Flasher{conn: mc, chip: defESP32}
			got, err := f.ChipRevision()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestESP32ReadErrorsPropagate(t *testing.T) {
	newF := func(readReg func(addr uint32) (uint32, error)) *Flasher {
		return &Flasher{conn: &mockConnection{readRegFunc: readReg}, chip: defESP32}
	}
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32EfuseWord1, esp32EfuseWord2},
		func(f *Flasher) error { _, err := f.MAC(); return err })
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32EfuseWord3, esp32EfuseWord5, esp32APBCtlDateReg},
		func(f *Flasher) error { _, err := f.ChipRevision(); return err })
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32EfuseWord3, esp32EfuseWord4, esp32EfuseWord6},
		func(f *Flasher) error { _, err := f.ChipFeatures(); return err })
}

func TestESP32ChipFeatures(t *testing.T) {
	tests := []struct {
		name  string
		word3 uint32
		word4 uint32
		word6 uint32
		want  []string
	}{
		{
			"minimal: BT disabled, single core, no rated freq, pkg 0, no vref, no blk3, coding none",
			(1 << 1) | (1 << 0), 0, 0,
			[]string{"Wi-Fi", "Single Core + LP Core", "Coding Scheme None"},
		},
		{
			"BT enabled, dual core, 240MHz, embedded flash+psram (pkg6), vref, blk3, coding 3/4",
			(6<<9 | 0<<2) | (1 << 13) | (1 << 14), // pkg_version=6 via bits[11:9]; freq rated; blk3
			1 << 8,                                // adc_vref nonzero
			1,                                     // coding scheme 1
			[]string{"Wi-Fi", "BT", "Dual Core + LP Core", "240MHz", "Embedded Flash", "Embedded PSRAM", "Vref calibration in eFuse", "BLK3 partially reserved", "Coding Scheme 3/4"},
		},
		{
			"160MHz, pkg 2 (flash only), coding 2 (repeat, unsupported)",
			(2<<9 | 0<<2) | (1 << 13) | (1 << 12),
			0,
			2,
			[]string{"Wi-Fi", "BT", "Dual Core + LP Core", "160MHz", "Embedded Flash", "Coding Scheme Repeat (UNSUPPORTED)"},
		},
		{
			"coding 3 (none, may contain encoding data)",
			(1 << 1) | (1 << 0), // BT disabled (bit1), single core (bit0)
			0,
			3,
			[]string{"Wi-Fi", "Single Core + LP Core", "Coding Scheme None (may contain encoding data)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockConnection{
				readRegFunc: func(addr uint32) (uint32, error) {
					switch addr {
					case esp32EfuseWord3:
						return tt.word3, nil
					case esp32EfuseWord4:
						return tt.word4, nil
					case esp32EfuseWord6:
						return tt.word6, nil
					}
					return 0, nil
				},
			}
			f := &Flasher{conn: mc, chip: defESP32}
			got, err := f.ChipFeatures()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
