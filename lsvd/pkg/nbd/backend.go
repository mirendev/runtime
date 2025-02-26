package nbd

import (
	"io"
	"os"
)

type BackendOpen interface {
	Open() Backend
	Close(b Backend)
}

type Backend interface {
	io.ReaderAt
	io.WriterAt

	ReadIntoConn(optional []byte, off int64, output *os.File) (bool, error)

	ZeroAt(off, sz int64) error
	Trim(off, sz int64) error

	Size() (int64, error)
	Sync() error

	Idle()
}
