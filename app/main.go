package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"syscall"

	// Uncomment this block to pass the first stage!
	"os"
	"os/exec"
)

// Usage: your_docker.sh run <image> <command> <arg1> <arg2> ...
func main() {
	imageString := os.Args[2]
	command := os.Args[3]
	args := os.Args[4:len(os.Args)]

	// create tmp directory
	tmp_dir, err := os.MkdirTemp("", "sandbox_*")
	if err != nil {
		fmt.Printf("Error creating tmp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(tmp_dir)

	// chmod 0755 temp directory
	err = os.Chmod(tmp_dir, 0755)
	if err != nil {
		fmt.Printf("Error chmod: %v\n", err)
		os.Exit(1)
	}

	// mkDirAll filepath.join(tmp_dir, /usr/local/bin) 0755
	err = os.MkdirAll(filepath.Join(tmp_dir, "/usr/local/bin"), 0755)
	if err != nil {
		fmt.Printf("Error mkdirall: %v\n", err)
		os.Exit(1)
	}

	// os.Link(docker-explorer full path, filepathjoin(tempDir, "/usr/local/bin", "docker-explorer"))
	err = os.Link("/usr/local/bin/docker-explorer", filepath.Join(tmp_dir, "/usr/local/bin", "docker-explorer"))
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}

	// get image info
	image := getImage(imageString)
	//fmt.Printf("image:%v\n", image)

	// get token
	token, err := getToken(image)
	if err != nil {
		fmt.Printf("error obtaining token: %v\n", err)
		os.Exit(1)
	}
	//fmt.Printf("obtained token : %v\n", token)

	layers, err := fetchImageManifest(image, token)
	if err != nil {
		fmt.Printf("error fetching image manifest: %v\n", err)
		os.Exit(1)
	}
	//fmt.Printf("obtained image manifest: %v\n", layers)

	registryUrl := "https://registry.hub.docker.com"
	//download layeres
	for _, layer := range layers {
		if err := downloadLayer(token, registryUrl, image.Name, layer); err != nil {
			fmt.Printf("Error downloading layer: %v\n", err)
			os.Exit(1)
		}
		//untar layer in tmp_dir
		untar(layer, tmp_dir)
	}

	//chroot temp_dir
	err = syscall.Chroot(tmp_dir)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}

	//chdir into /
	err = os.Chdir("/")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}

	cmd := exec.Command(command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID,
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		if exiterr, ok := err.(*exec.ExitError); ok {
			os.Exit(exiterr.ExitCode())
		}
	}
}

type Image struct {
	Name    string
	Version string
}

func (i *Image) String() string {
	return fmt.Sprintf("%s:%s", i.Name, i.Version)
}

func getVersion(parts []string) string {
	if len(parts) == 1 {
		return "latest"
	}
	return parts[1]
}

func getImage(imageString string) *Image {
	parts := strings.Split(imageString, ":")
	name := parts[0]
	version := getVersion(parts)
	return &Image{
		Name:    name,
		Version: version,
	}
}

type Token struct {
	Tok       string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
	IssuedAt  string `json:"issued_at"`
}

func (t *Token) String() string {
	return fmt.Sprintf("tok: %s - expiresIn: %d - issuedAt: %s", t.Tok, t.ExpiresIn, t.IssuedAt)
}

func getToken(image *Image) (*Token, error) {
	url := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/%s:pull", image.Name)
	response, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get error: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read body error: %v", err)
	}
	var responseToken Token
	err = json.Unmarshal(body, &responseToken)
	if err != nil {
		return nil, fmt.Errorf("unmarshal error: %v", err)
	}
	return &responseToken, nil
}

type OCIImageIndex struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Manifests     []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
		Platform  struct {
			Architecture string   `json:"architecture"`
			OS           string   `json:"os"`
			OSVersion    string   `json:"os.version,omitempty"` // Optional fields can be omitted if not used
			OSFeatures   []string `json:"os.features,omitempty"`
			Variant      string   `json:"variant,omitempty"`
		} `json:"platform"`
		Annotations map[string]string `json:"annotations,omitempty"` // Optional field for annotations
	} `json:"manifests"`
}

type OCIManifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Config        struct {
		MediaType string `json:"mediaType"`
		Size      int64  `json:"size"`
		Digest    string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		MediaType string   `json:"mediaType"`
		Size      int64    `json:"size"`
		Digest    string   `json:"digest"`
		URLs      []string `json:"urls,omitempty"`
	} `json:"layers"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type DockerManifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Config        struct {
		MediaType string `json:"mediaType"`
		Size      int64  `json:"size"`
		Digest    string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		MediaType string `json:"mediaType"`
		Size      int64  `json:"size"`
		Digest    string `json:"digest"`
	} `json:"layers"`
}

type DockerManifestList struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Manifests     []struct {
		MediaType string `json:"mediaType"`
		Size      int64  `json:"size"`
		Digest    string `json:"digest"`
		Platform  struct {
			Architecture string   `json:"architecture"`
			OS           string   `json:"os"`
			OSVersion    string   `json:"os.version,omitempty"`
			OSFeatures   []string `json:"os.features,omitempty"`
			Variant      string   `json:"variant,omitempty"`
			Features     []string `json:"features,omitempty"`
		} `json:"platform"`
	} `json:"manifests"`
}

func (im *OCIManifest) String() string {
	// Start with schema version and media type
	result := fmt.Sprintf("Schema Version: %d\nMedia Type: %s\nConfig:\n  Media Type: %s\n  Size: %d\n  Digest: %s\n",
		im.SchemaVersion, im.MediaType,
		im.Config.MediaType, im.Config.Size, im.Config.Digest,
	)

	// Add layer information
	result += "Layers:\n"
	for i, layer := range im.Layers {
		result += fmt.Sprintf("  Layer %d:\n    Media Type: %s\n    Size: %d\n    Digest: %s\n",
			i+1, layer.MediaType, layer.Size, layer.Digest,
		)
		if len(layer.URLs) > 0 {
			result += "    Urls:\n"
			for _, url := range layer.URLs {
				result += fmt.Sprintf("      - %s\n", url)
			}
		}
	}
	return result
}

func fetchImageManifest(image *Image, token *Token) ([]string, error) {
	//get call to https://registry.hub.docker.com/v2/library/{image}/manifests/latest
	//add header "Authorization = Bearer <token>"
	//add header "Accept = application/vnd.docker.distribution.manifest.v2+json"
	url := fmt.Sprintf("https://registry.hub.docker.com/v2/library/%s/manifests/latest", image.Name)
	//fmt.Printf("calling url: %s\n", url)

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %v", err)
	}

	// Add the necessary headers
	req.Header.Add("Authorization", "Bearer "+token.Tok)
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	//fmt.Printf("http resquest: %v\n", req)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %v", err)
	}

	//fmt.Printf("body: %s\n", string(body))

	//unmarshall into generic map to check the mediaType
	var genericMap map[string]interface{}
	if err := json.Unmarshal(body, &genericMap); err != nil {
		return nil, fmt.Errorf("unmarshaling into generic map: %v", err)
	}

	mediaType, ok := genericMap["mediaType"].(string)
	if !ok {
		return nil, fmt.Errorf("mediaType not found")
	}

	var digests []string
	switch mediaType {
	case "application/vnd.docker.distribution.manifest.v2+json":
		var imageManifest DockerManifest
		if err := json.Unmarshal(body, &imageManifest); err != nil {
			return nil, fmt.Errorf("unmarshaling into Image manifest %v", err)
		}
		digests = getLayerDigestsFromManifest(&imageManifest)
		return digests, nil

	case "application/vnd.oci.image.index.v1+json":
		var ociImageIndex OCIImageIndex
		if err := json.Unmarshal(body, &ociImageIndex); err != nil {
			return nil, fmt.Errorf("unmarshaling into OCI Image manifest %v", err)
		}
		digests = getManifestDigestsFromOCIManifestList(&ociImageIndex, "amd64", "linux")
		manifest, err := fetchManifest(image.Name, digests[0], token.Tok)
		//fmt.Printf("manifest: %v\n", string(manifest))
		if err != nil {
			return nil, fmt.Errorf("error fetch OCI Manifest %v", err)
		}
		var ociManifest OCIManifest
		if err := json.Unmarshal(manifest, &ociManifest); err != nil {
			return nil, fmt.Errorf("unmarshaling into OCI manifest %v", err)
		}
		//fmt.Printf("ociManifest: %v", ociManifest)
		digests = getOCILayerDigests(&ociManifest)
		return digests, nil
	case "application/vnd.docker.distribution.manifest.list.v2+json":
		var manifestList DockerManifestList
		if err := json.Unmarshal(body, &manifestList); err != nil {
			return nil, fmt.Errorf("unmarshaling into DockerManifestList %v", err)
		}
		digests = getManifestDigestsFromManifestList(&manifestList, "amd64", "linux")
	}
	return digests, nil
}

func fetchManifest(image, digest, token string) ([]byte, error) {
	baseURL := "https://registry.hub.docker.com"
	url := fmt.Sprintf("%s/v2/library/%s/manifests/%s", baseURL, image, digest)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("Authorization", "Bearer "+token)
	//fmt.Printf("http request: %v\n", req)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func getLayerDigestsFromManifest(manifest *DockerManifest) []string {
	var digests []string
	for _, layer := range manifest.Layers {
		digests = append(digests, layer.Digest)
	}
	return digests
}

// for a specific platform, os
func getManifestDigestsFromManifestList(manifestList *DockerManifestList, architecture, os string) []string {
	var digests []string
	for _, manifest := range manifestList.Manifests {
		if manifest.Platform.Architecture == architecture && manifest.Platform.OS == os {
			digests = append(digests, manifest.Digest)
			break
		}
	}
	return digests
}

func getManifestDigestsFromOCIManifestList(manifestList *OCIImageIndex, architecture, os string) []string {
	var digests []string
	for _, manifest := range manifestList.Manifests {
		if manifest.Platform.Architecture == architecture && manifest.Platform.OS == os {
			digests = append(digests, manifest.Digest)
			break
		}
	}
	return digests
}

func getOCILayerDigests(manifest *OCIManifest) []string {
	var layerDigests []string
	for _, layer := range manifest.Layers {
		layerDigests = append(layerDigests, layer.Digest)
	}
	return layerDigests
}

func downloadLayer(token *Token, registryURL, imageName, layerDigest string) error {
	url := fmt.Sprintf("%s/v2/%s/blobs/%s", registryURL, "library/"+imageName, layerDigest)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %v", err)
	}
	req.Header.Add("Authorization", "Bearer "+token.Tok)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Assuming you want to save the layer to disk
	outFile, err := os.Create(fmt.Sprintf("%s.tar", layerDigest))
	if err != nil {
		return fmt.Errorf("creating file: %v", err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return fmt.Errorf("saving layer: %v", err)
	}
	return nil
}

func untar(layerDigest, destination string) error {
	tarPath := layerDigest + ".tar"
	cmd := exec.Command("tar", "-xf", tarPath, "-C", destination)

	// Executes the command and captures any errors.
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("tar command failed: %w", err)
	}

	return nil
}
