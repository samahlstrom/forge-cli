package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type fakeBrowserSession struct {
	active bool
	engine string
	pid    int
}

type fakeBrowserRuntime struct {
	platform browserPlatform
	home     string
	paths    map[string]string
	resolved map[string]string
	files    map[string][]byte
	commands []browserCommand
	sessions map[string]*fakeBrowserSession
	removed  []string
	nextID   int

	agentVersion              string
	agentCaps                 bool
	agentOwner                string
	lightVersion              string
	lightArch                 string
	lightpandaInstallPathOnly bool
	chromeReady               bool
	fail                      map[string]error
}

func newFakeBrowserRuntime(platform browserPlatform) *fakeBrowserRuntime {
	return &fakeBrowserRuntime{
		platform: platform,
		home:     "/home/tester",
		paths: map[string]string{
			"agent-browser": "/tools/agent-browser",
			"lightpanda":    "/tools/lightpanda",
			"npm":           "/tools/npm",
			"brew":          "/tools/brew",
			"curl":          "/tools/curl",
			"bash":          "/tools/bash",
			"jq":            "/tools/jq",
			"shasum":        "/tools/shasum",
			"ps":            "/bin/ps",
		},
		resolved:     map[string]string{"/tools/agent-browser": "/tools/agent-browser"},
		files:        map[string][]byte{},
		sessions:     map[string]*fakeBrowserSession{},
		agentVersion: "0.32.1",
		agentCaps:    true,
		agentOwner:   "npm",
		lightVersion: "0.3.4",
		lightArch:    platform.Arch,
		chromeReady:  true,
		fail:         map[string]error{},
	}
}

func (f *fakeBrowserRuntime) deps() browserRuntimeDeps {
	return browserRuntimeDeps{
		platform: f.platform,
		homeDir:  f.home,
		pathEnv:  strings.Join([]string{"/tools", filepath.Join(f.home, ".local", "bin")}, string(os.PathListSeparator)),
		lookPath: func(name string) (string, error) {
			if path := f.paths[name]; path != "" {
				return path, nil
			}
			return "", fmt.Errorf("%s not found", name)
		},
		evalSymlinks: func(path string) (string, error) {
			if resolved := f.resolved[path]; resolved != "" {
				return resolved, nil
			}
			return path, nil
		},
		binaryArch: func(path, goos string) (string, error) {
			expected := f.paths["lightpanda"]
			if f.lightpandaInstallPathOnly {
				expected = filepath.Join(f.home, ".local", "bin", "lightpanda")
			}
			if path != expected {
				return "", fmt.Errorf("unexpected binary: %s", path)
			}
			return f.lightArch, nil
		},
		run: f.run,
		makeTempDir: func(_, pattern string) (string, error) {
			f.nextID++
			return filepath.Join("/tmp", strings.TrimSuffix(pattern, "*")+strconv.Itoa(f.nextID)), nil
		},
		removeAll: func(path string) error {
			f.removed = append(f.removed, path)
			for name := range f.files {
				if strings.HasPrefix(name, path+string(os.PathSeparator)) {
					delete(f.files, name)
				}
			}
			return nil
		},
		writeFile: func(path string, data []byte, _ os.FileMode) error {
			f.files[path] = append([]byte(nil), data...)
			return nil
		},
		readFile: func(path string) ([]byte, error) {
			data, ok := f.files[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return append([]byte(nil), data...), nil
		},
	}
}

func (f *fakeBrowserRuntime) run(_ context.Context, command browserCommand) ([]byte, error) {
	f.commands = append(f.commands, command)
	key := fakeCommandKey(command)
	if err := f.fail[key]; err != nil {
		return nil, err
	}

	switch command.Name {
	case "npm":
		if hasArg(command.Args, "list") {
			if f.agentOwner == "npm" {
				return []byte(`{"dependencies":{"agent-browser":{"version":"` + f.agentVersion + `"}}}`), nil
			}
			return []byte(`{"dependencies":{}}`), nil
		}
		if hasArg(command.Args, "install") {
			f.installAgentBrowser()
			return []byte("installed"), nil
		}
	case "brew":
		if hasArg(command.Args, "list") {
			if f.agentOwner == "brew" {
				return []byte("agent-browser " + f.agentVersion), nil
			}
			return nil, errors.New("not installed by brew")
		}
		if hasArg(command.Args, "install") || hasArg(command.Args, "upgrade") {
			f.installAgentBrowser()
			return []byte("installed"), nil
		}
	case "curl":
		return []byte("installer"), nil
	case "bash":
		if !f.lightpandaInstallPathOnly {
			f.paths["lightpanda"] = filepath.Join(f.home, ".local", "bin", "lightpanda")
		}
		f.lightVersion = "0.3.4"
		f.lightArch = f.platform.Arch
		return []byte("Lightpanda installed successfully"), nil
	case "lightpanda":
		if len(command.Args) > 0 && command.Args[0] == "version" {
			return []byte("Lightpanda " + f.lightVersion), nil
		}
	case "ps":
		return []byte(f.processTable()), nil
	case "agent-browser":
		return f.runAgentBrowser(command)
	}
	return []byte("ok"), nil
}

func (f *fakeBrowserRuntime) installAgentBrowser() {
	f.paths["agent-browser"] = "/tools/agent-browser"
	f.resolved["/tools/agent-browser"] = "/tools/agent-browser"
	f.agentVersion = "0.32.1"
	f.agentCaps = true
	f.agentOwner = "npm"
}

func (f *fakeBrowserRuntime) runAgentBrowser(command browserCommand) ([]byte, error) {
	args := command.Args
	if hasArg(args, "--version") {
		return []byte("agent-browser " + f.agentVersion), nil
	}
	if hasArg(args, "--help") {
		if !f.agentCaps {
			return []byte("legacy help"), nil
		}
		return []byte("--engine lightpanda --session --json session id session info session list screenshot [selector] [path] close"), nil
	}
	if hasArg(args, "install") {
		f.chromeReady = true
		return []byte("Chrome for Testing installed"), nil
	}
	if hasSequence(args, "session", "id") {
		f.nextID++
		return []byte(fmt.Sprintf("forge-smoke-%d", f.nextID)), nil
	}

	session := argValue(args, "--session")
	engine := argValue(args, "--engine")
	if hasSequence(args, "session", "info") {
		s := f.sessions[session]
		active := s != nil && s.active
		pid := 0
		if s != nil {
			pid = s.pid
			if engine == "" {
				engine = s.engine
			}
		}
		return json.Marshal(map[string]any{
			"success": true,
			"data": map[string]any{
				"active": active,
				"pid":    pid - 1,
				"runtime": map[string]any{
					"engine":          engine,
					"browserLaunched": active,
					"backgroundPid":   pid,
					"pageCount":       boolInt(active),
				},
			},
		})
	}
	if hasSequence(args, "session", "list") {
		var sessions []map[string]any
		for name, s := range f.sessions {
			if s.active {
				sessions = append(sessions, map[string]any{"name": name, "engine": s.engine})
			}
		}
		return json.Marshal(map[string]any{"success": true, "data": map[string]any{"sessions": sessions}})
	}
	if hasArg(args, "open") {
		installedLightpanda := strings.Contains(envValue(command.Env, "PATH"), filepath.Join(f.home, ".local", "bin"))
		if engine == "lightpanda" && f.paths["lightpanda"] == "" && !installedLightpanda {
			return nil, errors.New("lightpanda executable not found")
		}
		if engine == "chrome" && !f.chromeReady {
			return nil, errors.New("Chrome for Testing not found; run agent-browser install")
		}
		f.nextID++
		f.sessions[session] = &fakeBrowserSession{active: true, engine: engine, pid: 3000 + f.nextID}
		return []byte("opened"), nil
	}
	if hasArg(args, "screenshot") {
		path := args[len(args)-1]
		f.files[path] = croppedPNG()
		return []byte(path), nil
	}
	if hasArg(args, "close") {
		if s := f.sessions[session]; s != nil {
			s.active = false
		}
		return []byte("closed"), nil
	}
	if hasArg(args, "get") {
		return []byte("ready"), nil
	}
	if hasArg(args, "eval") {
		return []byte(`"ready"`), nil
	}
	if hasArg(args, "snapshot") {
		return []byte(`{"success":true,"data":{"snapshot":"button Ready"}}`), nil
	}
	return []byte("ok"), nil
}

func (f *fakeBrowserRuntime) processTable() string {
	var b strings.Builder
	for _, s := range f.sessions {
		if !s.active {
			continue
		}
		fmt.Fprintf(&b, "%d %d agent-browser daemon\n", s.pid-1, 1)
		fmt.Fprintf(&b, "%d %d %s browser\n", s.pid, s.pid-1, s.engine)
	}
	return b.String()
}

func fakeCommandKey(command browserCommand) string {
	args := command.Args
	switch command.Name {
	case "agent-browser":
		for _, operation := range []string{"--version", "--help", "install", "open", "screenshot", "close"} {
			if hasArg(args, operation) {
				engine := argValue(args, "--engine")
				if engine != "" {
					return "agent-browser-" + engine + "-" + strings.TrimPrefix(operation, "--")
				}
				return "agent-browser-" + strings.TrimPrefix(operation, "--")
			}
		}
	case "npm", "brew":
		if hasArg(args, "install") || hasArg(args, "upgrade") {
			return command.Name + "-install"
		}
	case "bash":
		return "lightpanda-install"
	case "lightpanda":
		return "lightpanda-version"
	}
	return command.Name
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func hasSequence(args []string, first, second string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == first && args[i+1] == second {
			return true
		}
	}
	return false
}

func argValue(args []string, name string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func croppedPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 90, B: 160, A: 255})
		}
	}
	var out bytes.Buffer
	_ = png.Encode(&out, img)
	return out.Bytes()
}

func TestParseBrowserVersion(t *testing.T) {
	got, err := parseBrowserVersion("agent-browser 0.31.0\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "0.31.0" || got.Less(browserVersion{0, 31, 0}) {
		t.Fatalf("parsed version = %s", got.String())
	}
	if _, err := parseBrowserVersion("agent-browser development"); err == nil {
		t.Fatal("malformed version must fail closed")
	}
}

func TestLightpandaAssetSelection(t *testing.T) {
	cases := []struct {
		platform browserPlatform
		asset    string
	}{
		{browserPlatform{OS: "darwin", Arch: "arm64"}, "lightpanda-aarch64-macos"},
		{browserPlatform{OS: "darwin", Arch: "amd64"}, "lightpanda-x86_64-macos"},
		{browserPlatform{OS: "linux", Arch: "arm64"}, "lightpanda-aarch64-linux"},
		{browserPlatform{OS: "linux", Arch: "amd64"}, "lightpanda-x86_64-linux"},
		{browserPlatform{OS: "linux", Arch: "amd64", WSL: true}, "lightpanda-x86_64-linux"},
	}
	for _, tc := range cases {
		got, err := lightpandaAsset(tc.platform)
		if err != nil || got != tc.asset {
			t.Fatalf("lightpandaAsset(%+v) = %q, %v; want %q", tc.platform, got, err, tc.asset)
		}
	}
}

func TestEnsureBrowserRuntimeHealthySecondRunHasNoInstallOrUpgrade(t *testing.T) {
	fake := newFakeBrowserRuntime(browserPlatform{OS: "darwin", Arch: "arm64"})
	if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
		t.Fatal(err)
	}
	first := len(fake.commands)
	if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
		t.Fatal(err)
	}
	for _, command := range fake.commands[first:] {
		if commandMutatesInstallation(command) {
			t.Fatalf("healthy rerun performed install/upgrade: %+v", command)
		}
	}
	assertSafeBrowserCommands(t, fake.commands)
}

func TestBrowserProvisioningDoesNotQuerySessionInfoAfterExactClose(t *testing.T) {
	fake := newFakeBrowserRuntime(browserPlatform{OS: "darwin", Arch: "arm64"})
	if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
		t.Fatal(err)
	}

	closed := map[string]bool{}
	for _, command := range fake.commands {
		if command.Name != "agent-browser" {
			continue
		}
		session := argValue(command.Args, "--session")
		if hasArg(command.Args, "close") {
			closed[session] = true
		}
		if closed[session] && hasSequence(command.Args, "session", "info") {
			t.Fatalf("queried session info after exact close for %q: %+v", session, command)
		}
	}
}

func TestBrowserSmokeUsesCanonicalNamedSessions(t *testing.T) {
	fake := newFakeBrowserRuntime(browserPlatform{OS: "darwin", Arch: "arm64"})
	if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
		t.Fatal(err)
	}
	canonical := regexp.MustCompile(`^forge-[a-z0-9][a-z0-9-]{0,23}-[0-9a-f]{8}-(dom|capture)-[0-9a-f]{16}$`)
	sessions := map[string]string{}
	for _, command := range fake.commands {
		if command.Name != "agent-browser" || !hasArg(command.Args, "--session") {
			continue
		}
		session := argValue(command.Args, "--session")
		if !canonical.MatchString(session) {
			t.Fatalf("non-canonical smoke session %q in %+v", session, command)
		}
		if engine := argValue(command.Args, "--engine"); engine != "" {
			if prior, exists := sessions[engine]; exists && prior != session {
				t.Fatalf("%s smoke split its owned session: %q then %q", engine, prior, session)
			}
			sessions[engine] = session
		}
	}
	if sessions["lightpanda"] == "" || sessions["chrome"] == "" || sessions["lightpanda"] == sessions["chrome"] {
		t.Fatalf("smokes must use distinct Lightpanda and Chrome sessions: %#v", sessions)
	}
}

func TestBrowserSmokeUsesOwnedShortSocketDirectory(t *testing.T) {
	fake := newFakeBrowserRuntime(browserPlatform{OS: "darwin", Arch: "arm64"})
	if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
		t.Fatal(err)
	}
	removedSocketDir := false
	for _, path := range fake.removed {
		removedSocketDir = removedSocketDir || strings.HasPrefix(path, "/tmp/forge-ab-")
	}
	if !removedSocketDir {
		t.Fatalf("owned socket directory was not removed: %v", fake.removed)
	}
	var socketDir string
	for _, command := range fake.commands {
		if command.Name != "agent-browser" {
			continue
		}
		got := envValue(command.Env, "AGENT_BROWSER_SOCKET_DIR")
		if !strings.HasPrefix(got, "/tmp/forge-ab-") || len(got) > len("/tmp/forge-ab-")+8 {
			t.Fatalf("browser command lacks a short owned socket directory: %+v", command)
		}
		if socketDir == "" {
			socketDir = got
		} else if socketDir != got {
			t.Fatalf("browser provisioning split its socket namespace: %q then %q", socketDir, got)
		}
	}
}

func TestEnsureBrowserRuntimeInstallsMissingAgentBrowser(t *testing.T) {
	fake := newFakeBrowserRuntime(browserPlatform{OS: "linux", Arch: "amd64"})
	delete(fake.paths, "agent-browser")
	fake.agentVersion = ""
	if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
		t.Fatal(err)
	}
	if !findCommand(fake.commands, "npm", "install", "agent-browser@latest") {
		t.Fatalf("missing official npm install command: %+v", fake.commands)
	}
}

func TestEnsureBrowserRuntimeUpgradesOwningNPMInstall(t *testing.T) {
	fake := newFakeBrowserRuntime(browserPlatform{OS: "darwin", Arch: "arm64"})
	fake.agentVersion = "0.20.14"
	fake.agentOwner = "derived"
	fake.resolved["/tools/agent-browser"] = "/custom/lib/node_modules/agent-browser/bin/agent-browser-darwin-arm64"
	if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
		t.Fatal(err)
	}
	if !findCommand(fake.commands, "npm", "--prefix", "/custom", "install", "agent-browser@latest") {
		t.Fatalf("did not upgrade the resolved npm prefix: %+v", fake.commands)
	}
}

func TestEnsureBrowserRuntimeUpgradesCapabilityDeficientVersion(t *testing.T) {
	fake := newFakeBrowserRuntime(browserPlatform{OS: "linux", Arch: "arm64"})
	fake.agentVersion = "0.32.1"
	fake.agentCaps = false
	if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
		t.Fatal(err)
	}
	if !findCommand(fake.commands, "npm", "install", "agent-browser@latest") {
		t.Fatalf("capability-deficient binary was not upgraded: %+v", fake.commands)
	}
}

func TestEnsureBrowserRuntimeRequiresReleasedLifecycleTarget(t *testing.T) {
	fake := newFakeBrowserRuntime(browserPlatform{OS: "linux", Arch: "amd64"})
	fake.agentVersion = "0.31.0"
	if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
		t.Fatal(err)
	}
	if !findCommand(fake.commands, "npm", "install", "agent-browser@latest") {
		t.Fatalf("agent-browser below the 0.32.1 lifecycle target was accepted: %+v", fake.commands)
	}
}

func TestEnsureBrowserRuntimeInstallsVerifiedLightpanda(t *testing.T) {
	for _, platform := range []browserPlatform{{OS: "darwin", Arch: "amd64"}, {OS: "linux", Arch: "arm64"}, {OS: "linux", Arch: "amd64", WSL: true}} {
		t.Run(platform.OS+"-"+platform.Arch+fmt.Sprint(platform.WSL), func(t *testing.T) {
			fake := newFakeBrowserRuntime(platform)
			delete(fake.paths, "lightpanda")
			if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
				t.Fatal(err)
			}
			if !findCommand(fake.commands, "curl", "https://pkg.lightpanda.io/install.sh") ||
				!findCommand(fake.commands, "bash", lightpandaVersion) {
				t.Fatalf("official verified installer not used: %+v", fake.commands)
			}
		})
	}
}

func TestEnsureBrowserRuntimeUsesOfficialLightpandaPathBeforeParentPathChanges(t *testing.T) {
	fake := newFakeBrowserRuntime(browserPlatform{OS: "darwin", Arch: "arm64"})
	delete(fake.paths, "lightpanda")
	fake.lightpandaInstallPathOnly = true
	if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(fake.home, ".local", "bin")
	for _, command := range fake.commands {
		if command.Name == "lightpanda" && !strings.Contains(envValue(command.Env, "PATH"), want) {
			t.Fatalf("installed Lightpanda was not made available to verification: %+v", command)
		}
	}
}

func TestEnsureBrowserRuntimeRejectsUnsupportedOrWrongPlatform(t *testing.T) {
	t.Run("native windows points to WSL without Unix commands", func(t *testing.T) {
		fake := newFakeBrowserRuntime(browserPlatform{OS: "windows", Arch: "amd64"})
		err := ensureBrowserRuntime(context.Background(), fake.deps())
		if !errors.Is(err, errLightpandaNeedsWSL) || !strings.Contains(err.Error(), "WSL2") {
			t.Fatalf("error = %v", err)
		}
		if len(fake.commands) != 0 {
			t.Fatalf("native Windows executed host commands: %+v", fake.commands)
		}
	})
	t.Run("musl", func(t *testing.T) {
		fake := newFakeBrowserRuntime(browserPlatform{OS: "linux", Arch: "amd64", Musl: true})
		if err := ensureBrowserRuntime(context.Background(), fake.deps()); err == nil || !strings.Contains(err.Error(), "glibc") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("unsupported architecture", func(t *testing.T) {
		fake := newFakeBrowserRuntime(browserPlatform{OS: "linux", Arch: "386"})
		if err := ensureBrowserRuntime(context.Background(), fake.deps()); err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("wrong installed binary architecture", func(t *testing.T) {
		fake := newFakeBrowserRuntime(browserPlatform{OS: "darwin", Arch: "arm64"})
		fake.lightArch = "amd64"
		if err := ensureBrowserRuntime(context.Background(), fake.deps()); err == nil || !strings.Contains(err.Error(), "architecture") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestEnsureBrowserRuntimeProvisionChromeAfterMissingBrowserSmoke(t *testing.T) {
	fake := newFakeBrowserRuntime(browserPlatform{OS: "linux", Arch: "amd64"})
	fake.chromeReady = false
	if err := ensureBrowserRuntime(context.Background(), fake.deps()); err != nil {
		t.Fatal(err)
	}
	if !findCommand(fake.commands, "agent-browser", "install") {
		t.Fatalf("Chrome for Testing was not provisioned: %+v", fake.commands)
	}
	assertChromeCropAndCleanup(t, fake.commands)
}

func TestEnsureBrowserRuntimeFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		key  string
		prep func(*fakeBrowserRuntime)
	}{
		{"agent install", "npm-install", func(f *fakeBrowserRuntime) { delete(f.paths, "agent-browser") }},
		{"lightpanda install", "lightpanda-install", func(f *fakeBrowserRuntime) { delete(f.paths, "lightpanda") }},
		{"lightpanda smoke", "agent-browser-lightpanda-open", func(*fakeBrowserRuntime) {}},
		{"chrome smoke", "agent-browser-chrome-screenshot", func(*fakeBrowserRuntime) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeBrowserRuntime(browserPlatform{OS: "linux", Arch: "amd64"})
			tc.prep(fake)
			fake.fail[tc.key] = errors.New("injected failure")
			if err := ensureBrowserRuntime(context.Background(), fake.deps()); err == nil || !strings.Contains(err.Error(), "injected failure") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRunSetupChecksBrowserBeforeToolkitMutationAndKeepsWindowsUsable(t *testing.T) {
	t.Run("runtime failure precedes toolkit writes", func(t *testing.T) {
		forgeHome := filepath.Join(t.TempDir(), "forge")
		t.Setenv("FORGE_HOME", forgeHome)
		fake := newFakeBrowserRuntime(browserPlatform{OS: "linux", Arch: "amd64"})
		fake.fail["agent-browser-lightpanda-open"] = errors.New("smoke failed")
		err := runSetupWithBrowserDeps(&cobra.Command{}, nil, fake.deps())
		if err == nil || !strings.Contains(err.Error(), "smoke failed") {
			t.Fatalf("error = %v", err)
		}
		if _, statErr := os.Stat(forgeHome); !os.IsNotExist(statErr) {
			t.Fatalf("toolkit mutated before browser verification: %v", statErr)
		}
	})
	t.Run("native Windows warning does not break existing setup", func(t *testing.T) {
		forgeHome := t.TempDir()
		t.Setenv("FORGE_HOME", forgeHome)
		if err := os.Mkdir(filepath.Join(forgeHome, "agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(forgeHome, "agents", "keep")
		if err := os.WriteFile(marker, []byte("existing"), 0o644); err != nil {
			t.Fatal(err)
		}
		fake := newFakeBrowserRuntime(browserPlatform{OS: "windows", Arch: "amd64"})
		if err := runSetupWithBrowserDeps(&cobra.Command{}, nil, fake.deps()); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(marker); err != nil || string(data) != "existing" {
			t.Fatalf("existing setup changed: %q, %v", data, err)
		}
	})
}

func assertSafeBrowserCommands(t *testing.T, commands []browserCommand) {
	t.Helper()
	for _, command := range commands {
		joined := command.Name + " " + strings.Join(command.Args, " ")
		for _, forbidden := range []string{"--headed", "--auto-connect", "close --all", "sudo"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("unsafe command %q", joined)
			}
		}
		if commandMutatesInstallation(command) && !envHas(command.Env, "CI=1") {
			t.Fatalf("mutating command is not explicitly noninteractive: %+v", command)
		}
	}
	assertChromeCropAndCleanup(t, commands)
}

func assertChromeCropAndCleanup(t *testing.T, commands []browserCommand) {
	t.Helper()
	var captureSession string
	var sawCrop, sawClose bool
	for _, command := range commands {
		if command.Name != "agent-browser" || argValue(command.Args, "--engine") != "chrome" {
			continue
		}
		session := argValue(command.Args, "--session")
		if hasArg(command.Args, "screenshot") {
			captureSession = session
			index := -1
			for i, arg := range command.Args {
				if arg == "screenshot" {
					index = i
					break
				}
			}
			if index < 0 || len(command.Args) <= index+2 || !strings.HasPrefix(command.Args[index+1], "#") {
				t.Fatalf("Chrome smoke is not selector-cropped: %+v", command)
			}
			sawCrop = true
		}
		if hasArg(command.Args, "close") && session == captureSession {
			sawClose = true
		}
	}
	if !sawCrop || !sawClose {
		t.Fatalf("Chrome crop/close smoke incomplete: crop=%v close=%v", sawCrop, sawClose)
	}
}

func commandMutatesInstallation(command browserCommand) bool {
	joined := strings.Join(command.Args, " ")
	return (command.Name == "npm" && strings.Contains(joined, "install")) ||
		(command.Name == "brew" && (strings.Contains(joined, "install") || strings.Contains(joined, "upgrade"))) ||
		command.Name == "curl" || command.Name == "bash" ||
		(command.Name == "agent-browser" && hasArg(command.Args, "install"))
}

func envHas(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}

func envValue(env []string, name string) string {
	prefix := name + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func findCommand(commands []browserCommand, name string, args ...string) bool {
	for _, command := range commands {
		if command.Name != name {
			continue
		}
		joined := " " + strings.Join(command.Args, " ") + " "
		matched := true
		for _, arg := range args {
			if !strings.Contains(joined, " "+arg+" ") {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
