package lifecycle_test

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os/exec"
	"strings"
	"testing"

	"github.com/buildpack/lifecycle"
	"github.com/buildpack/packs/img"
	"github.com/golang/mock/gomock"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"
)

func TestExporter(t *testing.T) {
	spec.Run(t, "Exporter", testExporter, spec.Report(report.Terminal{}))
}

func testExporter(t *testing.T, when spec.G, it spec.S) {
	var (
		exporter *lifecycle.Exporter
		// TODO: remove mockCtrl if remains unused
		mockCtrl       *gomock.Controller
		stdout, stderr *bytes.Buffer
		// repoStore      *testmock.MockStore
	)

	it.Before(func() {
		mockCtrl = gomock.NewController(t)
		// repoStore = testmock.NewMockStore(mockCtrl)

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
			var stackImage v1.Image
			var repoStore img.Store
			it.Before(func() {
				var err error
				stackImage, err = GetBusyboxWithEntrypoint()
				if err != nil {
					t.Fatalf("get busybox image for stack: %s", err)
				}

				imgName = "myorg/" + RandString(8)
				repoStore, err = img.NewDaemon(imgName)
				if err != nil {
					t.Fatal("repo store", imgName, err)
				}

				if err := exporter.Export("cmd/exporter/fixtures/first/launch", "cmd/exporter/fixtures/first/launch/app", stackImage, nil, repoStore); err != nil {
					t.Fatalf("Error: %s\n", err)
				}
			})
			it.After(func() {
				deleteImg(imgName)
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

			it("sets toml files and layer digests labels", func() {
				out, err := exec.Command("docker", "inspect", imgName, "--format", "{{ range $k, $v := .Config.Labels -}}{{ $k }}={{ $v }}\n{{ end -}}").CombinedOutput()
				if err != nil {
					fmt.Println(string(out))
					t.Fatal(err)
				}
				if !strings.Contains(string(out), "buildpack.id.layer1.diffid=sha256:") {
					t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "buildpack.id.layer1.diffid=sha256:")
				}
				if !strings.Contains(string(out), "buildpack.id.layer2.diffid=sha256:") {
					t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "buildpack.id.layer2.diffid=sha256:")
				}
				if !strings.Contains(string(out), "buildpack.id.layer1.toml=mykey = \"myval\"") {
					t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "buildpack.id.layer1.toml=mykey = \"myval\"")
				}
			})

			when("rebuilding when toml exists without directory", func() {
				it.Before(func() {
					prevImage, err := repoStore.Image()
					if err != nil {
						t.Fatal(err)
					}
					if err := exporter.Export("cmd/exporter/fixtures/second/launch", "cmd/exporter/fixtures/second/launch/app", stackImage, prevImage, repoStore); err != nil {
						t.Fatalf("Error: %s\n", err)
					}
				})

				it.Focus("reuses layers if there is a layer.toml file", func() {
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

func GetBusyboxWithEntrypoint() (v1.Image, error) {
	stackStore, err := img.NewRegistry("busybox")
	if err != nil {
		return nil, fmt.Errorf("get store for busybox: %s", err)
	}
	stackImage, err := stackStore.Image()
	if err != nil {
		return nil, fmt.Errorf("get image for SCRATCH: %s", err)
	}
	configFile, err := stackImage.ConfigFile()
	if err != nil {
		return nil, err
	}
	config := *configFile.Config.DeepCopy()
	config.Entrypoint = []string{"sh", "-c"}
	// TODO: Should we set working directory? It helps with unit tests
	// config.WorkingDir = appDir
	return mutate.Config(stackImage, config)
}

func RandString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a' + byte(rand.Intn(26))
	}
	return string(b)
}

func deleteImg(imgName string) error {
	if out, err := exec.Command("docker", "rmi", "-f", imgName).CombinedOutput(); err != nil {
		fmt.Println(string(out))
		return err
	}
	return nil
}
