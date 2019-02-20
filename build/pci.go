// +build none

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sero-cash/mine-pool/build"
)

var (

	// Files that end up in the gero-alltools*.zip archive.
	allToolsArchiveFiles = []string{
		executablePath("mine-pool"),
	}
)

var GOBIN, _ = filepath.Abs(filepath.Join("build", "bin"))

func executablePath(name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(GOBIN, name)
}

func main() {
	log.SetFlags(log.Lshortfile)

	if _, err := os.Stat(filepath.Join("build", "pci.go")); os.IsNotExist(err) {
		log.Fatal("this script must be run from the root of the repository")
	}
	if len(os.Args) < 2 {
		log.Fatal("need subcommand as first argument")
	}
	switch os.Args[1] {
	case "lint":
		doLint(os.Args[2:])
	case "xgo":
		doXgo(os.Args[2:])
	default:
		log.Fatal("unknown command ", os.Args[1])
	}
}

func buildFlags(env build.Environment) (flags []string) {
	var ld []string
	if env.Commit != "" {
		ld = append(ld, "-X", "main.gitCommit="+env.Commit)
	}
	if runtime.GOOS == "darwin" {
		ld = append(ld, "-s")
	}

	if len(ld) > 0 {
		flags = append(flags, "-ldflags", strings.Join(ld, " "))
	}
	return flags
}

func goTool(subcmd string, args ...string) *exec.Cmd {
	return goToolArch(runtime.GOARCH, os.Getenv("CC"), subcmd, args...)
}

func goToolArch(arch string, cc string, subcmd string, args ...string) *exec.Cmd {
	cmd := build.GoTool(subcmd, args...)
	cmd.Env = []string{"GOPATH=" + build.GOPATH()}
	if arch == "" || arch == runtime.GOARCH {
		cmd.Env = append(cmd.Env, "GOBIN="+GOBIN)
	} else {
		cmd.Env = append(cmd.Env, "CGO_ENABLED=1")
		cmd.Env = append(cmd.Env, "GOARCH="+arch)
	}
	if cc != "" {
		cmd.Env = append(cmd.Env, "CC="+cc)
	}
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GOPATH=") || strings.HasPrefix(e, "GOBIN=") {
			continue
		}
		cmd.Env = append(cmd.Env, e)
	}
	return cmd
}

// runs gometalinter on requested packages
func doLint(cmdline []string) {
	flag.CommandLine.Parse(cmdline)

	packages := []string{"./..."}
	if len(flag.CommandLine.Args()) > 0 {
		packages = flag.CommandLine.Args()
	}
	// Get metalinter and install all supported linters
	build.MustRun(goTool("get", "gopkg.in/alecthomas/gometalinter.v2"))
	build.MustRunCommand(filepath.Join(GOBIN, "gometalinter.v2"), "--install")

	// Run fast linters batched together
	configs := []string{
		"--vendor",
		"--tests",
		"--deadline=2m",
		"--disable-all",
		"--enable=goimports",
		"--enable=varcheck",
		"--enable=vet",
		"--enable=gofmt",
		"--enable=misspell",
		"--enable=goconst",
		"--min-occurrences=6", // for goconst
	}
	build.MustRunCommand(filepath.Join(GOBIN, "gometalinter.v2"), append(configs, packages...)...)

	// Run slow linters one by one
	for _, linter := range []string{"unconvert", "gosimple"} {
		configs = []string{"--vendor", "--tests", "--deadline=10m", "--disable-all", "--enable=" + linter}
		build.MustRunCommand(filepath.Join(GOBIN, "gometalinter.v2"), append(configs, packages...)...)
	}
}

// Cross compilation

func doXgo(cmdline []string) {
	var (
		alltools = flag.Bool("alltools", false, `Flag whether we're building all known tools, or only on in particular`)
	)
	flag.CommandLine.Parse(cmdline)
	env := build.Env()

	// Make sure xgo is available for cross compilation
	gogetxgo := goTool("get", "github.com/karalabe/xgo")
	build.MustRun(gogetxgo)

	// If all tools building is requested, build everything the builder wants
	args := append(buildFlags(env), flag.Args()...)
	fmt.Println(">>>>>>>>>>%t", args)
	if *alltools {
		args = append(args, []string{"--dest", GOBIN}...)
		for _, res := range allToolsArchiveFiles {
			fmt.Println(">>>>>>>" + res)
			if strings.HasPrefix(res, GOBIN) {
				// Binary tool found, cross build it explicitly
				args = append(args, "./"+filepath.Join("cmd", filepath.Base(res)))
				xgo := xgoTool(args)
				build.MustRun(xgo)
				args = args[:len(args)-1]
			}
		}
		return
	}
	// Otherwise xxecute the explicit cross compilation
	path := args[len(args)-1]
	args = append(args[:len(args)-1], []string{"--dest", GOBIN, path}...)
	fmt.Println(">>>>>>>>>>%t", args)
	xgo := xgoTool(args)
	build.MustRun(xgo)
}

func xgoTool(args []string) *exec.Cmd {
	fmt.Println(">>>>><<<<<>>>>>>>%t", GOBIN)
	cmd := exec.Command(filepath.Join(GOBIN, "xgo"), args...)
	fmt.Println(">>>>>>>>>>%t", cmd)
	cmd.Env = []string{
		"GOPATH=" + build.GOPATH(),
		"GOBIN=" + GOBIN,
	}
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GOPATH=") || strings.HasPrefix(e, "GOBIN=") {
			continue
		}
		cmd.Env = append(cmd.Env, e)
	}
	return cmd
}
