package logbuffer

import (
	"reflect"
	"testing"
)

func TestBufferKeepsNewestCompleteLines(t *testing.T) {
	buffer := New(3)
	_, _ = buffer.Write([]byte("one\ntwo\npartial"))
	_, _ = buffer.Write([]byte(" line\nthree\nfour\n"))

	want := []string{"partial line", "three", "four"}
	if got := buffer.Lines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %#v, want %#v", got, want)
	}
}

func TestLinesReturnsCopy(t *testing.T) {
	buffer := New(2)
	_, _ = buffer.Write([]byte("one\n"))
	lines := buffer.Lines()
	lines[0] = "changed"
	if got := buffer.Lines()[0]; got != "one" {
		t.Fatalf("stored line changed through returned slice: %q", got)
	}
}
