package io

import (
	"context"
	"io"
)

type readerAdapter struct {
	ctx context.Context
	rc  ReaderClient
}

func (r *readerAdapter) Read(p []byte) (n int, err error) {
	rr, err := r.rc.Read(r.ctx, int32(len(p)))
	if err != nil {
		return 0, err
	}

	c := copy(p, rr.Data())

	return c, nil
}

func AsReader(ctx context.Context, rc ReaderClient) io.Reader {
	return &readerAdapter{ctx: ctx, rc: rc}
}

type readerAtAdapter struct {
	ctx context.Context
	rc  ReaderAtClient
}

func (r *readerAtAdapter) ReadAt(p []byte, off int64) (n int, err error) {
	rr, err := r.rc.ReadAt(r.ctx, int32(len(p)), off)
	if err != nil {
		return 0, err
	}

	c := copy(p, rr.Data())

	return c, nil
}

func AsReaderAt(ctx context.Context, rc ReaderAtClient) io.ReaderAt {
	return &readerAtAdapter{ctx: ctx, rc: rc}
}

type writerAdapter struct {
	ctx context.Context
	wc  WriterClient
}

func (w *writerAdapter) Write(p []byte) (n int, err error) {
	res, err := w.wc.Write(w.ctx, p)
	if err != nil {
		return 0, err
	}

	return int(res.Count()), nil
}

func AsWriter(ctx context.Context, wc WriterClient) io.Writer {
	return &writerAdapter{ctx: ctx, wc: wc}
}

type serveReader struct {
	r io.Reader
}

func (s *serveReader) Read(ctx context.Context, state *ReaderRead) error {
	args := state.Args()

	p := make([]byte, args.Count())

	n, err := s.r.Read(p)
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetData(p[:n])

	return nil
}

func ServeReader(r io.Reader) Reader {
	return &serveReader{r: r}
}

type serveReaderAt struct {
	r io.ReaderAt
}

func (s *serveReaderAt) ReadAt(ctx context.Context, state *ReaderAtReadAt) error {
	args := state.Args()

	p := make([]byte, args.Count())

	n, err := s.r.ReadAt(p, args.Offset())
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetData(p[:n])

	return nil
}

func ServeReaderAt(r io.ReaderAt) ReaderAt {
	return &serveReaderAt{r: r}
}

type serveWriter struct {
	w io.Writer
}

func (s *serveWriter) Write(ctx context.Context, state *WriterWrite) error {
	args := state.Args()

	n, err := s.w.Write(args.Data())
	if err != nil {
		return err
	}

	res := state.Results()
	res.SetCount(int32(n))

	return nil
}

func ServeWriter(w io.Writer) Writer {
	return &serveWriter{w: w}
}
