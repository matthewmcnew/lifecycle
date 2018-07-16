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
	if flag.NArg() > 1 || repoName == "" || stackName == "" || launchDir == "" || appDir == "" {
		packs.Exit(packs.FailCode(packs.CodeInvalidArgs, "parse arguments"))
	}
	packs.Exit(analyzer())
}

func analyzer() error {
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

	origImage, err := repoStore.Image()
	if err != nil {
		return packs.FailErr(err, "get image for", repoName)
	}

	analyzer := &lifecycle.Analyzer{
		// TODO we probably need this so that we can choose to NOT create toml files for buildpacks we aren't using
		// Buildpacks: buildpacks.FromList(group.Buildpacks),
		Out: os.Stdout,
		Err: os.Stderr,
	}
	err = analyzer.Analyze(
		lifecycle.DefaultLaunchDir,
		origImage,
	)
	if err != nil {
		return packs.FailErrCode(err, packs.CodeFailedBuild)
	}

	return nil
}
