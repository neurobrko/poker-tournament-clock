//go:build windows

package sleepinhibit

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
)

const (
	esContinuous      = 0x80000000
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
)

var setThreadExecutionState = syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")

type Inhibitor struct {
	mu      sync.Mutex
	done    chan struct{}
	stopped chan struct{}
}

func New() *Inhibitor { return &Inhibitor{} }

func (i *Inhibitor) Start() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.done != nil {
		return nil
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	ready := make(chan error, 1)
	i.done, i.stopped = done, stopped
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(stopped)
		result, _, callErr := setThreadExecutionState.Call(esContinuous | esSystemRequired | esDisplayRequired)
		if result == 0 {
			if callErr == syscall.Errno(0) {
				callErr = errors.New("SetThreadExecutionState failed")
			}
			ready <- callErr
			return
		}
		ready <- nil
		<-done
		_, _, _ = setThreadExecutionState.Call(esContinuous)
	}()
	if err := <-ready; err != nil {
		i.done, i.stopped = nil, nil
		return err
	}
	return nil
}

func (i *Inhibitor) Stop() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.done == nil {
		return nil
	}
	close(i.done)
	<-i.stopped
	i.done, i.stopped = nil, nil
	return nil
}
