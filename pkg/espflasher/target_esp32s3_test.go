package espflasher

import (
	"bytes"
	"testing"
)

func TestESP32S3PostConnectUSBJTAG(t *testing.T) {
	var buf bytes.Buffer
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			if addr == esp32s3UARTDevBufNo {
				return esp32s3UARTDevBufNoUSBJTAGSerial, nil
			}
			// Return 0 for SWD conf read
			return 0, nil
		},
		writeRegFunc: func(addr, value, mask, delayUS uint32) error {
			return nil
		},
	}
	f := &Flasher{
		conn: mc,
		opts: &FlasherOptions{Logger: &StdoutLogger{W: &buf}},
	}

	err := esp32s3PostConnect(f)
	if err != nil {
		t.Fatalf("esp32s3PostConnect() error: %v", err)
	}
	if !f.usesUSB {
		t.Error("usesUSB should be true for USB-JTAG/Serial")
	}
}

func TestESP32S3PostConnectUSBOTG(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			if addr == esp32s3UARTDevBufNo {
				return esp32s3UARTDevBufNoUSBOTG, nil
			}
			return 0, nil
		},
	}
	f := &Flasher{
		conn: mc,
		opts: &FlasherOptions{},
	}

	err := esp32s3PostConnect(f)
	if err != nil {
		t.Fatalf("esp32s3PostConnect() error: %v", err)
	}
	if !f.usesUSB {
		t.Error("usesUSB should be true for USB-OTG")
	}
}

func TestESP32S3PostConnectUART(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			return 0, nil // Not USB
		},
	}
	f := &Flasher{
		conn: mc,
		opts: &FlasherOptions{},
	}

	err := esp32s3PostConnect(f)
	if err != nil {
		t.Fatalf("esp32s3PostConnect() error: %v", err)
	}
	if f.usesUSB {
		t.Error("usesUSB should be false for UART")
	}
}

func TestESP32S3MAC(t *testing.T) {
	mc := &mockConnection{
		readRegFunc: func(addr uint32) (uint32, error) {
			switch addr {
			case esp32s3EfuseBlock1Word0:
				return 0x60559ff7, nil
			case esp32s3EfuseBlock1Word0 + 4:
				return 0x00002ca2, nil
			}
			return 0, nil
		},
	}
	f := &Flasher{conn: mc, chip: defESP32S3}

	mac, err := f.MAC()
	if err != nil {
		t.Fatalf("MAC() error: %v", err)
	}
	if want := "2c:a2:60:55:9f:f7"; mac.String() != want {
		t.Errorf("MAC() = %s, want %s", mac, want)
	}
}

func TestESP32S3ChipRevision(t *testing.T) {
	tests := []struct {
		name        string
		word3       uint32
		word5       uint32
		block2word4 uint32
		want        ChipRevision
	}{
		{
			name:  "v0.0 non-eco0 path (blk_ver 1.0)",
			word3: 0, word5: 0, block2word4: 1,
			want: ChipRevision{0, 0},
		},
		{
			name:  "v1.1",
			word3: 1 << 18, word5: 1 << 24, block2word4: 0,
			want: ChipRevision{1, 1},
		},
		{
			name: "eco0 override: raw minor low3=0, blk_ver 1.1 -> forced v0.0",
			// raw_minor low bits (word3>>18)&0x7 = 0, hi (word5>>23)&1 = 0 -> raw_minor = 0
			// blk_ver_major (block2word4&0x3) = 1, blk_ver_minor ((word3>>24)&0x7) = 1
			word3: 1 << 24, word5: 2 << 24 /* raw_major=2, irrelevant once eco0 fires */, block2word4: 1,
			want: ChipRevision{0, 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockConnection{
				readRegFunc: func(addr uint32) (uint32, error) {
					switch addr {
					case esp32s3EfuseBlock1Word3:
						return tt.word3, nil
					case esp32s3EfuseBlock1Word5:
						return tt.word5, nil
					case esp32s3EfuseBlock2Word4:
						return tt.block2word4, nil
					}
					return 0, nil
				},
			}
			f := &Flasher{conn: mc, chip: defESP32S3}
			got, err := f.ChipRevision()
			if err != nil {
				t.Fatalf("ChipRevision() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ChipRevision() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestESP32S3ReadErrorsPropagate(t *testing.T) {
	newF := func(readReg func(addr uint32) (uint32, error)) *Flasher {
		return &Flasher{conn: &mockConnection{readRegFunc: readReg}, chip: defESP32S3}
	}
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32s3EfuseBlock1Word0, esp32s3EfuseBlock1Word0 + 4},
		func(f *Flasher) error { _, err := f.MAC(); return err })
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32s3EfuseBlock1Word3, esp32s3EfuseBlock1Word5, esp32s3EfuseBlock2Word4},
		func(f *Flasher) error { _, err := f.ChipRevision(); return err })
	assertRegisterErrorsPropagate(t, newF, []uint32{esp32s3EfuseBlock1Word3, esp32s3EfuseBlock1Word4, esp32s3EfuseBlock1Word5},
		func(f *Flasher) error { _, err := f.ChipFeatures(); return err })
}

func TestESP32S3ChipFeatures(t *testing.T) {
	tests := []struct {
		name  string
		word3 uint32
		word4 uint32
		word5 uint32
		want  []string
	}{
		{
			"no flash or psram",
			0, 0, 0,
			[]string{"Wi-Fi", "BT 5 (LE)", "Dual Core + LP Core", "240MHz"},
		},
		{
			"8MB flash TT, 2MB psram AP_1v8",
			1 << 27, (2 << 7) | (2 << 3) | 4, 0,
			[]string{"Wi-Fi", "BT 5 (LE)", "Dual Core + LP Core", "240MHz", "Embedded Flash 8MB (TT)", "Embedded PSRAM 2MB (AP_1v8)"},
		},
		{
			"unknown flash/psram caps",
			6 << 27, (3 << 3) | 7, 1 << 19,
			[]string{"Wi-Fi", "BT 5 (LE)", "Dual Core + LP Core", "240MHz", "Unknown Embedded Flash ()", "Unknown Embedded PSRAM ()"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockConnection{
				readRegFunc: func(addr uint32) (uint32, error) {
					switch addr {
					case esp32s3EfuseBlock1Word3:
						return tt.word3, nil
					case esp32s3EfuseBlock1Word4:
						return tt.word4, nil
					case esp32s3EfuseBlock1Word5:
						return tt.word5, nil
					}
					return 0, nil
				},
			}
			f := &Flasher{conn: mc, chip: defESP32S3}
			got, err := f.ChipFeatures()
			if err != nil {
				t.Fatalf("ChipFeatures() error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ChipFeatures() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ChipFeatures()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
