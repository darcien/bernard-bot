package commands

import (
	"sync"
	"time"
)

// background tracks goroutines spawned by deferred commands so graceful
// shutdown can wait for in-flight followups — http.Server.Shutdown only
// tracks HTTP connections, and the deferred response ends the HTTP request
// before the real work finishes.
var background sync.WaitGroup

func goBackground(fn func()) {
	background.Add(1)
	go func() {
		defer background.Done()
		fn()
	}()
}

// WaitBackground blocks until all background tasks finish or timeout elapses.
// Returns false on timeout.
func WaitBackground(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		background.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
