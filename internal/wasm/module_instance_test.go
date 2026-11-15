package wasm

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/experimental/sys"
	internalsys "github.com/tetratelabs/wazero/internal/sys"
	"github.com/tetratelabs/wazero/internal/sysfs"
	testfs "github.com/tetratelabs/wazero/internal/testing/fs"
	"github.com/tetratelabs/wazero/internal/testing/hammer"
	"github.com/tetratelabs/wazero/internal/testing/require"
)

func TestModuleInstance_String(t *testing.T) {
	s := newStore()

	tests := []struct {
		name, moduleName, expected string
	}{
		{
			name:       "empty",
			moduleName: "",
			expected:   "Module[]",
		},
		{
			name:       "not empty",
			moduleName: "math",
			expected:   "Module[math]",
		},
	}

	for _, tt := range tests {
		tc := tt

		t.Run(tc.name, func(t *testing.T) {
			// Ensure paths that can create the host module can see the name.
			m, err := s.Instantiate(testCtx, &Module{}, tc.moduleName, nil, nil)
			defer m.Close(testCtx) //nolint

			require.NoError(t, err)
			require.Equal(t, tc.expected, m.String())

			if name := m.Name(); name != "" {
				sm := s.Module(m.Name())
				if sm != nil {
					require.Equal(t, tc.expected, s.Module(m.Name()).String())
				} else {
					require.Zero(t, len(m.Name()))
				}
			}
		})
	}
}

func TestModuleInstance_Close(t *testing.T) {
	s := newStore()

	tests := []struct {
		name           string
		closer         func(context.Context, *ModuleInstance) error
		expectedClosed uint64
	}{
		{
			name: "Close()",
			closer: func(ctx context.Context, m *ModuleInstance) error {
				return m.Close(ctx)
			},
			expectedClosed: uint64(1),
		},
		{
			name: "CloseWithExitCode(255)",
			closer: func(ctx context.Context, m *ModuleInstance) error {
				return m.CloseWithExitCode(ctx, 255)
			},
			expectedClosed: uint64(255)<<32 + 1,
		},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(fmt.Sprintf("%s calls ns.CloseWithExitCode(module.name))", tc.name), func(t *testing.T) {
			moduleName := t.Name()
			m, err := s.Instantiate(testCtx, &Module{}, moduleName, nil, nil)
			require.NoError(t, err)

			// We use side effects to see if Close called ns.CloseWithExitCode (without repeating store_test.go).
			// One side effect of ns.CloseWithExitCode is that the moduleName can no longer be looked up.
			require.Equal(t, s.Module(moduleName), m)

			// Closing should not err.
			require.NoError(t, tc.closer(testCtx, m))

			require.Equal(t, tc.expectedClosed, m.Closed.Load())

			// Outside callers should be able to know it was closed.
			require.True(t, m.IsClosed())

			// Verify our intended side-effect
			require.Nil(t, s.Module(moduleName))

			// Verify no error closing again.
			require.NoError(t, tc.closer(testCtx, m))
		})
	}

	t.Run("calls Context.Close()", func(t *testing.T) {
		testFS := &sysfs.AdaptFS{FS: testfs.FS{"foo": &testfs.File{}}}
		sysCtx := internalsys.DefaultContext(testFS)
		fsCtx := sysCtx.FS()

		_, errno := fsCtx.OpenFile(testFS, "/foo", sys.O_RDONLY, 0)
		require.EqualErrno(t, 0, errno)

		m, err := s.Instantiate(testCtx, &Module{}, t.Name(), sysCtx, nil)
		require.NoError(t, err)

		// We use side effects to determine if Close in fact called Context.Close (without repeating sys_test.go).
		// One side effect of Context.Close is that it clears the openedFiles map. Verify our base case.
		_, ok := fsCtx.LookupFile(3)
		require.True(t, ok, "sysCtx.openedFiles was empty")

		// Closing should not err even when concurrently closed.
		hammer.NewHammer(t, 100, 10).Run(func(p, n int) {
			require.NoError(t, m.Close(testCtx))
			// closeWithExitCode is the one called during Store.CloseWithExitCode.
			require.NoError(t, m.closeWithExitCode(testCtx, 0))
		}, nil)
		if t.Failed() {
			return // At least one test failed, so return now.
		}

		// Verify our intended side-effect
		_, ok = fsCtx.LookupFile(3)
		require.False(t, ok, "expected no opened files")

		// Verify no error closing again.
		require.NoError(t, m.Close(testCtx))
	})

	t.Run("error closing", func(t *testing.T) {
		// Right now, the only way to err closing the sys context is if a File.Close erred.
		testFS := &sysfs.AdaptFS{FS: testfs.FS{"foo": &testfs.File{CloseErr: errors.New("error closing")}}}
		sysCtx := internalsys.DefaultContext(testFS)
		fsCtx := sysCtx.FS()

		_, errno := fsCtx.OpenFile(testFS, "/foo", sys.O_RDONLY, 0)
		require.EqualErrno(t, 0, errno)

		m, err := s.Instantiate(testCtx, &Module{}, t.Name(), sysCtx, nil)
		require.NoError(t, err)

		// In sys.FS, non syscall errors map to sys.EIO.
		require.EqualErrno(t, sys.EIO, m.Close(testCtx))

		// Verify our intended side-effect
		_, ok := fsCtx.LookupFile(3)
		require.False(t, ok, "expected no opened files")
	})
}

func TestModuleInstance_CallDynamic(t *testing.T) {
	s := newStore()

	tests := []struct {
		name           string
		closer         func(context.Context, *ModuleInstance) error
		expectedClosed uint64
	}{
		{
			name: "Close()",
			closer: func(ctx context.Context, m *ModuleInstance) error {
				return m.Close(ctx)
			},
			expectedClosed: uint64(1),
		},
		{
			name: "CloseWithExitCode(255)",
			closer: func(ctx context.Context, m *ModuleInstance) error {
				return m.CloseWithExitCode(ctx, 255)
			},
			expectedClosed: uint64(255)<<32 + 1,
		},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(fmt.Sprintf("%s calls ns.CloseWithExitCode(module.name))", tc.name), func(t *testing.T) {
			moduleName := t.Name()
			m, err := s.Instantiate(testCtx, &Module{}, moduleName, nil, nil)
			require.NoError(t, err)

			// We use side effects to see if Close called ns.CloseWithExitCode (without repeating store_test.go).
			// One side effect of ns.CloseWithExitCode is that the moduleName can no longer be looked up.
			require.Equal(t, s.Module(moduleName), m)

			// Closing should not err.
			require.NoError(t, tc.closer(testCtx, m))

			require.Equal(t, tc.expectedClosed, m.Closed.Load())

			// Verify our intended side-effect
			require.Nil(t, s.Module(moduleName))

			// Verify no error closing again.
			require.NoError(t, tc.closer(testCtx, m))
		})
	}

	t.Run("calls Context.Close()", func(t *testing.T) {
		testFS := &sysfs.AdaptFS{FS: testfs.FS{"foo": &testfs.File{}}}
		sysCtx := internalsys.DefaultContext(testFS)
		fsCtx := sysCtx.FS()

		_, errno := fsCtx.OpenFile(testFS, "/foo", sys.O_RDONLY, 0)
		require.EqualErrno(t, 0, errno)

		m, err := s.Instantiate(testCtx, &Module{}, t.Name(), sysCtx, nil)
		require.NoError(t, err)

		// We use side effects to determine if Close in fact called Context.Close (without repeating sys_test.go).
		// One side effect of Context.Close is that it clears the openedFiles map. Verify our base case.
		_, ok := fsCtx.LookupFile(3)
		require.True(t, ok, "sysCtx.openedFiles was empty")

		// Closing should not err.
		require.NoError(t, m.Close(testCtx))

		// Verify our intended side-effect
		_, ok = fsCtx.LookupFile(3)
		require.False(t, ok, "expected no opened files")

		// Verify no error closing again.
		require.NoError(t, m.Close(testCtx))
	})

	t.Run("error closing", func(t *testing.T) {
		// Right now, the only way to err closing the sys context is if a File.Close erred.
		testFS := &sysfs.AdaptFS{FS: testfs.FS{"foo": &testfs.File{CloseErr: errors.New("error closing")}}}
		sysCtx := internalsys.DefaultContext(testFS)
		fsCtx := sysCtx.FS()

		path := "/foo"
		_, errno := fsCtx.OpenFile(testFS, path, sys.O_RDONLY, 0)
		require.EqualErrno(t, 0, errno)

		m, err := s.Instantiate(testCtx, &Module{}, t.Name(), sysCtx, nil)
		require.NoError(t, err)

		// In sys.FS, non syscall errors map to sys.EIO.
		require.EqualErrno(t, sys.EIO, m.Close(testCtx))

		// Verify our intended side-effect
		_, ok := fsCtx.LookupFile(3)
		require.False(t, ok, "expected no opened files")
	})
}

func TestModuleInstance_CloseModuleOnCanceledOrTimeout(t *testing.T) {
	s := newStore()
	t.Run("timeout", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s, Sys: internalsys.DefaultContext(nil)}
		const duration = time.Second
		ctx, cancel := context.WithTimeout(context.Background(), duration)
		defer cancel()
		type arbitrary struct{}
		done := cc.CloseModuleOnCanceledOrTimeout(context.WithValue(ctx, arbitrary{}, "arbitrary")) // Wrapping arbitrary context.
		time.Sleep(duration * 2)
		defer done()

		// Resource shouldn't be released at this point.
		require.Equal(t, exitCodeFlag(exitCodeFlagResourceNotClosed), cc.Closed.Load()&exitCodeFlagMask)
		require.NotNil(t, cc.Sys)

		err := cc.FailIfClosed()
		require.EqualError(t, err, "module closed with context deadline exceeded")

		// The resource must be closed in FailIfClosed.
		require.Nil(t, cc.Sys)
	})

	t.Run("cancel", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s, Sys: internalsys.DefaultContext(nil)}
		ctx, cancel := context.WithCancel(context.Background())
		type arbitrary struct{}
		done := cc.CloseModuleOnCanceledOrTimeout(context.WithValue(ctx, arbitrary{}, "arbitrary")) // Wrapping arbitrary context.
		cancel()
		// Make sure nothing panics or otherwise gets weird with redundant call to cancel().
		cancel()
		cancel()
		defer done()
		time.Sleep(time.Second)

		// Resource shouldn't be released at this point.
		require.Equal(t, exitCodeFlag(exitCodeFlagResourceNotClosed), cc.Closed.Load()&exitCodeFlagMask)
		require.NotNil(t, cc.Sys)

		err := cc.FailIfClosed()
		require.EqualError(t, err, "module closed with context canceled")

		// The resource must be closed in FailIfClosed.
		require.Nil(t, cc.Sys)
	})

	t.Run("timeout over cancel", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s, Sys: internalsys.DefaultContext(nil)}
		const duration = time.Second
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// Wrap the cancel context by timeout.
		ctx, cancel = context.WithTimeout(ctx, duration)
		defer cancel()
		type arbitrary struct{}
		done := cc.CloseModuleOnCanceledOrTimeout(context.WithValue(ctx, arbitrary{}, "arbitrary")) // Wrapping arbitrary context.
		time.Sleep(duration * 2)
		defer done()

		// Resource shouldn't be released at this point.
		require.Equal(t, exitCodeFlag(exitCodeFlagResourceNotClosed), cc.Closed.Load()&exitCodeFlagMask)
		require.NotNil(t, cc.Sys)

		err := cc.FailIfClosed()
		require.EqualError(t, err, "module closed with context deadline exceeded")

		// The resource must be closed in FailIfClosed.
		require.Nil(t, cc.Sys)
	})

	t.Run("cancel over timeout", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s, Sys: internalsys.DefaultContext(nil)}
		ctx, cancel := context.WithCancel(context.Background())
		// Wrap the timeout context by cancel context.
		var timeoutDone context.CancelFunc
		ctx, timeoutDone = context.WithTimeout(ctx, time.Second*1000)
		defer timeoutDone()

		type arbitrary struct{}
		done := cc.CloseModuleOnCanceledOrTimeout(context.WithValue(ctx, arbitrary{}, "arbitrary")) // Wrapping arbitrary context.
		cancel()
		defer done()

		time.Sleep(time.Second)

		// Resource shouldn't be released at this point.
		require.Equal(t, exitCodeFlag(exitCodeFlagResourceNotClosed), cc.Closed.Load()&exitCodeFlagMask)
		require.NotNil(t, cc.Sys)

		err := cc.FailIfClosed()
		require.EqualError(t, err, "module closed with context canceled")

		// The resource must be closed in FailIfClosed.
		require.Nil(t, cc.Sys)
	})

	t.Run("cancel works", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s}
		ctx, cancel := context.WithCancel(context.Background())
		done := cc.CloseModuleOnCanceledOrTimeout(ctx)
		// The call ends before the context: the module must stay open.
		done()
		cancel()
		time.Sleep(100 * time.Millisecond)
		require.Nil(t, cc.FailIfClosed())
	})

	t.Run("no close on all resources canceled", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		w := &CloseWatch{m: cc, ctx: ctx, finished: true}
		w.fire()

		err := cc.FailIfClosed()
		require.Nil(t, err)
	})

	t.Run("reused watch ignores a late callback", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s}
		var w CloseWatch
		first, cancelFirst := context.WithCancel(context.Background())
		w.Start(cc, first).Stop()
		cancelFirst()

		second, cancelSecond := context.WithCancel(context.Background())
		defer cancelSecond()
		active := w.Start(cc, second)
		// A callback of the first call that runs now sees the second context.
		w.fire()
		active.Stop()
		require.Nil(t, cc.FailIfClosed())
	})

	t.Run("nested call gets its own watch", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s, Sys: internalsys.DefaultContext(nil)}
		var w CloseWatch
		outer, cancelOuter := context.WithCancel(context.Background())
		defer cancelOuter()
		outerWatch := w.Start(cc, outer)
		inner, cancelInner := context.WithCancel(context.Background())
		innerWatch := w.Start(cc, inner)
		require.True(t, innerWatch != outerWatch)
		innerWatch.Stop()
		cancelInner()
		cancelOuter()
		time.Sleep(100 * time.Millisecond)
		outerWatch.Stop()
		require.EqualError(t, cc.FailIfClosed(), "module closed with context canceled")
	})

	t.Run("background context", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s}
		done := cc.CloseModuleOnCanceledOrTimeout(context.Background())
		done()
		require.Nil(t, cc.FailIfClosed())
	})
}

func TestModuleInstance_CloseWithCtxErr(t *testing.T) {
	s := newStore()

	t.Run("context canceled", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		cc.CloseWithCtxErr(ctx)

		err := cc.FailIfClosed()
		require.EqualError(t, err, "module closed with context canceled")
	})

	t.Run("context timeout", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s}
		duration := time.Second
		ctx, cancel := context.WithTimeout(context.Background(), duration)
		defer cancel()

		time.Sleep(duration * 2)

		cc.CloseWithCtxErr(ctx)

		err := cc.FailIfClosed()
		require.EqualError(t, err, "module closed with context deadline exceeded")
	})

	t.Run("no error", func(t *testing.T) {
		cc := &ModuleInstance{ModuleName: "test", s: s}

		cc.CloseWithCtxErr(context.Background())

		err := cc.FailIfClosed()
		require.Nil(t, err)
	})
}

type mockCloser struct{ called int }

func (m *mockCloser) Close(context.Context) error {
	m.called++
	return nil
}

type freeRecordingMemory struct{ freed bool }

func (f *freeRecordingMemory) Reallocate(uint64) []byte { return nil }

func (f *freeRecordingMemory) Free() { f.freed = true }

func TestModuleInstance_ensureResourcesClosed(t *testing.T) {
	closer := &mockCloser{}

	for _, tc := range []struct {
		name string
		m    *ModuleInstance
	}{
		{m: &ModuleInstance{CodeCloser: closer}},
		{m: &ModuleInstance{Sys: internalsys.DefaultContext(nil)}},
		{m: &ModuleInstance{Sys: internalsys.DefaultContext(nil), CodeCloser: closer}},
	} {
		err := tc.m.ensureResourcesClosed(context.Background())
		require.NoError(t, err)
		require.Nil(t, tc.m.Sys)
		require.Nil(t, tc.m.CodeCloser)

		// Ensure multiple invocation is safe.
		err = tc.m.ensureResourcesClosed(context.Background())
		require.NoError(t, err)
	}
	require.Equal(t, 2, closer.called)

	t.Run("frees owned memory", func(t *testing.T) {
		owner := &mockModuleEngine{}
		buf := &freeRecordingMemory{}
		mem := &MemoryInstance{expBuffer: buf, ownerModuleEngine: owner}
		m := &ModuleInstance{MemoryInstance: mem, Engine: owner}

		require.NoError(t, m.ensureResourcesClosed(context.Background()))
		require.True(t, buf.freed)
		require.Nil(t, mem.expBuffer)
	})

	t.Run("does not free imported memory", func(t *testing.T) {
		owner := &mockModuleEngine{}
		importer := &mockModuleEngine{}
		buf := &freeRecordingMemory{}
		mem := &MemoryInstance{expBuffer: buf, ownerModuleEngine: owner}
		m := &ModuleInstance{MemoryInstance: mem, Engine: importer}

		require.NoError(t, m.ensureResourcesClosed(context.Background()))
		require.False(t, buf.freed)
		require.NotNil(t, mem.expBuffer)
	})
}
