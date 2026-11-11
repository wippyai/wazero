package version

import (
	"runtime/debug"
	"strings"
)

// Default is the default version value used when none was found.
const Default = "dev"

// version holds the current version from the go.mod of downstream users or set by ldflag for wazero CLI.
var version string

// GetWazeroVersion returns the current version of wazero either in the go.mod or set by ldflag for wazero CLI.
//
// If this is not CLI, this assumes that downstream users of wazero imports wazero as "github.com/tetratelabs/wazero".
// The returned string matches the require statement, or the replacement version
// when the module is replaced with a versioned dependency. Compilation caches
// must distinguish the implementation selected by the replacement.
// For example, if the go.mod has "require github.com/tetratelabs/wazero 0.1.2-12314124-abcd",
// then this returns "0.1.2-12314124-abcd".
//
// Note: this is tested in ./testdata/main_test.go with a separate go.mod to pretend as the wazero user.
func GetWazeroVersion() string {
	return version
}

func init() {
	if len(version) != 0 {
		return
	}

	info, ok := debug.ReadBuildInfo()
	if ok {
		for _, dep := range info.Deps {
			// Note: here's the assumption that wazero is imported as github.com/tetratelabs/wazero.
			if strings.Contains(dep.Path, "github.com/tetratelabs/wazero") {
				version = moduleVersion(dep)
			}
		}

		// In wazero CLI, wazero is a main module, so we have to get the version info from info.Main.
		if versionMissing(version) {
			version = info.Main.Version
		}
	}
	if versionMissing(version) {
		version = Default // don't return parens
	}
}

func moduleVersion(dep *debug.Module) string {
	if dep.Replace != nil && !versionMissing(dep.Replace.Version) {
		return dep.Replace.Version
	}
	return dep.Version
}

func versionMissing(ret string) bool {
	return ret == "" || ret == "(devel)" // pkg.go defaults to (devel)
}
