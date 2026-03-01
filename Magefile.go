//go:build mage

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type taskHelp struct {
	Name    string
	Summary string
	EnvVars []string
	Example string
}

var taskHelpItems = []taskHelp{
	{Name: "build", Summary: "Build backend", Example: "mage build"},
	{Name: "buildEmbed", Summary: "Build frontend embed assets and backend", Example: "mage buildEmbed"},
	{Name: "run", Summary: "Run backend", Example: "mage run"},
	{Name: "dev", Summary: "Run Go API + Vite dev server together", EnvVars: []string{"LIVED_WEB_DEV_PROXY_URL (auto-set to http://localhost:5173)"}, Example: "mage dev"},
	{Name: "test", Summary: "Run Go tests", Example: "mage test"},
	{Name: "dbSetup", Summary: "Create DB if needed", EnvVars: []string{"LIVED_DATABASE_URL or LIVED_POSTGRES_*"}, Example: "mage dbSetup"},
	{Name: "dbRecreate", Summary: "Drop and recreate DB", EnvVars: []string{"LIVED_DATABASE_URL or LIVED_POSTGRES_*"}, Example: "mage dbRecreate"},
	{Name: "dbMigrate", Summary: "Run DB migrations", EnvVars: []string{"LIVED_DATABASE_URL"}, Example: "mage dbMigrate"},
	{Name: "dbVerify", Summary: "Verify realm-scoped migration/index health", EnvVars: []string{"LIVED_DATABASE_URL"}, Example: "mage dbVerify"},
	{Name: "dbSetAdmin", Summary: "Grant admin role to account", EnvVars: []string{"NAME or ID", "LIVED_ADMIN_USERNAME (fallback)", "LIVED_ADMIN_ACCOUNT_ID (fallback)", "LIVED_DATABASE_URL"}, Example: "NAME=player1 mage dbSetAdmin"},
	{Name: "frontendInstall", Summary: "Install frontend deps", Example: "mage frontendInstall"},
	{Name: "frontendDev", Summary: "Run Vite dev server", Example: "mage frontendDev"},
	{Name: "frontendBuild", Summary: "Build frontend to web/dist", Example: "mage frontendBuild"},
	{Name: "frontendBuildEmbed", Summary: "Build frontend to src/server/webdist", Example: "mage frontendBuildEmbed"},
}

func Help() error {
	printTargets()

	if !isInteractiveTerminal() {
		return nil
	}

	return promptForTargetSelection()
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
	if err := run("go", "build", "./..."); err != nil {
		return fmt.Errorf("backend preflight build failed: %w", err)
	}

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

func DbMigrate() error {
	return run("go", "run", ".", "db", "migrate")
}

func DbVerify() error {
	return run("go", "run", ".", "db", "verify")
}

func DbSetAdmin() error {
	username := firstEnvValue("NAME", "LIVED_ADMIN_USERNAME")
	accountID := firstEnvValue("ID", "LIVED_ADMIN_ACCOUNT_ID")

	if username == "" && accountID == "" {
		return fmt.Errorf("set NAME or ID before running mage dbSetAdmin")
	}
	if username != "" && accountID != "" {
		return fmt.Errorf("set only one of NAME or ID")
	}

	if username != "" {
		return run("go", "run", ".", "db", "set-admin", "--username", username)
	}

	return run("go", "run", ".", "db", "set-admin", "--account-id", accountID)
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
	fmt.Println("Lived · Task Runner (Mage)")
	fmt.Println(strings.Repeat("-", 64))

	for index, item := range taskHelpItems {
		fmt.Printf("%2d) %-18s %s\n", index+1, item.Name, item.Summary)
		if len(item.EnvVars) > 0 {
			fmt.Printf("    env: %s\n", strings.Join(item.EnvVars, "; "))
		}
		fmt.Printf("    use: %s\n", item.Example)
	}

	fmt.Println(strings.Repeat("-", 64))
	fmt.Println("Tip: In an interactive terminal, enter a number/name to run it now.")
	fmt.Println("Tip: Press Enter to exit help without running anything.")
}

func isInteractiveTerminal() bool {
	stdinInfo, stdinErr := os.Stdin.Stat()
	stdoutInfo, stdoutErr := os.Stdout.Stat()
	if stdinErr != nil || stdoutErr != nil {
		return false
	}

	return (stdinInfo.Mode()&os.ModeCharDevice) != 0 && (stdoutInfo.Mode()&os.ModeCharDevice) != 0
}

func promptForTargetSelection() error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\nSelect target to run now (number/name, Enter to exit): ")

	choice, err := reader.ReadString('\n')
	if err != nil {
		return nil
	}

	selected := strings.TrimSpace(choice)
	if selected == "" {
		return nil
	}

	targetName := resolveTargetSelection(selected)
	if targetName == "" {
		fmt.Printf("Unknown target selection %q\n", selected)
		return nil
	}

	fmt.Printf("\nRunning target: %s\n", targetName)
	return runTargetByName(targetName)
}

func resolveTargetSelection(selected string) string {
	if index, err := strconv.Atoi(selected); err == nil {
		if index >= 1 && index <= len(taskHelpItems) {
			return taskHelpItems[index-1].Name
		}
		return ""
	}

	normalized := strings.ToLower(strings.TrimSpace(selected))
	for _, item := range taskHelpItems {
		if strings.ToLower(item.Name) == normalized {
			return item.Name
		}
	}

	return ""
}

func runTargetByName(targetName string) error {
	switch targetName {
	case "build":
		return Build()
	case "buildEmbed":
		return BuildEmbed()
	case "run":
		return Run()
	case "dev":
		return Dev()
	case "test":
		return Test()
	case "dbSetup":
		return DbSetup()
	case "dbRecreate":
		return DbRecreate()
	case "dbMigrate":
		return DbMigrate()
	case "dbVerify":
		return DbVerify()
	case "dbSetAdmin":
		return DbSetAdmin()
	case "frontendInstall":
		return FrontendInstall()
	case "frontendDev":
		return FrontendDev()
	case "frontendBuild":
		return FrontendBuild()
	case "frontendBuildEmbed":
		return FrontendBuildEmbed()
	default:
		return fmt.Errorf("unsupported target: %s", targetName)
	}
}

func firstEnvValue(keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}

	return ""
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
