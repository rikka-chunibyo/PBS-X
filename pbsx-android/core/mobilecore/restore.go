package mobilecore

import (
	"context"
	"io"
)

// Restore retains the original API: a complete restore into an empty target.
func (c *Client) Restore(store, namespace, source string, stamp int64, docs Documents, progress Progress) error {
	_, err := c.RestoreSelected(store, namespace, source, stamp, `["."]`, "fail", docs, progress)
	return err
}

type cancelReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *cancelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
