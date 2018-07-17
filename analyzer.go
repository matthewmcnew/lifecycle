package lifecycle

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/buildpack/packs"
	"github.com/google/go-containerregistry/pkg/v1"
)

type Analyzer struct {
	Buildpacks []string
	In         []byte
	Out, Err   io.Writer
}

func (a *Analyzer) Analyze(launchDir string, image v1.Image) error {
	if image == nil {
		return nil
	}
	config, err := a.getBuildMetadata(image)
	if err != nil {
		return packs.FailErr(err, "read metadata from previous image")
	}

	buildpacks := a.buildpacks()
	for _, buildpack := range config.Buildpacks {
		if buildpacks[buildpack.Key] == nil {
			continue
		}
		for name, metadata := range buildpack.Layers {
			path := filepath.Join(launchDir, buildpack.Key)
			if err := os.MkdirAll(path, 0755); err != nil {
				return packs.FailErr(err, "create directory buildpack", buildpack.Key)
			}
			fh, err := os.Create(filepath.Join(path, name+".toml"))
			if err != nil {
				return packs.FailErr(err, "create buildpack layer toml file", buildpack.Key, name)
			}
			defer fh.Close()
			if err := toml.NewEncoder(fh).Encode(metadata.Data); err != nil {
				return packs.FailErr(err, "marshal buildpack layer toml file", buildpack.Key, name)
			}
		}
	}

	return nil
}

func (a *Analyzer) getBuildMetadata(image v1.Image) (packs.BuildMetadata, error) {
	configFile, err := image.ConfigFile()
	if err != nil {
		return packs.BuildMetadata{}, packs.FailErr(err, "read config from image")
	}
	jsonConfig := configFile.Config.Labels[packs.BuildLabel]
	if jsonConfig == "" {
		return packs.BuildMetadata{}, nil
	}

	config := packs.BuildMetadata{}
	if err := json.Unmarshal([]byte(jsonConfig), &config); err != nil {
		return packs.BuildMetadata{}, packs.FailErr(err, "unmarshal config from image")
	}

	return config, err
}

func (a *Analyzer) buildpacks() map[string]interface{} {
	buildpacks := make(map[string]interface{}, len(a.Buildpacks))
	for _, buildpack := range a.Buildpacks {
		a := strings.Split(buildpack, "@")
		buildpacks[a[0]] = struct{}{}
	}
	return buildpacks
}
