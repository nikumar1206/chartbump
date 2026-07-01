package registry

import "time"

type VersionInfo struct {
	Version string
	Created time.Time // zero if unknown
}

func (v VersionInfo) String() string {
	if v.Created.IsZero() {
		return v.Version
	}
	return v.Version + " (" + v.Created.Format("2006-01-02") + ")"
}
