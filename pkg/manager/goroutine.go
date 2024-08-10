package manager

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// GoroutineManagerHooks allows hooking into the goroutine manager's lifecycle
type GoroutineManagerHooks struct {
	OnAfterRecover func() // Runs after recovering from a panic, but before stopping all goroutines
}

// GoroutineManager
type GoroutineManager struct {
	errsLock *sync.Mutex
	wg       *sync.WaitGroup

	goroutineCtx      context.Context
	cancelInternalCtx context.CancelCauseFunc

	errFinished error

	recoverFromPanics func(track bool) func()

	errGoroutineStopped error
}

// NewGoroutineManager creates a new goroutine manager
func NewGoroutineManager(
	ctx context.Context, // Parent context to use

	errs *error, // An error variable to collect panics and errors into

	hooks GoroutineManagerHooks, // Lifecycle hooks
) *GoroutineManager {
	var (
		errsLock sync.Mutex
		wg       sync.WaitGroup
	)

	internalCtx, cancelInternalCtx := context.WithCancelCause(ctx)

	errFinished := errors.New("finished") // This has to be a distinct error type for each panic handler, so we can't define it on the package level

	recoverFromPanics := func(track bool) func() {
		return func() {
			if track {
				defer wg.Done()
			}

			if err := recover(); err != nil {
				errsLock.Lock()
				defer errsLock.Unlock()

				var e error
				if v, ok := err.(error); ok {
					e = v
				} else {
					e = fmt.Errorf("%v", err)
				}

				if !(errors.Is(e, context.Canceled) && errors.Is(context.Cause(internalCtx), errFinished)) {
					*errs = errors.Join(*errs, e)

					if hook := hooks.OnAfterRecover; hook != nil {
						hook()
					}
				}

				cancelInternalCtx(errFinished)
			}
		}
	}

	return &GoroutineManager{
		&errsLock,
		&wg,

		internalCtx,
		cancelInternalCtx,

		errFinished,

		recoverFromPanics,

		errFinished,
	}
}

// Creates a panic collector that can be waited for to finish
func (m *GoroutineManager) CreateForegroundPanicCollector() func() {
	m.wg.Add(1)

	return m.recoverFromPanics(true)
}

// Creates a panic collector that can't be waited for to finish
func (m *GoroutineManager) CreateBackgroundPanicCollector() func() {
	return m.recoverFromPanics(false)
}

// Starts a goroutine that can't be waited for to finish and associates a panic collector
func (m *GoroutineManager) StartForegroundGoroutine(fn func()) {
	m.wg.Add(1)

	go func() {
		defer m.recoverFromPanics(true)()

		fn()
	}()
}

// Starts a goroutine that can't be waited for to finish and associates a panic collector
func (m *GoroutineManager) StartBackgroundGoroutine(fn func()) {
	go func() {
		defer m.recoverFromPanics(false)()

		fn()
	}()
}

// Stops both foreground and background goroutines by cancelling the goroutine context, but doesn't wait for them to finish
func (m *GoroutineManager) StopAllGoroutines() {
	m.cancelInternalCtx(m.errFinished)
}

// Waits for all foreground goroutines to finish
func (m *GoroutineManager) WaitForForegroundGoroutines() {
	m.wg.Wait()
}

// Gets the goroutine context that should be passed to any child goroutines
func (m *GoroutineManager) GetGoroutineCtx() context.Context {
	return m.goroutineCtx
}

// Gets the context cause that is set when a goroutine is stopped by m.StopAllGoroutines()
func (m *GoroutineManager) GetErrGoroutineStopped() error {
	return m.errGoroutineStopped
}
