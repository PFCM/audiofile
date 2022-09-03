package riff

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReader(t *testing.T) {
	// See if we can just read them without trouble.
	// TODO: real tests.
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
				t.Fatalf("opening RIFF reader: %v", err)
			}
			type chunkResult struct {
				chunk Chunk
				data  []byte
			}
			var chunks []chunkResult
			for {
				c, err := r.ReadChunk()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("getting chunk %d: %v", len(chunks), err)
				}
				data, err := io.ReadAll(c.Reader)
				if err != nil {
					t.Fatalf("reading chunk %d: %v", len(chunks), err)
				}
				if len(data) != int(c.Size) {
					t.Fatalf("reading chunk %d data: want: %d bytes, got: %d",
						len(chunks), c.Size, len(data))
				}
				chunks = append(chunks, chunkResult{
					chunk: *c,
					data:  data,
				})
			}
			fmt.Printf("%d chunks\n", len(chunks))
			for _, c := range chunks {
				fmt.Printf("%v: %d bytes\n", c.chunk, len(c.data))
			}
			t.Fatal("no")
		})
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()

	type chunk struct {
		id   string
		data []byte
	}
	var chunks []chunk
	for i := 0; i < 5; i++ {
		c := chunk{
			id:   fmt.Sprintf("%04d", i),
			data: make([]byte, rand.Intn(1000)),
		}
		_, err := rand.Read(c.data)
		if err != nil {
			t.Fatal(err)
		}
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
		if _, err := cw.Write(c.data); err != nil {
			t.Fatal(err)
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
	for {
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
			t.Fatalf("Size mismatch: Chunk has size %d, read %d bytes", chnk.Size, len(data))
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
		if !reflect.DeepEqual(got[i], chunks[i]) {
			t.Errorf("Chunk %d: mismatch:\nwant: %v\n got: %v", i, chunks[i], got[i])
		}
	}
}
