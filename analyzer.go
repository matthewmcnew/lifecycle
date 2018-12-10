package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/buildpack/lifecycle/image"
)

type Analyzer struct {
	Buildpacks []*Buildpack
	In         []byte
	Out, Err   *log.Logger
}

func (a *Analyzer) Analyze(image image.Image, launchDir string) error {
	found, err := image.Found()
	if err != nil {
		return err
	}

	if !found {
		a.Out.Printf("WARNING: skipping analyze, image '%s' not found or requires authentication to access\n", image.Name())
		return nil
	}
	metadata, err := a.getMetadata(image)
	if err != nil {
		return err
	}
	if metadata != nil {
		return a.analyze(launchDir, *metadata)
	}
	return nil
}

func (a *Analyzer) analyze(launchDir string, metadata AppImageMetadata) error {
	groupBPs := a.buildpacks()

	err := removeOldBackpackLayersNotInGroup(groupBPs, launchDir)
	if err != nil {
		return err
	}

	for groupBP := range groupBPs {
		analyzedDirectory :=
			analyzedDirectory{metadata, launchDir, groupBP, a}

		layers, err := analyzedDirectory.allLayers()
		if err != nil {
			return err
		}

		for layer := range layers {

			layerType := analyzedDirectory.classifyLayer(layer)

			switch layerType {
			case noMetaDataForLaunchLayer:
				if err := analyzedDirectory.removeLayer(layer); err != nil {
					return err
				}
			case noMetaDataForBuildLayer:
				// nothing to do
			case outdatedLaunchLayer:
				if err := analyzedDirectory.removeLayer(layer); err != nil {
					return err
				}

				if err := analyzedDirectory.restoreMetadata(layer); err != nil {
					return err
				}
			case outdatedBuildLayer:
				if err := analyzedDirectory.removeLayer(layer); err != nil {
					return err
				}
			case noCacheAvailable:
				if err := analyzedDirectory.restoreMetadata(layer); err != nil {
					return err
				}
			case exisitingCacheUptoDate:
				// nothing to do
			default:
				panic("This should never happen")
			}

		}
	}

	return nil
}

type analyzedDirectory struct {
	metaData  AppImageMetadata
	launchDir string
	groupBP   string
	analyzer  *Analyzer
}

const (
	noMetaDataForLaunchLayer = iota
	noMetaDataForBuildLayer
	outdatedLaunchLayer
	outdatedBuildLayer
	noCacheAvailable
	exisitingCacheUptoDate
)

func (ad *analyzedDirectory) classifyLayer(layer string) int {
	cachedToml, err := readTOML(ad.layerPath(layer) + ".toml")
	if err != nil {
		return noCacheAvailable
	}

	buildpackMetadata, ok := appImageMetadata(ad.groupBP, ad.metaData)
	if !ok {
		if cachedToml.Launch == false {
			return noMetaDataForBuildLayer
		} else {
			return noMetaDataForLaunchLayer
		}
	}

	layerMetadata, ok := buildpackMetadata.Layers[layer]
	if !ok {
		if cachedToml.Launch == false {
			return noMetaDataForBuildLayer
		} else {
			return noMetaDataForLaunchLayer
		}
	}

	sha, err := ioutil.ReadFile(ad.layerPath(layer + ".sha"))
	if err != nil {
		return noCacheAvailable
	}

	if string(sha) != layerMetadata.SHA {
		if layerMetadata.Build {
			return outdatedBuildLayer
		} else {
			return outdatedLaunchLayer
		}

	}

	return exisitingCacheUptoDate

}

func (ad *analyzedDirectory) layerPath(layer string) string {
	return filepath.Join(ad.launchDir, ad.groupBP, layer)
}

func (ad *analyzedDirectory) allLayers() (map[string]interface{}, error) {
	setOfLayers := make(map[string]interface{})
	buildpackMetadata, ok := appImageMetadata(ad.groupBP, ad.metaData)
	if ok {
		for layer := range buildpackMetadata.Layers {
			setOfLayers[layer] = struct{}{}
		}
	}

	bpDir := filepath.Join(ad.launchDir, ad.groupBP)
	layerTomls, err := filepath.Glob(filepath.Join(bpDir, "*.toml"))
	if err != nil {
		return nil, err
	}
	for _, layerToml := range layerTomls {
		name := strings.TrimRight(filepath.Base(layerToml), ".toml")
		setOfLayers[name] = struct{}{}
	}
	return setOfLayers, nil
}

func (ad *analyzedDirectory) restoreMetadata(layer string) error {
	buildpackMetadata, ok := appImageMetadata(ad.groupBP, ad.metaData)
	if !ok {
		return errors.New("frick")
	}

	layerMetadata, ok := buildpackMetadata.Layers[layer]
	if !ok {
		return errors.New("frick")
	}

	return writeTOML(filepath.Join(ad.launchDir, ad.groupBP, layer+".toml"), layerMetadata)
}

func (ad *analyzedDirectory) removeLayer(name string) error {
	ad.analyzer.Out.Printf("remove stale cached layer dir '%s'\n", ad.layerPath(name))
	if err := os.RemoveAll(ad.layerPath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(ad.layerPath(name) + ".sha"); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(ad.layerPath(name) + ".toml"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func appImageMetadata(groupBP string, metadata AppImageMetadata) (*BuildpackMetadata, bool) {
	for _, buildpackMetaData := range metadata.Buildpacks {
		if buildpackMetaData.ID == groupBP {
			return &buildpackMetaData, true
		}
	}

	return nil, false
}

func (a *Analyzer) getMetadata(image image.Image) (*AppImageMetadata, error) {
	label, err := image.Label(MetadataLabel)
	if err != nil {
		return nil, err
	}
	if label == "" {
		a.Out.Printf("WARNING: skipping analyze, previous image metadata was not found\n")
		return nil, nil
	}

	metadata := &AppImageMetadata{}
	if err := json.Unmarshal([]byte(label), metadata); err != nil {
		a.Out.Printf("WARNING: skipping analyze, previous image metadata was incompatible\n")
		return nil, nil
	}
	return metadata, nil
}

func (a *Analyzer) buildpacks() map[string]struct{} {
	buildpacks := make(map[string]struct{}, len(a.Buildpacks))
	for _, b := range a.Buildpacks {
		buildpacks[b.EscapedID()] = struct{}{}
	}
	return buildpacks
}

func removeOldBackpackLayersNotInGroup(groupBPs map[string]struct{}, launchDir string) error {
	cachedBPs, err := cachedBuildpacks(launchDir)
	if err != nil {
		return err
	}

	for _, cachedBP := range cachedBPs {
		_, exists := groupBPs[cachedBP]
		if !exists {
			if err := os.RemoveAll(filepath.Join(launchDir, cachedBP)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func cachedBuildpacks(launchDir string) ([]string, error) {
	cachedBps := make([]string, 0, 0)
	bpDirs, err := filepath.Glob(filepath.Join(launchDir, "*"))
	if err != nil {
		return nil, err
	}
	for _, dir := range bpDirs {
		cachedBps = append(cachedBps, filepath.Base(dir))
	}
	return cachedBps, nil
}

func writeTOML(path string, metadata LayerMetadata) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	fh, err := os.Create(path)
	if err != nil {
		return err
	}

	defer fh.Close()
	return toml.NewEncoder(fh).Encode(metadata)
}

func readTOML(path string) (*LayerMetadata, error) {
	var metadata LayerMetadata
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	_, err = toml.DecodeFile(path, &metadata)
	if err != nil {
		return nil, err
	}

	return &metadata, err
}
