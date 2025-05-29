package crawler

import (
	"net/url"
	"strconv"
	"testing"
)

func initTestingRuntimeModel() (m RuntimeModel) {
	u, _ := url.Parse("https://archiveofourown.org")

	return InitRuntimeModel(false, 0, *u, 0, nil)
}

func TestQueueUrlRepeatedly(t *testing.T) {
	m := initTestingRuntimeModel()

	initial := m.queue.Len()

	addUrl := "https://TestQueueUrlRepeatedly.com"

	m.queueUrl(addUrl)
	m.queueUrl(addUrl)

	if m.queue.Len() != initial+1 {
		t.Fail()
	}
}

func FuzzQueueUrlRange(f *testing.F) {
	f.Add(1, 2)

	f.Fuzz(func(t *testing.T, low int, high int) {
		// can't have a negative page number
		if low < 0 || high < 0 {
			t.SkipNow()
		}

		u, _ := url.Parse("https://FuzzQueueUrlRange.com?page=" + strconv.Itoa(low))

		m := initTestingRuntimeModel()

		initialSize := m.queue.Len()

		m.queueUrlRange(*u, high)

		added := m.queue.Len() - initialSize

		// if we're misordered we expect nothing
		if high < low && added != 0 {
			t.Fail()
		}

		// if we're in the correct order, we expect all urls from low to high inclusive
		if low <= high && added != high-low+1 {
			t.Fail()
		}
	})
}
