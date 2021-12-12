package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/mholt/archiver/v3"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"golang.org/x/sys/unix"

	//"strings"
	"syscall"
)

// - Alternatively
// - go:generate {{.CommandPrefix}} tar -cJvf assets.tar.xz -C assets/ .
// - go:generate {{.CommandPrefix}} chmod 655 assets.tar.xz
//go:generate {{.CommandPrefix}} poco unpack {{if .LocalBuild }}--local {{ end }} {{.Image}} assets
//go:generate {{.CommandPrefix}} poco pack-assets -C assets .
//go:embed assets.tar.xz
var assets embed.FS

func common() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name: "store",
			Value: "{{.App.Store}}",
			Usage: "Default application store. Empty for TMPDIR",
		},
		&cli.StringFlag{
			Name:  "entrypoint",
			Value: "{{.App.Entrypoint}}",
			Usage: "Default application entrypoint",
		},
		&cli.StringSliceFlag{
			Name:  "add-mounts",
			Usage: "Additional mountpoints",
		},
		&cli.StringSliceFlag{
			Name:  "mounts",
			Usage: "Default app mountpoints",
			{{ if .App.Mounts }}
			Value: &cli.StringSlice{"{{.App.Mounts | join "\",\"" }}"},
			{{ end }}
		},
	}
}

func main() {

	app := &cli.App{
		Name:        "{{.App.Name}}",
		Version:     "{{.App.Version}}",
		Author:      "{{.App.Author}}",
		Usage:       "{{.App.Name}}",
		Description: "{{.App.Description}}",
		Copyright:   `{{.App.Copyright}}
Built with poCo {{.App.PocoVersion}}`,
		Action:      start,
		Commands: []cli.Command{
			{
				Name:        "exec",
				Description: "execute program",
				Action:      execute,
				Flags:       common(),
			},
			{
				Name:        "uninstall",
				Description: "uninstall program",
				Action:      uninstall,
				Flags:       common(),
			},
		},
		Flags: common(),
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	return
}

func pivotRoot(newroot string) error {
	// Create new mount namespace so mounts don't leak
	if err := syscall.Unshare(syscall.CLONE_NEWNS); err != nil {
		return fmt.Errorf("Error creating mount namespace before pivot: %v", err)
	}

	putold := filepath.Join(newroot, ".pivot_root")

	// bind mount newroot to itself - this is a slight hack needed to satisfy the
	// pivot_root requirement that newroot and putold must not be on the same
	// filesystem as the current root
	if err := syscall.Mount(newroot, newroot, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return err
	}

	// create putold directory
	if err := os.MkdirAll(putold, 0700); err != nil {
		return err
	}

	// call pivot_root
	if err := syscall.PivotRoot(newroot, putold); err != nil {
		return err
	}

	// ensure current working directory is set to new root
	if err := os.Chdir("/"); err != nil {
		return err
	}

	// umount putold, which now lives at /.pivot_root
	putold = "/.pivot_root"
	if err := syscall.Unmount(putold, syscall.MNT_DETACH); err != nil {
		return err
	}

	// remove putold
	if err := os.RemoveAll(putold); err != nil {
		return err
	}

	return nil
}

func mountProc(newroot string) error {
	source := "proc"
	target := filepath.Join(newroot, "/proc")
	fstype := "proc"
	flags := 0
	data := ""

	os.MkdirAll(target, 0755)
	if err := syscall.Mount(
		source,
		target,
		fstype,
		uintptr(flags),
		data,
	); err != nil {
		return err
	}

	return nil
}

func mountBind(hostfolder, newroot, dst string, rw bool) error {
	source := hostfolder
	target := filepath.Join(newroot, dst)
	fstype := "bind"
	data := ""

	os.MkdirAll(target, 0755)

	if rw {
		if err := syscall.Mount(source, target, fstype, syscall.MS_BIND|syscall.MS_REC, data); err != nil {
			return err
		}
	} else {
		fmt.Println("Mount", hostfolder, "readonly")
		if err := syscall.Mount(source, target, fstype, syscall.MS_BIND|syscall.MS_REC|syscall.MS_RDONLY, data); err != nil {
			return err
		}

		// Remount to make it read only
		// see https://github.com/containerd/containerd/pull/1373/files
		if err := syscall.Mount("", target, "", syscall.MS_BIND|syscall.MS_REC|syscall.MS_RDONLY|unix.MS_REMOUNT, data); err != nil {
			return err
		}
	}

	return nil
}

func uninstall(c *cli.Context) error {
	store := storeParse(c.String("store"))
	if store != "" {
		return os.RemoveAll(store)
	}
	return nil
}

// This starts the real bundle entrypoint
// TODO: need to make this multi-platform
func execute(c *cli.Context) error {
	store := c.String("store")
	store = filepath.Join(store, "bundle")
	fmt.Println("Store at", store)
	fmt.Println("Pid:", os.Getpid())
	fmt.Println(mountProc(store))
	//fmt.Println(mountDev(store))

	for _, hostMount := range append(c.StringSlice("mounts"),c.StringSlice("add-mounts")...) {
		target := hostMount
		rw := true
		if strings.Contains(hostMount, ":") {
			dest := strings.Split(hostMount, ":")
			if len(dest) == 3 {
				fmt.Println("Mount with options")
				if dest[0] == "ro" {
					rw = false
					fmt.Println("Set", hostMount, "to", dest[0])
				}
				hostMount = dest[1]
				target = dest[2]
			} else if len(dest) == 2 {
				hostMount = dest[0]
				target = dest[1]
			} else {
				return errors.New("Invalid arguments for mount, it can be: fullpath, or source:target")
			}
		}
		fmt.Println("Mounting", hostMount, "to ", store, target)
		if err := mountBind(hostMount, store, target, rw); err != nil {
			return errors.Wrap(err, fmt.Sprintf("Failed mounting %s on rootfs", hostMount))
		}
	}

	fmt.Println(pivotRoot(store))

	// Support ./binary - ....
	args := c.Args()
	if len(c.Args()) > 0 && c.Args()[0] == "-" {
		args = c.Args().Tail()
	}

	fmt.Println("Args", args)
	cmd := exec.Command(c.String("entrypoint"), args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func storeParse(s string) string {
	// support $HOME passed as store
	home, _ := os.UserHomeDir()
	s = strings.ReplaceAll(s, "$HOME", home)
	return s
}

func start(c *cli.Context) error {
	store := storeParse(c.String("store"))

	// Setup store, used by the real process later on
	if store == "" {
		tempdir, err := ioutil.TempDir("", "{{.App.Name}}")
		if err != nil {
			return err

		}
		defer os.RemoveAll(tempdir)
		store = tempdir
	} else {
		if !filepath.IsAbs(store) {
			store, _ = filepath.Abs(store)
		}
	}

	os.MkdirAll(store, os.ModePerm)
	var version string
	if _, err := os.Stat(path.Join(store, "VERSION")); err == nil {
		d, err := ioutil.ReadFile(path.Join(store, "VERSION"))
		if err != nil {
			return err
		}
		version = string(d)
	}

	if version != "{{.App.Version}}" {
		fmt.Println("Extracting bundle data into", store)
		os.RemoveAll(path.Join(store, "bundle"))
		copyBinary(store)
		ioutil.WriteFile(path.Join(store, "VERSION"), []byte("{{.App.Version}}"), os.ModePerm)
	}

	var mounts []string

	for _, m := range append(c.StringSlice("mounts"), c.StringSlice("add-mounts")...) {
		mounts = append(mounts, []string{"--mounts", m}...)
	}

	// TODO: Custom default args injected from bundler
	cmd := exec.Command("/proc/self/exe",
		append(
			append(
				[]string{
					"exec",
					"--store",
					store,
					"--entrypoint",
					c.String("entrypoint"),
				},
				mounts...,
			),
			c.Args()...,
		)...,
	)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNS |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWUSER,
		UidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      os.Getuid(),
				Size:        1,
			},
		},
		GidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      os.Getgid(),
				Size:        1,
			},
		},
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	return cmd.Run()
}

func copyBinary(state string) {
	f, err := assets.Open("assets.tar.xz")
	if err != nil {
		panic(err)
	}
	err = copyFileContents(f, filepath.Join(state, "assets.tar.xz"))
	if err != nil {
		panic(err)
	}
	err = archiver.Unarchive(filepath.Join(state, "assets.tar.xz"), filepath.Join(state, "bundle"))
	if err != nil {
		panic(err)
	}
}

func copyFileContents(in fs.File, dst string) (err error) {
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()

	os.Chmod(dst, 0755)
	return
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}