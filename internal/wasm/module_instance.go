package wasm

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/sys"
)

// FailIfClosed returns a sys.ExitError if CloseWithExitCode was called.
func (m *ModuleInstance) FailIfClosed() (err error) {
	if closed := m.Closed.Load(); closed != 0 {
		switch closed & exitCodeFlagMask {
		case exitCodeFlagResourceClosed:
		case exitCodeFlagResourceNotClosed:
			// This happens when this module is closed asynchronously in CloseModuleOnCanceledOrTimeout,
			// and the closure of resources have been deferred here.
			_ = m.ensureResourcesClosed(context.Background())
		}
		return sys.NewExitError(uint32(closed >> 32)) // Unpack the high order bits as the exit code.
	}
	return nil
}

// CloseModuleOnCanceledOrTimeout take a context `ctx`, which might be a Cancel or Timeout context,
// and closes the module with the appropriate exit code when the context is canceled or its deadline
// is exceeded before the returned context.CancelFunc is called.
//
// Callers of this function must invoke the returned context.CancelFunc when the call ends.
// Engines that call repeatedly should reuse a CloseWatch instead.
func (m *ModuleInstance) CloseModuleOnCanceledOrTimeout(ctx context.Context) context.CancelFunc {
	if ctx.Done() == nil {
		// The context can never be canceled.
		return noopCancel
	}
	return (&CloseWatch{}).Start(m, ctx).Stop
}

func noopCancel() {}

// CloseWatch closes a module when the context of a call ends before the call
// does. One CloseWatch serves the sequential calls of one call engine; it uses
// context.AfterFunc instead of a goroutine per call that waits on ctx.Done().
type CloseWatch struct {
	mu       sync.Mutex
	m        *ModuleInstance
	ctx      context.Context
	stop     func() bool
	fireFunc func()
	finished bool
}

// Start watches ctx for one call of m and returns the watch to Stop when the
// call ends. A nested call while w is active gets a new watch.
func (w *CloseWatch) Start(m *ModuleInstance, ctx context.Context) *CloseWatch {
	if w.stop != nil {
		return (&CloseWatch{}).Start(m, ctx)
	}
	if ctx.Done() == nil {
		// A nested non-cancelable call must not stop its outer call's watch.
		return w
	}
	if w.fireFunc == nil {
		w.fireFunc = w.fire
	}
	w.mu.Lock()
	w.m, w.ctx, w.finished = m, ctx, false
	w.mu.Unlock()
	w.stop = context.AfterFunc(ctx, w.fireFunc)
	return w
}

// Stop ends the watch that Start began.
func (w *CloseWatch) Stop() {
	if w.stop == nil {
		return
	}
	w.mu.Lock()
	w.finished = true
	w.m, w.ctx = nil, nil
	w.mu.Unlock()
	w.stop()
	w.stop = nil
}

// fire runs when a watched context ends. A callback from an earlier call can run
// after a later Start; it then sees the context of the later call, which closes
// the module only if that context ended too.
func (w *CloseWatch) fire() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return
	}
	// Serialize closing with Stop and the next Start. An expired callback
	// must not close an instance after a later call has begun.
	m, err := w.m, w.ctx.Err()
	// This is the same logic as CloseWithCtxErr except this calls closeWithExitCodeWithoutClosingResource
	// so that we can defer the resource closure in FailIfClosed.
	switch {
	case errors.Is(err, context.Canceled):
		// TODO: figure out how to report error here.
		_ = m.closeWithExitCodeWithoutClosingResource(sys.ExitCodeContextCanceled)
	case errors.Is(err, context.DeadlineExceeded):
		// TODO: figure out how to report error here.
		_ = m.closeWithExitCodeWithoutClosingResource(sys.ExitCodeDeadlineExceeded)
	}
}

// CloseWithCtxErr closes the module with an exit code based on the type of
// error reported by the context.
//
// If the context's error is unknown or nil, the module does not close.
func (m *ModuleInstance) CloseWithCtxErr(ctx context.Context) {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		// TODO: figure out how to report error here.
		_ = m.CloseWithExitCode(ctx, sys.ExitCodeContextCanceled)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		// TODO: figure out how to report error here.
		_ = m.CloseWithExitCode(ctx, sys.ExitCodeDeadlineExceeded)
	}
}

// Name implements the same method as documented on api.Module
func (m *ModuleInstance) Name() string {
	return m.ModuleName
}

// String implements the same method as documented on api.Module
func (m *ModuleInstance) String() string {
	return fmt.Sprintf("Module[%s]", m.Name())
}

// Close implements the same method as documented on api.Module.
func (m *ModuleInstance) Close(ctx context.Context) (err error) {
	return m.CloseWithExitCode(ctx, 0)
}

// CloseWithExitCode implements the same method as documented on api.Module.
func (m *ModuleInstance) CloseWithExitCode(ctx context.Context, exitCode uint32) (err error) {
	if !m.setExitCode(exitCode, exitCodeFlagResourceClosed) {
		return nil // not an error to have already closed
	}
	_ = m.s.deleteModule(m)
	return m.ensureResourcesClosed(ctx)
}

// IsClosed implements the same method as documented on api.Module.
func (m *ModuleInstance) IsClosed() bool {
	return m.Closed.Load() != 0
}

func (m *ModuleInstance) closeWithExitCodeWithoutClosingResource(exitCode uint32) (err error) {
	if !m.setExitCode(exitCode, exitCodeFlagResourceNotClosed) {
		return nil // not an error to have already closed
	}
	_ = m.s.deleteModule(m)
	return nil
}

// closeWithExitCode is the same as CloseWithExitCode besides this doesn't delete it from Store.moduleList.
func (m *ModuleInstance) closeWithExitCode(ctx context.Context, exitCode uint32) (err error) {
	if !m.setExitCode(exitCode, exitCodeFlagResourceClosed) {
		return nil // not an error to have already closed
	}
	return m.ensureResourcesClosed(ctx)
}

type exitCodeFlag = uint64

const exitCodeFlagMask = 0xff

const (
	// exitCodeFlagResourceClosed indicates that the module was closed and resources were already closed.
	exitCodeFlagResourceClosed = 1 << iota
	// exitCodeFlagResourceNotClosed indicates that the module was closed while resources are not closed yet.
	exitCodeFlagResourceNotClosed
)

func (m *ModuleInstance) setExitCode(exitCode uint32, flag exitCodeFlag) bool {
	closed := flag | uint64(exitCode)<<32 // Store exitCode as high-order bits.
	return m.Closed.CompareAndSwap(0, closed)
}

// ensureResourcesClosed ensures that resources assigned to ModuleInstance is released.
// Only one call will happen per module, due to external atomic guards on Closed.
func (m *ModuleInstance) ensureResourcesClosed(ctx context.Context) (err error) {
	if closeNotifier := m.CloseNotifier; closeNotifier != nil { // experimental
		closeNotifier.CloseNotify(ctx, uint32(m.Closed.Load()>>32))
		m.CloseNotifier = nil
	}

	if sysCtx := m.Sys; sysCtx != nil { // nil if from HostModuleBuilder
		err = sysCtx.FS().Close()
		m.Sys = nil
	}

	if mem := m.MemoryInstance; mem != nil && mem.ownerModuleEngine == m.Engine {
		if mem.expBuffer != nil {
			mem.expBuffer.Free()
			mem.expBuffer = nil
		}
	}

	if m.CodeCloser != nil {
		if e := m.CodeCloser.Close(ctx); err == nil {
			err = e
		}
		m.CodeCloser = nil
	}
	return err
}

// Memory implements the same method as documented on api.Module.
func (m *ModuleInstance) Memory() api.Memory {
	return m.MemoryInstance
}

// ExportedMemory implements the same method as documented on api.Module.
func (m *ModuleInstance) ExportedMemory(name string) api.Memory {
	_, err := m.getExport(name, ExternTypeMemory)
	if err != nil {
		return nil
	}
	// We Assume that we have at most one memory.
	return m.MemoryInstance
}

// ExportedMemoryDefinitions implements the same method as documented on
// api.Module.
func (m *ModuleInstance) ExportedMemoryDefinitions() map[string]api.MemoryDefinition {
	// Special case as we currently only support one memory.
	if mem := m.MemoryInstance; mem != nil {
		// Now, find out if it is exported
		for name, exp := range m.Exports {
			if exp.Type == ExternTypeMemory {
				return map[string]api.MemoryDefinition{name: mem.definition}
			}
		}
	}
	return map[string]api.MemoryDefinition{}
}

// ExportedFunction implements the same method as documented on api.Module.
func (m *ModuleInstance) ExportedFunction(name string) api.Function {
	exp, err := m.getExport(name, ExternTypeFunc)
	if err != nil {
		return nil
	}
	return m.Engine.NewFunction(exp.Index)
}

// ExportedFunctionDefinitions implements the same method as documented on
// api.Module.
func (m *ModuleInstance) ExportedFunctionDefinitions() map[string]api.FunctionDefinition {
	result := map[string]api.FunctionDefinition{}
	for name, exp := range m.Exports {
		if exp.Type == ExternTypeFunc {
			result[name] = m.Source.FunctionDefinition(exp.Index)
		}
	}
	return result
}

// GlobalVal is an internal hack to get the lower 64 bits of a global.
func (m *ModuleInstance) GlobalVal(idx Index) uint64 {
	return m.Globals[idx].Val
}

// ExportedGlobal implements the same method as documented on api.Module.
func (m *ModuleInstance) ExportedGlobal(name string) api.Global {
	exp, err := m.getExport(name, ExternTypeGlobal)
	if err != nil {
		return nil
	}
	g := m.Globals[exp.Index]
	if g.Type.Mutable {
		return mutableGlobal{g: g}
	}
	return constantGlobal{g: g}
}

// NumGlobal implements experimental.InternalModule.
func (m *ModuleInstance) NumGlobal() int {
	return len(m.Globals)
}

// Global implements experimental.InternalModule.
func (m *ModuleInstance) Global(idx int) api.Global {
	return constantGlobal{g: m.Globals[idx]}
}
