package wav

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func cat(bs ...[]byte) []byte {
	if len(bs) == 0 {
		return nil
	}
	b := bs[0]
	for _, c := range bs[1:] {
		b = append(b, c...)
	}
	return b
}

func uint16le(i uint16) []byte {
	return binary.LittleEndian.AppendUint16(nil, i)
}

func uint32le(i uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, i)
}

func mkSubformat(f Format) []byte {
	return append(binary.LittleEndian.AppendUint16(nil, uint16(f)),
		0x0, 0x0, 0x0, 0x0, 0x10, 0x0, 0x80, 0x0,
		0x0, 0xAA, 0x0, 0x38, 0x9B, 0x71,
	)
}

func TestReadFmtChunk(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []byte
		out  *fmtChunk
	}{{
		name: "normal PCM",
		in: cat(
			// format number 1
			uint16le(uint16(PCM)),
			// 2 channels
			uint16le(2),
			// sample rate 44100
			uint32le(44100),
			// data rate, bytes per sec(?)
			uint32le(44100*2*2),
			// blockAlign, size of data blocks
			uint16le(2048),
			// bits per sample
			uint16le(16),
		),
		out: &fmtChunk{
			format:        PCM,
			channels:      2,
			sampleRate:    44100,
			dataRate:      44100 * 2 * 2,
			blockAlign:    2048,
			bitsPerSample: 16,
		},
	}, {
		name: "normal mu-law",
		in: cat(
			// format
			uint16le(uint16(MuLaw)),
			// 1 channel
			uint16le(1),
			// sample rate 22050
			uint32le(22050),
			// data rate is the same
			uint32le(22050),
			// blockAlign, set it to something
			uint16le(4096),
			// bits per sample must be 8 for the log encodings
			uint16le(8),
			// the extension size is set, to 0
			uint16le(0),
		),
		out: &fmtChunk{
			format:        MuLaw,
			channels:      1,
			sampleRate:    22050,
			dataRate:      22050,
			blockAlign:    4096,
			bitsPerSample: 8,
		},
	}, {
		name: "a-law no extension size",
		in: cat(
			uint16le(uint16(ALaw)),
			uint16le(1),
			uint32le(22050),
			uint32le(22050),
			uint16le(4096),
			uint16le(8),
		),
		out: nil,
	}, {
		name: "a-law wrong bits per sample",
		in: cat(
			uint16le(uint16(ALaw)),
			uint16le(1),
			uint32le(22050),
			uint32le(22050),
			uint16le(4096),
			uint16le(16),
			uint16le(0),
		),
		out: nil,
	}, {
		name: "valid extensible",
		in: cat(
			uint16le(uint16(Extensible)),
			uint16le(3),
			uint32le(48000),
			uint32le(48000*3*3),
			uint16le(2048*3),
			uint16le(32),
			// extension is 22 bytes.
			uint16le(22),
			uint16le(24),
			uint32le(0),
			mkSubformat(PCM),
		),
		out: &fmtChunk{
			format:             Extensible,
			channels:           3,
			sampleRate:         48000,
			dataRate:           48000 * 3 * 3,
			blockAlign:         2048 * 3,
			bitsPerSample:      32,
			validBitsPerSample: 24,
			channelMask:        0,
			subFormat:          PCM,
		},
	}, {
		name: "invalid subformat",
		in: cat(
			uint16le(uint16(Extensible)),
			uint16le(3),
			uint32le(48000),
			uint32le(48000*3*3),
			uint16le(2048*3),
			uint16le(32),
			// extension is 22 bytes.
			uint16le(22),
			uint16le(24),
			uint32le(0),
			mkSubformat(Extensible),
		),
	}, {
		name: "unknown subformat",
		in: cat(
			uint16le(uint16(Extensible)),
			uint16le(3),
			uint32le(48000),
			uint32le(48000*3*3),
			uint16le(2048*3),
			uint16le(32),
			// extension is 22 bytes.
			uint16le(22),
			uint16le(24),
			uint32le(0),
			mkSubformat(Format(0xff)),
		),
	}, {
		name: "far too small",
		in:   []byte{0, 2},
		out:  nil,
	}} {
		t.Run(c.name, func(t *testing.T) {
			r := bytes.NewReader(c.in)
			fc, err := readFmtChunk(r)
			if err != nil {
				if c.out != nil {
					t.Fatalf("unexpected error\nwant: %+v\n got: %v", *c.out, err)
				}
				return
			}
			if fc != *c.out {
				t.Fatalf("fmt mismatch:\nwant: %+v\n got: %+v", *c.out, fc)
			}
		})
	}
}

func TestRead(t *testing.T) {
	files, err := fs.Glob(os.DirFS("../testdata"), "*")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("../testdata", name)
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("opening file: %v", err)
			}
			defer f.Close()
			r, err := NewReader(f)
			if err != nil {
				t.Fatalf("opening WAV reader: %v", err)
			}
			samples, err := ReadFull8PCM(r)

			t.Fatal(samples, len(samples), len(samples[0]))

			t.Fatalf("format: %v, samplerate: %d, bit depth: %d, channels: %d, %d samples",
				r.Format(), r.Samplerate(), r.BitDepth(), r.Channels(), r.Samples())
		})
	}
}
