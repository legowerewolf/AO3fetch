package osc

import (
	"strconv"
)

func SetProgress(state int, progress float64) string {
	return "\x1b]9;4;" + strconv.Itoa(state) + ";" + strconv.Itoa(int(progress*100)) + "\x07"
}

func SetTitle(title string) string {
	return "\x1b]0;" + title + "\x07"
}
