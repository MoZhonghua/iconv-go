package iconv

import (
	"io"
	"syscall"
)

const bufferSize = 8 * 1024

// This implements io.ReadCloser interface
type Reader struct {
	source            io.Reader
	converter         *Converter
	buffer            []byte
	readPos, writePos int
	err               error
}

func NewReader(source io.Reader, fromEncoding string, toEncoding string) (*Reader, error) {
	// create a converter
	converter, err := NewConverter(fromEncoding, toEncoding)

	if err == nil {
		return NewReaderFromConverter(source, converter), err
	}

	// return the error
	return nil, err
}

func NewReaderFromConverter(source io.Reader, converter *Converter) (reader *Reader) {
	reader = new(Reader)

	// copy elements
	reader.source = source
	reader.converter = converter

	// create 8K buffers
	reader.buffer = make([]byte, bufferSize)

	return reader
}

func (this *Reader) fillBuffer() int {
	// slide existing data to beginning
	if this.readPos > 0 {
		// copy current bytes - is this guaranteed safe?
		copy(this.buffer, this.buffer[this.readPos:this.writePos])

		// adjust positions
		this.writePos -= this.readPos
		this.readPos = 0
	}

	// read new data into buffer at write position
	bytesRead, err := this.source.Read(this.buffer[this.writePos:])

	// adjust write position
	this.writePos += bytesRead

	// track any reader error / EOF
	if err != nil {
		this.err = err
	}
	return bytesRead
}

// Must be called to free memory
func (this *Reader) Close() error {
	return this.converter.Close()
}

// implement the io.Reader interface
func (this *Reader) Read(p []byte) (n int, err error) {
	// checks for when we have no data
	for this.writePos == 0 || this.readPos == this.writePos {
		// if we have an error / EOF, just return it
		if this.err != nil {
			return n, this.err
		}

		// else, fill our buffer
		this.fillBuffer()
	}

	// TODO: checks for when we have less data than len(p)

	// we should have an appropriate amount of data, convert it into the given buffer
	bytesRead, bytesWritten, err := this.converter.Convert(this.buffer[this.readPos:this.writePos], p)

	// adjust byte counters
	this.readPos += bytesRead
	n += bytesWritten

	// if we experienced an iconv error, check it
	if err != nil {
		// EINVAL:
		// An incomplete multibyte sequence is encountered in the input,
		// and the input byte sequence terminates after it.
		if err == syscall.EINVAL {
			// If we can read new data, then this should NOT be
			// considered as an error.
			newData := this.fillBuffer()
			if newData > 0 {
				return n, nil
			}
		}

		// E2BIG errors can be ignored (we'll get them often) as long
		// as at least 1 byte was written. If we experienced an E2BIG
		// and no bytes were written then the buffer is too small for
		// even the next character
		if err != syscall.E2BIG || bytesWritten == 0 {
			// track anything else
			this.err = err
		} else {
			// Should not return this.err
			// If we got EOF from source in last fillBuffer() call, and
			// there is still more data to process in buffer, in this
			// case, if we return this.err(=EOF), then data in buffer
			// will be lost.
			return n, nil
		}
	}

	// return our results
	return n, this.err
}
