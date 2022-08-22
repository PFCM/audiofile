package riff

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestReader(t *testing.T) {
	// See if we can just read them without trouble.
	// TODO: real tests.
	files, err := fs.Glob(os.DirFS("./testdata"), "*")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("./testdata", name)
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
				fmt.Printf("%+v\n", c)
				if len(data) != int(c.Size) {
					t.Fatalf("reading chunk %d data: want: %d bytes, got: %d",
						len(chunks), c.Size, len(data))
				}
				chunks = append(chunks, chunkResult{
					chunk: *c,
					data:  data,
				})
			}
			t.Fatal(chunks)
		})
	}
}
