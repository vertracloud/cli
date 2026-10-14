package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const barWidth = 30

// Progress draws a one-line bar on w while bytes move. Off a terminal both
// returned funcs do nothing. update may be called from several goroutines;
// redraws are throttled, and the last one (loaded == total) always shows.
// done clears the line.
func Progress(w io.Writer, label string) (update func(loaded, total int64), done func()) {
	f, ok := w.(*os.File)
	if !ok || !isTTY(f) {
		return func(int64, int64) {}, func() {}
	}
	var mu sync.Mutex
	var last time.Time
	update = func(loaded, total int64) {
		mu.Lock()
		defer mu.Unlock()
		if loaded < total && time.Since(last) < 100*time.Millisecond {
			return
		}
		last = time.Now()
		if total <= 0 {
			total = 1
		}
		pct := min(loaded*100/total, 100)
		filled := int(pct) * barWidth / 100
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		fmt.Fprintf(f, "\r\x1b[2K%s %s %3d%%", Cyan(Plain(label)), Dim(bar), pct)
	}
	done = func() {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprint(f, "\r\x1b[2K")
	}
	return update, done
}
