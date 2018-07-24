package main

import (
	"flag"
	"os"

	"github.com/buildpack/lifecycle"
	"github.com/buildpack/packs"
	"github.com/buildpack/packs/img"
)

var (
	repoName   string
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

	stackStore, err := img.NewRegistry(stackName + ":run")
	if err != nil {
		return packs.FailErr(err, "access", stackName+":run")
	}
	stackImage, err := stackStore.Image()
	if err != nil {
		return packs.FailErr(err, "get image for", stackName+":run")
	}

	exporter := &lifecycle.Exporter{
		Out: os.Stdout,
		Err: os.Stderr,
	}
	err = exporter.Export(
		launchDir,
		stackImage,
		repoStore,
	)
	if err != nil {
		return packs.FailErrCode(err, packs.CodeFailedBuild)
	}
	return nil
}
