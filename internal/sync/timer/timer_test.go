package timer_test

import (
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/sync"
	"github.com/acme/agent-wrapper/internal/sync/timer"
)

func render(t *testing.T, goos string, p timer.Params) map[string]string {
	t.Helper()
	units, err := timer.Render(goos, p)
	if err != nil {
		t.Fatalf("Render(%s): %v", goos, err)
	}
	out := map[string]string{}
	for _, u := range units {
		out[u.Name] = string(u.Content)
	}
	return out
}

func TestRenderLinuxDefault(t *testing.T) {
	got := render(t, "linux", timer.Default("linux"))
	if !strings.Contains(got["aw-sync.service"], "\nExecStart=/usr/local/bin/aw-sync once\n") {
		t.Errorf("service:\n%s", got["aw-sync.service"])
	}
	if !strings.Contains(got["aw-sync.timer"], "\nOnUnitActiveSec=300s\n") {
		t.Errorf("timer:\n%s", got["aw-sync.timer"])
	}
	if !strings.HasSuffix(got["aw-sync.service"], "\nStateDirectory=agent-wrapper\nStateDirectoryMode=0755\n") {
		t.Errorf("the default state dir must be created by systemd:\n%s", got["aw-sync.service"])
	}
	if strings.Contains(got["aw-sync.timer"], "Persistent=") {
		t.Errorf("Persistent= is inert with OnUnitActiveSec= and must not render:\n%s", got["aw-sync.timer"])
	}
}

// StateDirectory= would make systemd create /var/lib/agent-wrapper, which a
// custom state dir never uses.
func TestRenderLinuxOmitsStateDirectoryForACustomDir(t *testing.T) {
	service := render(t, "linux", timer.Params{Binary: "/usr/local/bin/aw-sync", StateDir: "/srv/aw", Interval: 5 * time.Minute})["aw-sync.service"]
	if strings.Contains(service, "StateDirectory") {
		t.Errorf("service:\n%s", service)
	}
	if !strings.HasSuffix(service, "\nExecStart=/usr/local/bin/aw-sync once --state-dir /srv/aw\n") {
		t.Errorf("service must end at ExecStart:\n%q", service)
	}
}

// A newline in a path would inject systemd directives; every control
// character is refused, naming the field.
func TestRenderRejectsControlCharacters(t *testing.T) {
	for _, c := range []struct {
		field string
		p     timer.Params
	}{
		{"Binary", timer.Params{Binary: "/usr/local/bin/aw-sync\nExecStartPre=/bin/sh", Interval: time.Minute}},
		{"StateDir", timer.Params{Binary: "/b", StateDir: "/srv/aw\tx", Interval: time.Minute}},
		{"StateDir", timer.Params{Binary: "/b", StateDir: "/srv/aw\x7f", Interval: time.Minute}},
	} {
		for _, goos := range []string{"linux", "darwin", "windows"} {
			_, err := timer.Render(goos, c.p)
			if err == nil || !strings.Contains(err.Error(), c.field) || !strings.Contains(err.Error(), "control character") {
				t.Errorf("%s %+v: err = %v; want a refusal naming %s", goos, c.p, err, c.field)
			}
		}
	}
}

func TestRenderLinuxQuotesAndPassesACustomStateDir(t *testing.T) {
	p := timer.Params{Binary: "/opt/agent wrapper/aw-sync", StateDir: "/srv/aw state", Interval: 15 * time.Minute}
	got := render(t, "linux", p)
	want := "\nExecStart=\"/opt/agent wrapper/aw-sync\" once --state-dir \"/srv/aw state\"\n"
	if !strings.Contains(got["aw-sync.service"], want) {
		t.Errorf("service:\n%s\nwant line %q", got["aw-sync.service"], want)
	}
	if !strings.Contains(got["aw-sync.timer"], "OnUnitActiveSec=900s") {
		t.Errorf("timer:\n%s", got["aw-sync.timer"])
	}
}

func TestRenderLinuxEscapesSystemdSpecifiers(t *testing.T) {
	got := render(t, "linux", timer.Params{Binary: "/opt/100%/aw-sync", Interval: time.Minute})
	if !strings.Contains(got["aw-sync.service"], "ExecStart=/opt/100%%/aw-sync once") {
		t.Errorf("a %% must be doubled for systemd:\n%s", got["aw-sync.service"])
	}
}

func TestRenderDarwinDefault(t *testing.T) {
	got := render(t, "darwin", timer.Default("darwin"))
	plist := got["com.agent-wrapper.aw-sync.plist"]
	for _, want := range []string{
		"<string>com.agent-wrapper.aw-sync</string>",
		"        <string>/usr/local/bin/aw-sync</string>\n        <string>once</string>\n    </array>",
		"<integer>300</integer>",
		"<string>" + timer.LogDir + "/aw-sync.log</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
	if strings.Contains(plist, "--state-dir") {
		t.Errorf("the default state dir must not be passed:\n%s", plist)
	}
}

func TestRenderDarwinEscapesXMLAndKeepsSpaces(t *testing.T) {
	p := timer.Params{Binary: "/Applications/A&B/aw-sync", StateDir: "/Library/Application Support/other", Interval: time.Hour}
	plist := render(t, "darwin", p)["com.agent-wrapper.aw-sync.plist"]
	for _, want := range []string{
		"<string>/Applications/A&amp;B/aw-sync</string>",
		"<string>--state-dir</string>",
		"<string>/Library/Application Support/other</string>",
		"<integer>3600</integer>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
}

func TestRenderWindowsDefault(t *testing.T) {
	xml := render(t, "windows", timer.Default("windows"))["aw-sync-task.xml"]
	for _, want := range []string{
		`<Command>C:\Program Files\AgentWrapper\aw-sync.exe</Command>`,
		"<Arguments>once</Arguments>",
		"<Interval>PT5M</Interval>",
		"<UserId>S-1-5-18</UserId>",
		"<BootTrigger>",
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("task xml lacks %q:\n%s", want, xml)
		}
	}
}

func TestRenderWindowsQuotesAStateDirWithSpaces(t *testing.T) {
	p := timer.Params{Binary: `C:\aw\aw-sync.exe`, StateDir: `D:\Agent Wrapper`, Interval: 10 * time.Minute}
	xml := render(t, "windows", p)["aw-sync-task.xml"]
	if !strings.Contains(xml, `<Arguments>once --state-dir &#34;D:\Agent Wrapper&#34;</Arguments>`) {
		t.Errorf("task xml:\n%s", xml)
	}
	if !strings.Contains(xml, "<Interval>PT10M</Interval>") {
		t.Errorf("task xml:\n%s", xml)
	}
}

// TestRenderWindowsEscapesArgumentsForCommandLineToArgvW guards against a
// naive quoter: an argument that ends in a backslash, or that contains a
// quote (already escaped or not), must round-trip through
// CommandLineToArgvW back to the exact word given.
func TestRenderWindowsEscapesArgumentsForCommandLineToArgvW(t *testing.T) {
	cases := []struct {
		name     string
		stateDir string
		want     string
	}{
		{
			name:     "a trailing backslash must not swallow the closing quote",
			stateDir: `D:\Agent Wrapper\`,
			want:     `<Arguments>once --state-dir &#34;D:\Agent Wrapper\\&#34;</Arguments>`,
		},
		{
			name:     "a bare quote inside a quoted argument is escaped",
			stateDir: `D:\a"b c`,
			want:     `<Arguments>once --state-dir &#34;D:\a\&#34;b c&#34;</Arguments>`,
		},
		{
			name:     "a backslash already preceding a quote is doubled",
			stateDir: `D:\a\"b c`,
			want:     `<Arguments>once --state-dir &#34;D:\a\\\&#34;b c&#34;</Arguments>`,
		},
		{
			name:     "no space, tab or quote needs no quoting at all",
			stateDir: `D:\plain`,
			want:     `<Arguments>once --state-dir D:\plain</Arguments>`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := timer.Params{Binary: `C:\aw\aw-sync.exe`, StateDir: c.stateDir, Interval: 5 * time.Minute}
			xml := render(t, "windows", p)["aw-sync-task.xml"]
			if !strings.Contains(xml, c.want) {
				t.Errorf("task xml:\n%s\nwant substring %q", xml, c.want)
			}
		})
	}
}

func TestUnitsCarryTheirInstallPaths(t *testing.T) {
	for goos, want := range map[string][]string{
		"linux":   {timer.ServicePath, timer.TimerPath},
		"darwin":  {timer.PlistPath},
		"windows": {""},
	} {
		units, err := timer.Render(goos, timer.Default(goos))
		if err != nil {
			t.Fatal(err)
		}
		if len(units) != len(want) {
			t.Fatalf("%s: %d units, want %d", goos, len(units), len(want))
		}
		for i, u := range units {
			if u.Path != want[i] {
				t.Errorf("%s unit %s: path %q, want %q", goos, u.Name, u.Path, want[i])
			}
		}
	}
}

func TestIntervalLimits(t *testing.T) {
	for _, bad := range []time.Duration{0, 30 * time.Second, 90 * time.Second, 25 * time.Hour} {
		if err := timer.ValidateInterval(bad); err == nil {
			t.Errorf("ValidateInterval(%s) = nil, want an error", bad)
		}
		if _, err := timer.Render("linux", timer.Params{Binary: "/b", Interval: bad}); err == nil {
			t.Errorf("Render accepted interval %s", bad)
		}
	}
	for _, good := range []time.Duration{time.Minute, 5 * time.Minute, 24 * time.Hour} {
		if err := timer.ValidateInterval(good); err != nil {
			t.Errorf("ValidateInterval(%s) = %v", good, err)
		}
	}
}

func TestUnsupportedOS(t *testing.T) {
	if err := timer.Supported("freebsd"); err == nil || !strings.Contains(err.Error(), "install-timer supports linux, darwin and windows") {
		t.Errorf("Supported(freebsd) = %v", err)
	}
	if _, err := timer.Render("freebsd", timer.Params{Binary: "/b", Interval: time.Minute}); err == nil {
		t.Error("Render(freebsd) succeeded")
	}
}

func TestDefaultUsesTheOSStateDir(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if got := timer.Default(goos).StateDir; got != sync.StateDir(goos) {
			t.Errorf("%s: %q", goos, got)
		}
	}
}
