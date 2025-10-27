package version

// Version is set at build time via -ldflags
var Version = "dev"

// Get returns the current version
func Get() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
