package trust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRoots builds a project and a home directory under a temp dir, so the
// corpus can name real paths without depending on the developer's machine.
func corpusRoots(t *testing.T) (Roots, string, string) {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	proj := filepath.Join(base, "proj")
	for _, d := range []string{home, proj, filepath.Join(home, ".ssh"), filepath.Join(proj, "src")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r := NewRoots(home, proj)
	return r, canonical(home), canonical(proj)
}

// corpus is the table this package is judged by. Each line states whether it
// should stop and ask the user, because that is the only question the rest of
// the system asks of the classifier.
//
// The autonomous half matters more than the protected half: a false prompt on
// `go build` or `cat /etc/os-release` costs the whole feature, while a missed
// host change costs one guardrail that was never a security boundary.
type corpusCase struct {
	cmd  string
	ask  bool
	want string // substring expected in Summary(), checked when ask is true
}

var corpus = []corpusCase{
	// --- ordinary project work: must never ask -----------------------------
	{cmd: "go build ./..."},
	{cmd: "go test ./internal/..."},
	{cmd: "npm ci"},
	{cmd: "npm install --save-dev vitest"},
	{cmd: "cargo build --release"},
	{cmd: "git status"},
	{cmd: "git reset --hard HEAD~1"},
	{cmd: "rm -rf ./dist"},
	{cmd: "rm -rf {PROJ}/node_modules"},
	{cmd: "mkdir -p {PROJ}/build"},
	{cmd: "touch {PROJ}/src/new.go"},
	{cmd: "sed -i.bak s/a/b/ ./src/main.go"},
	{cmd: "echo hello > ./out.txt"},
	{cmd: "grep -r TODO ./src"},
	{cmd: "curl -sSL https://example.com/x.tgz -o ./x.tgz"},
	{cmd: "cd {PROJ}/src && rm -rf ./generated"},

	// --- reading the host: must never ask ----------------------------------
	{cmd: "cat /etc/os-release"},
	{cmd: "ls -la /etc/nginx"},
	{cmd: "tail -n 100 /var/log/system.log"},
	{cmd: "systemctl status nginx"},
	{cmd: "brew list"},
	{cmd: "apt-get -v"},
	{cmd: "df -h /"},
	{cmd: "sudo -u deploy ./scripts/deploy.sh"},

	// --- remote work: governed by the task, not by this machine ------------
	{cmd: "ssh staging sudo systemctl restart nginx"},
	{cmd: "ssh -i {HOME}/.ssh/deploy_key deploy@staging uptime"},
	{cmd: "ssh staging 'apt-get install -y nginx'"},
	{cmd: "scp ./dist.tar.gz deploy@staging:/var/www/"},
	{cmd: "rsync -av ./dist/ deploy@staging:/var/www/"},
	{cmd: "docker compose up -d"},
	{cmd: "docker run --rm -it alpine sh"},
	{cmd: "kubectl apply -f k8s/deploy.yaml"},
	{cmd: "kubectl --context prod rollout restart deploy/api"},

	// --- changing this machine: must ask -----------------------------------
	{cmd: "echo '127.0.0.1 app.local' >> /etc/hosts", ask: true, want: "writes"},
	{cmd: "sudo systemctl restart nginx", ask: true, want: "controls service nginx"},
	{cmd: "systemctl --user enable myapp", ask: true, want: "controls service myapp"},
	{cmd: "launchctl load {HOME}/Library/LaunchAgents/com.foo.plist", ask: true, want: "controls service"},
	{cmd: "brew install nginx", ask: true, want: "installs package"},
	{cmd: "sudo apt-get install -y nginx", ask: true, want: "installs package"},
	{cmd: "sudo apt-get purge nginx", ask: true, want: "removes package"},
	{cmd: "npm install -g typescript", ask: true, want: "installs package"},
	{cmd: "sudo tee /etc/nginx/nginx.conf", ask: true, want: "writes"},
	{cmd: "sudo cp ./nginx.conf /etc/nginx/nginx.conf", ask: true, want: "writes"},
	{cmd: "sudo chmod 777 /usr/local/bin", ask: true, want: "writes"},
	{cmd: "sudo useradd -m deploy", ask: true, want: "changes user"},
	{cmd: "sudo ufw allow 80", ask: true, want: "changes firewall"},
	{cmd: "sudo sysctl -w net.ipv4.ip_forward=1", ask: true, want: "kernel parameter"},
	{cmd: "sudo mount /dev/disk2s1 /mnt", ask: true, want: "changes mounts"},
	{cmd: "sudo reboot", ask: true, want: "powers off or reboots"},
	{cmd: "sudo dd if=./img.iso of=/dev/disk2", ask: true, want: "writes"},
	{cmd: "echo 'export PATH=$PATH:/x' >> {HOME}/.zshrc", ask: true, want: "writes"},
	{cmd: "crontab -e", ask: true, want: "machine environment"},

	// --- wrappers and nesting ----------------------------------------------
	{cmd: "sh -c 'echo x > /etc/hosts'", ask: true, want: "writes"},
	{cmd: "sudo sh -c 'apt-get install -y nginx'", ask: true, want: "installs package"},
	{cmd: "sudo bash -lc 'systemctl restart nginx'", ask: true, want: "controls service nginx"},
	{cmd: "sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y nginx", ask: true, want: "installs package"},
	{cmd: "timeout 30 sudo systemctl restart nginx", ask: true, want: "controls service nginx"},
	{cmd: "nohup sudo systemctl restart nginx", ask: true, want: "controls service nginx"},
	{cmd: "sudo -- rm -rf /etc/nginx", ask: true, want: "deletes"},
	{cmd: "$SUDO apt-get install -y nginx", ask: true, want: "installs package"},

	// --- destructive regardless of zone ------------------------------------
	{cmd: "sudo rm -rf /", ask: true, want: "recursively deletes"},
	{cmd: "rm -rf {HOME}", ask: true, want: "recursively deletes"},
	{cmd: "rm -rf {PROJ}", ask: true, want: "recursively deletes"},

	// --- credentials: disclosure and modification, not use -----------------
	{cmd: "cat {HOME}/.ssh/id_rsa", ask: true, want: "discloses credential"},
	{cmd: "cp {HOME}/.aws/credentials /tmp/creds", ask: true, want: "discloses credential"},
	{cmd: "chmod 600 {HOME}/.ssh/id_rsa", ask: true, want: "modifies credential"},
	{cmd: "security dump-keychain", ask: true, want: "discloses credential"},
	{cmd: "gpg --export-secret-keys", ask: true, want: "discloses credential"},
	{cmd: "cat {HOME}/.ssh/known_hosts"},
	{cmd: "ssh-add {HOME}/.ssh/id_rsa"},
	{cmd: "curl --cert {HOME}/.ssh/client.pem https://example.com"},

	// --- things we cannot read: say so rather than guess -------------------
	{cmd: "echo x > \"$TARGET\"", ask: true, want: "does not determine statically"},
	{cmd: "eval \"$INSTALL_CMD\"", ask: true, want: "could not determine"},

	// --- a local write dressed as remote work ------------------------------
	{cmd: "rsync -av deploy@staging:/etc/nginx/ /etc/nginx/", ask: true, want: "writes"},
	{cmd: "docker cp api:/etc/nginx.conf /etc/nginx.conf", ask: true, want: "writes"},
}

func TestClassificationCorpus(t *testing.T) {
	roots, home, proj := corpusRoots(t)
	rep := strings.NewReplacer("{HOME}", home, "{PROJ}", proj)

	for _, tc := range corpus {
		t.Run(tc.cmd, func(t *testing.T) {
			cmd := rep.Replace(tc.cmd)
			as := ClassifyCommand(cmd, roots)
			_, ask := as.NeedsAgreement()
			if ask != tc.ask {
				t.Fatalf("ask = %v, want %v\ncommand: %s\nzone: %s\nsummary: %q\neffects: %s",
					ask, tc.ask, cmd, as.Zone(), as.Summary(), dumpEffects(as))
			}
			if tc.ask && tc.want != "" && !strings.Contains(as.Summary(), tc.want) {
				t.Fatalf("summary %q does not mention %q\ncommand: %s\neffects: %s",
					as.Summary(), tc.want, cmd, dumpEffects(as))
			}
		})
	}
}

func dumpEffects(as Assessment) string {
	if len(as.Effects) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for _, e := range as.Effects {
		b.WriteString("\n  ")
		b.WriteString(e.Zone.String() + " " + string(e.Kind) + " " + e.Res.String())
		if !e.Certain {
			b.WriteString(" (uncertain)")
		}
		b.WriteString(" <- " + e.Evidence)
	}
	return b.String()
}

// A remote target must be recorded, not merely tolerated: the UI says where the
// work is going, and a grant for one host must not cover another.
func TestRemoteTargets(t *testing.T) {
	roots, _, _ := corpusRoots(t)
	cases := []struct {
		cmd  string
		host string
		via  string
	}{
		{"ssh staging sudo systemctl restart nginx", "staging", "ssh"},
		{"ssh -p 2222 deploy@prod uptime", "deploy@prod", "ssh"},
		{"scp ./x deploy@staging:/tmp/x", "deploy@staging", "scp"},
		{"rsync -av ./dist/ web1:/var/www/", "web1", "rsync"},
		{"kubectl --context prod get pods", "prod", "kubectl"},
		{"docker -H tcp://build:2375 run alpine true", "tcp://build:2375", "docker"},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			as := ClassifyCommand(tc.cmd, roots)
			if len(as.Targets) == 0 {
				t.Fatalf("no targets recorded")
			}
			got := as.Targets[0]
			if got.Local || got.Host != tc.host || got.Via != tc.via {
				t.Fatalf("target = %+v, want host %q via %q", got, tc.host, tc.via)
			}
			if z := as.Zone(); z != ZoneRemote {
				t.Fatalf("zone = %s, want remote\neffects: %s", z, dumpEffects(as))
			}
		})
	}
}

// `ssh host cat ~/.ssh/id_rsa` pulls a secret into this transcript wherever the
// file lives, so credentials are the one zone remote work does not launder.
func TestRemoteDoesNotLaunderCredentials(t *testing.T) {
	roots, home, _ := corpusRoots(t)
	as := ClassifyCommand("ssh staging cat "+home+"/.ssh/id_rsa", roots)
	if _, ask := as.NeedsAgreement(); !ask {
		t.Fatalf("reading a key over ssh should ask\neffects: %s", dumpEffects(as))
	}
}

// A project that lives inside a system prefix is still the project. Anyone
// working in /opt/app or /usr/local/src would otherwise be prompted constantly.
func TestProjectInsideSystemPrefix(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	proj := filepath.Join(base, "opt", "app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	roots := NewRoots(home, proj)
	as := ClassifyCommand("rm -f "+proj+"/build/out.bin", roots)
	if _, ask := as.NeedsAgreement(); ask {
		t.Fatalf("deleting inside the project should not ask\neffects: %s", dumpEffects(as))
	}
}

// A `cd` we cannot read makes every later relative path a guess, and a guess
// about a mutation is not something to auto-approve.
func TestUnreadableCdMakesRelativePathsUncertain(t *testing.T) {
	roots, _, _ := corpusRoots(t)
	as := ClassifyCommand("cd \"$DIR\" && rm -f ./config.yaml", roots)
	if _, ask := as.NeedsAgreement(); !ask {
		t.Fatalf("a delete after an unreadable cd should ask\neffects: %s", dumpEffects(as))
	}
}

func TestUnparsedAsks(t *testing.T) {
	roots, _, _ := corpusRoots(t)
	as := ClassifyCommand("echo 'unterminated", roots)
	if !as.Unparsed {
		t.Fatalf("expected Unparsed")
	}
	if _, ask := as.NeedsAgreement(); !ask {
		t.Fatalf("an unreadable command line should ask")
	}
}

// The command lines that produced the false prompts, end to end.
func TestCommonIdiomsAreNotHostChanges(t *testing.T) {
	roots, _, proj := corpusRoots(t)
	for _, cmd := range []string{
		"go build ./... 2>/dev/null",
		"command -v rg >/dev/null 2>&1",
		"grep -q TODO ./src 2>/dev/null && echo found",
		"go test ./... > /tmp/test.out 2>&1",
		"cp ./dist/app /tmp/app",
		"mkdir -p /tmp/klaudia-build",
		"tar cf - ./src | gzip > /tmp/src.tgz",
	} {
		as := ClassifyCommandIn(cmd, proj, roots)
		if _, ask := as.NeedsAgreement(); ask {
			t.Errorf("%q would prompt: %s\n%s", cmd, as.Summary(), dumpEffects(as))
		}
	}
}

// …but the block-device case that /dev exists for still stops.
func TestWritingABlockDeviceStillAsks(t *testing.T) {
	roots, _, proj := corpusRoots(t)
	for _, cmd := range []string{
		"sudo dd if=./img.iso of=/dev/disk2",
		"sudo dd if=/dev/zero of=/dev/rdisk3 bs=1m",
	} {
		as := ClassifyCommandIn(cmd, proj, roots)
		if _, ask := as.NeedsAgreement(); !ask {
			t.Errorf("%q was allowed; writing a block device destroys a disk", cmd)
		}
	}
}
