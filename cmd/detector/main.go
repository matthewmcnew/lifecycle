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
	if err := toml.NewEncoder(groupFile).Encode(GroupToml{Repository: group.Repository, Buildpacks: buildpacks.MapOut(group.Buildpacks)}); err != nil {
		return packs.FailErr(err, "write buildpack group")
	}

	if err := ioutil.WriteFile(infoPath, info, 0666); err != nil {
		return packs.FailErr(err, "write detect info")
	}

	return nil
}

type OrderToml struct {
	Groups lifecycle.BuildpackOrder `toml:"groups"`
}
type GroupToml struct {
	Repository string
	Buildpacks []*lifecycle.SimpleBuildpack
}

func (o *OrderToml) Order(m lifecycle.BuildpackMap) lifecycle.BuildpackOrder {
	var bs lifecycle.BuildpackOrder
	for _, g := range o.Groups {
		bs = append(bs, lifecycle.BuildpackGroup{
			Repository: g.Repository,
			Buildpacks: m.Map(g.Buildpacks),
		})
	}
	return bs
}
