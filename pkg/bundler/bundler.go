// Copyright © 2021 Ettore Di Giacinto <mudler@mocaccino.org>
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program; if not, see <http://www.gnu.org/licenses/>.

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
	"github.com/mholt/archiver/v3"
	"github.com/otiai10/copy"
	"github.com/pkg/errors"
)

// App is the structure holding the application metadata
// All the fields are passed to the template rendering engine
type App struct {
	Name        string
	Version     string
	Copyright   string
	Author      string
	Description string
	Entrypoint  string
	Mounts      []string
	Attrs       []string
	Store       string
	PocoVersion string
	ExtractOnly bool
}

// bundleData is the parent structure which is used by the template
type bundleData struct {
	Image         string
	LocalBuild    bool
	App           App
	CommandPrefix string
	Compression   string
}

// Bundler is the poCo application
// bundler
type Bundler struct {
	stateDir   string
	renderData bundleData
}

// WithStateDir sets the bundler application state directory
func WithStateDir(s string) Option {
	return func(k *Bundler) error {
		k.stateDir = s
		return nil
	}
}

// WithCompression sets the bundle compression algorithm
func WithCompression(c string) Option {
	return func(k *Bundler) error {
		_, err := archiver.ByExtension(fmt.Sprintf(".tar.%s", c))
		if err != nil {
			return err
		}

		k.renderData.Compression = c
		return nil
	}
}

// WithRenderData sets the data to be rendered when creating the application bundle
func WithRenderData(image, commandprefix string, localbuild bool, a App) Option {
	return func(k *Bundler) error {
		k.renderData = bundleData{Image: image, LocalBuild: localbuild, CommandPrefix: commandprefix, App: a}
		return nil
	}
}

// Option is a Bundler option
type Option func(k *Bundler) error

// New instantiate a new bundler with the given options
func New(o ...Option) (*Bundler, error) {
	k := &Bundler{
		renderData: bundleData{
			Compression: "zst",
		},
	}
	for _, oo := range o {
		if err := oo(k); err != nil {
			return nil, err
		}
	}
	return k, nil
}

// Build creates a new binary located at dst
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

// Render creates the application data at dst
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
			// Drop '.template' from name
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

// DownloadImage downloads a container image locally.
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
