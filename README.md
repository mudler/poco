<h1 align="center">
  <br>
	<img src="https://user-images.githubusercontent.com/2420543/145685818-e4b25b4c-dd7c-46af-b9ca-66d27d4f17e0.png" width=128
         alt="logo"><br>
    poCo 
<br>
</h1>

<h3 align="center">    Containers -> Binaries<br>Create statically linked, portable binaries from container images </h3>
<p align="center">
  <a href="https://opensource.org/licenses/">
    <img src="https://img.shields.io/badge/licence-GPL3-brightgreen"
         alt="license">
  </a>
  <a href="https://github.com/mudler/poco/issues"><img src="https://img.shields.io/github/issues/mudler/poco"></a>
  <img src="https://img.shields.io/badge/made%20with-Go-blue">
  <img src="https://goreportcard.com/badge/github.com/mudler/poco" alt="go report card" />
</p>

<p align="center">
	 <br>
    A simple, static golang bundler! 

</p>


poCo (_portable_-_Containers_) packs and converts container images into single, portable, statically linked binaries leveraging golang native `embed`.

## :question: How it works

`poCo` is extremely simple in the design.

`poCo` generates and builds golang code which embeds the container content compressed as an asset. It does bundle the assets by using the native golang `embed` primitives. The resulting binary on the first run will extract the content over the application `store` and will execute the entrypoint in a pivotroot environment, without requiring special permissions.

## :computer: Install

Download poCO from the [releases](https://github.com/mudler/poco/releases) and install it in your `PATH`. poCO releases are statically built, so no dependencies (besides `golang` to create `bundles`, are required)

## :running: Run 

poCO is a no-frills binary bundler, we will see an example of how to bundle a container image into a binary.

Requires:

- `poco` installed
- `sudo`
- golang `>1.17` installed in the system where are you building

poCo bundles container images available remotely or locally (by specifying `--local` to the `bundle` subcommand).

For instance to pack the `alpine` image into a `sample` binary is as simple as:

```bash
CGO_ENABLED=0 ./poco bundle --image alpine --output sample
```

`CGO_ENABLED=0` will instruct the golang compiler behind the scenes to create a statically linked executable.

We can specify optionally a default entrypoint for the resulting binary with `--entrypoint`, which is by default set to `/bin/sh`.

You can run the `--help` subcommand on `sample` to inspect its output, and you will see there are available few options:

```
❯ ./sample --help     
NAME:
   sample - sample

USAGE:
    [global options] command [command options] [arguments...]

VERSION:
   0.1

DESCRIPTION:
   sample

AUTHOR:
   sample

COMMANDS:
   exec       
   uninstall  
   help, h    Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --store value       Default application store. Empty for TMPDIR
   --entrypoint value  Default application entrypoint (default: "/bin/sh")
   --add-mounts value  Additional mountpoints
   --mounts value      Default app mountpoints (default: "/sys", "/tmp", "/run")
   --help, -h          show help
   --version, -v       print the version
```

The binary has some defaults that can be override during build time with `bundle`, to run the application (in our case, `sh` from alpine), just run:

```
./sample # spawns a new /bin/sh shell
```

You can also pass all the args to the entrypoint of the binary (`/bin/sh`), by specifying `-`:

```
./sample - -c "echo foo"
```

See the `example/` folder for a more complete example.

Supports: `CGO_ENABLED`, `GOOS`, `GOARCH`, etc.

It can target all architectures supported by golang. 

### `bundle`

`bundle` creates a binary from a container image, it takes several options listed here:

| Flag              | Description                                                                                                                                                                                            |
|-------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| --entrypoint      | Default binary entrypoint. This is the first binary from the container image which will be executed. It defaults to `/bin/sh`                                                                          |
| --output          | Default binary output location                                                                                                                                                                         |
| --app-description | This is the description of the app that will be displayed in the resulting binary `--help`                                                                                                             |
| --app-author      | This is the author of the app that will be displayed in the resulting binary  `--help`                                                                                                                 |
| --app-name        | This is the name of the app that will be displayed in the resulting binary  `--help`                                                                                                                   |
| --app-version     | This is the version of the app that will be displayed in the resulting binary  `--help`. The version will be used between different binary bundles to handle upgrades.                                 |
| --local           | Tells poco to get the container image from the local Docker daemon instead of fetching it remotely. By default poco doesn't require a Docker daemon running locally                                    |
| --app-mounts      | A list of default mount binding for the app. The application runs in a chroot-alike environment, without access to the files of the system unless explictly mounted. Multiple mounts can be specified. |
| --app-store       | A default store for your app. This is where the bundle gets extracted before being executed, and where the real app data lives afterward on subsequent calls.                                          |
| --image           | The container image to bundle.                                                                                                                                                                         |
| --command-prefix  | Command prefix for auto-generated code. Usually you don't need to change that unless you are running the builds as root                                                                                |

#### Mounts

A `poCo` bundle runs in a sandboxed environment. To expose directories or files, the resulting binary in runtime tales the `--mounts` or `--add-mounts` option (also multiple times) to specify a list of directories or files to expose from the host environment.

While creating the binary, it is also possible to specify a default set, so the binary runs with the directory shared from the host already, this is possible by passing `--app-mounts`.

For instance, consider:

```bash
CGO_ENABLED=0 ./poco bundle --image alpine --output sample --app-mounts /tmp --app-mounts 'ro:/home/.bar:/home/.bar'
```

will create a `sample` binary with `alpine` which `/tmp` will be mapped `rw` and `/home/.bar` `ro` from the host.

#### Default store

Every application has a default store. By default, each application will unpack its content to a temporary directory. To change this behavior and persist data in the system which is running the app, specify a default location with `--app-store`.

For instance, the following will use the `~/.poco/alpine` folder to unpack the bundle content on the first run:
```bash
CGO_ENABLED=0 ./poco bundle --image alpine --output sample --app-store '$HOME/.poco/alpine'
```

Every application can indeed be uninstalled, which just deletes the default `app-store`:

```bash
./sample uninstall
```

#### Metadata

Every generated bundle will have a default --help which is being displayed. It is possible to set metadata such as `description`, `name`, `author`, `copyright` that will be automatically available in the resulting binary `--help`. 

The version is more relevant if a default `--app-store` is being specified. The `app-version` is used during the first run to determine if the installed bundle should be replaced or not.

### `render`

`render` allows to render the generated golang code into a specified directory. This is might be helpful if you want to change the generated binary before build.

```
$ mkdir alpine
$ ./poco render --image alpine alpine
$ ls alpine/
go.mod main.go
```

### `pack`

`pack` is an internal utility to pack directories as container images that can be `docker` loaded afterwards:

```
$ mkdir foo
$ touch foo/bar
$ poco pack myimage:tag foo --destination output.tar
$ docker load -i output.tar
$ docker push myimage:tag
```

### `pack-assets`

`pack-assets` is an internal utility to pack assets for the bundle.

```
$ mkdir foo
$ touch foo/bar
$ poco pack-assets -C foo .
$ ls
assets.tar.xz

```

### `unpack`

`unpack` is an internal utility to unpack a container image into a directory

```
$ mkdir alpine
$ poco unpack alpine alpine
$ ls alpine
bin etc usr ...
```

## :warning: Notes

During build sudo is required in order to preserve container permissions. 

## :mag: Examples

See the `examples` folder or [linuxbundles](https://github.com/mudler/linuxbundles) for a collection of popular distributions packaged or either [caramel](https://github.com/mocaccinoOS/caramel) for a more complete example involving popular apps.

The pipeline builds the `firefox` image which can be downloaded and to run locally as a standard binary:

```
./firefox
```
To uninstall:

```
./firefox uninstall
```

## :notebook: TODO

- [ ] Multi-platform support (Windows, MacOS at least..)

## :question: Why?

¯\_(ツ)_/¯

Someone told me this wasn't possible, so here we are.

I know there is flatpak and also tons of AppImages out there and this is just yet another bundler for most of you, so to make it clear: the scope of this project is not even comparable to them. 

While I was sketching this up I realized I wanted something VERY simple that doesn't gets in the way and opinionated enough that can leverage already existing container image - without the need of additional docs or specific procedures for users. 
This bundler might fit just simple and specific purposes - and most likely - people like me that doesn't have high end goals and rely on golang daily. 

So focus of this project is - to prove a point of course - and on semplicity and portability rather than, for example, security.

And besides, let's be frank. Building bundles with go+docker is really easy to go with as a stack.

## :notebook: Credits

Docker authors for the pivotroot code part, was very helpful read to get that right.

# 🐜 Contribution

You can improve this project by contributing in following ways:

- report bugs
- fix issues
- request features
- asking questions (just open an issue)

and any other way if not mentioned here.

## :notebook: Author

poCo is released under GPL-3, Copyright Ettore Di Giacinto <mudler@mocaccino.org>
