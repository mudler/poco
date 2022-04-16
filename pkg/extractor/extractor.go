package extractor

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	cp "github.com/otiai10/copy"
	ldd "github.com/u-root/u-root/pkg/ldd"
)

type config struct {
	files  []string
	outDir string
}

// WithFiles sets the input files of the packer
func WithFiles(s ...string) option {
	return func(k *config) error {
		k.files = s
		return nil
	}
}

// WithOutputDir sets the output dir of the packer
func WithOutputDir(s string) option {
	return func(k *config) error {
		k.outDir = s
		return nil
	}
}

// Option is an extractor option
type option func(k *config) error

// Extract a binary and its deps into a folder
func Extract(o ...option) error {
	config := &config{}
	for _, oo := range o {
		if err := oo(config); err != nil {
			return err
		}
	}

	files, err := ldd.Ldd(config.files)
	if err != nil {
		return err
	}

	// copy ldd libs
	for _, f := range files {
		fmt.Println("Found", f.Name(), f.FullName)
		p := path.Dir(f.FullName)

		os.MkdirAll(filepath.Join(config.outDir, p), os.ModePerm)
		if err := cp.Copy(f.FullName, filepath.Join(config.outDir, p, f.Name())); err != nil {
			return err
		}
	}

	// copy files
	for _, f := range config.files {
		p := path.Dir(f)

		os.MkdirAll(filepath.Join(config.outDir, p), os.ModePerm)
		if err := cp.Copy(f, filepath.Join(config.outDir, p, filepath.Base(f))); err != nil {
			return err
		}
	}
	return nil
}
