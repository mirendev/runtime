package lsvd

import "context"

type Context struct {
	context.Context

	buffers *Buffers
}

func NewContext(ctx context.Context) *Context {
	return &Context{
		Context: ctx,
		buffers: NewBuffers(),
	}
}

func (c *Context) Reset() {
	c.buffers.Reset()
}

func (c *Context) Close() {
	buf := c.buffers
	c.buffers = nil
	ReturnBuffers(buf)
}

func (c *Context) Allocate(sz int) []byte {
	data := c.buffers.alloc(sz)
	return data
}

func (c *Context) AllocateZero(sz int) []byte {
	data := c.buffers.alloc(sz)
	clear(data)
	return data
}

func (c *Context) Marker() int {
	return c.buffers.Marker()
}

func (c *Context) ResetTo(marker int) {
	c.buffers.ResetTo(marker)
}
