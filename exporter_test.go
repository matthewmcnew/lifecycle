package lifecycle_test

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/buildpack/lifecycle"
	"github.com/buildpack/lifecycle/testmock"
	"github.com/buildpack/packs/img"
	"github.com/golang/mock/gomock"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"
)

func TestExporter(t *testing.T) {
	rand.Seed(time.Now().UTC().UnixNano())
	spec.Run(t, "Exporter", testExporter, spec.Report(report.Terminal{}))
}

func testExporter(t *testing.T, when spec.G, it spec.S) {
	var (
		exporter       *lifecycle.Exporter
		mockCtrl       *gomock.Controller
		stdout, stderr *bytes.Buffer
		repoStore      *testmock.MockStore
	)

	it.Before(func() {
		mockCtrl = gomock.NewController(t)
		repoStore = testmock.NewMockStore(mockCtrl)

		stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
		exporter = &lifecycle.Exporter{
			Out: io.MultiWriter(stdout, it.Out()),
			Err: io.MultiWriter(stderr, it.Out()),
		}
	})

	it.After(func() {
		mockCtrl.Finish()
	})

	when("#Export", func() {
		when("a simple launch dir exists", func() {
			var imgName string
			var stackImage, firstImg v1.Image
			it.Before(func() {
				var err error
				stackImage, err = GetStackImage()
				if err != nil {
					t.Fatalf("get stack image: %s", err)
				}

				imgName = "myorg/" + RandString(8)
				repoStore.EXPECT().Write(gomock.Any()).Do(func(img v1.Image) error {
					firstImg = img
					return nil
				})

				if err := exporter.Export("cmd/exporter/fixtures/first/launch", "cmd/exporter/fixtures/first/launch/app", stackImage, nil, repoStore); err != nil {
					t.Fatalf("Error: %s\n", err)
				}
			})

			it("creates a runnable image", func() {
				out, err := exec.Command("docker", "run", "-w", "/launch/app", imgName).CombinedOutput()
				if err != nil {
					t.Fatalf("Error: %s\n", err)
				}

				if !strings.Contains(string(out), "text from layer 1") {
					t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "text from layer 1")
				}
				if !strings.Contains(string(out), "text from layer 2") {
					t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "text from layer 2")
				}
				if !strings.Contains(string(out), "Arg1 is 'MyArg'") {
					t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "Arg1 is 'MyArg'")
				}
			})

			it.Focus("sets toml files and layer digests labels", func() {
				configFile, err := firstImg.ConfigFile()
				if err != nil {
					t.Fatalf("Error: %s\n", err)
				}
				labels := configFile.Config.Labels

				if !strings.HasPrefix(labels["buildpack.id.layer1.diffid"], "sha256:") {
					t.Fatalf(`Label "%s" did not contain "%s"`, labels["buildpack.id.layer1.diffid"], "buildpack.id.layer1.diffid=sha256:")
				}
				if !strings.HasPrefix(labels["buildpack.id.layer2.diffid"], "sha256:") {
					t.Fatalf(`Label "%s" did not contain "%s"`, labels["buildpack.id.layer2.diffid"], "buildpack.id.layer2.diffid=sha256:")
				}
				if strings.TrimSpace(labels["buildpack.id.layer1.toml"]) != `mykey = "myval"` {
					t.Fatalf(`Label "%s" did not equal "%s"`, strings.TrimSpace(labels["buildpack.id.layer1.toml"]), `mykey = "myval"`)
				}
			})

			when("rebuilding when toml exists without directory", func() {
				var secondImg v1.Image
				it.Before(func() {
					repoStore.EXPECT().Write(gomock.Any()).Do(func(img v1.Image) error {
						secondImg = img
						return nil
					})
					if err := exporter.Export("cmd/exporter/fixtures/second/launch", "cmd/exporter/fixtures/second/launch/app", stackImage, firstImg, repoStore); err != nil {
						t.Fatalf("Error: %s\n", err)
					}
				})

				it("reuses layers if there is a layer.toml file", func() {
					fmt.Println("docker", "run", imgName)
					out, err := exec.Command("docker", "run", "-w", "/launch/app", imgName).CombinedOutput()
					if err != nil {
						fmt.Println(string(out))
						t.Fatal(err)
					}
					if !strings.Contains(string(out), "text from layer 1") {
						t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "text from layer 1")
					}
					if !strings.Contains(string(out), "text from new layer 2") {
						t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "text from new layer 2")
					}
				})
			})
		})
	}, spec.Parallel(), spec.Report(report.Terminal{}))
}

func GetStackImage() (v1.Image, error) {
	stackStore, err := img.NewDaemon("busybox")
	if err != nil {
		return nil, fmt.Errorf("get store for scratch: %s", err)
	}
	stackImage, err := stackStore.Image()
	if err != nil {
		return nil, fmt.Errorf("get image for scratch: %s", err)
	}
	return stackImage, nil
}

func RandString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a' + byte(rand.Intn(26))
	}
	return string(b)
}
