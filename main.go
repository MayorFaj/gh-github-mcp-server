package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	version = "dev"
	// Update this URL to point to the latest release of github-mcp-server
	releaseAPIURL = "https://api.github.com/repos/github/github-mcp-server/releases/latest"
)

func main() {
	// Show version if requested
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Printf("gh-github-mcp-server v%s\n", version)
		return
	}

	// Check if command is "stdio"
	if len(os.Args) > 1 && os.Args[1] == "stdio" {
		// Get GitHub token from CLI
		token, err := getGitHubToken()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting GitHub token: %s\n", err)
			os.Exit(1)
		}

		// Set the token as an environment variable
		os.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", token)

		// Find or download the server binary
		serverPath, err := ensureServerBinary()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error with server binary: %s\n", err)
			os.Exit(1)
		}

		fmt.Fprintf(os.Stderr, "Using server binary: %s\n", serverPath)

		// Execute the actual MCP server with stdio mode
		cmd := exec.Command(serverPath, "stdio")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Forward all arguments after "stdio"
		if len(os.Args) > 2 {
			cmd.Args = append(cmd.Args, os.Args[2:]...)
		}

		// Run the server
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running MCP server: %s\n", err)
			os.Exit(1)
		}
	} else {
		// Show usage
		fmt.Println("GitHub MCP Server CLI Extension")
		fmt.Println()
		fmt.Println("USAGE:")
		fmt.Println("  gh github-mcp-server stdio [flags] - Start the MCP server in stdio mode")
		fmt.Println()
		fmt.Println("FLAGS:")
		fmt.Println("  --read-only            Restrict the server to read-only operations")
		fmt.Println("  --log-file string      Path to log file")
		fmt.Println("  --gh-host string       Specify the GitHub hostname (for GitHub Enterprise)")
		fmt.Println()
		fmt.Println("This extension uses your GitHub CLI authentication")
		fmt.Println("to securely communicate with GitHub APIs.")
	}
}

// getGitHubToken gets the GitHub token from the GitHub CLI
func getGitHubToken() (string, error) {
	// Execute the GitHub CLI to get the token directly
	cmd := exec.Command("gh", "auth", "token")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("failed to get GitHub token: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to execute gh auth token: %w", err)
	}

	// Trim any whitespace/newlines
	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("received empty token from GitHub CLI")
	}

	return token, nil
}

// ensureServerBinary finds or downloads the server binary
func ensureServerBinary() (string, error) {
	// Try to find the binary first
	binPath := findServerBinary()
	if binPath != "" {
		return binPath, nil
	}

	fmt.Fprintf(os.Stderr, "GitHub MCP Server binary not found, downloading...\n")

	// Binary not found, download it
	binPath, err := downloadServerBinary()
	if err != nil {
		return "", fmt.Errorf("failed to download server binary: %w", err)
	}

	return binPath, nil
}

// findServerBinary locates the github-mcp-server binary
func findServerBinary() string {
	// Check if a specific path is provided via environment variable
	customPath := os.Getenv("GITHUB_MCP_SERVER_PATH")
	if customPath != "" && fileExists(customPath) {
		return customPath
	}

	// Check if workspace directory is specified
	workspaceDir := os.Getenv("GITHUB_MCP_SERVER_DIR")
	if workspaceDir != "" {
		binPath := filepath.Join(workspaceDir, "bin", binaryName())
		if fileExists(binPath) {
			return binPath
		}
	}

	// Check in the extension's data directory
	dataDir := getExtensionDataDir()
	binPath := filepath.Join(dataDir, "bin", binaryName())
	if fileExists(binPath) {
		return binPath
	}

	// Check standard locations
	execPath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(execPath)

		// Check for the binary in the 'bin' subdirectory
		binPath := filepath.Join(dir, "bin", binaryName())
		if fileExists(binPath) {
			return binPath
		}

		// Check next to the extension binary
		binPath = filepath.Join(dir, binaryName())
		if fileExists(binPath) {
			return binPath
		}
	}

	// Finally check if it's available in PATH
	path, lookErr := exec.LookPath("github-mcp-server")
	if lookErr == nil {
		return path
	}

	return ""
}

// downloadServerBinary downloads the latest release of the server binary
func downloadServerBinary() (string, error) {
	// Get information about the latest release
	assetURL, err := getLatestReleaseAssetURL()
	if err != nil {
		return "", err
	}

	// Create the data directory if it doesn't exist
	dataDir := getExtensionDataDir()
	binDir := filepath.Join(dataDir, "bin")
	err = os.MkdirAll(binDir, 0755)
	if err != nil {
		return "", fmt.Errorf("failed to create binary directory: %w", err)
	}

	// Download the asset
	fmt.Fprintf(os.Stderr, "Downloading from: %s\n", assetURL)
	resp, err := http.Get(assetURL)
	if err != nil {
		return "", fmt.Errorf("failed to download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download binary: HTTP %d", resp.StatusCode)
	}

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "github-mcp-server-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Save the downloaded file
	_, err = io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		return "", fmt.Errorf("failed to save downloaded file: %w", err)
	}

	// Extract the binary
	targetPath := filepath.Join(binDir, binaryName())
	if strings.HasSuffix(assetURL, ".zip") {
		err = extractFromZip(tmpFile.Name(), targetPath)
	} else if strings.HasSuffix(assetURL, ".tar.gz") {
		err = extractFromTarGz(tmpFile.Name(), targetPath)
	} else {
		// Direct binary download
		err = os.Rename(tmpFile.Name(), targetPath)
	}

	if err != nil {
		return "", fmt.Errorf("failed to extract binary: %w", err)
	}

	// Make the binary executable
	err = os.Chmod(targetPath, 0755)
	if err != nil {
		return "", fmt.Errorf("failed to make binary executable: %w", err)
	}

	return targetPath, nil
}

// getLatestReleaseAssetURL gets the URL for the appropriate asset from the latest release
func getLatestReleaseAssetURL() (string, error) {
	// Add a custom user agent
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", releaseAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "gh-github-mcp-server/"+version)

	// Use token if available for higher rate limits
	token, _ := getGitHubToken()
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	// Make the request
	fmt.Fprintf(os.Stderr, "Requesting latest release info from: %s\n", releaseAPIURL)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get latest release info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get latest release info: HTTP %d - %s",
			resp.StatusCode, string(body))
	}

	// Parse the response
	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to parse release info: %w", err)
	}

	// Print all assets for debugging
	fmt.Fprintf(os.Stderr, "Found release with %d assets:\n", len(release.Assets))
	for _, asset := range release.Assets {
		fmt.Fprintf(os.Stderr, "  - %s\n", asset.Name)
	}

	// Find the appropriate asset for the current platform
	assetPattern := fmt.Sprintf("github-mcp-server.*%s.*%s", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(os.Stderr, "Looking for pattern: %s\n", assetPattern)

	// Try standard naming convention
	for _, asset := range release.Assets {
		lowerName := strings.ToLower(asset.Name)
		if strings.Contains(lowerName, strings.ToLower(runtime.GOOS)) &&
			strings.Contains(lowerName, strings.ToLower(runtime.GOARCH)) {
			fmt.Fprintf(os.Stderr, "Found matching asset: %s\n", asset.Name)
			return asset.BrowserDownloadURL, nil
		}
	}

	// Try macOS alternatives (mac, macos, osx)
	if runtime.GOOS == "darwin" {
		fmt.Fprintf(os.Stderr, "Trying macOS alternatives: mac, macos, osx\n")
		macVariants := []string{"mac", "macos", "osx"}
		for _, variant := range macVariants {
			for _, asset := range release.Assets {
				lowerName := strings.ToLower(asset.Name)
				if strings.Contains(lowerName, variant) &&
					(strings.Contains(lowerName, runtime.GOARCH) ||
						strings.Contains(lowerName, "x86_64") ||
						strings.Contains(lowerName, "amd64")) {
					fmt.Fprintf(os.Stderr, "Found matching asset with macOS alternative: %s\n", asset.Name)
					return asset.BrowserDownloadURL, nil
				}
			}
		}
	}

	// Try architecture alternatives
	if runtime.GOARCH == "amd64" {
		fmt.Fprintf(os.Stderr, "Trying architecture alternatives: x86_64\n")
		for _, asset := range release.Assets {
			lowerName := strings.ToLower(asset.Name)
			if strings.Contains(lowerName, strings.ToLower(runtime.GOOS)) &&
				strings.Contains(lowerName, "x86_64") {
				fmt.Fprintf(os.Stderr, "Found matching asset with architecture alternative: %s\n", asset.Name)
				return asset.BrowserDownloadURL, nil
			}
		}
	}

	// As a last resort, look for a potentially universal binary for the OS
	fmt.Fprintf(os.Stderr, "Looking for any binary for %s\n", runtime.GOOS)
	for _, asset := range release.Assets {
		lowerName := strings.ToLower(asset.Name)
		if strings.Contains(lowerName, strings.ToLower(runtime.GOOS)) &&
			!strings.Contains(lowerName, ".sha") &&
			!strings.Contains(lowerName, ".md5") &&
			!strings.Contains(lowerName, "src") &&
			!strings.Contains(lowerName, "source") {
			fmt.Fprintf(os.Stderr, "Found OS-matching asset: %s\n", asset.Name)
			return asset.BrowserDownloadURL, nil
		}
	}

	// If this is macOS/amd64, maybe suggest building from source
	if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" {
		return "", fmt.Errorf("no suitable binary found for %s/%s. Consider building from source: "+
			"go build -o bin/github-mcp-server ./cmd/github-mcp-server", runtime.GOOS, runtime.GOARCH)
	}

	return "", fmt.Errorf("no suitable binary found for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// extractFromZip extracts a binary from a zip file
func extractFromZip(zipPath, targetPath string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	// Find the binary file in the zip
	var binaryFile *zip.File
	for _, file := range reader.File {
		if strings.Contains(file.Name, "github-mcp-server") &&
			!strings.HasSuffix(file.Name, "/") {
			binaryFile = file
			break
		}
	}

	if binaryFile == nil {
		return fmt.Errorf("binary not found in zip")
	}

	// Extract the file
	src, err := binaryFile.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// extractFromTarGz extracts a binary from a tar.gz file
func extractFromTarGz(tarPath, targetPath string) error {
	file, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	// Find and extract the binary
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if strings.Contains(header.Name, "github-mcp-server") &&
			header.Typeflag == tar.TypeReg {
			dst, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			defer dst.Close()

			_, err = io.Copy(dst, tr)
			return err
		}
	}

	return fmt.Errorf("binary not found in tar.gz")
}

// getExtensionDataDir returns the data directory for this extension
func getExtensionDataDir() string {
	var dataDir string
	if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
		dataDir = filepath.Join(xdgDataHome, "gh-github-mcp-server")
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			// Fallback to current directory
			return "."
		}

		if runtime.GOOS == "windows" {
			dataDir = filepath.Join(homeDir, "AppData", "Local", "gh-github-mcp-server")
		} else if runtime.GOOS == "darwin" {
			dataDir = filepath.Join(homeDir, "Library", "Application Support", "gh-github-mcp-server")
		} else {
			dataDir = filepath.Join(homeDir, ".local", "share", "gh-github-mcp-server")
		}
	}

	// Create the directory if it doesn't exist
	_ = os.MkdirAll(dataDir, 0755)
	return dataDir
}

// binaryName returns the name of the binary with platform-specific extension
func binaryName() string {
	baseName := "github-mcp-server"
	if runtime.GOOS == "windows" {
		return baseName + ".exe"
	}
	return baseName
}

// fileExists checks if a file exists and is not a directory
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
