package version

import (
	"runtime/debug"
)

func Get(version string) string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version == "(devel)" {
			if commit, ok := BuildVersion(info); ok {
				return commit
			}
		}
		return info.Main.Version
	}
	return "(none)"
}

func BuildVersion(info *debug.BuildInfo) (string, bool) {
	var (
		revision string
		dirty    string
	)
	for _, v := range info.Settings {
		switch v.Key {
		case "vcs.revision":
			revision = v.Value
		case "vcs.modified":
			dirty = " (dirty)"
		}
	}
	if revision == "" {
		return "", false
	}
	return revision + dirty, true
}
