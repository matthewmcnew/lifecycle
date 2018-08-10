package main

import (
	"flag"
	"io/ioutil"
	"log"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/buildpack/packs"

	"github.com/buildpack/lifecycle"
)

var (
	buildpackPath string
	orderPath     string
	groupPath     string
	infoPath      string
)

func init() {
	packs.InputBPPath(&buildpackPath)
	packs.InputBPOrderPath(&orderPath)

	packs.InputBPGroupPath(&groupPath)
	packs.InputDetectInfoPath(&infoPath)
}

func main() {
	flag.Parse()
	if flag.NArg() != 0 || buildpackPath == "" || orderPath == "" || groupPath == "" || infoPath == "" {
		packs.Exit(packs.FailCode(packs.CodeInvalidArgs, "parse arguments"))
	}
	packs.Exit(detect())
}

func detect() error {
	buildpacks, err := lifecycle.NewBuildpackMap(buildpackPath)
	if err != nil {
		return packs.FailErr(err, "read buildpack directory")
	}

	var order OrderToml
	if _, err := toml.DecodeFile(orderPath, &order); err != nil {
		return packs.FailErr(err, "read buildpack order")
	}

	log := log.New(os.Stderr, "", log.LstdFlags)
	info, group := order.Order(buildpacks).Detect(log, lifecycle.DefaultAppDir)
	if len(group.Buildpacks) == 0 {
		return packs.FailCode(packs.CodeFailedDetect, "detect")
	}

	groupFile, err := os.Create(groupPath)
	if err != nil {
		return packs.FailErr(err, "create buildpack group file")
	}
	defer groupFile.Close()
	if err := toml.NewEncoder(groupFile).Encode(convertFromLifecycleBuildpackGroup(group)); err != nil {
		return packs.FailErr(err, "write buildpack group")
	}

	if err := ioutil.WriteFile(infoPath, info, 0666); err != nil {
		return packs.FailErr(err, "write detect info")
	}

	return nil
}

type Buildpack struct {
	ID      string `toml:"id"`
	Version string `toml:"version"`
}

type BuildpackGroup struct {
	Repository string      `toml:"repository"`
	Buildpacks []Buildpack `toml:"buildpacks"`
}

type OrderToml struct {
	Groups []BuildpackGroup `toml:"groups"`
}

func (o *OrderToml) Order(m lifecycle.BuildpackMap) lifecycle.BuildpackOrder {
	var bs lifecycle.BuildpackOrder
	for _, g := range o.Groups {
		var buildpacks []lifecycle.BuildpackMapIDVersion
		for _, b := range g.Buildpacks {
			buildpacks = append(buildpacks, lifecycle.BuildpackMapIDVersion(b))
		}
		bs = append(bs, lifecycle.BuildpackGroup{
			Repository: g.Repository,
			Buildpacks: m.FromList(buildpacks),
		})
	}
	return bs
}

func convertFromLifecycleBuildpackGroup(group *lifecycle.BuildpackGroup) BuildpackGroup {
	out := BuildpackGroup{Repository: group.Repository, Buildpacks: make([]Buildpack, len(group.Buildpacks))}
	for i, b := range group.Buildpacks {
		out.Buildpacks[i].ID = b.ID
		out.Buildpacks[i].Version = b.Version
	}
	return out
}
