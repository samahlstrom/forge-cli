package cmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const (
	lightpandaVersion      = "0.3.4"
	lightpandaInstallerURL = "https://pkg.lightpanda.io/install.sh"
	browserProbeURL        = "data:text/html,%3Cmain%20id%3D%22forge-probe%22%3E%3Cbutton%20id%3D%22forge-probe-button%22%20onclick%3D%22document.getElementById%28%27forge-probe-status%27%29.textContent%3D%27ready%27%22%3EReady%3C%2Fbutton%3E%3Cspan%20id%3D%22forge-probe-status%22%3Eready%3C%2Fspan%3E%3C%2Fmain%3E"
)

var (
	minimumAgentBrowserVersion = browserVersion{0, 32, 1}
	errLightpandaNeedsWSL      = errors.New("Lightpanda has no native Windows binary; install WSL2 and rerun forge setup inside WSL")
	versionPattern             = regexp.MustCompile(`([0-9]+)\.([0-9]+)\.([0-9]+)`)
)

type browserVersion struct {
	major int
	minor int
	patch int
}

func (v browserVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v browserVersion) Less(other browserVersion) bool {
	if v.major != other.major {
		return v.major < other.major
	}
	if v.minor != other.minor {
		return v.minor < other.minor
	}
	return v.patch < other.patch
}

type browserPlatform struct {
	OS   string
	Arch string
	WSL  bool
	Musl bool
}

type browserCommand struct {
	Name string
	Args []string
	Env  []string
	Dir  string
}

type browserRuntimeDeps struct {
	platform browserPlatform
	homeDir  string
	pathEnv  string
	baseEnv  []string

	lookPath     func(string) (string, error)
	evalSymlinks func(string) (string, error)
	binaryArch   func(path, goos string) (string, error)
	run          func(context.Context, browserCommand) ([]byte, error)
	makeTempDir  func(dir, pattern string) (string, error)
	removeAll    func(string) error
	writeFile    func(string, []byte, os.FileMode) error
	readFile     func(string) ([]byte, error)
}

type browserSessionInfo struct {
	Data struct {
		Active  bool `json:"active"`
		PID     int  `json:"pid"`
		Runtime struct {
			Engine          string `json:"engine"`
			BrowserLaunched bool   `json:"browserLaunched"`
			BackgroundPID   int    `json:"backgroundPid"`
			PageCount       int    `json:"pageCount"`
		} `json:"runtime"`
	} `json:"data"`
}

type browserProcess struct {
	pid     int
	ppid    int
	command string
}

func newBrowserRuntimeDeps() browserRuntimeDeps {
	home, _ := os.UserHomeDir()
	return browserRuntimeDeps{
		platform:     currentBrowserPlatform(),
		homeDir:      home,
		pathEnv:      os.Getenv("PATH"),
		baseEnv:      os.Environ(),
		lookPath:     exec.LookPath,
		evalSymlinks: filepath.EvalSymlinks,
		binaryArch:   binaryArchitecture,
		run:          runBrowserCommand,
		makeTempDir:  os.MkdirTemp,
		removeAll:    os.RemoveAll,
		writeFile:    os.WriteFile,
		readFile:     os.ReadFile,
	}
}

func currentBrowserPlatform() browserPlatform {
	p := browserPlatform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if p.OS != "linux" {
		return p
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		p.WSL = true
	} else if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		p.WSL = strings.Contains(strings.ToLower(string(data)), "microsoft")
	}
	if out, err := exec.Command("ldd", "--version").CombinedOutput(); err == nil || len(out) > 0 {
		p.Musl = strings.Contains(strings.ToLower(string(out)), "musl")
	}
	return p
}

func runBrowserCommand(ctx context.Context, command browserCommand) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = command.Env
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message != "" {
			return nil, fmt.Errorf("%s: %w", message, err)
		}
		return nil, err
	}
	return out, nil
}

func ensureBrowserRuntime(ctx context.Context, deps browserRuntimeDeps) error {
	if _, err := lightpandaAsset(deps.platform); err != nil {
		return err
	}
	if deps.platform.Musl {
		return errors.New("Lightpanda release binaries require glibc; musl/Alpine is unsupported (use a glibc host or WSL2)")
	}

	tmp, err := deps.makeTempDir("", "forge-browser-setup-*")
	if err != nil {
		return fmt.Errorf("create isolated browser setup directory: %w", err)
	}
	defer deps.removeAll(tmp)

	config := filepath.Join(tmp, "agent-browser.json")
	if err := deps.writeFile(config, []byte("{}\n"), 0o600); err != nil {
		return fmt.Errorf("write isolated agent-browser config: %w", err)
	}
	if err := ensureAgentBrowser(ctx, deps, tmp, config); err != nil {
		return err
	}
	if err := ensureLightpanda(ctx, deps, tmp); err != nil {
		return err
	}
	if err := smokeLightpanda(ctx, deps, tmp, config); err != nil {
		return fmt.Errorf("Lightpanda capability smoke failed: %w", err)
	}
	if err := smokeChrome(ctx, deps, tmp, config, "initial"); err != nil {
		if !isMissingChrome(err) {
			return fmt.Errorf("Chrome fallback smoke failed: %w", err)
		}
		if _, installErr := runBrowser(ctx, deps, tmp, "agent-browser", agentArgs(config, "install")...); installErr != nil {
			return fmt.Errorf("install Chrome for Testing: %w", installErr)
		}
		if retryErr := smokeChrome(ctx, deps, tmp, config, "installed"); retryErr != nil {
			return fmt.Errorf("verify Chrome for Testing after install: %w", retryErr)
		}
	}
	return nil
}

func parseBrowserVersion(output string) (browserVersion, error) {
	match := versionPattern.FindStringSubmatch(output)
	if len(match) != 4 {
		return browserVersion{}, fmt.Errorf("could not parse semantic version from %q", strings.TrimSpace(output))
	}
	parts := [3]int{}
	for i := range parts {
		value, err := strconv.Atoi(match[i+1])
		if err != nil {
			return browserVersion{}, fmt.Errorf("parse version component %q: %w", match[i+1], err)
		}
		parts[i] = value
	}
	return browserVersion{parts[0], parts[1], parts[2]}, nil
}

func lightpandaAsset(platform browserPlatform) (string, error) {
	if platform.OS == "windows" {
		return "", errLightpandaNeedsWSL
	}
	switch platform.OS + "/" + platform.Arch {
	case "darwin/arm64":
		return "lightpanda-aarch64-macos", nil
	case "darwin/amd64":
		return "lightpanda-x86_64-macos", nil
	case "linux/arm64":
		return "lightpanda-aarch64-linux", nil
	case "linux/amd64":
		return "lightpanda-x86_64-linux", nil
	default:
		return "", fmt.Errorf("unsupported Lightpanda host %s/%s; supported hosts are macOS or glibc Linux on arm64 or amd64", platform.OS, platform.Arch)
	}
}

func ensureAgentBrowser(ctx context.Context, deps browserRuntimeDeps, dir, config string) error {
	path, lookupErr := deps.lookPath("agent-browser")
	healthy := false
	if lookupErr == nil {
		version, caps, err := probeAgentBrowser(ctx, deps, dir, config)
		healthy = err == nil && !version.Less(minimumAgentBrowserVersion) && caps
		if err != nil && !errors.Is(err, errBrowserCapabilities) {
			return fmt.Errorf("inspect agent-browser at %s: %w", path, err)
		}
	}
	if healthy {
		return nil
	}
	if err := installAgentBrowser(ctx, deps, dir, path, lookupErr == nil); err != nil {
		return fmt.Errorf("install compatible agent-browser: %w", err)
	}
	path, err := deps.lookPath("agent-browser")
	if err != nil {
		return errors.New("agent-browser install completed but no executable resolves on PATH")
	}
	version, caps, err := probeAgentBrowser(ctx, deps, dir, config)
	if err != nil {
		return fmt.Errorf("verify agent-browser at %s: %w", path, err)
	}
	if version.Less(minimumAgentBrowserVersion) {
		return fmt.Errorf("agent-browser %s remains below required %s after install", version, minimumAgentBrowserVersion)
	}
	if !caps {
		return fmt.Errorf("agent-browser %s lacks required engine/session/screenshot/cleanup capabilities after install", version)
	}
	return nil
}

var errBrowserCapabilities = errors.New("agent-browser capability inspection failed")

func probeAgentBrowser(ctx context.Context, deps browserRuntimeDeps, dir, config string) (browserVersion, bool, error) {
	out, err := runBrowser(ctx, deps, dir, "agent-browser", "--version")
	if err != nil {
		return browserVersion{}, false, err
	}
	version, err := parseBrowserVersion(string(out))
	if err != nil {
		return browserVersion{}, false, err
	}
	if version.Less(minimumAgentBrowserVersion) {
		return version, false, nil
	}
	checks := [][]string{
		agentArgs(config, "--help"),
		agentArgs(config, "session", "--help"),
		agentArgs(config, "screenshot", "--help"),
		agentArgs(config, "close", "--help"),
	}
	var help strings.Builder
	for _, args := range checks {
		out, runErr := runBrowser(ctx, deps, dir, "agent-browser", args...)
		if runErr != nil {
			return version, false, fmt.Errorf("%w: %v", errBrowserCapabilities, runErr)
		}
		help.Write(out)
		help.WriteByte('\n')
	}
	text := help.String()
	for _, required := range []string{"--engine", "lightpanda", "--session", "--json", "session id", "session info", "session list", "[selector] [path]", "close"} {
		if !strings.Contains(text, required) {
			return version, false, nil
		}
	}
	return version, true, nil
}

func installAgentBrowser(ctx context.Context, deps browserRuntimeDeps, dir, existingPath string, exists bool) error {
	_, npmErr := deps.lookPath("npm")
	_, brewErr := deps.lookPath("brew")
	if !exists {
		switch {
		case npmErr == nil:
			_, err := runBrowser(ctx, deps, dir, "npm", "install", "--global", "agent-browser@latest", "--no-audit", "--no-fund")
			return err
		case brewErr == nil:
			_, err := runBrowser(ctx, deps, dir, "brew", "install", "agent-browser")
			return err
		default:
			return errors.New("agent-browser is missing and neither npm nor Homebrew is available")
		}
	}

	if brewErr == nil {
		if out, err := runBrowser(ctx, deps, dir, "brew", "list", "--versions", "agent-browser"); err == nil && strings.TrimSpace(string(out)) != "" {
			_, err = runBrowser(ctx, deps, dir, "brew", "upgrade", "agent-browser")
			return err
		}
	}
	if npmErr == nil {
		if out, err := runBrowser(ctx, deps, dir, "npm", "list", "--global", "--depth=0", "--json", "agent-browser"); err == nil && npmOwnsAgentBrowser(out) {
			_, err = runBrowser(ctx, deps, dir, "npm", "install", "--global", "agent-browser@latest", "--no-audit", "--no-fund")
			return err
		}
		if prefix := npmPrefixForExecutable(deps, existingPath); prefix != "" {
			_, err := runBrowser(ctx, deps, dir, "npm", "--prefix", prefix, "install", "--global", "agent-browser@latest", "--no-audit", "--no-fund")
			return err
		}
	}
	return fmt.Errorf("resolved agent-browser at %s is incompatible but its package manager is unknown; update that installation so the same PATH entry resolves to >= %s", existingPath, minimumAgentBrowserVersion)
}

func npmOwnsAgentBrowser(output []byte) bool {
	var result struct {
		Dependencies map[string]json.RawMessage `json:"dependencies"`
	}
	return json.Unmarshal(output, &result) == nil && result.Dependencies["agent-browser"] != nil
}

func npmPrefixForExecutable(deps browserRuntimeDeps, path string) string {
	resolved, err := deps.evalSymlinks(path)
	if err != nil {
		return ""
	}
	normalized := filepath.ToSlash(resolved)
	marker := "/lib/node_modules/agent-browser/"
	index := strings.Index(normalized, marker)
	if index <= 0 {
		return ""
	}
	return filepath.FromSlash(normalized[:index])
}

func ensureLightpanda(ctx context.Context, deps browserRuntimeDeps, dir string) error {
	path, err := deps.lookPath("lightpanda")
	if err != nil {
		if err := installLightpanda(ctx, deps, dir); err != nil {
			return fmt.Errorf("install Lightpanda %s: %w", lightpandaVersion, err)
		}
		path, err = deps.lookPath("lightpanda")
		if err != nil {
			return fmt.Errorf("Lightpanda installed into %s but does not resolve on PATH; add that directory to PATH and rerun forge setup", filepath.Join(deps.homeDir, ".local", "bin"))
		}
	}
	arch, err := deps.binaryArch(path, deps.platform.OS)
	if err != nil {
		return fmt.Errorf("inspect Lightpanda binary architecture at %s: %w", path, err)
	}
	if arch != deps.platform.Arch {
		return fmt.Errorf("Lightpanda binary architecture %s does not match host %s; refusing to run a wrong-platform artifact at %s", arch, deps.platform.Arch, path)
	}
	out, err := runBrowser(ctx, deps, dir, "lightpanda", "version")
	if err != nil {
		return fmt.Errorf("run lightpanda version: %w", err)
	}
	if !versionPattern.Match(out) {
		return fmt.Errorf("lightpanda version returned unrecognized output %q", strings.TrimSpace(string(out)))
	}
	return nil
}

func installLightpanda(ctx context.Context, deps browserRuntimeDeps, dir string) error {
	for _, tool := range []string{"curl", "bash", "jq"} {
		if _, err := deps.lookPath(tool); err != nil {
			return fmt.Errorf("official verified installer requires %s on PATH", tool)
		}
	}
	if _, err := deps.lookPath("sha256sum"); err != nil {
		if _, fallbackErr := deps.lookPath("shasum"); fallbackErr != nil {
			return errors.New("official verified installer requires sha256sum or shasum")
		}
	}
	installDir := filepath.Join(deps.homeDir, ".local", "bin")
	if !pathContains(deps.pathEnv, installDir) {
		return fmt.Errorf("official Lightpanda installer targets %s, which is not on PATH; add it to PATH and rerun forge setup", installDir)
	}
	script := filepath.Join(dir, "lightpanda-install.sh")
	if _, err := runBrowser(ctx, deps, dir, "curl", "--fail", "--silent", "--show-error", "--location", "--output", script, lightpandaInstallerURL); err != nil {
		return fmt.Errorf("download official installer: %w", err)
	}
	_, err := runBrowserWithEnv(ctx, deps, dir, []string{"LIGHTPANDA_DIR=" + installDir, "LIGHTPANDA_VERSION=" + lightpandaVersion}, "bash", script, lightpandaVersion)
	return err
}

func pathContains(pathEnv, want string) bool {
	want = filepath.Clean(want)
	for _, path := range filepath.SplitList(pathEnv) {
		if filepath.Clean(path) == want {
			return true
		}
	}
	return false
}

func smokeLightpanda(ctx context.Context, deps browserRuntimeDeps, dir, config string) (err error) {
	session, err := createSmokeSession(dir, "dom")
	if err != nil {
		return err
	}
	var roots []int
	defer func() {
		cleanupErr := closeAndVerifySession(ctx, deps, dir, config, "lightpanda", session, roots)
		if err == nil && cleanupErr != nil {
			err = cleanupErr
		}
	}()
	commands := [][]string{
		{"open", browserProbeURL},
		{"snapshot", "--json"},
		{"get", "text", "#forge-probe-status"},
		{"eval", "document.getElementById('forge-probe-status').textContent"},
		{"click", "#forge-probe-button"},
		{"console"},
		{"errors"},
		{"network", "requests"},
	}
	for _, args := range commands {
		if _, err = runBrowser(ctx, deps, dir, "agent-browser", sessionArgs(config, "lightpanda", session, args...)...); err != nil {
			return err
		}
	}
	roots, err = inspectActiveSession(ctx, deps, dir, config, "lightpanda", session)
	return err
}

func smokeChrome(ctx context.Context, deps browserRuntimeDeps, dir, config, attempt string) (err error) {
	session, err := createSmokeSession(dir, "capture")
	if err != nil {
		return err
	}
	var roots []int
	defer func() {
		cleanupErr := closeAndVerifySession(ctx, deps, dir, config, "chrome", session, roots)
		if err == nil && cleanupErr != nil {
			err = cleanupErr
		}
	}()
	for _, args := range [][]string{{"set", "viewport", "800", "600"}, {"open", browserProbeURL}, {"wait", "#forge-probe"}} {
		if _, err = runBrowser(ctx, deps, dir, "agent-browser", sessionArgs(config, "chrome", session, args...)...); err != nil {
			return err
		}
	}
	roots, err = inspectActiveSession(ctx, deps, dir, config, "chrome", session)
	if err != nil {
		return err
	}
	shot := filepath.Join(dir, "chrome-selector-"+attempt+".png")
	if _, err = runBrowser(ctx, deps, dir, "agent-browser", sessionArgs(config, "chrome", session, "screenshot", "#forge-probe", shot)...); err != nil {
		return err
	}
	data, err := deps.readFile(shot)
	if err != nil {
		return fmt.Errorf("read Chrome selector screenshot: %w", err)
	}
	imageConfig, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode Chrome selector screenshot: %w", err)
	}
	if imageConfig.Width <= 0 || imageConfig.Height <= 0 || imageConfig.Width >= 800 || imageConfig.Height >= 600 {
		return fmt.Errorf("Chrome screenshot is not a selector crop: %dx%d", imageConfig.Width, imageConfig.Height)
	}
	return nil
}

func createSmokeSession(dir, purpose string) (string, error) {
	if purpose != "dom" && purpose != "capture" {
		return "", fmt.Errorf("invalid browser smoke session purpose %q", purpose)
	}
	worktree := sha256.Sum256([]byte(filepath.Clean(dir)))
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate browser smoke session nonce: %w", err)
	}
	return "forge-fc-cvt-" + hex.EncodeToString(worktree[:4]) + "-" + purpose + "-" + hex.EncodeToString(nonce), nil
}

func inspectActiveSession(ctx context.Context, deps browserRuntimeDeps, dir, config, engine, session string) ([]int, error) {
	out, err := runBrowser(ctx, deps, dir, "agent-browser", sessionArgs(config, engine, session, "--json", "session", "info")...)
	if err != nil {
		return nil, fmt.Errorf("inspect session %s: %w", session, err)
	}
	info, err := parseSessionInfo(out)
	if err != nil {
		return nil, err
	}
	if !info.Data.Active || !info.Data.Runtime.BrowserLaunched || info.Data.Runtime.Engine != engine {
		return nil, fmt.Errorf("session %s is not an active %s browser", session, engine)
	}
	processes, err := listBrowserProcesses(ctx, deps, dir)
	if err != nil {
		return nil, err
	}
	roots := []int{info.Data.PID, info.Data.Runtime.BackgroundPID}
	pids := attributableProcessIDs(processes, roots, engine)
	if len(pids) == 0 {
		return nil, fmt.Errorf("could not attribute a live %s process to session %s", engine, session)
	}
	return pids, nil
}

func closeAndVerifySession(ctx context.Context, deps browserRuntimeDeps, dir, config, engine, session string, attributed []int) error {
	if session == "" {
		return nil
	}
	if _, err := runBrowser(ctx, deps, dir, "agent-browser", sessionArgs(config, engine, session, "close")...); err != nil {
		return fmt.Errorf("close exact session %s: %w", session, err)
	}
	list, err := runBrowser(ctx, deps, dir, "agent-browser", agentArgs(config, "--json", "session", "list")...)
	if err != nil {
		return fmt.Errorf("list sessions after closing %s: %w", session, err)
	}
	if sessionListed(list, session) {
		return fmt.Errorf("session %s remains in session list after exact close", session)
	}
	processes, err := listBrowserProcesses(ctx, deps, dir)
	if err != nil {
		return err
	}
	for _, process := range processes {
		for _, pid := range attributed {
			if process.pid == pid {
				return fmt.Errorf("session %s left attributable %s process %d running", session, engine, pid)
			}
		}
	}
	return nil
}

func parseSessionInfo(output []byte) (browserSessionInfo, error) {
	var info browserSessionInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return info, fmt.Errorf("parse agent-browser session info JSON: %w", err)
	}
	return info, nil
}

func sessionListed(output []byte, session string) bool {
	var result struct {
		Data struct {
			Sessions []struct {
				Name string `json:"name"`
			} `json:"sessions"`
		} `json:"data"`
	}
	if json.Unmarshal(output, &result) != nil {
		return true
	}
	for _, item := range result.Data.Sessions {
		if item.Name == session {
			return true
		}
	}
	return false
}

func listBrowserProcesses(ctx context.Context, deps browserRuntimeDeps, dir string) ([]browserProcess, error) {
	out, err := runBrowser(ctx, deps, dir, "ps", "-axo", "pid=,ppid=,command=")
	if err != nil {
		return nil, fmt.Errorf("inspect browser process tree: %w", err)
	}
	var processes []browserProcess
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		ppid, ppidErr := strconv.Atoi(fields[1])
		if pidErr == nil && ppidErr == nil {
			processes = append(processes, browserProcess{pid: pid, ppid: ppid, command: strings.Join(fields[2:], " ")})
		}
	}
	return processes, nil
}

func attributableProcessIDs(processes []browserProcess, roots []int, engine string) []int {
	descendant := map[int]bool{}
	for _, root := range roots {
		if root > 0 {
			descendant[root] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, process := range processes {
			if descendant[process.ppid] && !descendant[process.pid] {
				descendant[process.pid] = true
				changed = true
			}
		}
	}
	var ids []int
	for _, process := range processes {
		command := strings.ToLower(process.command)
		isEngine := engine == "lightpanda" && strings.Contains(command, "lightpanda")
		isEngine = isEngine || engine == "chrome" && (strings.Contains(command, "chrome") || strings.Contains(command, "chromium"))
		if descendant[process.pid] && isEngine {
			ids = append(ids, process.pid)
		}
	}
	return ids
}

func binaryArchitecture(path, goos string) (string, error) {
	switch goos {
	case "darwin":
		file, err := macho.Open(path)
		if err != nil {
			return "", err
		}
		defer file.Close()
		switch file.Cpu {
		case macho.CpuArm64:
			return "arm64", nil
		case macho.CpuAmd64:
			return "amd64", nil
		}
	case "linux":
		file, err := elf.Open(path)
		if err != nil {
			return "", err
		}
		defer file.Close()
		switch file.Machine {
		case elf.EM_AARCH64:
			return "arm64", nil
		case elf.EM_X86_64:
			return "amd64", nil
		}
	}
	return "", fmt.Errorf("unsupported executable architecture in %s", path)
}

func isMissingChrome(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "chrome for testing") && (strings.Contains(message, "not found") || strings.Contains(message, "install"))
}

func agentArgs(config string, args ...string) []string {
	return append([]string{"--config", config}, args...)
}

func sessionArgs(config, engine, session string, args ...string) []string {
	base := []string{"--config", config, "--engine", engine, "--session", session}
	return append(base, args...)
}

func runBrowser(ctx context.Context, deps browserRuntimeDeps, dir, name string, args ...string) ([]byte, error) {
	return runBrowserWithEnv(ctx, deps, dir, nil, name, args...)
}

func runBrowserWithEnv(ctx context.Context, deps browserRuntimeDeps, dir string, extra []string, name string, args ...string) ([]byte, error) {
	return deps.run(ctx, browserCommand{Name: name, Args: args, Dir: dir, Env: browserCommandEnv(deps, shortBrowserSocketDir(dir), extra)})
}

func shortBrowserSocketDir(dir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(dir)))
	// Supported browser hosts are Darwin/Linux. Keep the socket root short enough
	// for the canonical named session on macOS's 103-byte Unix-socket limit.
	return filepath.Join(string(os.PathSeparator), "tmp", "forge-ab-"+hex.EncodeToString(sum[:4]))
}

func browserCommandEnv(deps browserRuntimeDeps, socketDir string, extra []string) []string {
	base := deps.baseEnv
	if len(base) == 0 {
		base = []string{"HOME=" + deps.homeDir, "PATH=" + deps.pathEnv}
	}
	env := make([]string, 0, len(base)+10+len(extra))
	for _, item := range base {
		name := item
		if index := strings.IndexByte(item, '='); index >= 0 {
			name = item[:index]
		}
		if strings.HasPrefix(name, "AGENT_BROWSER_") || name == "CI" || strings.HasPrefix(name, "NPM_CONFIG_") || name == "HOMEBREW_NO_AUTO_UPDATE" {
			continue
		}
		env = append(env, item)
	}
	env = append(env,
		"CI=1",
		"AGENT_BROWSER_HEADED=false",
		"AGENT_BROWSER_AUTO_CONNECT=false",
		"AGENT_BROWSER_SOCKET_DIR="+socketDir,
		"HOMEBREW_NO_AUTO_UPDATE=1",
		"NPM_CONFIG_AUDIT=false",
		"NPM_CONFIG_FUND=false",
		"NPM_CONFIG_UPDATE_NOTIFIER=false",
	)
	return append(env, extra...)
}
