package update

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"testing"
)

type responseBody []byte

func (body responseBody) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func TestCheckSelectsNewerStableReleaseForArchitecture(t *testing.T) {
	body := fmt.Sprintf(`{"tag_name":"v0.2.10","assets":[
			{"name":"YourPet-Windows-x64.zip","size":12,"state":"uploaded","digest":"sha256:%064x"},
			{"name":"YourPet-Windows-ARM64.zip","size":34,"state":"uploaded","digest":"sha256:%064x"}]}`, 1, 2)
	client := &http.Client{Transport: responseBody(body)}

	for _, test := range []struct {
		installed, arch string
		machine         uint16
		size            int64
	}{
		{"0.2.9", "arm64", pe.IMAGE_FILE_MACHINE_ARM64, 34},
		{"0.2.9", "amd64", pe.IMAGE_FILE_MACHINE_AMD64, 12},
		{"0.2.10", "amd64", 0, 0},
		{"0.3.0", "amd64", 0, 0},
	} {
		release, err := check(client, latestRelease, test.installed, test.arch)
		if err != nil {
			t.Fatal(err)
		}
		if test.size == 0 {
			if release != nil {
				t.Fatalf("%s must not install %s", test.installed, release.Tag)
			}
		} else if release == nil || release.Tag != "v0.2.10" || release.machine != test.machine || release.size != test.size {
			t.Fatalf("wrong update for %s: %+v", test.arch, release)
		}
	}
}

func TestDownloadVerifiesZipAndArchitecture(t *testing.T) {
	// Minimal PE header sufficient for debug/pe, with no application code.
	executable := make([]byte, 96)
	copy(executable, "MZ")
	binary.LittleEndian.PutUint32(executable[0x3c:], 64)
	copy(executable[64:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(executable[68:], pe.IMAGE_FILE_MACHINE_AMD64)
	binary.LittleEndian.PutUint16(executable[86:], pe.IMAGE_FILE_EXECUTABLE_IMAGE)
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	entry, err := zipWriter.Create("YourPet.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(executable); err != nil {
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	data := archive.Bytes()
	client := &http.Client{Transport: responseBody(data)}

	release := Release{size: int64(len(data)), digest: sha256.Sum256(data), machine: pe.IMAGE_FILE_MACHINE_AMD64}
	got, err := release.download(client, downloadBase)
	if err != nil || !bytes.Equal(got, executable) {
		t.Fatalf("extract update: %v", err)
	}
	wrongArch := release
	wrongArch.machine = pe.IMAGE_FILE_MACHINE_ARM64
	if _, err := wrongArch.download(client, downloadBase); err == nil {
		t.Fatal("accepted update for another architecture")
	}
	badHash := release
	badHash.digest[0] ^= 0xff
	if _, err := badHash.download(client, downloadBase); err == nil {
		t.Fatal("accepted an archive with a different digest")
	}
}
