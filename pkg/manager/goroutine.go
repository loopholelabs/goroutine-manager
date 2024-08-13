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

// GoroutineManager provides panic handling and lifecycle management for
// goroutines.
type GoroutineManager struct {
	errs     *error
	errsLock *sync.Mutex
	wg       *sync.WaitGroup

	internalCtx       context.Context
	cancelInternalCtx context.CancelCauseFunc

	errFinished error

	hooks GoroutineManagerHooks
}

// NewGoroutineManager creates a new goroutine manager.
//
// Errors caused by panics are stored in errs, which must only be accessed
// after Wait() returns.
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

	return &GoroutineManager{
		errs,
		&errsLock,
		&wg,

		internalCtx,
		cancelInternalCtx,

		errFinished,

		hooks,
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

// Starts a goroutine that can be waited for to finish and associates a panic collector
func (m *GoroutineManager) StartForegroundGoroutine(fn func(context.Context)) {
	m.wg.Add(1)

	go func() {
		defer m.recoverFromPanics(true)()

		fn(m.internalCtx)
	}()
}

// Starts a goroutine that can't be waited for to finish and associates a panic collector
func (m *GoroutineManager) StartBackgroundGoroutine(fn func(context.Context)) {
	go func() {
		defer m.recoverFromPanics(false)()

		fn(m.internalCtx)
	}()
}

// Stops both foreground and background goroutines by cancelling the goroutine
// context, but doesn't wait for them to finish.
//
// Since the goroutine context is cancelled, goroutines started after
// StopAllGoroutines() is called may return immediately.
func (m *GoroutineManager) StopAllGoroutines() {
	m.cancelInternalCtx(m.errFinished)
}

// Waits for all foreground goroutines to finish. All calls must return before
// starting new foreground goroutines.
func (m *GoroutineManager) Wait() {
	m.wg.Wait()
}

// Gets the goroutine context that should be passed to any child goroutines
func (m *GoroutineManager) Context() context.Context {
	return m.internalCtx
}

// Gets the context cause that is set when a goroutine is stopped by m.StopAllGoroutines()
func (m *GoroutineManager) GetErrGoroutineStopped() error {
	return m.errFinished
}

// recoverFromPanics recovers the last panic and adds the error to errors list.
// It musT be called from a defer statement, otherwise recover() returns nil.
func (m *GoroutineManager) recoverFromPanics(track bool) func() {
	return func() {
		if track {
			defer m.wg.Done()
		}

		if err := recover(); err != nil {
			m.errsLock.Lock()
			defer m.errsLock.Unlock()

			var e error
			if v, ok := err.(error); ok {
				e = v
			} else {
				e = fmt.Errorf("%v", err)
			}

			if !(errors.Is(e, context.Canceled) && errors.Is(context.Cause(m.internalCtx), m.errFinished)) {
				*m.errs = errors.Join(*m.errs, e)

				if hook := m.hooks.OnAfterRecover; hook != nil {
					hook()
				}
			}

			m.cancelInternalCtx(m.errFinished)
		}
	}
}
