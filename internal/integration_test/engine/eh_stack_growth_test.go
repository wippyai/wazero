package adhoc

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/internal/platform"
	"github.com/tetratelabs/wazero/internal/testing/require"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// TestEHStackGrowth checks that try_table handlers stay valid when the stack
// grows (and moves) between the try_table entry and the throw.
func TestEHStackGrowth(t *testing.T) {
	cfgs := map[string]wazero.RuntimeConfig{
		"interpreter": wazero.NewRuntimeConfigInterpreter(),
	}
	if platform.CompilerSupported() {
		cfgs["compiler"] = wazero.NewRuntimeConfigCompiler()
	}
	for name, cfg := range cfgs {
		cfg := cfg.WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling)
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			r := wazero.NewRuntimeWithConfig(ctx, cfg)
			defer r.Close(ctx)

			mod, err := r.InstantiateWithConfig(ctx, buildEHStackGrowthModule(),
				wazero.NewModuleConfig().WithStartFunctions())
			require.NoError(t, err)

			chain := mod.ExportedFunction("chain")
			direct := mod.ExportedFunction("direct")
			for _, n := range []uint64{1, 10, 1500, 100, 1500} {
				res, err := chain.Call(ctx, n)
				require.NoError(t, err)
				require.Equal(t, n*(n+1)/2, res[0], "chain(%d)", n)

				res, err = direct.Call(ctx, n)
				require.NoError(t, err)
				require.Equal(t, n+7, res[0], "direct(%d)", n)
			}
		})
	}
}

// buildEHStackGrowthModule builds:
//
//	(tag $e (param i32))
//	;; Each level catches the exception from the level below, adds n and rethrows.
//	(func $rec (param $n i32) (result i32)
//	  block (result i32)
//	    try_table (result i32) (catch $e 0)
//	      local.get $n
//	      i32.eqz
//	      if
//	        i32.const 0
//	        throw $e
//	      end
//	      local.get $n
//	      i32.const 1
//	      i32.sub
//	      call $rec
//	    end
//	    return
//	  end
//	  local.get $n
//	  i32.add
//	  throw $e)
//	;; Throws 7 from the deepest level, with no handler in between.
//	(func $rec2 (param $n i32) (result i32)
//	  local.get $n
//	  i32.eqz
//	  if
//	    i32.const 7
//	    throw $e
//	  end
//	  local.get $n
//	  i32.const 1
//	  i32.sub
//	  call $rec2)
//	(func (export "chain") (param $n i32) (result i32)
//	  block (result i32)
//	    try_table (result i32) (catch $e 0)
//	      local.get $n
//	      call $rec
//	    end
//	  end)
//	(func (export "direct") (param $n i32) (result i32)
//	  block (result i32)
//	    try_table (result i32) (catch $e 0)
//	      local.get $n
//	      call $rec2
//	    end
//	  end
//	  local.get $n
//	  i32.add)
func buildEHStackGrowthModule() []byte {
	const i32 = wasm.ValueTypeI32
	catchTag0Label0 := []byte{1, wasm.CatchKindCatch, 0, 0}
	tryTable := append([]byte{wasm.OpcodeBlock, byte(i32), wasm.OpcodeTryTable, byte(i32)}, catchTag0Label0...)

	rec := append([]byte{}, tryTable...)
	rec = append(rec,
		wasm.OpcodeLocalGet, 0, wasm.OpcodeI32Eqz,
		wasm.OpcodeIf, 0x40, wasm.OpcodeI32Const, 0, wasm.OpcodeThrow, 0, wasm.OpcodeEnd,
		wasm.OpcodeLocalGet, 0, wasm.OpcodeI32Const, 1, wasm.OpcodeI32Sub, wasm.OpcodeCall, 0,
		wasm.OpcodeEnd, wasm.OpcodeReturn, wasm.OpcodeEnd,
		wasm.OpcodeLocalGet, 0, wasm.OpcodeI32Add, wasm.OpcodeThrow, 0, wasm.OpcodeEnd)

	rec2 := []byte{
		wasm.OpcodeLocalGet, 0, wasm.OpcodeI32Eqz,
		wasm.OpcodeIf, 0x40, wasm.OpcodeI32Const, 7, wasm.OpcodeThrow, 0, wasm.OpcodeEnd,
		wasm.OpcodeLocalGet, 0, wasm.OpcodeI32Const, 1, wasm.OpcodeI32Sub, wasm.OpcodeCall, 1,
		wasm.OpcodeEnd,
	}

	chain := append([]byte{}, tryTable...)
	chain = append(chain, wasm.OpcodeLocalGet, 0, wasm.OpcodeCall, 0,
		wasm.OpcodeEnd, wasm.OpcodeEnd, wasm.OpcodeEnd)

	direct := append([]byte{}, tryTable...)
	direct = append(direct, wasm.OpcodeLocalGet, 0, wasm.OpcodeCall, 1,
		wasm.OpcodeEnd, wasm.OpcodeEnd,
		wasm.OpcodeLocalGet, 0, wasm.OpcodeI32Add, wasm.OpcodeEnd)

	m := &wasm.Module{
		TypeSection: []wasm.FunctionType{
			{Params: []wasm.ValueType{i32}, Results: []wasm.ValueType{i32}},
			{Params: []wasm.ValueType{i32}},
		},
		TagSection:      []wasm.Tag{{Type: 1}},
		FunctionSection: []wasm.Index{0, 0, 0, 0},
		ExportSection: []wasm.Export{
			{Type: wasm.ExternTypeFunc, Name: "chain", Index: 2},
			{Type: wasm.ExternTypeFunc, Name: "direct", Index: 3},
		},
		CodeSection: []wasm.Code{{Body: rec}, {Body: rec2}, {Body: chain}, {Body: direct}},
	}
	return encodeModule(m)
}

func BenchmarkEHTryTableDeep(b *testing.B) {
	if !platform.CompilerSupported() {
		b.Skip()
	}
	ctx := context.Background()
	cfg := wazero.NewRuntimeConfigCompiler().
		WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling)
	r := wazero.NewRuntimeWithConfig(ctx, cfg)
	defer r.Close(ctx)
	mod, err := r.InstantiateWithConfig(ctx, buildEHStackGrowthModule(),
		wazero.NewModuleConfig().WithStartFunctions())
	require.NoError(b, err)
	chain := mod.ExportedFunction("chain")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := chain.Call(ctx, 1000); err != nil {
			b.Fatal(err)
		}
	}
}
