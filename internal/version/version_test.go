package version

import (
	"runtime/debug"
	"testing"
)

func TestModuleVersionUsesVersionedReplacement(t *testing.T) {
	for _, test := range []struct {
		name string
		dep  debug.Module
		want string
	}{
		{
			name: "published upstream",
			dep:  debug.Module{Version: "v1.12.0"},
			want: "v1.12.0",
		},
		{
			name: "versioned fork has a distinct cache identity",
			dep: debug.Module{Version: "v1.12.0", Replace: &debug.Module{
				Path: "github.com/wippyai/wazero", Version: "v1.12.1-wippy.2",
			}},
			want: "v1.12.1-wippy.2",
		},
		{
			name: "local replacement preserves require fallback",
			dep: debug.Module{Version: "v1.12.0", Replace: &debug.Module{
				Path: "../wazero",
			}},
			want: "v1.12.0",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := moduleVersion(&test.dep); got != test.want {
				t.Fatalf("cache version = %q, want %q", got, test.want)
			}
		})
	}
}
