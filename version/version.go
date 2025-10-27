package version

// Version is set at build time via -ldflags
var Version = "0.0.1-alpha"

// Get returns the current version
func Get() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
