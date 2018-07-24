package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/google/go-containerregistry/pkg/v1"

	"github.com/buildpack/lifecycle"
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
)

func init() {
	packs.InputStackName(&stackName)
	packs.InputUseDaemon(&useDaemon)
	packs.InputUseHelpers(&useHelpers)

	flag.StringVar(&launchDir, "launch", "/launch", "launch directory")
}

func main() {
	flag.Parse()
	if flag.NArg() > 1 || flag.Arg(0) == "" || stackName == "" || launchDir == "" {
		packs.Exit(packs.FailCode(packs.CodeInvalidArgs, "parse arguments"))
	}
	repoName = flag.Arg(0)
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

	exporter := &lifecycle.Exporter{
		Out: os.Stdout,
		Err: os.Stderr,
	}
	err = exporter.Export(
		launchDir,
		stackImage,
		origImage,
		repoStore,
	)
	if err != nil {
		return packs.FailErrCode(err, packs.CodeFailedBuild)
	}
	return nil
}
