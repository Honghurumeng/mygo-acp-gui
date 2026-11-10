package acp

import (
	"bufio"
	"io"
)

// lineReader splits an ACP stream into newline-delimited messages.
//
// Only '\n' terminates a line. A trailing '\r' is stripped for CRLF streams.
// The reader never splits on U+2028 / U+2029: those are valid JSON string
// content and some agents emit them in message text.
type lineReader struct {
	br         *bufio.Reader
	pendingErr error
	eof        bool
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{br: bufio.NewReaderSize(r, 64*1024)}
}

// next returns the next line without its terminator. A final line without a
// trailing newline is returned once, followed by io.EOF.
func (l *lineReader) next() ([]byte, error) {
	if l.pendingErr != nil {
		err := l.pendingErr
		l.pendingErr = nil
		return nil, err
	}
	if l.eof {
		return nil, io.EOF
	}

	line, err := l.br.ReadBytes('\n')
	if err != nil {
		if err == io.EOF {
			l.eof = true
		} else {
			l.pendingErr = err
		}
	}
	if len(line) == 0 {
		return nil, err
	}

	if line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line, nil
}
