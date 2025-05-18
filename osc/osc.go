package osc

import (
	"fmt"
)

func SetProgress(state int, progress float64) string {
	return fmt.Sprintf("\x1b]9;4;%d;%3.0f\x07", state, progress)
}

func SetTitle(title string) string {
	return fmt.Sprintf("\x1b]0;%s\x07", title)
}
