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
	Reader io.Reader
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
