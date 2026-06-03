package common

import (
	"strconv"
	"strings"
)

// SemVer represents a parsed semantic version.
type SemVer struct {
	Major int
	Minor int
	Patch int
}

// ParseSemVer parses a semantic version string.
// It strips a leading "v"/"V", splits on ".", defaults missing components to 0,
// and ignores pre-release/build metadata (e.g. "-beta.1+build").
func ParseSemVer(s string) (SemVer, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")

	// Strip pre-release / build metadata
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		s = s[:idx]
	}

	parts := strings.SplitN(s, ".", 4)
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return SemVer{}, ErrInvalidVersion
	}

	var sv SemVer
	var err error
	sv.Major, err = parseVersionComponent(parts[0])
	if err != nil {
		return SemVer{}, err
	}
	if len(parts) > 1 {
		sv.Minor, err = parseVersionComponent(parts[1])
		if err != nil {
			return SemVer{}, err
		}
	}
	if len(parts) > 2 {
		sv.Patch, err = parseVersionComponent(parts[2])
		if err != nil {
			return SemVer{}, err
		}
	}
	return sv, nil
}

func parseVersionComponent(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

// ErrInvalidVersion is returned when a version string cannot be parsed.
var ErrInvalidVersion = errInvalidVersion()

type invalidVersionError struct{}

func errInvalidVersion() invalidVersionError { return invalidVersionError{} }

func (invalidVersionError) Error() string { return "invalid version string" }

// CompareSemVer compares two semantic versions.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func CompareSemVer(a, b SemVer) int {
	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1
		}
		return 1
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// SemVerGreaterOrEqual returns true if a >= b.
func SemVerGreaterOrEqual(a, b SemVer) bool {
	return CompareSemVer(a, b) >= 0
}

// ParseAndCompareSemVer parses both version strings and returns whether
// version >= minimum.
func ParseAndCompareSemVer(version, minimum string) (bool, error) {
	v, err := ParseSemVer(version)
	if err != nil {
		return false, err
	}
	m, err := ParseSemVer(minimum)
	if err != nil {
		return false, err
	}
	return SemVerGreaterOrEqual(v, m), nil
}
