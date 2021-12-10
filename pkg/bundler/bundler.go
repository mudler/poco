package bundler

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"io/fs"

	"github.com/Masterminds/sprig/v3"
	containerdarchive "github.com/containerd/containerd/archive"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/otiai10/copy"
	"github.com/pkg/errors"
)

type App struct {
	Name        string
	Version     string
	Copyright   string
	Author      string
	Description string
	Entrypoint  string
	Mounts      []string
	Store       string
	PocoVersion string
}

type bundleData struct {
	Image         string
	LocalBuild    bool
	App           App
	CommandPrefix string
}

type Bundler struct {
	stateDir   string
	renderData bundleData
}

func WithStateDir(s string) Option {
	return func(k *Bundler) error {
		k.stateDir = s
		return nil
	}
}

func WithRenderData(image, commandprefix string, localbuild bool, a App) Option {
	return func(k *Bundler) error {
		k.renderData = bundleData{Image: image, LocalBuild: localbuild, CommandPrefix: commandprefix, App: a}
		return nil
	}
}

type Option func(k *Bundler) error

func New(o ...Option) *Bundler {
	k := &Bundler{}
	for _, oo := range o {
		oo(k)
	}
	return k
}
func (k *Bundler) Build(dst string, args ...string) error {
	tempdir, err := ioutil.TempDir("", "bundler")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempdir)
	err = k.Render(tempdir)
	if err != nil {
		return err
	}
	oFile := path.Base(dst)

	err = k.goBuild(tempdir, oFile)
	if err != nil {
		return err
	}

	return copy.Copy(path.Join(tempdir, oFile), dst)
}

func (k *Bundler) goBuild(rendered string, binary string, args ...string) error {
	cmd := exec.Command("go", "mod", "verify")
	cmd.Dir = rendered
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failure while running 'go mod verify': %s", string(out))
	}

	cmd = exec.Command("go", "mod", "tidy")
	cmd.Dir = rendered
	cmd.Env = os.Environ()
	out, err = cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failure while running 'go mod tidy': %s", string(out))
	}

	cmd = exec.Command("go", "generate", "")
	cmd.Dir = rendered
	cmd.Env = os.Environ()
	out, err = cmd.CombinedOutput()
	fmt.Println(string(out))
	if err != nil {
		return errors.Wrapf(err, "failure while running 'go generate': %s", string(out))
	}

	args = append([]string{"build", "-o", binary}, args...)
	cmd = exec.Command("go", args...)
	cmd.Dir = rendered
	cmd.Env = os.Environ()
	out, err = cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failure while running 'go build': %s", string(out))
	}

	return nil
}

func (k *Bundler) Render(dst string) error {
	return fs.WalkDir(
		assets,
		".",
		func(path string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				return nil
			}

			f, err := assets.Open(path)
			if err != nil {
				return err
			}
			dat, err := ioutil.ReadAll(f)
			if err != nil {
				return err
			}
			buf := bytes.NewBufferString("")

			filename := filepath.Base(path)
			filename = strings.ReplaceAll(filename, ".template", "")
			dir := filepath.Dir(path)

			// strip "gen/" from the assets, as it is where the files are
			dir = strings.TrimLeft(dir, "gen")

			// render file
			t := template.Must(template.New("render").Funcs(sprig.TxtFuncMap()).Parse(string(dat)))
			t.Execute(buf, k.renderData)

			os.MkdirAll(filepath.Join(dst, dir), os.ModePerm)

			return ioutil.WriteFile(filepath.Join(dst, dir, filename), buf.Bytes(), os.ModePerm)
		},
	)
}

func (k *Bundler) DownloadImage(image, dst string, local bool) error {
	os.MkdirAll(dst, os.ModePerm)
	ref, err := name.ParseReference(image)
	if err != nil {
		return err
	}

	var img v1.Image

	if local {
		img, err = daemon.Image(ref, daemon.WithUnbufferedOpener())
		if err != nil {
			return errors.Wrap(err, "failure while retreiving image from daemon")
		}
	} else {
		// If we fail to provide from daemon, get it remotely
		img, err = remote.Image(ref)
		if err != nil {
			return errors.Wrap(err, "failure while downloading image")
		}
	}

	reader := mutate.Extract(img)

	defer reader.Close()

	_, err = containerdarchive.Apply(context.Background(), dst, reader)
	if err != nil {
		return errors.Wrap(err, "failure while extracting image")
	}
	return nil
}
