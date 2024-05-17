// package riff reads and writes RIFF files.
package riff

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Chunk is a RIFF chunk.
type Chunk struct {
	// Identifier is the 4 byte ASCII identifier for the chunk.
	Identifier string
	// Size is the number of bytes in the chunk.
	Size int
	// Reader is a reader which will read the whole chunk. It
	// will return io.EOF after Size bytes.
	io.Reader
}

// Reader reads RIFF files, one chunk at a time. It does the smallest amount of
// decoding possible and tries to avoid having to know what types of chunks to
// expect where or what they mean.
type Reader struct {
	// Form is the type of the RIFF file.
	Form string

	r       io.Reader
	hdr     chunkHeader
	chunk   Chunk
	pad     bool
	scratch [4096]byte
}

// NewReader validates the RIFF header and returns a Reader ready to read
// chunks. It performs many small reads, a buffered reader is advised.
func NewReader(r io.Reader) (*Reader, error) {
	var rh chunkHeader
	if err := readChunkHeader(r, &rh); err != nil {
		return nil, err
	}
	if rh.id != [4]byte{'R', 'I', 'F', 'F'} {
		return nil, fmt.Errorf("expected ID RIFF in first chunk, found: %q", rh.id)
	}
	// Next 4 bytes should be the form type.
	var f [4]byte
	if _, err := io.ReadFull(r, f[:]); err != nil {
		if err == io.EOF {
			err = errors.New("unexpected EOF, expecting form ID")
		}
		return nil, err
	}

	// The overall size doesn't actually matter, we expect to just read
	// until EOF anyway.
	return &Reader{Form: string(f[:]), r: r, pad: rh.pad}, nil
}

// ReadChunk reads the next chunk. The data in the chunk is only valid
// until the next call to ReadChunk.
func (r *Reader) ReadChunk() (*Chunk, error) {
	// Make sure the previous chunk was read all the way through,
	// so the underlying reader is at the start of the next chunk.
	if r.chunk.Reader != nil {
		for {
			_, err := r.chunk.Reader.Read(r.scratch[:])
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
		}
	}
	// Make sure to read the pad byte if present.
	if r.hdr.pad {
		_, err := r.r.Read(r.scratch[:1])
		if err == io.EOF {
			return nil, errors.New("unexpected EOF: missing pad byte")
		}
		if err != nil {
			return nil, err
		}
	}

	// Now we're ready to read the next chunk.
	if err := readChunkHeader(r.r, &r.hdr); err != nil {
		return nil, err
	}
	r.chunk.Identifier = string(r.hdr.id[:])
	r.chunk.Size = int(r.hdr.size)

	r.chunk.Reader = &io.LimitedReader{R: r.r, N: int64(r.hdr.size)}

	return &r.chunk, nil
}

type chunkHeader struct {
	id   [4]byte
	size uint32
	pad  bool // true if we need to read one extra padding byte
}

// readChunkHeader populates the provided chunkHeader from the given reader.
func readChunkHeader(r io.Reader, ch *chunkHeader) error {
	if ch == nil {
		// should not be possible.
		return errors.New("nil chunkHeader")
	}
	// first is the ID.
	if _, err := io.ReadFull(r, ch.id[:]); err != nil {
		return err
	}
	// Then the size.
	var rawSize [4]byte
	if _, err := io.ReadFull(r, rawSize[:]); err != nil {
		return err
	}
	ch.size = binary.LittleEndian.Uint32(rawSize[:])
	// There will be padding if the size is an odd number.
	ch.pad = ch.size%2 == 1
	return nil
}

// Writer writes RIFF files.
type Writer struct {
	ws io.WriteSeeker
	// written is the number of bytes written into the overall RIFF chunk.
	written uint32

	scratch []byte
}

// NewWriter constructs a new Writer, ready to write RIFF chunks.
func NewWriter(ws io.WriteSeeker, form string) (*Writer, error) {
	// First write the RIFF header, the form id and empty space
	// for the size.
	hdr := []byte{'R', 'I', 'F', 'F'}
	if len(form) != 4 {
		return nil, fmt.Errorf("invalid form ID: %q", form)
	}
	hdr = append(hdr, 0, 0, 0, 0)
	hdr = append(hdr, []byte(form)...)

	if _, err := ws.Write(hdr); err != nil {
		return nil, err
	}
	return &Writer{
		ws:      ws,
		written: 4, // The form counts.
	}, nil
}

// NewChunk starts a new chunk, returning a writer for the caller to write the
// data portion to. Closing the returned writer ends the chunk.
func (w *Writer) NewChunk(identifier string) (io.WriteCloser, error) {
	// First write the identifier and some empty space for the size.
	if len(identifier) != 4 {
		return nil, fmt.Errorf("invalid chunk identifier: %q", identifier)
	}
	if err := w.write([]byte(identifier)); err != nil {
		return nil, err
	}
	if err := w.write(w.uint32(0)); err != nil {
		return nil, err
	}
	return newChunkWriter(w), nil
}

// WriteChunk writes appropriate chunk metadata, and copies all the data from
// the chunks reader into the writer. It should not be called if a writer from
// NewChunk is active.
func (w *Writer) WriteChunk(c *Chunk) error {
	if len(c.Identifier) != 4 {
		return fmt.Errorf("invalid chunk identifier: %q", c.Identifier)
	}
	if err := w.write([]byte(c.Identifier)); err != nil {
		return err
	}
	if err := w.write(w.uint32(uint32(c.Size))); err != nil {
		return err
	}
	n, err := io.Copy(w.ws, c.Reader)
	w.written += uint32(n)
	return err
}

// write behaves like w.ws.Write, except that it updates the Writers byte
// counter by the number of bytes written.
func (w *Writer) write(p []byte) error {
	n, err := w.ws.Write(p)
	w.written += uint32(n)
	return err
}

// Close closes the writer and finalizes the metadata. It does not close the
// underlying writer.
func (w *Writer) Close() error {
	// All we need to write is the size.
	if _, err := w.ws.Seek(4, io.SeekStart); err != nil {
		return err
	}
	_, err := w.ws.Write(w.uint32(w.written))
	return err
}

// uint32 encodes a uint32 appropriately into w.scratch and returns the slice.
// The data is only valid until the next time someone uses w.scratch.
func (w *Writer) uint32(u uint32) []byte {
	return binary.LittleEndian.AppendUint32(w.getScratch(4)[:0], u)
}

func (w *Writer) getScratch(n int) []byte {
	if cap(w.scratch) < n {
		w.scratch = make([]byte, n)
	}
	return w.scratch[:n]
}

// chunkWriter implements io.WriteCloser. When it is closed, it writes how many
// bytes it has written.
type chunkWriter struct {
	w       *Writer
	written uint32
}

func newChunkWriter(w *Writer) *chunkWriter {
	return &chunkWriter{w: w}
}

func (c *chunkWriter) Write(p []byte) (int, error) {
	n, err := c.w.ws.Write(p)
	c.written += uint32(n)
	return n, err
}

func (c *chunkWriter) Close() error {
	// Seek back 4 bytes further than we have written.
	if _, err := c.w.ws.Seek(-(int64(c.written) + 4), io.SeekCurrent); err != nil {
		return err
	}
	// Write the size.
	// TODO: reuse the buffer
	var buf [4]byte
	if _, err := c.w.ws.Write(binary.LittleEndian.AppendUint32(buf[:0], c.written)); err != nil {
		return err
	}
	// seek back to the end
	_, err := c.w.ws.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	// TODO: write the pad byte
	// update the total size
	c.w.written += c.written
	return nil
}
