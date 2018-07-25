package lifecycle

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"

	"github.com/buildpack/packs"
	"github.com/buildpack/packs/img"
)

type Exporter struct {
	Buildpacks []*Buildpack
	In         []byte
	Out, Err   io.Writer
}

func (e *Exporter) Export(launchDir string, stackImage, origImage v1.Image) (v1.Image, error) {
	tmpDir, err := ioutil.TempDir("", "pack.export.layer")
	if err != nil {
		return nil, packs.FailErr(err, "create temp directory")
	}
	defer os.RemoveAll(tmpDir)

	stackDigest, err := stackImage.Digest()
	if err != nil {
		return nil, packs.FailErr(err, "stack digest")
	}
	metadata := packs.BuildMetadata{
		// App:        packs.AppMetadata{},
		Buildpacks: []packs.BuildpackMetadata{},
		Stack: packs.StackMetadata{
			SHA: stackDigest.String(),
		},
	}

	repoImage, err := e.addDirAsLayer(stackImage, filepath.Join(tmpDir, "app.tgz"), filepath.Join(launchDir, "app"), "launch/app")
	if err != nil {
		return nil, packs.FailErr(err, "append droplet to stack")
	}

	for _, buildpack := range e.Buildpacks {
		bpMetadata := packs.BuildpackMetadata{Key: buildpack.ID}
		repoImage, bpMetadata.Layers, err = e.addBuildpackLayer(buildpack.ID, tmpDir, launchDir, repoImage, origImage)
		if err != nil {
			return nil, packs.FailErr(err, "append layers")
		}
		metadata.Buildpacks = append(metadata.Buildpacks, bpMetadata)
	}

	// TODO: This appears to be the correct answer. Is it?
	webCommand, err := e.webCommand(filepath.Join(launchDir, "app", "metadata.toml"))
	if err != nil {
		return nil, packs.FailErr(err, "read web command from metadata")
	}
	// TODO should below be startCommand(repoImage, "/packs/launcher", webCommand)
	repoImage, err = e.startCommand(repoImage, webCommand)
	if err != nil {
		return nil, packs.FailErr(err, "set start command")
	}

	buildJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, packs.FailErr(err, "get encode metadata")
	}
	repoImage, err = img.Label(repoImage, packs.BuildLabel, string(buildJSON))
	if err != nil {
		return nil, packs.FailErr(err, "set metdata label")
	}

	return repoImage, nil
}

// TODO move this back to lib (somehow)
func (e *Exporter) startCommand(image v1.Image, cmd ...string) (v1.Image, error) {
	configFile, err := image.ConfigFile()
	if err != nil {
		return nil, err
	}
	config := *configFile.Config.DeepCopy()
	config.Cmd = cmd
	return mutate.Config(image, config)
}

func (e *Exporter) webCommand(tomlPath string) (string, error) {
	launch := LaunchTOML{}
	if _, err := toml.DecodeFile(tomlPath, &launch); err != nil {
		return "", err
	}
	for _, process := range launch.Processes {
		if process.Type == "web" {
			return process.Command, nil
		}
	}
	return "", errors.New("Missing process with web type")
}

func (e *Exporter) addBuildpackLayer(id, tmpDir, launchDir string, repoImage v1.Image, origImage v1.Image) (v1.Image, map[string]packs.LayerMetadata, error) {
	metadata := make(map[string]packs.LayerMetadata)
	layers, err := ioutil.ReadDir(filepath.Join(launchDir, id))
	if err != nil {
		return nil, nil, err
	}
	for _, layer := range layers {
		if !layer.IsDir() {
			continue
		}
		dir := filepath.Join(launchDir, id, layer.Name())
		tarFile := filepath.Join(tmpDir, fmt.Sprintf("layer.%s.%s.tgz", id, layer.Name()))
		if err := e.createTarFile(tarFile, dir, filepath.Join("launch", id, layer.Name())); err != nil {
			return nil, nil, packs.FailErr(err, "tar", dir, "to", tarFile)
		}
		var topLayer v1.Layer
		repoImage, topLayer, err = img.Append(repoImage, tarFile)
		if err != nil {
			return nil, nil, packs.FailErr(err, "append droplet to image")
		}

		diffid, err := topLayer.DiffID()
		if err != nil {
			return nil, nil, packs.FailErr(err, "calculate layer diffid")
		}
		var tomlData interface{}
		if _, err := toml.DecodeFile(dir+".toml", &tomlData); err != nil {
			if !os.IsNotExist(err) {
				return nil, nil, packs.FailErr(err, "calculate layer diffid")
			}
		}
		metadata[layer.Name()] = packs.LayerMetadata{SHA: diffid.String(), Data: tomlData}
	}
	for _, layer := range layers {
		if layer.IsDir() || !strings.HasSuffix(layer.Name(), ".toml") {
			continue
		}
		file := filepath.Join(launchDir, id, layer.Name())
		if _, err := os.Stat(strings.TrimSuffix(file, ".toml")); err == nil {
			// directory exists, it was handled above
			continue
		} else if !os.IsNotExist(err) {
			return nil, nil, packs.FailErr(err, "checking for existence of matching dir for", file)
		}

		if txt, err := ioutil.ReadFile(file); err != nil {
			return nil, nil, packs.FailErr(err, "reading toml file", file)
		} else {
			repoImage, err = img.Label(repoImage, fmt.Sprintf("%s.%s", id, layer.Name()), string(txt))
			if err != nil {
				return nil, nil, packs.FailErr(err, "set layer toml on image for", id, layer.Name())
			}
		}

		if origImage == nil {
			return nil, nil, errors.New("toml file layer expected, but no previous image")
		}

		config, err := origImage.ConfigFile()
		if err != nil {
			return nil, nil, packs.FailErr(err, "find config from origImage")
		}
		digestKey := fmt.Sprintf("%s.%s.diffid", id, strings.TrimSuffix(layer.Name(), ".toml"))
		digest := config.Config.Labels[digestKey]
		if digest == "" {
			return nil, nil, fmt.Errorf("could not find '%s' in %+v", digestKey, config.Config.Labels)
		}
		// TODO ; don't hardcode algorithm
		hash := v1.Hash{
			Algorithm: "sha256",
			Hex:       strings.TrimPrefix(digest, "sha256:"),
		}
		origLayer, err := origImage.LayerByDiffID(hash)
		if err != nil {
			return nil, nil, packs.FailErr(err, "find previous layer", id, layer.Name())
		}
		repoImage, err = mutate.AppendLayers(repoImage, origLayer)
		if err != nil {
			return nil, nil, packs.FailErr(err, "append layer from previous image", id, layer.Name())
		}
		repoImage, err = img.Label(repoImage, digestKey, digest)
		if err != nil {
			return nil, nil, packs.FailErr(err, "set layer digest on image for", id, layer.Name())
		}
	}
	return repoImage, metadata, nil
}

func (e *Exporter) addDirAsLayer(image v1.Image, tarFile, fsDir, tarDir string) (v1.Image, error) {
	if err := e.createTarFile(tarFile, fsDir, tarDir); err != nil {
		return nil, packs.FailErr(err, "tar", fsDir, "to", tarFile)
	}
	newImage, _, err := img.Append(image, tarFile)
	if err != nil {
		return nil, packs.FailErr(err, "append droplet to stack")
	}
	return newImage, nil
}

func (e *Exporter) createTarFile(tarFile, fsDir, tarDir string) error {
	fh, err := os.Create(tarFile)
	if err != nil {
		return fmt.Errorf("create file for tar: %s", err)
	}
	defer fh.Close()
	gzw := gzip.NewWriter(fh)
	defer gzw.Close()
	tw := tar.NewWriter(gzw)
	defer tw.Close()

	return filepath.Walk(fsDir, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.Mode().IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(fsDir, file)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return err
		}
		header.Name = filepath.Join(tarDir, relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		return f.Close()
	})

	return nil
}
