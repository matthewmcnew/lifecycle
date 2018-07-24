package lifecycle

import (
	"archive/tar"
	"compress/gzip"
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
	In       []byte
	Out, Err io.Writer
}

func (e *Exporter) Export(launchDir, appDir string, stackImage v1.Image, repoStore img.Store) error {
	origImage, err := repoStore.Image()
	if err != nil {
		origImage = nil
	}

	tmpDir, err := ioutil.TempDir("", "pack.export.layer")
	if err != nil {
		return packs.FailErr(err, "create temp directory")
	}
	defer os.RemoveAll(tmpDir)

	tarFile := filepath.Join(tmpDir, "app.tgz")
	if err := e.createTarFile(tarFile, appDir, "launch/app"); err != nil {
		return packs.FailErr(err, "tar", appDir, "to", tarFile)
	}
	repoImage, _, err := img.Append(stackImage, tarFile)
	if err != nil {
		return packs.FailErr(err, "append droplet to stack")
	}

	repoImage, err = e.addBuildpackLayers(tmpDir, launchDir, repoImage, origImage)
	if err != nil {
		return packs.FailErr(err, "append layers")
	}

	// TODO: This appears to be the correct answer. Is it?
	webCommand, err := e.webCommand(filepath.Join(appDir, "metadata.toml"))
	if err != nil {
		return packs.FailErr(err, "read web command from metadata")
	}
	// TODO should below be startCommand(repoImage, "/packs/launcher", webCommand)
	repoImage, err = e.startCommand(repoImage, webCommand)
	if err != nil {
		return packs.FailErr(err, "set start command")
	}

	if err := repoStore.Write(repoImage); err != nil {
		return packs.FailErrCode(err, packs.CodeFailedUpdate, "write")
	}
	return nil
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

func (e *Exporter) addBuildpackLayers(tmpDir, launchDir string, repoImage v1.Image, origImage v1.Image) (v1.Image, error) {
	ids, err := ioutil.ReadDir(launchDir)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if id.Name() == "app" || !id.IsDir() {
			continue
		}
		layers, err := ioutil.ReadDir(filepath.Join(launchDir, id.Name()))
		if err != nil {
			return nil, err
		}
		for _, layer := range layers {
			if !layer.IsDir() {
				continue
			}
			dir := filepath.Join(launchDir, id.Name(), layer.Name())
			tarFile := filepath.Join(tmpDir, fmt.Sprintf("layer.%s.%s.tgz", id.Name(), layer.Name()))
			if err := e.createTarFile(tarFile, dir, filepath.Join("launch", id.Name(), layer.Name())); err != nil {
				return nil, packs.FailErr(err, "tar", dir, "to", tarFile)
			}
			var topLayer v1.Layer
			repoImage, topLayer, err = img.Append(repoImage, tarFile)
			if err != nil {
				return nil, packs.FailErr(err, "append droplet to image")
			}

			diffid, err := topLayer.DiffID()
			if err != nil {
				return nil, packs.FailErr(err, "calculate layer diffid")
			}
			repoImage, err = img.Label(repoImage, fmt.Sprintf("%s.%s.diffid", id.Name(), layer.Name()), diffid.String())
			if err != nil {
				return nil, packs.FailErr(err, "set layer diffid on image for", id.Name(), layer.Name())
			}

			if txt, err := ioutil.ReadFile(dir + ".toml"); os.IsNotExist(err) {
				// file doesn't exist
			} else if err != nil {
				return nil, packs.FailErr(err, "reading toml file for", dir)
			} else {
				repoImage, err = img.Label(repoImage, fmt.Sprintf("%s.%s.toml", id.Name(), layer.Name()), string(txt))
				if err != nil {
					return nil, packs.FailErr(err, "set layer toml on image for", id.Name(), layer.Name())
				}
			}
		}
		for _, layer := range layers {
			if layer.IsDir() || !strings.HasSuffix(layer.Name(), ".toml") {
				continue
			}
			file := filepath.Join(launchDir, id.Name(), layer.Name())
			if _, err := os.Stat(strings.TrimSuffix(file, ".toml")); err == nil {
				// directory exists, it was handled above
				continue
			} else if !os.IsNotExist(err) {
				return nil, packs.FailErr(err, "checking for existence of matching dir for", file)
			}

			if txt, err := ioutil.ReadFile(file); err != nil {
				return nil, packs.FailErr(err, "reading toml file", file)
			} else {
				repoImage, err = img.Label(repoImage, fmt.Sprintf("%s.%s", id.Name(), layer.Name()), string(txt))
				if err != nil {
					return nil, packs.FailErr(err, "set layer toml on image for", id.Name(), layer.Name())
				}
			}

			if origImage == nil {
				return nil, errors.New("toml file layer expected, but no previous image")
			}

			config, err := origImage.ConfigFile()
			if err != nil {
				return nil, packs.FailErr(err, "find config from origImage")
			}
			digestKey := fmt.Sprintf("%s.%s.diffid", id.Name(), strings.TrimSuffix(layer.Name(), ".toml"))
			digest := config.Config.Labels[digestKey]
			if digest == "" {
				return nil, fmt.Errorf("could not find '%s' in %+v", digestKey, config.Config.Labels)
			}
			// TODO ; don't hardcode algorithm
			hash := v1.Hash{
				Algorithm: "sha256",
				Hex:       strings.TrimPrefix(digest, "sha256:"),
			}
			origLayer, err := origImage.LayerByDiffID(hash)
			if err != nil {
				return nil, packs.FailErr(err, "find previous layer", id.Name(), layer.Name())
			}
			repoImage, err = mutate.AppendLayers(repoImage, origLayer)
			if err != nil {
				return nil, packs.FailErr(err, "append layer from previous image", id.Name(), layer.Name())
			}
			repoImage, err = img.Label(repoImage, digestKey, digest)
			if err != nil {
				return nil, packs.FailErr(err, "set layer digest on image for", id.Name(), layer.Name())
			}
		}
	}
	return repoImage, nil
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
