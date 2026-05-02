package settings

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"pfeifer.dev/mapd/params"
)

// GitHub release configuration — CHANGE THESE
const (
	releaseOwner = "NikMoq"
	releaseRepo  = "mapd"
	releaseAsset = "mapd-russia-tiles.tar.gz"
)

// ReleaseInfo represents GitHub release API response (minimal fields)
type ReleaseInfo struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int    `json:"size"`
	} `json:"assets"`
}

// GetLatestRelease fetches the latest release info from GitHub API
func GetLatestRelease() (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest",
		releaseOwner, releaseRepo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "mapd-russia/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "fetch release")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("GitHub API returned %s", resp.Status)
	}

	var info ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, errors.Wrap(err, "decode release")
	}
	return &info, nil
}

// CheckReleaseUpdate returns true if a newer release is available
func CheckReleaseUpdate() (bool, string, error) {
	latest, err := GetLatestRelease()
	if err != nil {
		return false, "", err
	}

	localVer := getParamString("MapdRussiaVersion", "")
	if localVer == latest.TagName {
		return false, latest.TagName, nil
	}

	return true, latest.TagName, nil
}

// DownloadAndExtractRelease downloads the tile archive and extracts it
func DownloadAndExtractRelease(version string) error {
	url := fmt.Sprintf(
		"https://github.com/%s/%s/releases/download/%s/%s",
		releaseOwner, releaseRepo, version, releaseAsset,
	)

	slog.Info("Downloading mapd-russia tiles", "version", version, "url", url)

	// Download to temp file
	tmpFile := filepath.Join(params.GetBaseOpPath(), "tmp", releaseAsset)
	if err := os.MkdirAll(filepath.Dir(tmpFile), 0o775); err != nil {
		return errors.Wrap(err, "mkdir tmp")
	}

	if err := downloadFile(url, tmpFile); err != nil {
		return errors.Wrap(err, "download archive")
	}
	defer os.Remove(tmpFile)

	// Extract
	slog.Info("Extracting tiles...")
	if err := extractTarGz(tmpFile, params.GetBaseOpPath()); err != nil {
		return errors.Wrap(err, "extract archive")
	}

	// Save version
	if err := params.PutParam(params.ParamPath("MapdRussiaVersion"), []byte(version)); err != nil {
		return errors.Wrap(err, "save version param")
	}
	slog.Info("Tiles updated", "version", version)
	return nil
}

// getParamString reads a string param, returning defaultVal if missing.
func getParamString(name string, defaultVal string) string {
	data, err := params.GetParam(params.ParamPath(name))
	if err != nil || len(data) == 0 {
		return defaultVal
	}
	return string(data)
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return errors.Wrap(err, "http get")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return errors.Wrap(err, "create file")
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return errors.Wrap(err, "write file")
	}

	if err := out.Sync(); err != nil {
		return errors.Wrap(err, "fsync")
	}

	slog.Info("Download complete", "bytes", written)
	return nil
}

func extractTarGz(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return errors.Wrap(err, "open archive")
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return errors.Wrap(err, "gzip reader")
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "tar next")
		}
		if header == nil {
			continue
		}

		// Security: prevent directory traversal
		target := filepath.Join(dst, header.Name)
		if !strings.HasPrefix(target, filepath.Clean(dst)+string(os.PathSeparator)) {
			return errors.Errorf("tar path traversal: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return errors.Wrap(err, "mkdir")
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return errors.Wrap(err, "mkdir parent")
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return errors.Wrap(err, "create file")
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return errors.Wrap(err, "write file")
			}
			if err := out.Sync(); err != nil {
				out.Close()
				return errors.Wrap(err, "fsync")
			}
			out.Close()
		}
	}
	return nil
}
