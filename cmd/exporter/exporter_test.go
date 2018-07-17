package main_test

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"
)

func Test(t *testing.T) {
	rand.Seed(time.Now().UTC().UnixNano())
	compiledPath, cleanupFunc := buildExporter()
	defer cleanupFunc()

	spec.Run(t, "exporter", func(t *testing.T, when spec.G, it spec.S) {
		when("a simple launch dir exists", func() {
			var imgName string
			it.Before(func() {
				imgName = "myorg/" + RandString(8)
				fmt.Println("IMGNAME:", imgName)
				if err := runExport(compiledPath, "fixtures/first/launch", "packs/v3:run", imgName+":v1"); err != nil {
					t.Fatal(err)
				}
			})
			// it.After(func() {
			// 	deleteImg(imgName + ":v1")
			// })

			it("creates a runnable image", func() {
				out, err := exec.Command("docker", "run", imgName+":v1").CombinedOutput()
				if err != nil {
					fmt.Println(string(out))
					t.Fatal(err)
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
				out, err := exec.Command("docker", "inspect", imgName+":v1", "--format", "{{ range $k, $v := .Config.Labels -}}{{ $k }}={{ $v }}\n{{ end -}}").CombinedOutput()
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
					if err := runExport(compiledPath, "fixtures/second/launch", "packs/v3:run", imgName+":v2", imgName+":v1"); err != nil {
						t.Fatal(err)
					}
				})
				it.After(func() {
					deleteImg(imgName + ":v2")
				})

				it("reuses layers if there is a layer.toml file", func() {
					out, err := exec.Command("docker", "run", imgName+":v2").CombinedOutput()
					if err != nil {
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

func runExport(compiledPath, launchDir, stackName string, imgNames ...string) error {
	launchDir, err := filepath.Abs(launchDir)
	if err != nil {
		return err
	}
	args := append([]string{"-daemon", "-launch", launchDir, "-app", filepath.Join(launchDir, "app"), "-stack", stackName}, imgNames...)
	fmt.Println(compiledPath, args)
	cmd := exec.Command(compiledPath, args...)
	cmd.Dir = filepath.Join(launchDir, "app")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Println("OUT:", string(out))
		return fmt.Errorf("run analyzer: %s", err)
	}
	return nil
}

func deleteImg(imgName string) error {
	if out, err := exec.Command("docker", "rmi", "-f", imgName).CombinedOutput(); err != nil {
		fmt.Println(string(out))
		return err
	}
	return nil
}

func buildExporter() (string, func()) {
	tmpDir, err := ioutil.TempDir("", "v3.exporter.build.")
	if err != nil {
		panic(fmt.Sprintf("temp dir: %s", err))
	}
	path := filepath.Join(tmpDir, "exporter")
	cmd := exec.Command("go", "build", "-o", path, "main.go")
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.RemoveAll(tmpDir)
		fmt.Println(string(out))
		panic(fmt.Sprintf("build exporter: %s", err))
	}
	return path, func() { os.RemoveAll(tmpDir) }
}

func RandString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a' + byte(rand.Intn(26))
	}
	return string(b)
}
