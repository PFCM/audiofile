package wav

import (
	"encoding/binary"
	"fmt"
	"iter"
	"math"
	"slices"
)

func from8PCMTo16PCM(b byte) int16 { return (int16(b) - 128) << 8 }
func from8PCMTo24PCM(b byte) int32 { return (int32(b) - 128) << 16 }

func from8PCMToFloat32(b byte) float32 {
	const div float32 = 1.0 / 128
	return float32(b)*div - 1
}

func from8PCMToFloat64(b byte) float64 {
	const div float64 = 1.0 / 128
	return float64(b)*div - 1
}

const maxInt16 = int16(1<<15 - 1)

func from16PCMTo8PCM(i int16) byte       { return byte((i >> 8) + 128) }
func from16PCMTo24PCM(i int16) int32     { return int32(i) << 8 }
func from16PCMToFloat32(i int16) float32 { return float32(i) / float32(maxInt16) }
func from16PCMToFloat64(i int16) float64 { panic("not implemented") }

func from24PCMTo8PCM(i int32) byte       { return byte((i >> 16) + 128) }
func from24PCMTo16PCM(i int32) int16     { return int16(i >> 8) }
func from24PCMToFloat32(i int32) float32 { panic("not implemented") }
func from24PCMToFloat64(i int32) float64 { panic("not implemented") }

func fromFloat32To8PCM(f float32) byte       { return byte((f + 1) * 128) }
func fromFloat32To16PCM(f float32) int16     { return int16(f * float32(maxInt16)) }
func fromFloat32To24PCM(f float32) int32     { panic("not implemented") }
func fromFloat32ToFloat64(f float32) float64 { return float64(f) }

func fromFloat64To8PCM(f float64) byte     { return byte((f + 1) * 128) }
func fromFloat64To16PCM(f float64) int16   { panic("not implemented") }
func fromFloat64To24PCM(f float64) int32   { panic("not implemented") }
func fromFloat64To32PCM(f float64) float32 { return float32(f) }

func as8PCM(b []byte) iter.Seq[byte] { return slices.Values(b) }

func as16PCM(b []byte) (iter.Seq[int16], error) {
	if err := checkSize(2, b); err != nil {
		return nil, err
	}
	return func(yield func(int16) bool) {
		for len(b) > 0 {
			i := int16(binary.LittleEndian.Uint16(b))
			if !yield(i) {
				return
			}
			b = b[2:]
		}
	}, nil
}

func as24PCM(b []byte) (iter.Seq[int32], error) {
	if err := checkSize(3, b); err != nil {
		return nil, err
	}
	return func(yield func(int32) bool) {
		for len(b) > 0 {
			// Little endian, hopefully.
			lo, mid, hi := int32(b[0]), int32(b[1]), int32(b[2])
			i := lo & (mid << 8) & (hi << 16)
			if !yield(i) {
				return
			}
			b = b[3:]
		}
	}, nil
}

func asFloat32(b []byte) (iter.Seq[float32], error) {
	if err := checkSize(4, b); err != nil {
		return nil, err
	}
	return func(yield func(float32) bool) {
		for len(b) > 0 {
			bits := binary.LittleEndian.Uint32(b)
			if !yield(math.Float32frombits(bits)) {
				return
			}
			b = b[4:]
		}
	}, nil
}

func asFloat64(b []byte) (iter.Seq[float64], error) {
	if err := checkSize(4, b); err != nil {
		return nil, err
	}
	return func(yield func(float64) bool) {
		for len(b) > 0 {
			bits := binary.LittleEndian.Uint64(b)
			if !yield(math.Float64frombits(bits)) {
				return
			}
			b = b[8:]
		}
	}, nil
}

func deinterleave[T any](n int, it iter.Seq[T]) iter.Seq[[]T] {
	return func(yield func([]T) bool) {
		var (
			out = make([]T, n)
			i   = 0
		)
		for t := range it {
			out[i] = t
			i++
			if i >= n {
				if !yield(out) {
					return
				}
				i = 0
			}
		}
	}
}

func checkSize(bytesPerSample int, b []byte) error {
	if l := len(b); l%bytesPerSample != 0 {
		return fmt.Errorf("bad length for buffer of %d bit samples: %d bytes", bytesPerSample*8, l)
	}
	return nil
}
