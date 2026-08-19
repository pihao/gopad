package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"runtime/debug"
	"sync"
	"time"
)

// buildTime is stamped by the build:
//
//	go build -ldflags "-X gopad/internal/server.buildTime=<RFC3339>"
//
// When absent (plain `go build`), the commit time from VCS build info is
// used instead.
var buildTime string

type versionInfo struct {
	Tag       string `json:"tag"`       // release tag, "" when the commit isn't tagged
	Commit    string `json:"commit"`    // short commit hash, "" when built outside a repo
	Dirty     bool   `json:"dirty"`     // true when built from a modified work tree
	BuildTime int64  `json:"buildTime"` // unix seconds, 0 when unknown
}

// pseudoVersionRE matches Go module pseudo-versions such as
// v0.0.0-20250819034523-0123456789ab, which name a commit, not a tag.
var pseudoVersionRE = regexp.MustCompile(`-\d{14}-[0-9a-f]{12}(\+dirty)?$`)

// readVersion assembles the build's identity from debug.ReadBuildInfo.
// Go 1.24+ stamps the main module version from VCS tags.
var readVersion = sync.OnceValue(func() versionInfo {
	var v versionInfo
	if t, err := time.Parse(time.RFC3339, buildTime); err == nil {
		v.BuildTime = t.Unix()
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}
	if ver := bi.Main.Version; ver != "" && ver != "(devel)" && !pseudoVersionRE.MatchString(ver) {
		v.Tag = ver
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				v.Commit = s.Value[:7]
			}
		case "vcs.time":
			if v.BuildTime == 0 {
				if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
					v.BuildTime = t.Unix()
				}
			}
		case "vcs.modified":
			v.Dirty = s.Value == "true"
		}
	}
	return v
})

// handleVersion reports the build's tag/commit and build time.
func handleVersion(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(readVersion())
}
