// package wav reads and writes wav files
package wav

//go:generate stringer -type=Format

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/pfcm/audiofile/riff"
)

// Format are the allowed formats for samples in a wav file.
type Format uint16

const (
	// PCM is linear, integer samples. If the bit size is < 8 they are
	// unsigned and offset to the midpoint of the range. Otherwise they are
	// signed with a normal twos complement representation.
	PCM Format = 0x0001
	// IEEEFloat is a floating point between -1 and 1 (maybe?), with either
	// 32 or 64 bits per sample.
	IEEEFloat Format = 0x0003
	// ALaw is an 8 bit log PCM format.
	ALaw Format = 0x0006
	// MuLaw is an 8 bit log PCM format.
	MuLaw Format = 0x0007
	// Extensible is used when the file has: PCM data with more than 16 bits
	// per sample, the number of bits per sample is different from the size
	// of the data blocks divided by the number of channels, there are more
	// than two channels or the mapping from channels to loudspeakers is
	// explicitly specified.
	Extensible Format = 0xFFFE
)

// fmtChunk holds all the data from the fmt chunk.
type fmtChunk struct {
	format             Format
	channels           uint16
	sampleRate         uint32
	dataRate           uint32 // bytes per second
	blockAlign         uint16 // data block size, in bytes
	bitsPerSample      uint16
	validBitsPerSample uint16 // optional, ignore if 0
	channelMask        uint32 // optional, probably ignored all the time anyway
	subFormat          Format // optional, and probably overly simplistic
}

var fmtMagic = [14]byte{0x0, 0x0, 0x0, 0x0, 0x10, 0x0, 0x80, 0, 0, 0xAA, 0, 0x38, 0x9B, 0x71}

func readFmtChunk(r io.Reader) (fc fmtChunk, err error) {
	// any eof is an unexpected eof.
	defer func() {
		if err == io.EOF {
			err = errors.New("unexpected EOF")
		}
	}()

	var scratch [16]byte
	// It must be at least 16 bytes long, so read the first 16 bytes.
	if _, err := io.ReadFull(r, scratch[:]); err != nil {
		return fmtChunk{}, err
	}

	raw := scratch[:]
	get16 := func() uint16 {
		x := binary.LittleEndian.Uint16(raw)
		raw = raw[2:]
		return x
	}
	get32 := func() uint32 {
		x := binary.LittleEndian.Uint32(raw)
		raw = raw[4:]
		return x
	}
	// Fill in the mandatory fields.
	fc.format = Format(get16()) // 2
	fc.channels = get16()       // 4
	fc.sampleRate = get32()     // 8
	fc.dataRate = get32()       // 12
	fc.blockAlign = get16()     // 14
	fc.bitsPerSample = get16()  // 16
	// Check if the extended fields should be set.
	switch fc.format {
	case PCM:
		return fc, nil
	case ALaw, MuLaw:
		// There should be two more bytes, holding a zero.
		var cb [2]byte
		if _, err := io.ReadFull(r, cb[:]); err != nil {
			return fmtChunk{}, err
		}
		if size := binary.LittleEndian.Uint16(cb[:]); size != 0 {
			return fmtChunk{}, fmt.Errorf("format %s, expect cbSize 0, got %d", fc.format, size)
		}
		// bitsPerSample must be 8.
		if fc.bitsPerSample != 8 {
			return fmtChunk{}, fmt.Errorf("format %s, expect 8 bits per sample, got %d", fc.format, fc.bitsPerSample)
		}
		return fc, nil
	case Extensible:
		// The size of the extension has to be 22, so read the next 24
		// bytes.
		var ext [24]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return fmtChunk{}, err
		}
		raw = ext[:]
		if size := get16(); size != 22 {
			return fmtChunk{}, fmt.Errorf("format %s, expect cbSize 22, got %d", fc.format, size)
		}
		fc.validBitsPerSample = get16()
		fc.channelMask = get32()
		// The first 2 bytes of the subformat are the actual format.
		fc.subFormat = Format(get16())
		// Validate the sub format
		switch fc.subFormat {
		case Extensible:
			return fmtChunk{}, errors.New("invalid subformat Extensible")
		default:
			return fmtChunk{}, fmt.Errorf("invalid/unknown subformat %d", fc.subFormat)
		case PCM, ALaw, MuLaw, IEEEFloat:
			// ok
		}
		// The remainder should a specific magic string.
		if !bytes.Equal(fmtMagic[:], raw) {
			return fmtChunk{}, fmt.Errorf("format %s, bad magic string (%x) in subformat", fc.format, raw)
		}
		return fc, nil
	}

	// default, unknown format
	return fmtChunk{}, fmt.Errorf("unknown wav format %d", fc.format)
}

// Reader reads audio from a wav file.
// TODO: a method to get the number of samples.
type Reader struct {
	r   *riff.Reader
	fmt fmtChunk
	// data is a reader into the data chunk of the file.
	data io.Reader
	// dataBytes is the total number of bytes in the data chunk.
	dataBytes int
	// scratch buffer to read raw bytes into before converting.
	scratch []byte
}

// NewReader reads validates the initial metadata of the files and returns a
// Reader, ready to read audio frames. It can make a lot of small reads, so
// passing in a bufio.Reader may be wise.
func NewReader(r io.Reader) (*Reader, error) {
	rr, err := riff.NewReader(r)
	if err != nil {
		return nil, err
	}
	if rr.Form != "WAVE" {
		return nil, fmt.Errorf("bad wav file form, expect WAVE, found: %q", rr.Form)
	}
	// TODO: we probably shouldn't assume the fmt chunk is always next?
	chunk, err := rr.ReadChunk()
	if err != nil {
		return nil, err
	}
	if chunk.Identifier != "fmt " {
		return nil, fmt.Errorf("expected fmt chunk, got %q", chunk.Identifier)
	}
	fc, err := readFmtChunk(chunk.Reader)
	if err != nil {
		return nil, err
	}
	// Find the data chunk.
	var data *riff.Chunk
	for {
		c, err := rr.ReadChunk()
		if err == io.EOF {
			return nil, errors.New("finding data chunk: unexpected EOF")
		}
		if err != nil {
			return nil, err
		}
		if c.Identifier == "data" {
			data = c
			break
		}
		// TODO: deal with fact chunk here
	}

	return &Reader{
		r:         rr,
		fmt:       fc,
		data:      data.Reader,
		dataBytes: data.Size,
	}, nil
}

// Format returns the sample format of the wav file. If the main format is
// Extensible, then this returns the subformat.
func (r *Reader) Format() Format {
	if r.fmt.format == Extensible {
		return r.fmt.subFormat
	}
	return r.fmt.format
}

// Samplerate returns the sample rate of the wav file.
func (r *Reader) Samplerate() int {
	return int(r.fmt.sampleRate)
}

// BitDepth returns the number of bits per sample in the file.
func (r *Reader) BitDepth() int {
	return int(r.fmt.bitsPerSample)
}

// Channels returns the number of channels in the audio file.
func (r *Reader) Channels() int {
	return int(r.fmt.channels)
}

// Samples returns the total number of samples per channel in the audio file.
func (r *Reader) Samples() int {
	return r.dataBytes / int(r.fmt.blockAlign)
}

// Read reads raw, undecoded, interleaved bytes from the data chunk.
func (r *Reader) Read(b []byte) (int, error) {
	return r.data.Read(b)
}

// Read8PCM reads and de-interleaves the data into the provided slice of slices.
// The channels are assumed to be the first index and all slices are assumed to
// be the same length. If the bit depth is > 8, or the format is not PCM samples
// are converted to linear and centered around 128. Returns the number of samples
// read (per channel).
func (r *Reader) Read8PCM(data [][]byte) (int, error) {
	// Figure out how to convert the data
	var nextSample func([]byte) (byte, []byte)
	switch f := r.Format(); f {
	case PCM:
		// It's already PCM, but we may have to reduce the bit depth.
		switch bd := r.BitDepth(); {
		case bd <= 8:

			// 1 byte samples, easy.
			nextSample = nextByte
		case bd <= 16:
			// 2 byte samples. They will be signed, so we need to
			// center them.
			nextSample = func(bs []byte) (byte, []byte) {
				i, bs := nextInt16(bs)
				return int16ToByte(i), bs
			}
		default:
			return 0, fmt.Errorf("bit depth %d -> byte not implemented", bd)
		}
	default:
		return 0, fmt.Errorf("format %v -> PCM not implemented", f)
	}
	return readInto(data, r, nextSample)
}

// Read16PCM fills the provided slices with PCM int16 data from the file.
func (r *Reader) Read16PCM(data [][]int16) (int, error) {
	var nextSample func([]byte) (int16, []byte)
	switch f := r.Format(); f {
	case PCM:
		switch bd := r.BitDepth(); {
		case bd <= 8:
			nextSample = func(bs []byte) (int16, []byte) {
				b, bs := nextByte(bs)
				// b is centered around 128, we need to make it
				// signed, center it around 0 and extend it to
				// the full scale.
				i := int16(b) - 128
				return i * (1 << 8), bs
			}
		case bd <= 16:
			// as-is
			nextSample = nextInt16
		default:
			return 0, fmt.Errorf("bit depth %d -> int16 not implemented", bd)
		}
	default:
		return 0, fmt.Errorf("format %v -> PCM not implementated", f)
	}
	return readInto(data, r, nextSample)
}

// Read32Float reads some of the data into 32 bit floats.
func (r *Reader) Read32Float(data [][]float32) (int, error) {
	var nextSample func([]byte) (float32, []byte)
	switch f := r.Format(); f {
	case PCM:
		switch bd := r.BitDepth(); {
		case bd <= 8:
			// 1 byte per sample
			const div float32 = 1.0 / 128
			nextSample = func(bs []byte) (float32, []byte) {
				s, bs := nextByte(bs)
				return float32(s)*div - 1, bs
			}
		case bd <= 16:
			// 2 bytes per sample
			const div float32 = 1.0 / float32(math.MaxInt16)
			nextSample = func(bs []byte) (float32, []byte) {
				i, bs := nextInt16(bs)
				return float32(i) * div, bs
			}
		default:
			return 0, fmt.Errorf("PCM bit depth %d -> float 32 not implemented", bd)
		}
	case IEEEFloat:
		switch bd := r.BitDepth(); {
		case bd <= 32:
			// 4 bytes per sample
			nextSample = nextFloat32
		case bd <= 64:
			// 8 bytes per sample
			nextSample = func(bs []byte) (float32, []byte) {
				s, bs := nextFloat64(bs)
				// There is no different scaling, just a cast
				// should work.
				return float32(s), bs
			}
		default:
			// wow
			return 0, fmt.Errorf("bit depth %d -> 32 not implemented", bd)
		}
	}
	return readInto(data, r, nextSample)
}

// Read64Float reads some of the data into 64 bit floats.
// TODO: this could probably share more code with Read32Float
func (r *Reader) Read64Float(data [][]float64) (int, error) {
	var nextSample func([]byte) (float64, []byte)
	switch f := r.Format(); f {
	case PCM:
		switch bd := r.BitDepth(); {
		case bd <= 8:
			// 1 byte per sample
			const div float64 = 1.0 / 128
			nextSample = func(bs []byte) (float64, []byte) {
				s, bs := nextByte(bs)
				return float64(s)*div - 1, bs
			}
		case bd <= 16:
			// 2 bytes per sample
			const div float64 = 1.0 / float64(math.MaxInt16)
			nextSample = func(bs []byte) (float64, []byte) {
				i, bs := nextInt16(bs)
				return float64(i) * div, bs
			}
		default:
			return 0, fmt.Errorf("PCM bit depth %d -> float 32 not implemented", bd)
		}
	case IEEEFloat:
		switch bd := r.BitDepth(); {
		case bd <= 32:
			// 4 bytes per sample
			nextSample = func(bs []byte) (float64, []byte) {
				s, bs := nextFloat32(bs)
				return float64(s), bs
			}
		case bd <= 64:
			// 8 bytes per sample
			nextSample = nextFloat64
		default:
			// wow
			return 0, fmt.Errorf("bit depth %d -> 32 not implemented", bd)
		}
	}
	return readInto(data, r, nextSample)
}

func readInto[T any](data [][]T, r *Reader, next func([]byte) (T, []byte)) (int, error) {
	if len(data) != r.Channels() {
		return 0, fmt.Errorf("wrong number of channels: got: %d, file has: %d", len(data), r.Channels())
	}
	nSamples := len(data[0])
	// Number of bytes to read to get nSamples.
	nBytes := nSamples * int(r.fmt.blockAlign)
	raw, err := r.readN(nBytes)
	if err != nil {
		return 0, err
	}
	// decode and de-interleave
	for j := range data[0] {
		for c := range data {
			data[c][j], raw = next(raw)
		}
	}
	if len(raw) != 0 {
		// TODO: not zero
		return 0, fmt.Errorf("internal error: could use all the bytes: %d/%d left", len(raw), nBytes)
	}
	return nSamples, nil
}

// readN reads a certain number of bytes into the scratch buffer and returns it.
func (r *Reader) readN(n int) ([]byte, error) {
	if cap(r.scratch) < n {
		r.scratch = make([]byte, n)
	}
	scratch := r.scratch[:n]
	gotN, err := io.ReadFull(r, scratch)
	return scratch[:gotN], err
}

// nextByte pulls the next byte from raw and returns raw moved along by one.
// It will panic if raw is empty.
func nextByte(raw []byte) (byte, []byte) {
	return raw[0], raw[1:]
}

// nextInt16 reads a little-endian two's complement int16 from the first two
// bytes in raw and returns raw moved along by two. It will panic if raw has <2
// bytes.
func nextInt16(raw []byte) (int16, []byte) {
	return int16(binary.LittleEndian.Uint16(raw)), raw[2:]
}

// nextFloat32 reads a little-endian IEEE-754 32 bit float from the first 4
// bytes of raw and returns raw moved along by 4. It will panic if raw has <4
// bytes.
func nextFloat32(raw []byte) (float32, []byte) {
	bits := binary.LittleEndian.Uint32(raw)
	return math.Float32frombits(bits), raw[:4]
}

// nextFloat64 reads a little-endian IEEE-754 64 bit float from the first 8
// bytes of raw and returns raw moved along by 8. It will panic if raw has <8
// bytes.
func nextFloat64(raw []byte) (float64, []byte) {
	bits := binary.LittleEndian.Uint64(raw)
	return math.Float64frombits(bits), raw[:8]
}

// int16ToByte converts a two's complement int16 sample into an offset byte.
// Quantization is just done by truncation.
func int16ToByte(i int16) byte {
	// truncate first so we can center without overflow.
	b := i >> 8
	return byte(b + 128)
}

// ReadFull8PCM reads all the audio data, deinterleaving and converting to 8 bit
// PCM if necessary.
func ReadFull8PCM(r *Reader) ([][]byte, error) {
	return readAll(r.Read8PCM, r.Channels(), r.Samples())
}

// ReadFull16PCM reads all the audio data, deinterleaving and converting to 16
// bit PCM if necessary.
func ReadFull16PCM(r *Reader) ([][]int16, error) {
	return readAll(r.Read16PCM, r.Channels(), r.Samples())
}

// ReadFull32Float reads all the audio data, deinterleaving and converting to 32
// bit floats if necessary.
func ReadFull32Float(r *Reader) ([][]float32, error) {
	return readAll(r.Read32Float, r.Channels(), r.Samples())
}

// ReadFull64Float reads all the audio data, deinterleaving and converting to 64
// bit floats if necessary.
func ReadFull64Float(r *Reader) ([][]float64, error) {
	return readAll(r.Read64Float, r.Channels(), r.Samples())
}

func readAll[T any](read func([][]T) (int, error), channels, samples int) ([][]T, error) {
	data := makeSlices[T](channels, samples)
	n, err := read(data)
	if err != nil {
		return nil, err
	}
	if n != samples {
		return nil, fmt.Errorf("internal error: read %d samples, expected %d", n, samples)
	}
	return data, nil
}

// makeSlices makes a slice of slices of a provided shape that shares a single
// contiguous backing array. The returned slices should therefore never be
// appended to.
func makeSlices[T any](iSize, jSize int) [][]T {
	base := make([]T, iSize*jSize)
	var (
		out [][]T
	)
	for i := 0; i < iSize; i++ {
		out = append(out, base[i*jSize:(i+1)*jSize])
	}
	return out
}
