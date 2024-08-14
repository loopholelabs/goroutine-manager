# Goroutine Manager

A Context, Dependency and Error-aware Goroutine Manager.

[![License: Apache 2.0](https://img.shields.io/github/license/loopholelabs/goroutine-manager)](./LICENSE)
[![Discord](https://dcbadge.vercel.app/api/server/JYmFhtdPeu?style=flat)](https://loopholelabs.io/discord)
[![hydrun CI](https://github.com/loopholelabs/goroutine-manager/actions/workflows/hydrun.yaml/badge.svg)](https://github.com/loopholelabs/goroutine-manager/actions/workflows/hydrun.yaml)
![Go Version](https://img.shields.io/badge/go%20version-%3E=1.21-61CFDD.svg)
[![Go Reference](https://pkg.go.dev/badge/github.com/loopholelabs/goroutine-manager.svg)](https://pkg.go.dev/github.com/loopholelabs/goroutine-manager)

## Overview

Goroutine Manager is a context-aware Go library designed to manage goroutine lifecycles, errors, and dependencies.

It enables you to:

- **Start, stop, and wait for goroutines**: Goroutine Manager allows you to start and gracefully stop both background and foreground goroutines. It also ensures that foreground goroutines exit cleanly before proceeding.
- **Handle panics and errors in goroutines safely**: Goroutine Manager collects panics across goroutines throughout their entire lifecycle, helping you avoid the typical complexities of asynchronous error handling.
- **Define dependencies and shutdown cascades**: As a context-aware tool, Goroutine Manager enables you to declaratively define dependencies between multiple goroutines and orchestrate context cancellations and shutdowns smoothly.

## Installation

You can add Goroutine Manager to your Go project by running the following:

```shell
$ go get github.com/loopholelabs/goroutine-manager/...@latest
```

## Tutorial

### 1. Starting a Goroutine with the Goroutine Manager

Goroutine Manager is primarily used through the `GoroutineManager` struct. In most typical use cases, it is started as follows:

```go
// Example parent context, usually this is the HTTP request context or your main application context for graceful shutdowns
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

var errs error
defer func() { // Just an example; you probably want to handle your errors differently in production
	if errs != nil {
		panic(errs)
	}
}()

goroutineManager := manager.NewGoroutineManager(
	ctx,
	&errs,
	manager.GoroutineManagerHooks{},
)
defer goroutineManager.Wait()
defer goroutineManager.StopAllGoroutines()
defer goroutineManager.CreateBackgroundPanicCollector()()
```

This setup ensures that any panics occurring after the last line will be collected into the `errs` variable. Any goroutines started after it will be stopped and waited for until they finish executing if a panic occurs or the stack unwinds, e.g., after a `return`.

To start a goroutine, you can use `StartForegroundGoroutine` or `StartBackgroundGoroutine`. Foreground goroutines are "tracked" and can be waited for to finish executing with `Wait`, while background goroutines are for "fire and forget" scenarios. Any context-aware libraries used in a goroutine should be passed the context returned by `Context` (which is also provided as an argument to `StartForegroundGoroutine` and `StartBackgroundGoroutine`) and should block until they have finished executing. This ensures that during a graceful shutdown, these dependencies will also be shut down, and in the case of foreground goroutines, will be waited for. Note that panics in both foreground and background goroutines lead to `Context` being canceled, and the errors will be collected into `errs`.

### 2. Handling Externally Started Goroutines with the Goroutine Manager

If you have a goroutine that is started externally but you still need to react to any panics/errors in that goroutine and include it in your lifecycle (e.g., to stop other goroutines on an error), you can use `CreateForegroundPanicCollector` or `CreateBackgroundPanicCollector`. This is very useful if an error occurs in a hook in an external library that doesn’t bubble up errors from hooks, for example:

```go
cfg.Locker_handler = func() {
	defer goroutineManager.CreateBackgroundPanicCollector()()

	if err := to.SendEvent(&packets.Event{
		Type: packets.EventPreLock,
	}); err != nil {
		panic(errors.Join(ErrCouldNotSendEvent, err))
	}

	input.storage.Lock()

	if err := to.SendEvent(&packets.Event{
		Type: packets.EventPostLock,
	}); err != nil {
		panic(errors.Join(ErrCouldNotSendEvent, err))
	}
}
```

`Locker_handler` doesn’t return any errors, so handling an error from calling `to.SendEvent` is difficult aside from logging it. Using `CreateBackgroundPanicCollector` allows the error to be collected into `errs` and the `GoroutineCtx` to be canceled when appropriate. This can be used to shut down whatever is calling `Locker_handler` in response to an error in the hook. It is also very useful in defer functions. Often, defer functions are used like this:

```go
defer forwardedPorts.Close()
```

However, this is problematic if `Close` returns an error. By deferring a panic collector, it becomes possible to collect the errors that occur during cleanup/stack unwinding into `errs` and react to them accordingly:

```go
defer func() {
	defer goroutineManager.CreateForegroundPanicCollector()()

	if err := forwardedPorts.Close(); err != nil {
		panic(err)
	}
}()
```

### 4. Gracefully Stopping Goroutines and Waiting for Them to Finish Executing

To gracefully stop a goroutine, simply call `StopAllGoroutines()`, or simply `return` if you're using the setup described above. `StopAllGoroutines` cancels `Context` with a special cause that is unique to each Goroutine Manager, which can be retrieved by calling `GetErrGoroutineStopped()`. `StartForegroundGoroutine`, `CreateBackgroundPanicCollector`, etc., handle any `context.Context` with this cause as a graceful shutdown, which means that `errs` will be `nil` on a graceful shutdown instead of containing `context.Canceled`. This allows you to distinguish between "intentional" context cancellations, e.g., one caused by sending an interrupt signal to a program, and "unintentional" context cancellations, e.g., one caused by a request timing out.

### 5. Handling Dependencies Between Goroutines

To handle dependencies between goroutines, e.g., if one goroutine needs to be shut down and waited for before another goroutine to prevent data corruption, you can use proxy contexts. For example, if you want to ensure that a goroutine using `firecrackerCtx` does not shut down before `hypervisorCtx` has been canceled, you can intercept the context and handle it correctly as follows:

```go
var ongoingResumeWg sync.WaitGroup

firecrackerCtx, cancelFirecrackerCtx := context.WithCancel(rescueCtx)
go func() {
	<-hypervisorCtx.Done() // We use hypervisorCtx, not goroutineManager.InternalCtx here since this resource outlives the function call

	ongoingResumeWg.Wait()

	cancelFirecrackerCtx()
}()
```

Using a secondary `rescueCtx` as the parent context for the proxy context is helpful here to time out such dependencies, for example, by sending a second interrupt signal to your interrupt handler. Specifics for handling complex dependencies will depend on your individual use case and need to be figured out on a case-by-case basis.

🚀 That's it! We can’t wait to see what you’re going to build with the Goroutine Manager.

## Reference

To make getting started with Goroutine Manager easier, take a look at the following examples:

- [Starting a Goroutine](https://github.com/loopholelabs/drafter/blob/use-go-resource-manager/cmd/drafter-forwarder/main.go#L114-L118)
- [Handling Externally Started Goroutines](https://github.com/loopholelabs/drafter/blob/use-go-resource-manager/pkg/roles/mounter.go#L782-L798)
- [Handling Errors in `defer`ed Functions](https://github.com/loopholelabs/drafter/blob/use-go-resource-manager/cmd/drafter-forwarder/main.go#L106-L112)
- [Gracefully Stopping Goroutines and Waiting for Them to Finish Executing](https://github.com/loopholelabs/drafter/blob/use-go-resource-manager/pkg/roles/mounter.go#L132-L139)
- [Handling Dependencies Between Goroutines](https://github.com/loopholelabs/drafter/blob/use-go-resource-manager/pkg/roles/runner.go#L142-L174)
- [Test cases](./pkg/manager/goroutine_test.go)

## Contributing

Bug reports and pull requests are welcome on GitHub at [https://github.com/loopholelabs/goroutine-manager](https://github.com/loopholelabs/goroutine-manager). For more contribution information check out [the contribution guide](./CONTRIBUTING.md).

## License

This project is available as open source under the terms of the [Apache License, Version 2.0](./LICENSE).

## Code of Conduct

Everyone interacting in the Goroutine Manager project's codebases, issue trackers, chat rooms and mailing lists is expected to follow the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md).

## Project Managed By:

[![https://loopholelabs.io](https://cdn.loopholelabs.io/loopholelabs/LoopholeLabsLogo.svg)](https://loopholelabs.io)
