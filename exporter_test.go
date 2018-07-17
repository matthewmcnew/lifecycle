package lifecycle_test

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/buildpack/lifecycle"
	"github.com/buildpack/lifecycle/testmock"
	"github.com/buildpack/packs/img"
	"github.com/golang/mock/gomock"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"
)

func TestExporter(t *testing.T) {
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
			it("creates a runnable image", func() {
				stackStore, err := img.NewRegistry("busybox")
				if err != nil {
					t.Fatalf("get store for SCRATCH: %s", err)
				}
				stackImage, err := stackStore.Image()
				if err != nil {
					t.Fatalf("get image for SCRATCH: %s", err)
				}
				repoName := "testwithrandomness"
				repoStore, err := img.NewDaemon(repoName)
				if err != nil {
					t.Fatal("repo store", repoName, err)
				}

				if err := exporter.Export("cmd/exporter/fixtures/first/launch", "cmd/exporter/fixtures/first/launch/app", stackImage, nil, repoStore); err != nil {
					t.Fatalf("Error: %s\n", err)
				}

				out, err := exec.Command("docker", "run", "-w", "/launch/app", repoName).CombinedOutput()
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

			// it("sets toml files and layer digests labels", func() {
			// 	out, err := exec.Command("docker", "inspect", imgName+":v1", "--format", "{{ range $k, $v := .Config.Labels -}}{{ $k }}={{ $v }}\n{{ end -}}").CombinedOutput()
			// 	if err != nil {
			// 		fmt.Println(string(out))
			// 		t.Fatal(err)
			// 	}
			// 	if !strings.Contains(string(out), "buildpack.id.layer1.diffid=sha256:") {
			// 		t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "buildpack.id.layer1.diffid=sha256:")
			// 	}
			// 	if !strings.Contains(string(out), "buildpack.id.layer2.diffid=sha256:") {
			// 		t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "buildpack.id.layer2.diffid=sha256:")
			// 	}
			// 	if !strings.Contains(string(out), "buildpack.id.layer1.toml=mykey = \"myval\"") {
			// 		t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "buildpack.id.layer1.toml=mykey = \"myval\"")
			// 	}
			// })
			//
			// when("rebuilding when toml exists without directory", func() {
			// 	it.Before(func() {
			// 		if err := runExport(compiledPath, "fixtures/second/launch", "packs/v3:run", imgName+":v2", imgName+":v1"); err != nil {
			// 			t.Fatal(err)
			// 		}
			// 	})
			// 	it.After(func() {
			// 		deleteImg(imgName + ":v2")
			// 	})
			//
			// 	it("reuses layers if there is a layer.toml file", func() {
			// 		out, err := exec.Command("docker", "run", imgName+":v2").CombinedOutput()
			// 		if err != nil {
			// 			t.Fatal(err)
			// 		}
			// 		if !strings.Contains(string(out), "text from layer 1") {
			// 			t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "text from layer 1")
			// 		}
			// 		if !strings.Contains(string(out), "text from new layer 2") {
			// 			t.Fatalf(`Output "%s" did not contain "%s"`, string(out), "text from new layer 2")
			// 		}
			// 	})
			// })
		})
	}, spec.Parallel(), spec.Report(report.Terminal{}))
}
