package acp

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestLineReader(t *testing.T) {
	u2028 := string(rune(0x2028))
	input := "one\n" + "two\r\n" + "\n" + "three" + u2028 + "four\n" + "tail"
	lr := newLineReader(iotest.OneByteReader(strings.NewReader(input)))

	want := []string{"one", "two", "", "three" + u2028 + "four", "tail"}
	for i, expected := range want {
		got, err := lr.next()
		if err != nil {
			t.Fatalf("line %d: unexpected error: %v", i, err)
		}
		if string(got) != expected {
			t.Errorf("line %d = %q, want %q", i, got, expected)
		}
	}

	if line, err := lr.next(); !errors.Is(err, io.EOF) || line != nil {
		t.Errorf("after last line: got %q, %v; want nil, EOF", line, err)
	}
}

func TestLineReaderDoesNotSplitU2028(t *testing.T) {
	const content = `{"text":"a` + "\u2028" + `b"}`
	lr := newLineReader(strings.NewReader(content + "\n"))
	line, err := lr.next()
	if err != nil {
		t.Fatal(err)
	}
	if string(line) != content {
		t.Errorf("line = %q, want the whole JSON line", line)
	}
}
