// Package version provides version information for the Stellar backend.
package version

import "fmt"

// These variables are set at build time using -ldflags
var (
	// Name is the application name
	Name = "Stellar"

	// Version is the semantic version (set via -ldflags at build time)
	Version = "0.2.0"

	// BuildTime is the build timestamp (set via -ldflags at build time)
	BuildTime = ""

	// GitCommit is the git commit hash (set via -ldflags at build time)
	GitCommit = ""
)

// Info contains version information
type Info struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	BuildTime string `json:"buildTime,omitempty"`
	GitCommit string `json:"gitCommit,omitempty"`
}

// GetInfo returns the current version information
func GetInfo() Info {
	return Info{
		Name:      Name,
		Version:   Version,
		BuildTime: BuildTime,
		GitCommit: GitCommit,
	}
}

// String returns a formatted version string
func (i Info) String() string {
	s := fmt.Sprintf("%s v%s", i.Name, i.Version)
	if i.GitCommit != "" {
		s += fmt.Sprintf(" (%s)", i.GitCommit[:min(7, len(i.GitCommit))])
	}
	if i.BuildTime != "" {
		s += fmt.Sprintf(" built %s", i.BuildTime)
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
