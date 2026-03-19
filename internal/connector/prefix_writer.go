package connector

import (
	"bytes"
	"io"
)

// prefixWriter wraps an io.Writer, prepending a prefix to each line.
type prefixWriter struct {
	prefix string
	w      io.Writer
	atBOL  bool // true if we're at the beginning of a line
}

func newPrefixWriter(prefix string, w io.Writer) *prefixWriter {
	return &prefixWriter{prefix: prefix, w: w, atBOL: true}
}

func (pw *prefixWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		if pw.atBOL {
			if _, err := pw.w.Write([]byte(pw.prefix)); err != nil {
				return written, err
			}
			pw.atBOL = false
		}
		// Find the next newline.
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			// Check for \r (carriage return used in progress lines).
			idx = bytes.IndexByte(p, '\r')
			if idx < 0 {
				n, err := pw.w.Write(p)
				return written + n, err
			}
			// Write up to and including the \r.
			n, err := pw.w.Write(p[:idx+1])
			written += n
			if err != nil {
				return written, err
			}
			p = p[idx+1:]
			pw.atBOL = true
			continue
		}
		// Write up to and including the newline.
		n, err := pw.w.Write(p[:idx+1])
		written += n
		if err != nil {
			return written, err
		}
		p = p[idx+1:]
		pw.atBOL = true
	}
	return written, nil
}
