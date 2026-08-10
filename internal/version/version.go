package version

import "fmt"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func String() string {
	return fmt.Sprintf("vitrina %s (%s) built %s", Version, Commit, BuildTime)
}
