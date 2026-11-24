package update

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	latestRelease = "https://api.github.com/repos/timkimtim/YourPet/releases/latest"
	downloadBase  = "https://github.com/timkimtim/YourPet/releases/download/"
	maxArchive    = 32 << 20
	maxExecutable = 64 << 20
)

type Release struct {
	Tag     string
	name    string
	size    int64
	digest  [sha256.Size]byte
	machine uint16
}

func githubClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			switch req.URL.Host {
			case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
				if req.URL.Scheme == "https" && len(via) < 5 {
					return nil
				}
			}
			return errors.New("unexpected redirect from GitHub")
		},
	}
}

func Check(current, arch string) (*Release, error) {
	return check(githubClient(15*time.Second), latestRelease, current, arch)
}

func check(client *http.Client, endpoint, current, arch string) (*Release, error) {
	data, err := fetch(client, endpoint, "application/vnd.github+json", 1<<20)
	if err != nil {
		return nil, err
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
		Assets     []struct {
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			State  string `json:"state"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return nil, err
	}
	if release.Draft || release.Prerelease {
		return nil, nil
	}
	installed, ok := version(current)
	if !ok || !strings.HasPrefix(release.Tag, "v") {
		return nil, errors.New("invalid release version")
	}
	published, ok := version(release.Tag[1:])
	if !ok {
		return nil, errors.New("invalid GitHub release tag")
	}
	if !newer(published, installed) {
		return nil, nil
	}
	result := &Release{Tag: release.Tag}
	switch arch {
	case "amd64":
		result.name, result.machine = "YourPet-Windows-x64.zip", pe.IMAGE_FILE_MACHINE_AMD64
	case "arm64":
		result.name, result.machine = "YourPet-Windows-ARM64.zip", pe.IMAGE_FILE_MACHINE_ARM64
	default:
		return nil, fmt.Errorf("unsupported Windows architecture %s", arch)
	}
	for _, asset := range release.Assets {
		if asset.Name != result.name || asset.State != "uploaded" {
			continue
		}
		hash, prefixed := strings.CutPrefix(asset.Digest, "sha256:")
		digest, err := hex.DecodeString(hash)
		if asset.Size <= 0 || asset.Size > maxArchive || !prefixed || err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("invalid size or SHA-256 for %s", asset.Name)
		}
		result.size = asset.Size
		copy(result.digest[:], digest)
		return result, nil
	}
	return nil, fmt.Errorf("%s is missing from release %s", result.name, result.Tag)
}

func version(s string) ([3]uint64, bool) {
	var v [3]uint64
	parts := strings.Split(s, ".")
	if len(parts) != len(v) {
		return v, false
	}
	for i, part := range parts {
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil || strconv.FormatUint(value, 10) != part {
			return v, false
		}
		v[i] = value
	}
	return v, true
}

func newer(a, b [3]uint64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func (r *Release) Download() ([]byte, error) {
	return r.download(githubClient(3*time.Minute), downloadBase+r.Tag+"/"+r.name)
}

func (r *Release) download(client *http.Client, address string) ([]byte, error) {
	data, err := fetch(client, address, "application/zip", r.size)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != r.size || sha256.Sum256(data) != r.digest {
		return nil, errors.New("download does not match the release size and SHA-256")
	}
	return extractExecutable(data, r.machine)
}

func extractExecutable(data []byte, machine uint16) ([]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var entry *zip.File
	for _, f := range z.File {
		if f.Name == "YourPet.exe" {
			if entry != nil {
				return nil, errors.New("duplicate YourPet.exe entries")
			}
			entry = f
		}
	}
	if entry == nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > maxExecutable {
		return nil, errors.New("invalid or missing YourPet.exe in the release ZIP")
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	executable, err := readBounded(reader, maxExecutable)
	if err != nil {
		return nil, err
	}
	file, err := pe.NewFile(bytes.NewReader(executable))
	if err != nil {
		return nil, fmt.Errorf("invalid Windows executable: %w", err)
	}
	defer file.Close()
	if file.Machine != machine || file.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 ||
		file.Characteristics&pe.IMAGE_FILE_DLL != 0 {
		return nil, errors.New("update is not an executable for this Windows architecture")
	}
	return executable, nil
}

func fetch(client *http.Client, address, accept string, limit int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "YourPet")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned HTTP %d", res.StatusCode)
	}
	return readBounded(res.Body, limit)
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("GitHub response exceeds the size limit")
	}
	return data, nil
}
