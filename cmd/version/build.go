package version

import (
	"runtime"

	logging "github.com/sirupsen/logrus"
)

var (
	// make linter happy.

	// Revision contains revision.
	Revision = "unknown"

	// Version contains version string.
	Version = "unknown"

	// BareVersion contains version string without 'v' prefix.
	BareVersion = "unknown"

	// BuildDate contains build date.
	BuildDate = "unknown"
)

type printer interface {
	Println(...interface{})
}

// LogVersion logs build version info.
func LogVersion(p printer) {
	if p == nil {
		p = logging.WithField("module", "version")
	}
	p.Println("crossmesh", BareVersion, Revision, BuildDate, runtime.GOOS, runtime.GOARCH)
}
