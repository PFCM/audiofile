package wav

import (
	"encoding/binary"
	"io"

	"github.com/pfcm/audiofile/riff"
)

// Writer writes wav files.
type Writer struct {
	w *riff.Writer
}

// NewWriter initialises a wav writer.
func NewWriter(ws io.WriteSeeker, format Format) (*Writer, error) {
	rw, err := riff.NewWriter(ws, "WAVE")
	if err != nil {
		return nil, err
	}
	// Write the fmt chunk.
	// TODO: actually put the values in the fmt chunk
	fc := fmtChunk{format: format}
	wc, err := rw.NewChunk("fmt ")
	if err != nil {
		return nil, err
	}
	if err := writeFmtChunk(wc, fc); err != nil {
		return nil, err
	}
	if err := wc.Close(); err != nil {
		return nil, err
	}
	return &Writer{w: rw}, nil
}

// TODO: scratch buffer?
func writeFmtChunk(w io.Writer, fc fmtChunk) error {
	scratch := make([]byte, 0, 16)

	put16 := func(u uint16) {
		scratch = binary.LittleEndian.AppendUint16(scratch, u)
	}
	put32 := func(u uint32) {
		scratch = binary.LittleEndian.AppendUint32(scratch, u)
	}

	put16(uint16(fc.format))
	put16(fc.channels)
	put32(fc.sampleRate)
	put32(fc.dataRate)
	put16(fc.blockAlign)
	put16(fc.bitsPerSample)
	switch fc.format {
	case PCM:
		// it is done.
	case ALaw, MuLaw:
		put16(0)
	case Extensible:
		put16(22)
		put16(fc.validBitsPerSample)
		put32(fc.channelMask)
		put16(uint16(fc.subFormat))
		// Add the magic string
		scratch = append(scratch, fmtMagic[:]...)
	}
	_, err := w.Write(scratch)
	return err
}
