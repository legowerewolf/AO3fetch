package logbuffer

import (
	"strings"
	"testing"
)

func FuzzGetAtMostFromEnd(f *testing.F) {
	f.Add("Hello, World!", 1)
	f.Add("Hello, World!", 10)
	f.Add("Hello, World!\nLine2\nLine3\nLine4\nLine5", 3)

	f.Fuzz(func(t *testing.T, s string, l int) {
		buf := NewLogBuffer()
		buf.Write([]byte(s))

		expectedLineCount := max(0, min(strings.Count(s, "\n")+1, l))

		lines := buf.GetAtMostFromEnd(l)

		if len(lines) != expectedLineCount {

			t.Fail()
		}
	})
}
