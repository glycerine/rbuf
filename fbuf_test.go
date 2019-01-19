package rbuf

import (
	"io"
	"testing"

	cv "github.com/glycerine/goconvey/convey"
)

func TestFloatBufReadWrite(t *testing.T) {
	b := NewFloat64RingBuf(5)

	data := []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	cv.Convey("Given a Float64RingBuf of size 5", t, func() {
		cv.Convey("Write() and ReadFloat64() should put and get floats", func() {
			b.Reset()

			n, err := b.Write(data[:5])
			cv.So(n, cv.ShouldEqual, 5)
			cv.So(err, cv.ShouldEqual, nil)
			cv.So(b.Readable, cv.ShouldEqual, 5)
			cv.So(b.Values(), cv.ShouldResemble, data[:5])

			sink := make([]float64, 3)
			n, err = b.ReadFloat64(sink)
			cv.So(n, cv.ShouldEqual, 3)
			cv.So(b.Values(), cv.ShouldResemble, data[3:5])
			cv.So(sink, cv.ShouldResemble, data[:3])
		})

		cv.Convey("Write() more than 5 items should give back ErrShortWrite", func() {
			b.Reset()

			cv.So(b.Readable, cv.ShouldEqual, 0)
			n, err := b.Write(data[:10])
			cv.So(n, cv.ShouldEqual, 5)
			cv.So(err, cv.ShouldEqual, io.ErrShortWrite)
			cv.So(b.Readable, cv.ShouldEqual, 5)
			cv.So(b.Values(), cv.ShouldResemble, data[:5])

			sink := make([]float64, 3)
			n, err = b.ReadFloat64(sink)
			cv.So(n, cv.ShouldEqual, 3)
			cv.So(b.Values(), cv.ShouldResemble, data[3:5])
			cv.So(sink, cv.ShouldResemble, data[:3])
		})

		cv.Convey("WriteAndMaybeOverwriteOldestData() should auto advance", func() {
			b.Reset()

			n, err := b.WriteAndMaybeOverwriteOldestData(data[:5])
			cv.So(err, cv.ShouldEqual, nil)
			cv.So(n, cv.ShouldEqual, 5)
			cv.So(b.Values(), cv.ShouldResemble, data[:5])

			n, err = b.WriteAndMaybeOverwriteOldestData(data[5:7])
			cv.So(n, cv.ShouldEqual, 2)
			cv.So(b.Values(), cv.ShouldResemble, data[2:7])

			n, err = b.WriteAndMaybeOverwriteOldestData(data[0:9])
			cv.So(err, cv.ShouldEqual, nil)
			cv.So(n, cv.ShouldEqual, 9)
			cv.So(b.Values(), cv.ShouldResemble, data[4:9])
		})
	})
}
