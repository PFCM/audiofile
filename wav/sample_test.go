package wav

import (
	"iter"
	"slices"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRoundTrip(t *testing.T) {
	byteValues := func() iter.Seq[byte] {
		return func(yield func(byte) bool) {
			for i := 0; i < 256; i++ {
				if !yield(byte(i)) {
					return
				}
			}
		}
	}
	sixteenBitValues := func() iter.Seq[int16] {
		return func(yield func(int16) bool) {
			// Seems surprising to test every value, but it's fast
			// enough for now.
			for i := -32768; i <= 32767; i += 1 {
				if !yield(int16(i)) {
					return
				}
			}
		}
	}
	// First all the round trips that don't involve any loss of precision.
	for _, c := range []struct {
		name string
		test func(t *testing.T)
	}{{
		name: "8PCM/16PCM",
		test: mkRoundTripTest(from8PCMTo16PCM, from16PCMTo8PCM, byteValues),
	}, {
		name: "8PCM/24PCM",
		test: mkRoundTripTest(from8PCMTo24PCM, from24PCMTo8PCM, byteValues),
	}, {
		name: "8PCM/Float32",
		test: mkRoundTripTest(from8PCMToFloat32, fromFloat32To8PCM, byteValues),
	}, {
		name: "8PCM/Float64",
		test: mkRoundTripTest(from8PCMToFloat64, fromFloat64To8PCM, byteValues),
	}, {
		name: "16PCM/24PCM",
		test: mkRoundTripTest(from16PCMTo24PCM, from24PCMTo16PCM, sixteenBitValues),
	}, {
		name: "16PCM/Float32",
		test: mkRoundTripTest(from16PCMToFloat32, fromFloat32To16PCM, sixteenBitValues),
	}} {
		t.Run(c.name, c.test)
	}
}

func mkRoundTripTest[A comparable, B any](aToB func(A) B, bToA func(B) A, values func() iter.Seq[A]) func(t *testing.T) {
	return func(t *testing.T) {
		for a := range values() {
			b := aToB(a)
			got := bToA(b)
			if a != got {
				t.Errorf("round trip failure: %v -> %v -> %v", a, b, got)
			}
		}
	}
}

func TestDeinterleave(t *testing.T) {
	const num = 10
	in := make([]int, num)
	for i := range in {
		in[i] = i
	}

	groups := func(n int) [][]int {
		var (
			out [][]int
			tmp = make([]int, n)
		)
		for i := 0; i < num; i++ {
			tmp[i%n] = i
			if i%n == n-1 {
				out = append(out, slices.Clone(tmp))
			}
		}
		return out
	}

	for i := 1; i <= num; i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			var (
				want = groups(i)
				got  [][]int
			)
			for g := range deinterleave(i, slices.Values(in)) {
				got = append(got, slices.Clone(g))
			}
			if d := cmp.Diff(got, want); d != "" {
				t.Errorf("deinterleave(%d): mismatch (-got, +want):\n%v", i, d)
			}
		})
	}
}
