//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
)

func Help() {
	printTargets()
}

func Build() error {
	return run("go", "build", "./...")
}

func BuildEmbed() error {
	if err := FrontendBuildEmbed(); err != nil {
		return err
	}
	return run("go", "build", "-tags", "embed_frontend", "./...")
}

func Run() error {
	return run("go", "run", ".", "run")
}

func Dev() error {
	frontendCmd := exec.Command(npmBin(), "run", "dev")
	frontendCmd.Dir = "web"
	frontendCmd.Stdout = os.Stdout
	frontendCmd.Stderr = os.Stderr
	frontendCmd.Stdin = os.Stdin

	if err := frontendCmd.Start(); err != nil {
		return err
	}

	backendCmd := exec.Command("go", "run", ".", "run")
	backendCmd.Dir = "."
	backendCmd.Stdout = os.Stdout
	backendCmd.Stderr = os.Stderr
	backendCmd.Stdin = os.Stdin
	backendCmd.Env = withFrontendDevProxy(os.Environ(), "http://localhost:5173")

	if err := backendCmd.Start(); err != nil {
		_ = frontendCmd.Process.Kill()
		_ = frontendCmd.Wait()
		return err
	}

	errCh := make(chan error, 2)

	go func() {
		errCh <- frontendCmd.Wait()
	}()

	go func() {
		errCh <- backendCmd.Wait()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case sig := <-sigCh:
		fmt.Printf("received %s, shutting down dev processes\n", sig.String())
		_ = backendCmd.Process.Signal(os.Interrupt)
		_ = frontendCmd.Process.Signal(os.Interrupt)
		_ = backendCmd.Process.Kill()
		_ = frontendCmd.Process.Kill()
		<-errCh
		<-errCh
		return nil
	case err := <-errCh:
		_ = backendCmd.Process.Kill()
		_ = frontendCmd.Process.Kill()
		<-errCh
		if err == nil {
			return nil
		}
		return err
	}
}

func Test() error {
	return run("go", "test", "./...")
}

func DbSetup() error {
	return run("go", "run", ".", "db", "setup")
}

func DbRecreate() error {
	return run("go", "run", ".", "db", "setup", "--recreate")
}

func FrontendInstall() error {
	return runInDir("web", npmBin(), "install")
}

func FrontendDev() error {
	return runInDir("web", npmBin(), "run", "dev")
}

func FrontendBuild() error {
	return runInDir("web", npmBin(), "run", "build")
}

func FrontendBuildEmbed() error {
	return runInDir("web", npmBin(), "run", "build:embed")
}

func printTargets() {
	fmt.Println("Lived task targets:")
	fmt.Println("  mage build                Build backend")
	fmt.Println("  mage buildEmbed           Build frontend embed assets and backend with embed tag")
	fmt.Println("  mage run                  Run backend")
	fmt.Println("  mage dev                  Run Go API + Vite dev server together")
	fmt.Println("  mage test                 Run Go tests")
	fmt.Println("  mage dbSetup              Create DB if needed")
	fmt.Println("  mage dbRecreate           Drop and recreate DB")
	fmt.Println("  mage frontendInstall      Install frontend deps")
	fmt.Println("  mage frontendDev          Run Vite dev server")
	fmt.Println("  mage frontendBuild        Build frontend to web/dist")
	fmt.Println("  mage frontendBuildEmbed   Build frontend to src/server/webdist")
}

func npmBin() string {
	if runtime.GOOS == "windows" {
		return "npm.cmd"
	}
	return "npm"
}

func run(name string, args ...string) error {
	return runInDir(".", name, args...)
}

func withFrontendDevProxy(env []string, value string) []string {
	for i, entry := range env {
		if strings.HasPrefix(entry, "LIVED_WEB_DEV_PROXY_URL=") {
			env[i] = "LIVED_WEB_DEV_PROXY_URL=" + value
			return env
		}
	}

	return append(env, "LIVED_WEB_DEV_PROXY_URL="+value)
}

func runInDir(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
