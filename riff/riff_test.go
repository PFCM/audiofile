package riff

import (
	"bytes"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()

	type chunk struct {
		id   string
		data []byte
	}
	var (
		src = rand.NewChaCha8([32]byte{})
		rng = rand.New(src)
		chunks []chunk
	)
	for i := range 11 {
		c := chunk{
			id:   fmt.Sprintf("%04d", i),
			data: make([]byte, rng.IntN(1000)),
		}
		if _, err := src.Read(c.data); err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, c)
	}

	// It would be nicer to just do this in memory, but bytes.Buffer is not
	// a WriteSeeker.
	path := filepath.Join(dir, "test.riff")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	w, err := NewWriter(f, "test")
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range chunks {
		cw, err := w.NewChunk(c.id)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := cw.Write(c.data); err != nil {
			t.Fatal(err)
		} else if n != len(c.data) {
			t.Fatalf("didn't write all the bytes?\nexpected: %d, got: %d", n, len(c.data))
		}
		if err := cw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	// Write them all again, using the WriteChunk method.
	for _, c := range chunks {
		chnk := &Chunk{
			Identifier: c.id,
			Size:       len(c.data),
			Reader:     bytes.NewReader(c.data),
		}
		if err := w.WriteChunk(chnk); err != nil {
			t.Fatal(err)
		}
	}

	// Finish writing.
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// Open it again and read it.
	f, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}

	var got []chunk
	for i := 0; ; i++ {
		chnk, err := r.ReadChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(chnk.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) != chnk.Size {
			t.Fatalf("Size mismatch: Chunk %d has size %x, read %x bytes", i, chnk.Size, len(data))
		}
		got = append(got, chunk{
			id:   chnk.Identifier,
			data: data,
		})
	}
	// We wrote everything twice.
	chunks = append(chunks, chunks...)
	if len(got) != len(chunks) {
		t.Fatalf("Wrong number of chunks: wrote %d, read %d", len(chunks), len(got))
	}
	for i := range chunks {
		if d := cmp.Diff(got[i], chunks[i], cmp.AllowUnexported(chunk{})); d != "" {
			t.Errorf("Chunk %d: mismatch (-got, +want):\n%v", i, d)
		}
	}
}
