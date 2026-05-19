package nextcloud

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

// Client handles Nextcloud API interactions via WebDAV and OCS.
type Client struct {
	baseURL    string
	username   string
	password   string
	uploadDir  string
	httpClient *http.Client
	log        *slog.Logger
}

// NewClient creates a new Nextcloud client. baseURL trailing slash is trimmed.
func NewClient(baseURL, username, password, uploadDir string, log *slog.Logger) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		username:  username,
		password:  password,
		uploadDir: uploadDir,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		log: log,
	}
}

// davPath returns the full WebDAV URL for the given remote path.
func (c *Client) davPath(remotePath string) string {
	return c.baseURL + "/remote.php/dav/files/" + c.username + remotePath
}

// mkCol creates a collection (directory) at the given remote path via MKCOL.
// Accepts 201 (created) and 405 (already exists).
func (c *Client) mkCol(remotePath string) error {
	req, err := http.NewRequest("MKCOL", c.davPath(remotePath), nil)
	if err != nil {
		return fmt.Errorf("creating MKCOL request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("MKCOL %s: %w", remotePath, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusMethodNotAllowed {
		return fmt.Errorf("MKCOL %s returned %d", remotePath, resp.StatusCode)
	}
	return nil
}

// ensureDir creates all directories in the path by calling mkCol for each segment.
func (c *Client) ensureDir(remotePath string) error {
	parts := strings.Split(strings.Trim(remotePath, "/"), "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		current += "/" + part
		if err := c.mkCol(current); err != nil {
			return fmt.Errorf("ensureDir %s: %w", current, err)
		}
	}
	return nil
}

// Upload uploads a local file to the given remote path via WebDAV PUT.
// It ensures the parent directory exists before uploading.
func (c *Client) Upload(localPath, remotePath string) error {
	parentDir := path.Dir(remotePath)
	if parentDir != "/" && parentDir != "." {
		if err := c.ensureDir(parentDir); err != nil {
			return fmt.Errorf("ensuring parent dir: %w", err)
		}
	}

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening local file %s: %w", localPath, err)
	}
	defer f.Close()

	req, err := http.NewRequest(http.MethodPut, c.davPath(remotePath), f)
	if err != nil {
		return fmt.Errorf("creating PUT request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", remotePath, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("PUT %s returned %d", remotePath, resp.StatusCode)
	}

	c.log.Info("uploaded file to nextcloud", "local", localPath, "remote", remotePath)
	return nil
}

// ocsShareResponse is the XML response from the OCS sharing API.
type ocsShareResponse struct {
	XMLName xml.Name `xml:"ocs"`
	Meta    struct {
		StatusCode int    `xml:"statuscode"`
		Message    string `xml:"message"`
	} `xml:"meta"`
	Data struct {
		URL   string `xml:"url"`
		Token string `xml:"token"`
	} `xml:"data"`
}

// ShareFile creates a public share link for the given remote path and returns the download URL.
func (c *Client) ShareFile(remotePath string) (string, error) {
	ocsURL := c.baseURL + "/ocs/v2.php/apps/files_sharing/api/v1/shares"

	formData := url.Values{}
	formData.Set("path", remotePath)
	formData.Set("shareType", "3")
	formData.Set("permissions", "1")

	req, err := http.NewRequest(http.MethodPost, ocsURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("creating OCS share request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("OCS-APIRequest", "true")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("OCS share request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading OCS response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("OCS share returned %d: %s", resp.StatusCode, string(body))
	}

	var ocsResp ocsShareResponse
	if err := xml.Unmarshal(body, &ocsResp); err != nil {
		return "", fmt.Errorf("parsing OCS response: %w", err)
	}

	if ocsResp.Meta.StatusCode != 200 {
		return "", fmt.Errorf("OCS share failed with status %d: %s", ocsResp.Meta.StatusCode, ocsResp.Meta.Message)
	}

	return ocsResp.Data.URL + "/download", nil
}

func (c *Client) RemotePath(localPath, livekitRoom string) string {
	sanitizedRoom := strings.ReplaceAll(livekitRoom, "/", "_")
	filename := path.Base(localPath)
	return c.uploadDir + "/" + sanitizedRoom + "/" + filename
}

func (c *Client) UploadAndShare(localPath, livekitRoom string) (string, error) {
	remotePath := c.RemotePath(localPath, livekitRoom)

	if err := c.Upload(localPath, remotePath); err != nil {
		return "", fmt.Errorf("uploading file: %w", err)
	}

	shareURL, err := c.ShareFile(remotePath)
	if err != nil {
		return "", fmt.Errorf("creating share link: %w", err)
	}

	c.log.Info("uploaded and shared file", "local", localPath, "remote", remotePath, "url", shareURL)
	return shareURL, nil
}

// DeleteFile deletes a file at the given remote path via HTTP DELETE.
// Accepts 200 and 204 as success.
func (c *Client) DeleteFile(remotePath string) error {
	req, err := http.NewRequest(http.MethodDelete, c.davPath(remotePath), nil)
	if err != nil {
		return fmt.Errorf("creating DELETE request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", remotePath, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("DELETE %s returned %d", remotePath, resp.StatusCode)
	}
	return nil
}

// FileInfo holds metadata about a file or directory on Nextcloud.
type FileInfo struct {
	Path         string
	LastModified time.Time
	IsDir        bool
}

// propfindResponse is the XML multistatus response from a PROPFIND request.
type propfindResponse struct {
	XMLName   xml.Name   `xml:"multistatus"`
	Responses []davEntry `xml:"response"`
}

type davEntry struct {
	Href     string        `xml:"href"`
	Propstat []davPropStat `xml:"propstat"`
}

type davPropStat struct {
	Prop   davProp `xml:"prop"`
	Status string  `xml:"status"`
}

type davProp struct {
	LastModified string `xml:"getlastmodified"`
	ResourceType struct {
		Collection *struct{} `xml:"collection"`
	} `xml:"resourcetype"`
}

// ListFolder lists files in the given remote directory via PROPFIND with Depth 2.
// Returns nil (not an error) for 404.
func (c *Client) ListFolder(remotePath string) ([]FileInfo, error) {
	propfindBody := `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop>
    <d:getlastmodified/>
    <d:resourcetype/>
  </d:prop>
</d:propfind>`

	req, err := http.NewRequest("PROPFIND", c.davPath(remotePath), strings.NewReader(propfindBody))
	if err != nil {
		return nil, fmt.Errorf("creating PROPFIND request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Depth", "2")
	req.Header.Set("Content-Type", "application/xml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PROPFIND %s: %w", remotePath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading PROPFIND response: %w", err)
	}

	if resp.StatusCode != 207 { // 207 Multi-Status
		return nil, fmt.Errorf("PROPFIND %s returned %d", remotePath, resp.StatusCode)
	}

	var pfResp propfindResponse
	if err := xml.Unmarshal(body, &pfResp); err != nil {
		return nil, fmt.Errorf("parsing PROPFIND response: %w", err)
	}

	davPrefix := "/remote.php/dav/files/" + c.username

	var files []FileInfo
	for _, entry := range pfResp.Responses {
		// Strip URL-encoding from href and remove DAV prefix to get relative path
		decodedHref, err := url.PathUnescape(entry.Href)
		if err != nil {
			decodedHref = entry.Href
		}
		relPath := strings.TrimPrefix(decodedHref, davPrefix)
		// Trim trailing slash for consistent comparison
		relPath = strings.TrimRight(relPath, "/")

		// Skip the folder itself
		if relPath == strings.TrimRight(remotePath, "/") {
			continue
		}

		var fi FileInfo
		fi.Path = relPath

		for _, propstat := range entry.Propstat {
			if !strings.Contains(propstat.Status, "200") {
				continue
			}
			p := propstat.Prop

			// Parse last modified date
			if p.LastModified != "" {
				t, err := time.Parse(time.RFC1123, p.LastModified)
				if err != nil {
					t, err = time.Parse(time.RFC1123Z, p.LastModified)
					if err != nil {
						c.log.Warn("failed to parse last modified date", "value", p.LastModified, "error", err)
					}
				}
				if err == nil {
					fi.LastModified = t
				}
			}

			fi.IsDir = p.ResourceType.Collection != nil
		}

		files = append(files, fi)
	}

	return files, nil
}

func (c *Client) Cleanup(retentionDays int) error {
	files, err := c.ListFolder(c.uploadDir)
	if err != nil {
		return fmt.Errorf("listing folder for cleanup: %w", err)
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	dirsWithFiles := make(map[string]bool)
	var dirs []string

	for _, fi := range files {
		if fi.IsDir {
			dirs = append(dirs, fi.Path)
			continue
		}
		if fi.LastModified.IsZero() {
			dirsWithFiles[path.Dir(fi.Path)] = true
			continue
		}
		if fi.LastModified.Before(cutoff) {
			c.log.Info("deleting old file", "path", fi.Path, "lastModified", fi.LastModified)
			if err := c.DeleteFile(fi.Path); err != nil {
				c.log.Error("failed to delete file during cleanup", "path", fi.Path, "error", err)
				dirsWithFiles[path.Dir(fi.Path)] = true
			}
		} else {
			dirsWithFiles[path.Dir(fi.Path)] = true
		}
	}

	for _, dir := range dirs {
		if !dirsWithFiles[dir] {
			c.log.Info("deleting empty directory", "path", dir)
			if err := c.DeleteFile(dir); err != nil {
				c.log.Error("failed to delete empty directory", "path", dir, "error", err)
			}
		}
	}

	return nil
}

func (c *Client) RunCleanupLoop(ctx context.Context, retentionDays int) {
	if retentionDays <= 0 {
		return
	}

	if err := c.Cleanup(retentionDays); err != nil {
		c.log.Error("cleanup failed", "error", err)
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Cleanup(retentionDays); err != nil {
				c.log.Error("cleanup failed", "error", err)
			}
		}
	}
}
