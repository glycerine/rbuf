package rbuf

// AtomicFixedSizeRingBuf: Synchronized version of FixedSizeRingBuf,
// safe for concurrent access.
//
// copyright (c) 2014, Jason E. Aten
// license: MIT
//
// Some text from the Golang standard library doc is adapted and
// reproduced in fragments below to document the expected behaviors
// of the interface functions Read()/Write()/ReadFrom()/WriteTo() that
// are implemented here. Those descriptions (see
// http://golang.org/pkg/io/#Reader for example) are
// copyright 2010 The Go Authors.

import (
	"io"
	"sync"
)

// AtomicFixedSizeRingBuf: see FixedSizeRingBuf for the full
// details; this is the same, just safe for current access
// (and thus paying the price of synchronization on each call
// as well.)
//
type AtomicFixedSizeRingBuf struct {
	A        [2][]byte // a pair of ping/pong buffers. Only one is active.
	Use      int       // which A buffer is in active use, 0 or 1
	N        int       // MaxViewInBytes, the size of A[0] and A[1] in bytes.
	Beg      int       // start of data in A[Use]
	Readable int       // number of bytes available to read in A[Use]
	tex      sync.Mutex
}

// get the length of the largest read that we can provide to a contiguous slice
// without an extra linearizing copy of all bytes internally.
func (b *AtomicFixedSizeRingBuf) ContigLen() int {
	b.tex.Lock()
	defer b.tex.Unlock()

	extent := b.Beg + b.Readable
	firstContigLen := intMin2(extent, b.N) - b.Beg
	return firstContigLen
}

// constructor. NewAtomicFixedSizeRingBuf will allocate internally
// two buffers of size maxViewInBytes.
func NewAtomicFixedSizeRingBuf(maxViewInBytes int) *AtomicFixedSizeRingBuf {
	n := maxViewInBytes
	r := &AtomicFixedSizeRingBuf{
		Use: 0, // 0 or 1, whichever is actually in use at the moment.
		// If we are asked for Bytes() and we wrap, linearize into the other.

		N:        n,
		Beg:      0,
		Readable: 0,
	}
	r.A[0] = make([]byte, n, n)
	r.A[1] = make([]byte, n, n)

	return r
}

// from the standard library description of Bytes():
// Bytes() returns a slice of the contents of the unread portion of the buffer.
// If the caller changes the contents of the
// returned slice, the contents of the buffer will change provided there
//  are no intervening method calls on the Buffer.
//
// The largest slice Bytes ever returns is bounded above by the maxViewInBytes
// value used when calling NewAtomicFixedSizeRingBuf().
func (b *AtomicFixedSizeRingBuf) Bytes() []byte {
	b.tex.Lock()
	defer b.tex.Unlock()

	extent := b.Beg + b.Readable
	if extent <= b.N {
		// we fit contiguously in this buffer without wrapping to the other
		return b.A[b.Use][b.Beg:(b.Beg + b.Readable)]
	}

	// wrap into the other buffer
	src := b.Use
	dest := 1 - b.Use

	n := copy(b.A[dest], b.A[src][b.Beg:])
	n += copy(b.A[dest][n:], b.A[src][0:(extent%b.N)])

	b.Use = dest
	b.Beg = 0

	return b.A[b.Use][:n]
}

// Read():
//
// From bytes.Buffer.Read(): Read reads the next len(p) bytes
// from the buffer or until the buffer is drained. The return
// value n is the number of bytes read. If the buffer has no data
// to return, err is io.EOF (unless len(p) is zero); otherwise it is nil.
//
//  from the description of the Reader interface,
//     http://golang.org/pkg/io/#Reader
//
/*
Reader is the interface that wraps the basic Read method.

Read reads up to len(p) bytes into p. It returns the number
of bytes read (0 <= n <= len(p)) and any error encountered.
Even if Read returns n < len(p), it may use all of p as scratch
space during the call. If some data is available but not
len(p) bytes, Read conventionally returns what is available
instead of waiting for more.

When Read encounters an error or end-of-file condition after
successfully reading n > 0 bytes, it returns the number of bytes
read. It may return the (non-nil) error from the same call or
return the error (and n == 0) from a subsequent call. An instance
of this general case is that a Reader returning a non-zero number
of bytes at the end of the input stream may return
either err == EOF or err == nil. The next Read should
return 0, EOF regardless.

Callers should always process the n > 0 bytes returned before
considering the error err. Doing so correctly handles I/O errors
that happen after reading some bytes and also both of the
allowed EOF behaviors.

Implementations of Read are discouraged from returning a zero
byte count with a nil error, and callers should treat that
situation as a no-op.
*/
//
func (b *AtomicFixedSizeRingBuf) Read(p []byte) (n int, err error) {
	return b.ReadAndMaybeAdvance(p, true)
}

// ReadWithoutAdvance(): if you want to Read the data and leave
// it in the buffer, so as to peek ahead for example.
func (b *AtomicFixedSizeRingBuf) ReadWithoutAdvance(p []byte) (n int, err error) {
	return b.ReadAndMaybeAdvance(p, false)
}

func (b *AtomicFixedSizeRingBuf) ReadAndMaybeAdvance(p []byte, doAdvance bool) (n int, err error) {
	b.tex.Lock()
	defer b.tex.Unlock()

	if len(p) == 0 {
		return 0, nil
	}
	if b.Readable == 0 {
		return 0, io.EOF
	}
	extent := b.Beg + b.Readable
	if extent <= b.N {
		n += copy(p, b.A[b.Use][b.Beg:extent])
	} else {
		n += copy(p, b.A[b.Use][b.Beg:b.N])
		if n < len(p) {
			n += copy(p[n:], b.A[b.Use][0:(extent%b.N)])
		}
	}
	if doAdvance {
		b.unatomic_advance(n)
	}
	return
}

//
// Write writes len(p) bytes from p to the underlying data stream.
// It returns the number of bytes written from p (0 <= n <= len(p))
// and any error encountered that caused the write to stop early.
// Write must return a non-nil error if it returns n < len(p).
//
func (b *AtomicFixedSizeRingBuf) Write(p []byte) (n int, err error) {
	b.tex.Lock()
	defer b.tex.Unlock()

	for {
		if len(p) == 0 {
			// nothing (left) to copy in; notice we shorten our
			// local copy p (below) as we read from it.
			return
		}

		writeCapacity := b.N - b.Readable
		if writeCapacity <= 0 {
			// we are all full up already.
			return n, io.ErrShortWrite
		}
		if len(p) > writeCapacity {
			err = io.ErrShortWrite
			// leave err set and
			// keep going, write what we can.
		}

		writeStart := (b.Beg + b.Readable) % b.N

		upperLim := intMin2(writeStart+writeCapacity, b.N)

		k := copy(b.A[b.Use][writeStart:upperLim], p)

		n += k
		b.Readable += k
		p = p[k:]

		// we can fill from b.A[b.Use][0:something] from
		// p's remainder, so loop
	}
}

// WriteTo and ReadFrom avoid intermediate allocation and copies.

// WriteTo avoids intermediate allocation and copies.
// WriteTo writes data to w until there's no more data to write
// or when an error occurs. The return value n is the number of
// bytes written. Any error encountered during the write is also returned.
func (b *AtomicFixedSizeRingBuf) WriteTo(w io.Writer) (n int64, err error) {
	b.tex.Lock()
	defer b.tex.Unlock()

	if b.Readable == 0 {
		return 0, io.EOF
	}

	extent := b.Beg + b.Readable
	firstWriteLen := intMin2(extent, b.N) - b.Beg
	secondWriteLen := b.Readable - firstWriteLen
	if firstWriteLen > 0 {
		m, e := w.Write(b.A[b.Use][b.Beg:(b.Beg + firstWriteLen)])
		n += int64(m)
		b.unatomic_advance(m)

		if e != nil {
			return n, e
		}
		// all bytes should have been written, by definition of
		// Write method in io.Writer
		if m != firstWriteLen {
			return n, io.ErrShortWrite
		}
	}
	if secondWriteLen > 0 {
		m, e := w.Write(b.A[b.Use][0:secondWriteLen])
		n += int64(m)
		b.unatomic_advance(m)

		if e != nil {
			return n, e
		}
		// all bytes should have been written, by definition of
		// Write method in io.Writer
		if m != secondWriteLen {
			return n, io.ErrShortWrite
		}
	}

	return n, nil
}

// ReadFrom avoids intermediate allocation and copies.
// ReadFrom() reads data from r until EOF or error. The return value n
// is the number of bytes read. Any error except io.EOF encountered
// during the read is also returned.
func (b *AtomicFixedSizeRingBuf) ReadFrom(r io.Reader) (n int64, err error) {
	b.tex.Lock()
	defer b.tex.Unlock()

	for {
		writeCapacity := b.N - b.Readable
		if writeCapacity <= 0 {
			// we are all full
			return n, nil
		}
		writeStart := (b.Beg + b.Readable) % b.N
		upperLim := intMin2(writeStart+writeCapacity, b.N)

		m, e := r.Read(b.A[b.Use][writeStart:upperLim])
		n += int64(m)
		b.Readable += m
		if e == io.EOF {
			return n, nil
		}
		if e != nil {
			return n, e
		}
	}
}

// Reset quickly forgets any data stored in the ring buffer. The
// data is still there, but the ring buffer will ignore it and
// overwrite those buffers as new data comes in.
func (b *AtomicFixedSizeRingBuf) Reset() {
	b.tex.Lock()
	defer b.tex.Unlock()

	b.Beg = 0
	b.Readable = 0
	b.Use = 0
}

// Advance(): non-standard, but better than Next(),
// because we don't have to unwrap our buffer and pay the cpu time
// for the copy that unwrapping may need.
// Useful in conjuction/after ReadWithoutAdvance() above.
func (b *AtomicFixedSizeRingBuf) Advance(n int) {
	b.tex.Lock()
	defer b.tex.Unlock()

	b.unatomic_advance(n)
}

// unatomic_advance(): private implementation of Advance() without
// the locks. See Advance() above for description.
// Necessary so that other methods that already hold
// locks can advance, and there are no recursive mutexes
// in Go.
func (b *AtomicFixedSizeRingBuf) unatomic_advance(n int) {
	if n <= 0 {
		return
	}
	if n > b.Readable {
		n = b.Readable
	}
	b.Readable -= n
	b.Beg = (b.Beg + n) % b.N
}

// Adopt(): non-standard.
//
// For efficiency's sake, (possibly) take ownership of
// already allocated slice offered in me.
//
// If me is large we will adopt it, and we will potentially then
// write to the me buffer.
// If we already have a bigger buffer, copy me into the existing
// buffer instead.
func (b *AtomicFixedSizeRingBuf) Adopt(me []byte) {
	b.tex.Lock()
	defer b.tex.Unlock()

	n := len(me)
	if n > b.N {
		b.A[0] = me
		b.A[1] = make([]byte, n, n)
		b.N = n
		b.Use = 0
		b.Beg = 0
		b.Readable = n
	} else {
		// we already have a larger buffer, reuse it.
		copy(b.A[0], me)
		b.Use = 0
		b.Beg = 0
		b.Readable = n
	}
}

// keep the atomic_rbuf.go standalone and usable without
// the rbuf.go file, by simply duplicating intMin from rbuf.go
//
func intMin2(a, b int) int {
	if a < b {
		return a
	} else {
		return b
	}
}
