package lifecycle_test

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildpack/lifecycle"
	"github.com/buildpack/lifecycle/testmock"
	"github.com/buildpack/packs"
	"github.com/golang/mock/gomock"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"
)

func TestAnalyzer(t *testing.T) {
	spec.Run(t, "Analyzer", testAnalyzer, spec.Report(report.Terminal{}))
}

//go:generate mockgen -package testmock -destination testmock/image.go github.com/google/go-containerregistry/pkg/v1 Image

func testAnalyzer(t *testing.T, when spec.G, it spec.S) {
	var (
		analyzer       *lifecycle.Analyzer
		mockCtrl       *gomock.Controller
		stdout, stderr *bytes.Buffer
		launchDir      string
		image          *testmock.MockImage
	)

	it.Before(func() {
		mockCtrl = gomock.NewController(t)
		image = testmock.NewMockImage(mockCtrl)

		var err error
		launchDir, err = ioutil.TempDir("", "lifecycle-launch-dir")
		if err != nil {
			t.Fatalf("Error: %s\n", err)
		}

		stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
		analyzer = &lifecycle.Analyzer{
			// Buildpacks: []*lifecycle.Buildpack{
			// 	{ID: "buildpack1-id", Dir: buildpackDir},
			// 	{ID: "buildpack2-id", Dir: buildpackDir},
			// },
			Out: io.MultiWriter(stdout, it.Out()),
			Err: io.MultiWriter(stderr, it.Out()),
		}
	})

	it.After(func() {
		os.RemoveAll(launchDir)
		mockCtrl.Finish()
	})

	when("#Analyze", func() {
		when("image exists and has labels", func() {
			it.Before(func() {
				var configFile = &v1.ConfigFile{}
				configFile.Config.Labels = map[string]string{packs.BuildLabel: `{
					"buildpacks": [
						{
							"key": "buildpack.node",
							"layers": {
								"nodejs": {
									"data": {"akey": "avalue", "bkey": "bvalue"}
								},
								"node_modules": {
									"data": {"version": "1234"}
								}
							}
						},
						{
							"key": "buildpack.go",
							"layers": {
								"go": {
									"data": {"version": "1.10"}
								}
							}
						}
					]
				}`}
				image.EXPECT().ConfigFile().Return(configFile, nil)
			})
			it("uses labels to populate the launch dir", func() {
				if err := analyzer.Analyze(launchDir, image); err != nil {
					t.Fatalf("Error: %s\n", err)
				}

				if txt, err := ioutil.ReadFile(filepath.Join(launchDir, "buildpack.node", "nodejs.toml")); err != nil {
					t.Fatalf("Error: %s\n", err)
				} else if string(txt) != `akey = "avalue"`+"\n"+`bkey = "bvalue"`+"\n" {
					t.Fatalf(`Error: expected "%s" to be toml encoded nodejs.toml`, txt)
				}
				if txt, err := ioutil.ReadFile(filepath.Join(launchDir, "buildpack.node", "node_modules.toml")); err != nil {
					t.Fatalf("Error: %s\n", err)
				} else if string(txt) != `version = "1234"`+"\n" {
					t.Fatalf(`Error: expected "%s" to be toml encoded node_modules.toml`, txt)
				}
				if txt, err := ioutil.ReadFile(filepath.Join(launchDir, "buildpack.go", "go.toml")); err != nil {
					t.Fatalf("Error: %s\n", err)
				} else if string(txt) != `version = "1.10"`+"\n" {
					t.Fatalf(`Error: expected "%s" to be toml encoded node_modules.toml`, txt)
				}
			})
		})
		when("image exists but is missing config", func() {
			it.Before(func() {
				var configFile = &v1.ConfigFile{}
				image.EXPECT().ConfigFile().Return(configFile, nil)
			})
			it("does nothing and succeeds", func() {
				if err := analyzer.Analyze(launchDir, image); err != nil {
					t.Fatalf("Error: %s\n", err)
				}
			})
		})
		when("image is nil", func() {
			it("does nothing and succeeds", func() {
				if err := analyzer.Analyze(launchDir, nil); err != nil {
					t.Fatalf("Error: %s\n", err)
				}
			})
		})
	})
}

// func testExists(t *testing.T, paths ...string) {
// 	t.Helper()
// 	for _, p := range paths {
// 		if _, err := os.Stat(p); err != nil {
// 			t.Fatalf("Error: %s\n", err)
// 		}
// 	}
// }
