package rbuf

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	cv "github.com/smartystreets/goconvey/convey"
)

// new tests just for atomic version

func TestAtomicRingBufSafeConcurrently001(t *testing.T) {

	cv.Convey("Given a AtomicFixedSizeRingBuf", t, func() {
		cv.Convey("concurrent access to the ring should never corrupt it or deadlock, so writing bytes numbered in order should result in reading those bytes in the same numeric order. NB: This test takes about 30 seconds to run on my laptop, with maxRingSize = 20 and N = 10.\n\n", func() {

			maxRingSize := 20
			for ringSize := 1; ringSize < maxRingSize; ringSize++ {
				b := NewAtomicFixedSizeRingBuf(ringSize)

				for bufsz := 1; bufsz < 2*ringSize+1; bufsz++ {
					fmt.Printf("*** testing ringSize = %d  and Read/Write with bufsz = %d\n", ringSize, bufsz)

					writerDone := make(chan bool)
					readerDone := make(chan bool)

					//N := 255 // biggest N that fits in a byte is 255
					N := 10
					bufsz := 1
					go func() {
						// writer, write numbers from 0..N in order.
						by := make([]byte, bufsz)
						lastWritten := -1
						for lastWritten < N {
							for j := 0; j < bufsz; j++ {
								by[j] = byte(lastWritten + 1 + j)
							}
							n, err := b.Write(by)
							if err != nil && err.Error() != "short write" {
								panic(fmt.Errorf("wrote n=%d, error: '%s'", n, err))
							}
							if n > 0 {
								//fmt.Printf("writer wrote from %d .. %d\n", lastWritten+1, lastWritten+n)
							}
							lastWritten += n
						}
						close(writerDone)
					}()

					go func() {
						// reader, expect to read 0..N in order
						by := make([]byte, bufsz)
						lastRead := -1
						for lastRead < N {
							n, err := b.Read(by)
							if err != nil && err.Error() != "EOF" {
								panic(err)
							}
							for j := 0; j < n; j++ {
								//cv.So(by[j], cv.ShouldEqual, lastRead+1+j)
								if by[j] != byte(lastRead+1+j) {
									panic(fmt.Sprintf("expected by[j]=%v  to equal  lastRead+1+j == %v, but did not", by[j], lastRead+1+j))
								}
							}
							if n > 0 {
								//fmt.Printf("reader read from %d .. %d\n", lastRead+1, lastRead+n)
							}
							lastRead += n
						}
						close(readerDone)
					}()

					<-writerDone
					<-readerDone
				} // end bufsz loop
			} // end ringSize loop
		})
	})
}

// same set of tests for non-atomic rbuf:
func TestAtomicRingBufReadWrite(t *testing.T) {
	b := NewAtomicFixedSizeRingBuf(5)

	data := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	cv.Convey("Given a AtomicFixedSizeRingBuf of size 5", t, func() {
		cv.Convey("Write(), Bytes(), and Read() should put and get bytes", func() {
			n, err := b.Write(data[0:5])
			cv.So(n, cv.ShouldEqual, 5)
			cv.So(err, cv.ShouldEqual, nil)
			cv.So(b.Readable, cv.ShouldEqual, 5)
			if n != 5 {
				fmt.Printf("should have been able to write 5 bytes.\n")
			}
			if err != nil {
				panic(err)
			}
			cv.So(b.Bytes(), cv.ShouldResemble, data[0:5])

			sink := make([]byte, 3)
			n, err = b.Read(sink)
			cv.So(n, cv.ShouldEqual, 3)
			cv.So(b.Bytes(), cv.ShouldResemble, data[3:5])
			cv.So(sink, cv.ShouldResemble, data[0:3])
		})

		cv.Convey("Write() more than 5 should give back ErrShortWrite", func() {
			b.Reset()
			cv.So(b.Readable, cv.ShouldEqual, 0)
			n, err := b.Write(data[0:10])
			cv.So(n, cv.ShouldEqual, 5)
			cv.So(err, cv.ShouldEqual, io.ErrShortWrite)
			cv.So(b.Readable, cv.ShouldEqual, 5)
			if n != 5 {
				fmt.Printf("should have been able to write 5 bytes.\n")
			}
			cv.So(b.Bytes(), cv.ShouldResemble, data[0:5])

			sink := make([]byte, 3)
			n, err = b.Read(sink)
			cv.So(n, cv.ShouldEqual, 3)
			cv.So(b.Bytes(), cv.ShouldResemble, data[3:5])
			cv.So(sink, cv.ShouldResemble, data[0:3])
		})

		cv.Convey("we should be able to wrap data and then get it back in Bytes()", func() {
			b.Reset()

			n, err := b.Write(data[0:3])
			cv.So(n, cv.ShouldEqual, 3)
			cv.So(err, cv.ShouldEqual, nil)

			sink := make([]byte, 3)
			n, err = b.Read(sink) // put b.beg at 3
			cv.So(n, cv.ShouldEqual, 3)
			cv.So(err, cv.ShouldEqual, nil)
			cv.So(b.Readable, cv.ShouldEqual, 0)

			n, err = b.Write(data[3:8]) // wrap 3 bytes around to the front
			cv.So(n, cv.ShouldEqual, 5)
			cv.So(err, cv.ShouldEqual, nil)

			by := b.Bytes()
			cv.So(by, cv.ShouldResemble, data[3:8]) // but still get them back from the ping-pong buffering

		})

		cv.Convey("AtomicFixedSizeRingBuf::WriteTo() should work with wrapped data", func() {
			b.Reset()

			n, err := b.Write(data[0:3])
			cv.So(n, cv.ShouldEqual, 3)
			cv.So(err, cv.ShouldEqual, nil)

			sink := make([]byte, 3)
			n, err = b.Read(sink) // put b.beg at 3
			cv.So(n, cv.ShouldEqual, 3)
			cv.So(err, cv.ShouldEqual, nil)
			cv.So(b.Readable, cv.ShouldEqual, 0)

			n, err = b.Write(data[3:8]) // wrap 3 bytes around to the front

			var bb bytes.Buffer
			m, err := b.WriteTo(&bb)

			cv.So(m, cv.ShouldEqual, 5)
			cv.So(err, cv.ShouldEqual, nil)

			by := bb.Bytes()
			cv.So(by, cv.ShouldResemble, data[3:8]) // but still get them back from the ping-pong buffering

		})

		cv.Convey("AtomicFixedSizeRingBuf::ReadFrom() should work with wrapped data", func() {
			b.Reset()
			var bb bytes.Buffer
			n, err := b.ReadFrom(&bb)
			cv.So(n, cv.ShouldEqual, 0)
			cv.So(err, cv.ShouldEqual, nil)

			// write 4, then read 4 bytes
			m, err := b.Write(data[0:4])
			cv.So(m, cv.ShouldEqual, 4)
			cv.So(err, cv.ShouldEqual, nil)

			sink := make([]byte, 4)
			k, err := b.Read(sink) // put b.beg at 4
			cv.So(k, cv.ShouldEqual, 4)
			cv.So(err, cv.ShouldEqual, nil)
			cv.So(b.Readable, cv.ShouldEqual, 0)
			cv.So(b.Beg, cv.ShouldEqual, 4)

			bbread := bytes.NewBuffer(data[4:9])
			n, err = b.ReadFrom(bbread) // wrap 4 bytes around to the front, 5 bytes total.

			by := b.Bytes()
			cv.So(by, cv.ShouldResemble, data[4:9]) // but still get them back continguous from the ping-pong buffering
		})

	})

}
