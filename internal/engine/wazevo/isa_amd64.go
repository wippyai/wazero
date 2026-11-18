//go:build amd64

package wazevo

import (
	"encoding/binary"

	"github.com/tetratelabs/wazero/internal/engine/wazevo/backend"
	"github.com/tetratelabs/wazero/internal/engine/wazevo/backend/isa/amd64"
)

func newMachine() backend.Machine {
	return amd64.NewBackend()
}

// unwindStack is a function to unwind the stack, and appends return addresses to `returnAddresses` slice.
// The implementation must be aligned with the ABI/Calling convention.
func unwindStack(sp, fp, top uintptr, returnAddresses []uintptr) []uintptr {
	return amd64.UnwindStack(sp, fp, top, returnAddresses)
}

// goCallStackView is a function to get a view of the stack before a Go call, which
// is the view of the stack allocated in CompileGoFunctionTrampoline.
func goCallStackView(stackPointerBeforeGoCall *uint64) []uint64 {
	return amd64.GoCallStackView(stackPointerBeforeGoCall)
}

// adjustClonedStack is a function to adjust the stack after it is grown.
// More precisely, absolute addresses (frame pointers) in the stack must be adjusted.
func adjustClonedStack(oldsp, oldTop, sp, fp, top uintptr) {
	amd64.AdjustClonedStack(oldsp, oldTop, sp, fp, top)
}

// goCallFrameSize returns the size of the Go function trampoline frame at sp:
// the region from sp up to and including the caller's RBP and the return address.
func goCallFrameSize(_ []byte, sp, fp uintptr) uintptr {
	return fp + 16 - sp
}

// adjustRestoredGoCallFrame fixes the absolute caller RBP saved in a trampoline
// frame that is restored onto a stack whose top moved by delta.
func adjustRestoredGoCallFrame(frame []byte, fpOffset, delta uintptr) {
	callerRBP := binary.LittleEndian.Uint64(frame[fpOffset:])
	binary.LittleEndian.PutUint64(frame[fpOffset:], callerRBP+uint64(delta))
}
