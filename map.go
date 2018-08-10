package lifecycle

import (
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type BuildpackMap map[string]*Buildpack

func NewBuildpackMap(dir string) (BuildpackMap, error) {
	buildpacks := BuildpackMap{}
	glob := filepath.Join(dir, "*", "*", "buildpack.toml")
	files, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	for _, bpTOML := range files {
		buildpackDir := filepath.Dir(bpTOML)
		base, version := filepath.Split(buildpackDir)
		_, id := filepath.Split(filepath.Clean(base))
		var buildpack Buildpack
		if _, err := toml.DecodeFile(bpTOML, &buildpack); err != nil {
			return nil, err
		}
		buildpack.Dir = buildpackDir
		buildpacks[id+"@"+version] = &buildpack
	}
	return buildpacks, nil
}

func (m BuildpackMap) Map(l []*Buildpack) []*Buildpack {
	out := make([]*Buildpack, 0, len(l))
	for _, i := range l {
		ref := i.ID + "@" + i.Version
		if i.Version == "" {
			ref += "latest"
		}
		if bp, ok := m[ref]; ok {
			out = append(out, bp)
		}
	}
	return out
}

func (m BuildpackMap) MapOut(l []*Buildpack) []*SimpleBuildpack {
	out := make([]*SimpleBuildpack, 0, len(l))
	for _, b := range l {
		out = append(out, &SimpleBuildpack{ID: b.ID, Version: b.Version})
	}
	return out
}
