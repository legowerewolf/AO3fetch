package buildinfo

import (
	"errors"
	"runtime/debug"
	"strings"
)

func GetBuildSettings() (*map[string]string, error) {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return nil, errors.New("build information not available")
	}

	settings := make(map[string]string)
	for _, setting := range buildInfo.Settings {
		settings[setting.Key] = setting.Value
	}

	settings["GOVERSION"] = buildInfo.GoVersion

	settings["vcs.revision.withModified"] = settings["vcs.revision"]
	if settings["vcs.modified"] == "true" {
		settings["vcs.revision.withModified"] += "+"
	}

	settings["GOARCH.withVersion"] = settings["GOARCH"] + "/" + settings["GO"+strings.ToUpper(settings["GOARCH"])]

	return &settings, nil
}
