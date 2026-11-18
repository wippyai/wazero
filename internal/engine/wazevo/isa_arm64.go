//go:build arm64

package wazevo

import (
	"encoding/binary"

	"github.com/tetratelabs/wazero/internal/engine/wazevo/backend"
	"github.com/tetratelabs/wazero/internal/engine/wazevo/backend/isa/arm64"
)

func newMachine() backend.Machine {
	return arm64.NewBackend()
}

// unwindStack is a function to unwind the stack, and appends return addresses to `returnAddresses` slice.
// The implementation must be aligned with the ABI/Calling convention.
func unwindStack(sp, fp, top uintptr, returnAddresses []uintptr) []uintptr {
	return arm64.UnwindStack(sp, fp, top, returnAddresses)
}

// goCallStackView is a function to get a view of the stack before a Go call, which
// is the view of the stack allocated in CompileGoFunctionTrampoline.
func goCallStackView(stackPointerBeforeGoCall *uint64) []uint64 {
	return arm64.GoCallStackView(stackPointerBeforeGoCall)
}

// adjustClonedStack is a function to adjust the stack after it is grown.
// More precisely, absolute addresses (frame pointers) in the stack must be adjusted.
func adjustClonedStack(oldsp, oldTop, sp, fp, top uintptr) {
	// TODO: currently, the frame pointers are not used, and saved old sps are relative to the current stack pointer,
	//  so no need to adjustment on arm64. However, when we make it absolute, which in my opinion is better perf-wise
	//  at the expense of slightly costly stack growth, we need to adjust the pushed frame pointers.
}

// goCallFrameSize returns the size of the Go function trampoline frame at sp:
// (frame_size, sliceSize), the Go call slice, (return address, size_of_arg_ret)
// and the arg/ret slot.
func goCallFrameSize(frame []byte, _, _ uintptr) uintptr {
	frameSize := uintptr(binary.LittleEndian.Uint64(frame))
	argRetSize := uintptr(binary.LittleEndian.Uint64(frame[16+frameSize+8:]))
	return 16 + frameSize + 16 + argRetSize
}

// adjustRestoredGoCallFrame is a no-op: the arm64 frames hold no absolute stack addresses.
func adjustRestoredGoCallFrame([]byte, uintptr, uintptr) {}
