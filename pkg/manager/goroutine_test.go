package manager

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var testErr = errors.New("test error")

func TestForegroundPanicRecover(t *testing.T) {
	t.Parallel()

	testFn := func(t *testing.T) (errs error) {
		t.Helper()

		m := NewGoroutineManager(context.Background(), &errs, GoroutineManagerHooks{})

		// Verification function needs to be registered before the panic and
		// recovery so it runs after them.
		defer func() {
			require.ErrorIs(t, errs, testErr)
			requireNotBlocked(t, m)
			requireDone(t, m)
		}()
		defer m.Wait()
		defer m.StopAllGoroutines()
		defer m.CreateForegroundPanicCollector()()

		panic(testErr)
	}
	err := testFn(t)
	require.ErrorIs(t, err, testErr)
}

func TestBackgroundPanicRecover(t *testing.T) {
	t.Parallel()

	testFn := func(t *testing.T) (errs error) {
		t.Helper()

		m := NewGoroutineManager(context.Background(), &errs, GoroutineManagerHooks{})

		// Verification function needs to be registered before the panic and
		// recovery so it runs after them.
		defer func() {
			require.ErrorIs(t, errs, testErr)
			requireNotBlocked(t, m)
			requireDone(t, m)
		}()
		defer m.Wait()
		defer m.StopAllGoroutines()
		defer m.CreateBackgroundPanicCollector()()

		panic(testErr)
	}
	err := testFn(t)
	require.ErrorIs(t, err, testErr)
}

func TestForegroundGoroutine(t *testing.T) {
	t.Parallel()

	var errs error
	m := NewGoroutineManager(context.Background(), &errs, GoroutineManagerHooks{})

	done := make(chan any)
	m.StartForegroundGoroutine(func(_ context.Context) {
		<-done
		panic(testErr)
	})

	// Verify goroutine manager is blocked and no error is set yet.
	requireBlocked(t, m)
	requireNotDone(t, m)
	require.NoError(t, errs)

	// Unblock goroutine to cause the panic.
	close(done)

	// Verify goroutine manager unblocks and the panic error is set.
	requireNotBlocked(t, m)
	requireDone(t, m)
	require.ErrorIs(t, errs, testErr)
}

func TestBackgroundGoroutine(t *testing.T) {
	t.Parallel()

	var errs error
	m := NewGoroutineManager(context.Background(), &errs, GoroutineManagerHooks{})

	done := make(chan any)
	m.StartBackgroundGoroutine(func(_ context.Context) {
		<-done
		panic(testErr)
	})

	// Verify goroutine manager is not blocked by background goroutines.
	requireNotBlocked(t, m)

	// Verify goroutine manager context is not cancelled and no error is set.
	requireNotDone(t, m)
	require.NoError(t, errs)

	// Unblock goroutine to cause the panic.
	close(done)

	// Verify goroutine manager is still not blocked, but its context is not
	// done and the panic error is set.
	requireNotBlocked(t, m)
	requireDone(t, m)
	require.ErrorIs(t, errs, testErr)
}

func TestStopAllGoroutines(t *testing.T) {
	t.Parallel()

	var errs error
	m := NewGoroutineManager(context.Background(), &errs, GoroutineManagerHooks{})

	bgStarted := make(chan any)
	bgDone := make(chan any)
	m.StartBackgroundGoroutine(func(ctx context.Context) {
		close(bgStarted)
		<-ctx.Done()
		close(bgDone)
	})

	fgStarted := make(chan any)
	fgDone := make(chan any)
	m.StartForegroundGoroutine(func(ctx context.Context) {
		close(fgStarted)
		<-ctx.Done()
		close(fgDone)
	})

	// Wait for goroutines to start.
	<-bgStarted
	<-fgStarted

	// Verify goroutines are not done.
	require.Never(t, func() bool {
		<-bgDone
		return true
	}, 50*time.Millisecond, time.Millisecond)
	require.Never(t, func() bool {
		<-fgDone
		return true
	}, 50*time.Millisecond, time.Millisecond)

	// Verify goroutine manager is blocked and no error is set.
	requireBlocked(t, m)
	requireNotDone(t, m)
	require.NoError(t, errs)

	// Stop all goroutines.
	m.StopAllGoroutines()
	m.Wait()

	// Verify goroutines are done.
	<-bgDone
	<-fgDone

	// Verify goroutine manager is unblocked and no error is set because the
	// goroutines didn't panic.
	requireNotBlocked(t, m)
	requireDone(t, m)
	require.NoError(t, errs)
}

func TestMultipleGoroutines(t *testing.T) {
	t.Parallel()

	var errs error
	m := NewGoroutineManager(context.Background(), &errs, GoroutineManagerHooks{})

	fg1 := make(chan any)
	m.StartForegroundGoroutine(func(_ context.Context) {
		<-fg1
	})

	fg2 := make(chan any)
	go func() {
		defer m.CreateForegroundPanicCollector()()
		<-fg2
	}()

	bg1 := make(chan any)
	m.StartBackgroundGoroutine(func(_ context.Context) {
		<-bg1
	})

	// Verify goroutine manager is blocked and no error is set.
	requireBlocked(t, m)
	requireNotDone(t, m)
	require.NoError(t, errs)

	// Unblock one foreground goroutine.
	close(fg1)

	// Verify goroutine manager is still blocked.
	requireBlocked(t, m)
	requireNotDone(t, m)
	require.NoError(t, errs)

	// Unblock background goroutine.
	close(bg1)

	// Verify goroutine manager is still blocked.
	requireBlocked(t, m)
	requireNotDone(t, m)
	require.NoError(t, errs)

	// Unblock second goroutine.
	close(fg2)

	// Verify goroutine manager is now unblocked, but context is still not done
	// since there was no panic.
	requireNotBlocked(t, m)
	requireNotDone(t, m)
	require.NoError(t, errs)
}

func TestReuseGoroutineManager(t *testing.T) {
	t.Parallel()

	var errs error
	m := NewGoroutineManager(context.Background(), &errs, GoroutineManagerHooks{})

	// Start a few goroutines and wait.
	fg1 := make(chan any)
	m.StartForegroundGoroutine(func(_ context.Context) {
		<-fg1
	})

	fg2 := make(chan any)
	m.StartForegroundGoroutine(func(_ context.Context) {
		<-fg2
	})

	close(fg1)
	close(fg2)
	m.Wait()

	// Start another set of goroutines and wait again.
	fg3 := make(chan any)
	m.StartForegroundGoroutine(func(_ context.Context) {
		<-fg3
	})

	fg4 := make(chan any)
	m.StartForegroundGoroutine(func(_ context.Context) {
		<-fg4
	})

	close(fg3)
	close(fg4)
	m.Wait()

	// Start more goroutines using the internal context and stop them.
	m.StartForegroundGoroutine(func(ctx context.Context) {
		<-ctx.Done()
	})

	m.StartForegroundGoroutine(func(ctx context.Context) {
		<-ctx.Done()
	})

	m.StopAllGoroutines()
	m.Wait()

	// Verify goroutines started after stop return immediately.
	done := false
	m.StartForegroundGoroutine(func(ctx context.Context) {
		select {
		case <-ctx.Done():
			done = true
			return
		default:
		}
		panic(testErr)
	})
	m.Wait()
	require.True(t, done)
	require.NoError(t, errs)
}

func TestMultipleGoroutineErrors(t *testing.T) {
	t.Parallel()

	var errs error
	m := NewGoroutineManager(context.Background(), &errs, GoroutineManagerHooks{})

	err1 := errors.New("test error 1")
	fg1 := make(chan any)
	m.StartForegroundGoroutine(func(ctx context.Context) {
		<-fg1
		panic(err1)
	})

	err2 := errors.New("test error 2")
	m.StartForegroundGoroutine(func(ctx context.Context) {
		<-ctx.Done()
		panic(err2)
	})

	// Verify goroutine manager is blocked and no error is set yet.
	requireBlocked(t, m)
	requireNotDone(t, m)
	require.NoError(t, errs)

	// Unblock first goroutine to cause a panic.
	close(fg1)

	// Verify second goroutine and the goroutine manager were unblocked by the
	// panic.
	requireNotBlocked(t, m)

	// Verify both panics were captured.
	require.ErrorIs(t, errs, err2)
	require.ErrorIs(t, errs, err1)
}

func TestParentContext(t *testing.T) {
	t.Parallel()

	var errs error
	ctx, cancel := context.WithCancel(context.Background())
	m := NewGoroutineManager(ctx, &errs, GoroutineManagerHooks{})

	m.StartForegroundGoroutine(func(ctx context.Context) {
		<-ctx.Done()
		panic(testErr)
	})

	// Verify goroutine manager is blocked and no error is set yet.
	requireBlocked(t, m)
	requireNotDone(t, m)
	require.NoError(t, errs)

	// Cancel parent context.
	cancel()

	// Verify goroutine manager is unblocked and the panic error is set.
	requireNotBlocked(t, m)
	requireDone(t, m)
	require.ErrorIs(t, errs, testErr)
}

func TestHooks_OnAfterRecover(t *testing.T) {
	t.Parallel()

	var counter atomic.Uint64
	var errs error
	m := NewGoroutineManager(context.Background(), &errs, GoroutineManagerHooks{
		OnAfterRecover: func() {
			counter.Add(1)
		},
	})

	for i := 0; i < 300; i++ {
		m.StartForegroundGoroutine(func(_ context.Context) {
			panic(testErr)
		})
	}

	m.Wait()
	require.Equal(t, uint64(300), counter.Load())
}

// requireBlocked fails if the goroutine manager Wait() method is not blocked.
func requireBlocked(t *testing.T, m *GoroutineManager) {
	t.Helper()

	require.Never(t, func() bool {
		m.Wait()
		return true
	}, 100*time.Millisecond, time.Millisecond, "goroutine manager is not blocked")
}

// requireBlocked fails if the goroutine manager Wait() method is blocked.
func requireNotBlocked(t *testing.T, m *GoroutineManager) {
	t.Helper()

	require.Eventually(t, func() bool {
		m.Wait()
		return true
	}, 10*time.Millisecond, time.Millisecond, "goroutine manager is blocked")
}

// requireDone fails if the goroutine manager Context() is not done.
func requireDone(t *testing.T, m *GoroutineManager) {
	t.Helper()

	select {
	case <-m.Context().Done():
	default:
		t.Fatalf("expected goroutine context to not be done")
	}
}

// requireDone fails if the goroutine manager Context() is done.
func requireNotDone(t *testing.T, m *GoroutineManager) {
	t.Helper()

	select {
	case <-m.Context().Done():
		t.Fatalf("expected goroutine context to not be done")
	default:
	}
}
