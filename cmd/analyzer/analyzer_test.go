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
	compiledPath, cleanupFunc := buildAnalyzer()
	defer cleanupFunc()

	spec.Run(t, "analyzer", func(t *testing.T, when spec.G, it spec.S) {
		when("previous image exists", func() {
			var imgName, tmpDir string
			it.Before(func() {
				imgName = RandString(8)
				err := createPreviousImg(imgName)
				if err != nil {
					t.Fatal("create previous image", err)
				}
				tmpDir, err = ioutil.TempDir("", "v3.analyzer.")
				if err != nil {
					t.Fatal("create tmp dir", err)
				}
				if err := os.Mkdir(filepath.Join(tmpDir, "launch"), 0755); err != nil {
					t.Fatal("create launch dir", err)
				}
				if err := ioutil.WriteFile(filepath.Join(tmpDir, "group.toml"), []byte(`buildpacks = ["buildpack.node@1.0.0"]`), 0644); err != nil {
					t.Fatal("create launch dir", err)
				}
			})
			it.After(func() {
				deleteImg(imgName)
				os.RemoveAll(tmpDir)
			})

			it("puts toml files on disk", func() {
				if out, err := exec.Command(compiledPath, "-daemon", "-launch", filepath.Join(tmpDir, "launch"), "-group", filepath.Join(tmpDir, "group.toml"), imgName).CombinedOutput(); err != nil {
					fmt.Println("OUT:", string(out))
					t.Fatal("run analyzer", err)
				}

				if txt, err := ioutil.ReadFile(filepath.Join(tmpDir, "launch", "buildpack.node", "nodejs.toml")); err != nil {
					t.Fatalf("Error: %s\n", err)
				} else if strings.TrimSpace(string(txt)) != `version = "1.2.3"` {
					t.Fatalf(`Error: expected "%s" to be toml encoded nodejs.toml`, strings.TrimSpace(string(txt)))
				}
			})
		})
	}, spec.Parallel(), spec.Report(report.Terminal{}))
}

func createPreviousImg(imgName string) error {
	tmpDir, err := ioutil.TempDir("", "lifecycle.analyzer.dockerfile.")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := ioutil.WriteFile(filepath.Join(tmpDir, "emptyfile"), []byte(""), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(`
		FROM scratch
		LABEL sh.packs.build '{"buildpacks":[{"key":"buildpack.node","layers":{"nodejs":{"data":{"version":"1.2.3"}}}}]}'
		COPY emptyfile /
	`), 0644); err != nil {
		return err
	}
	if out, err := exec.Command("docker", "build", "-t", imgName, tmpDir).CombinedOutput(); err != nil {
		fmt.Println(string(out))
		return err
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

func RandString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a' + byte(rand.Intn(26))
	}
	return string(b)
}

func buildAnalyzer() (string, func()) {
	tmpDir, err := ioutil.TempDir("", "v3.analyzer.build.")
	if err != nil {
		panic(fmt.Sprintf("temp dir: %s", err))
	}
	path := filepath.Join(tmpDir, "analyzer")
	cmd := exec.Command("go", "build", "-o", path, "main.go")
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.RemoveAll(tmpDir)
		fmt.Println(string(out))
		panic(fmt.Sprintf("build analyzer: %s", err))
	}
	return path, func() { os.RemoveAll(tmpDir) }
}
