package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"

	"github.com/buildpack/packs"
	"github.com/buildpack/packs/img"
)

var (
	repoName   string
	prevName   string
	stackName  string
	useDaemon  bool
	useHelpers bool
	launchDir  string
	appDir     string
)

func init() {
	packs.InputStackName(&stackName)
	packs.InputUseDaemon(&useDaemon)
	packs.InputUseHelpers(&useHelpers)

	flag.StringVar(&launchDir, "launch", "/launch", "launch directory")
	flag.StringVar(&appDir, "app", "/launch/app", "app directory")
}

func main() {
	flag.Parse()
	repoName = flag.Arg(0)
	if flag.NArg() >= 2 {
		prevName = flag.Arg(1)
	}
	if flag.NArg() > 2 || repoName == "" || stackName == "" || launchDir == "" || appDir == "" {
		packs.Exit(packs.FailCode(packs.CodeInvalidArgs, "parse arguments"))
	}
	packs.Exit(export())
}

func export() error {
	if useHelpers {
		if err := img.SetupCredHelpers(repoName, stackName); err != nil {
			return packs.FailErr(err, "setup credential helpers")
		}
	}

	newRepoStore := img.NewRegistry
	if useDaemon {
		newRepoStore = img.NewDaemon
	}
	repoStore, err := newRepoStore(repoName)
	if err != nil {
		return packs.FailErr(err, "access", repoName)
	}

	fmt.Println("=== read stack")
	stackStore, err := img.NewRegistry(stackName)
	if err != nil {
		return packs.FailErr(err, "access", stackName)
	}
	stackImage, err := stackStore.Image()
	if err != nil {
		return packs.FailErr(err, "get image for", stackName)
	}

	var origImage v1.Image
	if prevName != "" {
		fmt.Println("=== read previous image", prevName)
		store, err := newRepoStore(prevName)
		if err != nil {
			return packs.FailErr(err, "access", prevName)
		}
		origImage, err = store.Image()
		if err != nil {
			return packs.FailErr(err, "get image for", prevName)
		}
	}

	fmt.Println("=== create temp dir")
	tmpDir, err := ioutil.TempDir("", "pack.export.layer")
	if err != nil {
		return packs.FailErr(err, "create temp directory")
	}
	defer os.RemoveAll(tmpDir)

	fmt.Println("=== add app as layer")
	tarFile := filepath.Join(tmpDir, "app.tgz")
	args := []string{"-czf", tarFile, fmt.Sprintf("--transform=s,%s,launch/app,", strings.TrimPrefix(appDir, "/")), appDir}
	fmt.Println("tar", args)
	if _, err := packs.Run("tar", args...); err != nil {
		return packs.FailErr(err, "tar", appDir, "to", tarFile)
	}
	repoImage, _, err := img.Append(stackImage, tarFile)
	if err != nil {
		return packs.FailErr(err, "append droplet to", stackName)
	}

	// TODO: remove below line
	cmd := exec.Command("tar", "tf", tarFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	fmt.Println("=== add other layers")
	repoImage, err = addBuildpackLayers(tmpDir, repoImage, origImage)
	if err != nil {
		return packs.FailErr(err, "append layers")
	}

	// TODO: This appears to be the correct answer. Is it?
	webCommand, err := WebCommand(filepath.Join(appDir, "metadata.toml"))
	if err != nil {
		return packs.FailErr(err, "read web command from metadata")
	}
	repoImage, err = StartCommand(repoImage, "/packs/launcher", webCommand)
	if err != nil {
		return packs.FailErr(err, "set start command")
	}

	fmt.Println("=== write image", repoName)
	if err := repoStore.Write(repoImage); err != nil {
		return packs.FailErrCode(err, packs.CodeFailedUpdate, "write", repoName)
	}
	return nil
}

// TODO move this back to lib (somehow)
func StartCommand(image v1.Image, cmd ...string) (v1.Image, error) {
	configFile, err := image.ConfigFile()
	if err != nil {
		return nil, err
	}
	config := *configFile.Config.DeepCopy()
	config.Cmd = cmd
	return mutate.Config(image, config)
}

// TODO delete these once this is inside lifecycle proper
type Process struct {
	Type    string `toml:"type"`
	Command string `toml:"command"`
}

type LaunchTOML struct {
	Processes []Process `toml:"processes"`
}

func WebCommand(tomlPath string) (string, error) {
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

func addBuildpackLayers(tmpDir string, repoImage v1.Image, origImage v1.Image) (v1.Image, error) {
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
			if _, err := packs.Run("tar", "-czf", tarFile, fmt.Sprintf("--transform=s,%s,launch/%s/%s,", strings.TrimPrefix(dir, "/"), id.Name(), layer.Name()), dir); err != nil {
				return nil, packs.FailErr(err, "tar", appDir, "to", tarFile)
			}
			var topLayer v1.Layer
			repoImage, topLayer, err = img.Append(repoImage, tarFile)
			if err != nil {
				return nil, packs.FailErr(err, "append droplet to", stackName)
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

			// fmt.Printf("OrigImage: %+v\n", origImage)
			config, err := origImage.ConfigFile()
			if err != nil {
				return nil, packs.FailErr(err, "find config from origImage", prevName)
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
