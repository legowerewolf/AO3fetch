package logbuffer

import (
	"fmt"
	"io"
	"strings"
)

type LogBuffer struct {
	stream *[]string
}

func NewLogBuffer() LogBuffer {
	return LogBuffer{
		stream: &[]string{},
	}
}

func (l LogBuffer) Write(p []byte) (n int, err error) {
	str := string(p)

	strs := strings.Split(str, "\n")

	if len(*l.stream) == 0 {
		*l.stream = append(*l.stream, "")
	}

	if len(strs) >= 1 {
		(*l.stream)[len(*l.stream)-1] += strs[0]
	}

	if len(strs) >= 2 {
		*l.stream = append(*l.stream, strs[1:]...)
	}

	return len(p), nil
}

func (l LogBuffer) GetAtMostFromEnd(lines int) []string {
	if lines <= 0 {
		return []string{}
	}

	if lines > len(*l.stream) {
		return *l.stream
	}

	return (*l.stream)[len(*l.stream)-lines:]
}

func (l LogBuffer) Dump(writer io.Writer) {
	for _, line := range *l.stream {
		fmt.Fprintln(writer, line)
	}
}
