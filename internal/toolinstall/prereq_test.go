package toolinstall

import (
	"reflect"
	"strings"
	"testing"
)

func TestMissingPrereqs(t *testing.T) {
	t.Parallel()
	has := func(present ...string) func(string) bool {
		return func(p string) bool {
			for _, x := range present {
				if x == p {
					return true
				}
			}
			return false
		}
	}
	cases := []struct {
		name     string
		requires []string
		has      func(string) bool
		want     []string
	}{
		{"all present", []string{"curl", "unzip"}, has("curl", "unzip"), nil},
		{"kiro on a box without unzip", []string{"curl", "unzip"}, has("curl", "tar"), []string{"unzip"}},
		{"order kept", []string{"curl", "tar", "bzip2"}, has(), []string{"curl", "tar", "bzip2"}},
		{"alternative satisfied by the second", []string{"sha256sum|shasum"}, has("shasum"), nil},
		{"alternative with none present", []string{"sha256sum|shasum"}, has(), []string{"sha256sum|shasum"}},
		{"blank entries ignored", []string{" ", ""}, has(), nil},
		{"nil lookup never blocks", []string{"unzip"}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MissingPrereqs(tc.requires, tc.has); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("MissingPrereqs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPackageManagerFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		goos    string
		release string
		want    string
	}{
		{"ubuntu", "linux", "ID=ubuntu\nID_LIKE=debian\n", "apt"},
		{"debian", "linux", `ID="debian"`, "apt"},
		{"mint via ID_LIKE", "linux", "ID=linuxmint\nID_LIKE=\"ubuntu debian\"", "apt"},
		{"fedora", "linux", "ID=fedora", "dnf"},
		{"rhel-like", "linux", "ID=rocky\nID_LIKE=\"rhel centos fedora\"", "dnf"},
		{"amazon linux", "linux", "ID=\"amzn\"\nID_LIKE=\"centos rhel fedora\"", "dnf"},
		{"alpine", "linux", "ID=alpine", "apk"},
		{"arch", "linux", "ID=arch", "pacman"},
		{"opensuse", "linux", "ID=opensuse-leap\nID_LIKE=\"suse opensuse\"", "zypper"},
		{"unknown distro", "linux", "ID=nixos", ""},
		{"no os-release", "linux", "", ""},
		{"macOS is Homebrew", "darwin", "", "brew"},
		{"windows has no table row", "windows", "ID=ubuntu", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PackageManagerFor(tc.goos, ParseOSRelease(tc.release)).Name; got != tc.want {
				t.Errorf("PackageManagerFor(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestPrereqRemedy(t *testing.T) {
	t.Parallel()
	apt := PackageManagerFor("linux", ParseOSRelease("ID=ubuntu"))
	cases := []struct {
		name    string
		missing []string
		pm      PackageManager
		want    string
	}{
		{"nothing missing", nil, apt, ""},
		{
			"kiro on ubuntu",
			[]string{"unzip"},
			apt,
			"The installer needs unzip, which is not installed on this machine. Install it with: sudo apt-get install -y unzip (Debian/Ubuntu), then press Re-check.",
		},
		{
			"two programs",
			[]string{"tar", "bzip2"},
			apt,
			"The installer needs tar and bzip2, which are not installed on this machine. Install them with: sudo apt-get install -y tar bzip2 (Debian/Ubuntu), then press Re-check.",
		},
		{
			"alternatives map to the first program's package",
			[]string{"sha256sum|shasum"},
			PackageManagerFor("linux", ParseOSRelease("ID=alpine")),
			"The installer needs sha256sum or shasum, which is not installed on this machine. Install it with: sudo apk add coreutils (Alpine), then press Re-check.",
		},
		{
			"unknown family",
			[]string{"unzip", "curl"},
			PackageManager{},
			"The installer needs unzip and curl, which are not installed on this machine. Install unzip and curl with your system package manager, then press Re-check.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PrereqRemedy(tc.missing, tc.pm)
			if got != tc.want {
				t.Errorf("PrereqRemedy =\n  %q\nwant\n  %q", got, tc.want)
			}
			if strings.ContainsRune(got, '—') {
				t.Errorf("remedy carries an em-dash: %q", got)
			}
		})
	}
}
