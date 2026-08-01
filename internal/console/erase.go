package console

import "io"

// eraseLine returns the cursor to the start of the line and clears it.
const eraseLine = "\r\x1b[2K"

// EraseLineBefore returns a writer that clears the current terminal line ahead
// of everything written through it, in the same write.
//
// A progress bar paints one line and repaints it in place, while log records go
// to the same terminal with no knowledge of each other. Without this, a record
// lands on top of the bar's line or strands a copy of it. With it, the record
// wipes the line the bar painted and prints on a clean one, and the bar paints
// itself again on its next update.
//
// The erase and the record are one write because writes to one terminal are
// serialised inside the process, which is what stops a record and a bar frame
// from landing inside each other.
func EraseLineBefore(w io.Writer) io.Writer {
	return eraseLineWriter{w: w}
}

type eraseLineWriter struct {
	w io.Writer
}

func (e eraseLineWriter) Write(record []byte) (int, error) {
	buf := make([]byte, 0, len(eraseLine)+len(record))
	buf = append(buf, eraseLine...)
	buf = append(buf, record...)

	written, err := e.w.Write(buf)

	// The caller is owed a count of its own bytes, so the prefix comes back off,
	// and the result stays inside the range io.Writer promises.
	written -= len(eraseLine)
	written = max(written, 0)
	written = min(written, len(record))

	// A writer that stops early without saying why still owes its caller an
	// error, which io.Writer requires alongside a short count.
	if err == nil && written < len(record) {
		return written, io.ErrShortWrite
	}

	return written, err //nolint:wrapcheck // a writer passes its own error through
}
