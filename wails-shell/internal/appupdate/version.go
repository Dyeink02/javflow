// Package appupdate owns the portable desktop update contract.
//
// Ownership summary:
// 1) normalize JavFlow's numeric release labels for comparison
// 2) keep the 0.4.4/0.4.40 shorthand rule explicit and testable
// 3) keep version policy independent from GitHub transport and Wails UI code
//
// File map for maintainers:
// 1) version parsing and project-specific normalization
// 2) numeric comparison helpers
// 3) display-safe canonical version formatting
package appupdate

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultCurrentVersion is the safe source-tree fallback used by tests and by
// builds that do not provide a linker-injected version.
const DefaultCurrentVersion = "0.4.4"

// BuildVersion is replaced by the Wails build script from package.json (or by
// the release tag in CI). Keeping the runtime version in the native binary
// prevents the updater from silently retaining an older hard-coded value when
// a later release such as 0.4.41 is built.
var BuildVersion = DefaultCurrentVersion

func ProductVersion() string {
	value := strings.TrimSpace(BuildVersion)
	if value == "" {
		return DefaultCurrentVersion
	}
	return value
}

type Version struct {
	major int
	minor int
	patch int
}

// ParseVersion accepts tags such as "v0.4.40" and the project's shorthand
// "0.4.4". A one-digit patch is the abbreviated form of the corresponding
// two-digit patch ending in zero, so 0.4.4 and 0.4.40 compare equally.
func ParseVersion(raw string) (Version, error) {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(strings.TrimPrefix(value, "v"), "V")
	if value == "" {
		return Version{}, fmt.Errorf("版本号为空")
	}

	parts := strings.Split(value, ".")
	if len(parts) > 3 {
		return Version{}, fmt.Errorf("版本号格式无效：%s", raw)
	}
	for len(parts) < 3 {
		parts = append(parts, "0")
	}

	numbers := [3]int{}
	for index, part := range parts {
		if part == "" {
			return Version{}, fmt.Errorf("版本号格式无效：%s", raw)
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return Version{}, fmt.Errorf("版本号格式无效：%s", raw)
		}
		// JavFlow historically displayed 0.4.4 while the release ordering
		// needs the unambiguous 0.4.40 form. Preserve that convention here.
		if index == 2 && len(part) == 1 && number > 0 {
			number *= 10
		}
		numbers[index] = number
	}

	return Version{major: numbers[0], minor: numbers[1], patch: numbers[2]}, nil
}

func CompareVersions(left string, right string) (int, error) {
	leftVersion, err := ParseVersion(left)
	if err != nil {
		return 0, err
	}
	rightVersion, err := ParseVersion(right)
	if err != nil {
		return 0, err
	}

	return leftVersion.Compare(rightVersion), nil
}

func (version Version) Compare(other Version) int {
	if version.major != other.major {
		if version.major < other.major {
			return -1
		}
		return 1
	}
	if version.minor != other.minor {
		if version.minor < other.minor {
			return -1
		}
		return 1
	}
	if version.patch < other.patch {
		return -1
	}
	if version.patch > other.patch {
		return 1
	}
	return 0
}

func (version Version) String() string {
	return fmt.Sprintf("%d.%d.%d", version.major, version.minor, version.patch)
}
