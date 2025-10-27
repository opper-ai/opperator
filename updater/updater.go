package updater

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"opperator/version"
)

const (
	githubAPIURL    = "https://api.github.com/repos/opper-ai/opperator/releases/latest"
	githubReleasesURL = "https://api.github.com/repos/opper-ai/opperator/releases"
	githubRepo      = "opper-ai/opperator"
)

// Release represents a GitHub release
type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset represents a release asset
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// UpdateInfo contains information about an available update
type UpdateInfo struct {
	Available      bool
	CurrentVersion string
	LatestVersion  string
	DownloadURL    string
	ChecksumURL    string
}

// CheckForUpdates checks if a new version is available
func CheckForUpdates() (*UpdateInfo, error) {
	currentVersion := version.Get()

	// Fetch latest release from GitHub
	release, err := fetchLatestRelease()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest release: %w", err)
	}

	// Compare versions
	updateAvailable, err := isNewerVersion(currentVersion, release.TagName)
	if err != nil {
		return nil, fmt.Errorf("failed to compare versions: %w", err)
	}

	info := &UpdateInfo{
		Available:      updateAvailable,
		CurrentVersion: currentVersion,
		LatestVersion:  release.TagName,
	}

	if updateAvailable {
		// Find the appropriate asset for this platform
		assetName := getAssetNameForPlatform(release.TagName)
		checksumName := "SHA256SUMS"

		for _, asset := range release.Assets {
			if asset.Name == assetName {
				info.DownloadURL = asset.BrowserDownloadURL
			}
			if asset.Name == checksumName {
				info.ChecksumURL = asset.BrowserDownloadURL
			}
		}

		if info.DownloadURL == "" {
			return nil, fmt.Errorf("no matching asset found for platform %s/%s", runtime.GOOS, runtime.GOARCH)
		}
	}

	return info, nil
}

// fetchLatestRelease fetches the latest release from GitHub API
func fetchLatestRelease() (*Release, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Try to fetch the latest release (excludes pre-releases)
	resp, err := client.Get(githubAPIURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// If 404, try fetching all releases (including pre-releases)
	if resp.StatusCode == http.StatusNotFound {
		return fetchLatestFromAllReleases(client)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API returned status %d: %s", resp.StatusCode, string(body))
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

// fetchLatestFromAllReleases fetches all releases and returns the most recent one
func fetchLatestFromAllReleases(client *http.Client) (*Release, error) {
	resp, err := client.Get(githubReleasesURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases found on GitHub")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API returned status %d: %s", resp.StatusCode, string(body))
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}

	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases found on GitHub")
	}

	// Return the first release (most recent)
	return &releases[0], nil
}

// isNewerVersion compares two version strings and returns true if latest is newer than current
func isNewerVersion(current, latest string) (bool, error) {
	// Remove 'v' prefix if present
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")

	// Handle dev version
	if current == "dev" {
		return true, nil
	}

	// Simple string comparison for now (we'll improve this with proper semver parsing)
	// This handles cases like: 0.1.0 < 0.2.0, 1.0.0 < 2.0.0, etc.
	return current != latest && latest > current, nil
}

// getAssetNameForPlatform returns the expected asset name for the current platform
func getAssetNameForPlatform(version string) string {
	// Format: opperator-{version}-{os}-{arch}.tar.gz
	// Example: opperator-v0.1.0-darwin-arm64.tar.gz
	os := runtime.GOOS
	arch := runtime.GOARCH

	ext := ".tar.gz"
	if os == "windows" {
		ext = ".zip"
	}

	return fmt.Sprintf("opperator-%s-%s-%s%s", version, os, arch, ext)
}

// DownloadAndInstall downloads and installs the update
func DownloadAndInstall(info *UpdateInfo) error {
	// Create temp directory for download
	tmpDir, err := os.MkdirTemp("", "opperator-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Download the archive
	archivePath := filepath.Join(tmpDir, filepath.Base(info.DownloadURL))
	if err := downloadFile(archivePath, info.DownloadURL); err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}

	// Download checksums if available
	var checksums map[string]string
	if info.ChecksumURL != "" {
		checksumPath := filepath.Join(tmpDir, "SHA256SUMS")
		if err := downloadFile(checksumPath, info.ChecksumURL); err != nil {
			return fmt.Errorf("failed to download checksums: %w", err)
		}
		checksums, err = parseChecksums(checksumPath)
		if err != nil {
			return fmt.Errorf("failed to parse checksums: %w", err)
		}
	}

	// Verify checksum
	if checksums != nil {
		if err := verifyChecksum(archivePath, checksums); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Extract binary
	binaryPath, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		return fmt.Errorf("failed to extract binary: %w", err)
	}

	// Replace current binary
	if err := replaceBinary(binaryPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	return nil
}

// downloadFile downloads a file from url to filepath
func downloadFile(filepath string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// parseChecksums parses SHA256SUMS file
func parseChecksums(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	checksums := make(map[string]string)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			checksums[parts[1]] = parts[0]
		}
	}
	return checksums, nil
}

// verifyChecksum verifies the file checksum
func verifyChecksum(filepath string, checksums map[string]string) error {
	filename := filepath[strings.LastIndex(filepath, "/")+1:]
	expectedChecksum, ok := checksums[filename]
	if !ok {
		return fmt.Errorf("no checksum found for %s", filename)
	}

	file, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}

	actualChecksum := hex.EncodeToString(hash.Sum(nil))
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	return nil
}

// extractBinary extracts the binary from the archive
func extractBinary(archivePath, destDir string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		// Find the binary file (should be the only file in the archive)
		if header.Typeflag == tar.TypeReg {
			destPath := filepath.Join(destDir, filepath.Base(header.Name))
			outFile, err := os.Create(destPath)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", err
			}
			outFile.Close()

			// Make executable
			if err := os.Chmod(destPath, 0755); err != nil {
				return "", err
			}

			return destPath, nil
		}
	}

	return "", fmt.Errorf("no binary found in archive")
}

// replaceBinary replaces the current binary with the new one
func replaceBinary(newBinaryPath string) error {
	// Get current executable path
	currentPath, err := os.Executable()
	if err != nil {
		return err
	}

	// Resolve symlinks
	currentPath, err = filepath.EvalSymlinks(currentPath)
	if err != nil {
		return err
	}

	// Backup current binary
	backupPath := currentPath + ".backup"
	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	// Copy new binary to current location
	if err := copyFile(newBinaryPath, currentPath); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, currentPath)
		return fmt.Errorf("failed to copy new binary: %w", err)
	}

	// Make executable
	if err := os.Chmod(currentPath, 0755); err != nil {
		return err
	}

	// Remove backup
	os.Remove(backupPath)

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
