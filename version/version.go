package version

var (
	// Version is the main version at the moment.
	Version = "1.0.0"
)

// Versioning should follow the SemVer guidelines
// https://semver.org/

// GetVersion returns a string representation of the version
func GetVersion() string {
	version := "\n[TIE MAIN VERSION]\n"
	version += Version

	version += "\n"

	return version
}

// returns jsonrpc representation of the version
func GetVersionJsonrpc() string {
	return "v" + Version
}