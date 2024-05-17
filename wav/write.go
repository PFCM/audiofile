package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/pfcm/audiofile/riff"
)

// FileFormat holds all of the information needed to fully describe how to
// understand the samples in a wav file.
type FileFormat struct {
	// Format is the format of a single sample.
	Format Format
	// BitDepth is how many bits used to describe each sample.
	BitDepth int
	// Channels is the number of audio channels in the file.
	Channels int
	// SampleRate is the number of samples to play per second.
	SampleRate int
}

func (ff FileFormat) chunk() (fmtChunk, error) {
	bytesPerSample := max(8, ff.BitDepth) / 8
	return fmtChunk{
		format:     ff.Format,
		channels:   uint16(ff.Channels),
		sampleRate: uint32(ff.SampleRate),
		// TODO: not correct for weird bit depths
		dataRate:      uint32(bytesPerSample * ff.Channels * ff.SampleRate),
		blockAlign:    uint16(bytesPerSample * ff.Channels),
		bitsPerSample: uint16(ff.BitDepth),
	}, nil
}

// Writer writes wav files.
type Writer struct {
	fmt fmtChunk
	w   *riff.Writer
	// dc is the data chunk, where the samples are actually written.
	dc io.WriteCloser

	scratch []byte
}

// NewWriter initialises a wav writer.
func NewWriter(ws io.WriteSeeker, ff FileFormat) (*Writer, error) {
	fc, err := ff.chunk()
	if err != nil {
		return nil, err
	}
	return newWriter(ws, fc)
}

func newWriter(ws io.WriteSeeker, fc fmtChunk) (*Writer, error) {
	rw, err := riff.NewWriter(ws, "WAVE")
	if err != nil {
		return nil, err
	}
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
	dc, err := rw.NewChunk("data")
	if err != nil {
		return nil, err
	}
	return &Writer{
		fmt: fc,
		w:   rw,
		dc:  dc,
	}, nil
}

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

func (w *Writer) format() Format {
	if w.fmt.format == Extensible {
		return w.fmt.subFormat
	}
	return w.fmt.format
}

// Write implements io.Writer, writing raw bytes to the data chunk. These should
// represent samples in the appropriate format for the file, converted to bytes
// in interleaved. Usually it will be easier to use one of the other write
// methods.
func (w *Writer) Write(p []byte) (int, error) {
	if w.dc == nil {
		return 0, errors.New("Write called after Close")
	}
	return w.dc.Write(p)
}

// Write8PCM writes the provided 8 bit PCM samples to the file, converting to
// the file's format if necessary. The first index of the provided samples
// should have a slice per channel (the first index) and each channel should
// have the same number of samples. Returns the number of bytes eventually
// written to the file.
func (w *Writer) Write8PCM(samples [][]byte) (int, error) {
	if err := w.checkChannels(len(samples)); err != nil {
		return 0, err
	}
	var appendSample func([]byte, byte) []byte
	switch f := w.format(); f {
	case PCM:
		switch bd := w.fmt.bitsPerSample; {
		case bd <= 8:
			// :)
			appendSample = func(bs []byte, b byte) []byte {
				return append(bs, b)
			}
		case bd <= 16:
			// convert to signed and extend.
			appendSample = func(bs []byte, b byte) []byte {
				i := int16(b) - 128
				i <<= 8
				return binary.LittleEndian.AppendUint16(bs, uint16(i))
			}
		default:
			return 0, fmt.Errorf("writing 8 bit PCM -> %d bit PCM not implemented", bd)
		}
	default:
		return 0, fmt.Errorf("writing 8 bit PCM -> %v not implemented", f)
	}
	// TODO: actually reuse a scratch buffer.
	return writeSamples(w, w.scratch, samples, appendSample)
}

// Write16PCM writes the provided 16 bit PCM samples to the file, converting to
// the file's format if necessary. The first index of the provided samples
// should have a slice per channel (the first index) and each channel should
// have the same number of samples. Returns the number of bytes eventually
// written to the file.
func (w *Writer) Write16PCM(samples [][]int16) (int, error) {
	if err := w.checkChannels(len(samples)); err != nil {
		return 0, err
	}
	var appendSample func([]byte, int16) []byte
	switch f := w.format(); f {
	case PCM:
		switch bd := w.fmt.bitsPerSample; {
		case bd <= 8:
		case bd <= 16:
			appendSample = func(bs []byte, i int16) []byte {
				return binary.LittleEndian.AppendUint16(bs, uint16(i))
			}
		default:
			return 0, fmt.Errorf("writing 16 bit PCM -> %v bit PCM not implemented", bd)
		}
	default:
		return 0, fmt.Errorf("writing 16 bit PCM -> %v not implemented", f)
	}
	return writeSamples(w, w.scratch, samples, appendSample)
}

func (w *Writer) checkChannels(channels int) error {
	if channels == int(w.fmt.channels) {
		return nil
	}
	return fmt.Errorf("wrong number of channels %d: expect %d", channels, w.fmt.channels)
}

func writeSamples[T any](
	w io.Writer,
	scratch []byte,
	samples [][]T,
	appendSample func([]byte, T) []byte,
) (int, error) {
	scratch = scratch[:0]
	for i := range samples[0] {
		for c := range samples {
			scratch = appendSample(scratch, samples[c][i])
		}
	}
	return w.Write(scratch)
}

// Close finalises the file.
func (w *Writer) Close() error {
	if err := w.dc.Close(); err != nil {
		return err
	}
	w.dc = nil
	if err := w.w.Close(); err != nil {
		return err
	}
	return nil
}
