package agent

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"netscope/internal/agent/proto"
)

// Platforms the agent is built for.
var Platforms = []string{"linux-amd64", "linux-arm64", "linux-armv7", "windows-amd64", "windows-arm64"}

// platformOf normalises the operating system family an agent reports (older agents send
// none: they all run on Linux).
func platformOf(p string) string {
	if p == "windows" {
		return "windows"
	}
	return "linux"
}

// Binary is an agent binary the instance can hand out.
type Binary struct {
	Platform string `json:"platform"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type binEntry struct {
	size    int64
	mod     time.Time
	sha     string
	checked time.Time
}

// binCache remembers the checksums of the binaries (recomputed when a file changes).
type binCache struct {
	mu sync.Mutex
	m  map[string]*binEntry
}

// BinaryPath returns the file of a platform's binary ("" if the platform is unknown).
func (s *Service) BinaryPath(platform string) string {
	for _, p := range Platforms {
		if p == platform && s.BinDir != "" {
			return filepath.Join(s.BinDir, "netscope-agent-"+p)
		}
	}
	return ""
}

// Binary returns the binary of a platform (nil if it is not available).
func (s *Service) Binary(platform string) *Binary {
	path := s.BinaryPath(platform)
	if path == "" {
		return nil
	}
	s.bins.mu.Lock()
	defer s.bins.mu.Unlock()
	if s.bins.m == nil {
		s.bins.m = map[string]*binEntry{}
	}
	e := s.bins.m[platform]
	if e != nil && time.Since(e.checked) < time.Minute {
		return &Binary{Platform: platform, Size: e.size, SHA256: e.sha}
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		delete(s.bins.m, platform)
		return nil
	}
	if e == nil || e.size != fi.Size() || !e.mod.Equal(fi.ModTime()) {
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return nil
		}
		e = &binEntry{size: fi.Size(), mod: fi.ModTime(), sha: hex.EncodeToString(h.Sum(nil))}
		s.bins.m[platform] = e
	}
	e.checked = time.Now()
	return &Binary{Platform: platform, Size: e.size, SHA256: e.sha}
}

// Binaries lists the available binaries.
func (s *Service) Binaries() []Binary {
	out := []Binary{}
	for _, p := range Platforms {
		if b := s.Binary(p); b != nil {
			out = append(out, *b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Platform < out[j].Platform })
	return out
}

// offer returns the update for an agent whose version differs from the instance (the
// binaries are built together with the instance). Development builds are not offered.
func (s *Service) offer(platform, arch, version string) *proto.Update {
	if s.Version == "" || s.Version == "dev" || version == "" || version == s.Version {
		return nil
	}
	b := s.Binary(platformOf(platform) + "-" + arch)
	if b == nil {
		return nil
	}
	return &proto.Update{Version: s.Version, Path: proto.PathBinary + b.Platform, SHA256: b.SHA256}
}

// InstallScript returns install.sh for an instance reachable at base (no trailing slash).
func InstallScript(base string) string {
	return strings.ReplaceAll(installScript, "@@URL@@", shellQuote(base))
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

//go:embed install.ps1
var installPS1 string

// InstallScriptWindows returns install.ps1 for an instance reachable at base (no trailing
// slash): the PowerShell counterpart of install.sh.
func InstallScriptWindows(base string) string {
	return strings.ReplaceAll(installPS1, "'@@URL@@'", "'"+strings.ReplaceAll(base, "'", "''")+"'")
}

// installScript installs the agent as a service of its own unprivileged user. Placeholder
// @@URL@@ is the quoted instance URL. Its output is German like install.ps1 and the agent
// itself: it runs on the target host, not in a request (i18n:ignore).
const installScript = `#!/bin/sh
# NetScope-Agent installieren (von der NetScope-Instanz ausgeliefert).
#
#   curl -fsSL <URL>/agent/install.sh | sudo sh -s -- --token nse_… [--docker]
#   curl -fsSL <URL>/agent/install.sh | sudo sh -s -- --uninstall
#
# Der Agent läuft als eigener Benutzer netscope-agent (ohne root), liest nur und verbindet
# sich von sich aus mit NetScope. --docker nimmt ihn in die Gruppe docker auf, damit er
# Container sieht – das entspricht root-Rechten auf diesem System.
set -eu

URL=@@URL@@
TOKEN=''
DOCKER=0
UNINSTALL=0
FINGERPRINT=''
while [ $# -gt 0 ]; do
  case "$1" in
    --token) TOKEN="${2:-}"; shift 2 ;;
    --docker) DOCKER=1; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    --url) URL="${2:-}"; shift 2 ;;
    --fingerprint) FINGERPRINT="${2:-}"; shift 2 ;;
    *) echo "Unbekannte Option: $1" >&2; exit 2 ;;
  esac
done

NAME=netscope-agent
DIR=/var/lib/netscope-agent
BIN="$DIR/netscope-agent"
CONF="$DIR/agent.json"
UNIT=/etc/systemd/system/netscope-agent.service
INITD=/etc/init.d/netscope-agent

if [ "$(id -u)" != 0 ]; then
  echo "Bitte als root ausführen (… | sudo sh -s -- …)." >&2
  exit 1
fi

has() { command -v "$1" >/dev/null 2>&1; }
systemd() { has systemctl && [ -d /run/systemd/system ]; }

if [ "$UNINSTALL" = 1 ]; then
  if systemd && [ -f "$UNIT" ]; then
    systemctl disable --now netscope-agent >/dev/null 2>&1 || true
    rm -f "$UNIT"
    systemctl daemon-reload
  fi
  if [ -f "$INITD" ]; then
    rc-service netscope-agent stop >/dev/null 2>&1 || true
    rc-update del netscope-agent >/dev/null 2>&1 || true
    rm -f "$INITD"
  fi
  rm -rf "$DIR"
  if id "$NAME" >/dev/null 2>&1; then
    if has userdel; then userdel "$NAME" 2>/dev/null || true; elif has deluser; then deluser "$NAME" 2>/dev/null || true; fi
  fi
  echo "NetScope-Agent entfernt. In NetScope unter Agents kann der Eintrag gelöscht werden."
  exit 0
fi

case "$TOKEN" in
  nse_*) ;;
  *) echo "--token nse_… fehlt (Installationsbefehl aus NetScope → Agents kopieren)." >&2; exit 2 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7*|armv8l) ARCH=armv7 ;;
  *) echo "Architektur $(uname -m) wird nicht unterstützt (amd64, arm64, armv7)." >&2; exit 1 ;;
esac

fetch() {
  if has curl; then curl -fsSL ${INSECURE:-} "$1" -o "$2"
  elif has wget; then wget -q ${INSECURE_WGET:-} -O "$2" "$1"
  else echo "curl oder wget wird benötigt." >&2; exit 1; fi
}
if [ -n "$FINGERPRINT" ]; then INSECURE=-k; INSECURE_WGET=--no-check-certificate; fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
echo "Lade NetScope-Agent (linux-$ARCH) von $URL …"
fetch "$URL/agent/bin/linux-$ARCH" "$TMP/agent"
fetch "$URL/agent/bin/linux-$ARCH.sha256" "$TMP/sum"
WANT=$(cut -d' ' -f1 < "$TMP/sum")
if has sha256sum; then GOT=$(sha256sum "$TMP/agent" | cut -d' ' -f1)
else GOT=$(openssl dgst -sha256 "$TMP/agent" | sed 's/.*= //'); fi
if [ "$WANT" != "$GOT" ]; then
  echo "Prüfsumme passt nicht – Download beschädigt?" >&2
  exit 1
fi

if ! id "$NAME" >/dev/null 2>&1; then
  NOLOGIN=/usr/sbin/nologin; [ -x "$NOLOGIN" ] || NOLOGIN=/sbin/nologin
  if has useradd; then useradd --system --home-dir "$DIR" --no-create-home --shell "$NOLOGIN" "$NAME"
  elif has adduser; then adduser -S -D -H -h "$DIR" -s "$NOLOGIN" "$NAME"
  else echo "useradd/adduser fehlt." >&2; exit 1; fi
fi
if [ "$DOCKER" = 1 ]; then
  if getent group docker >/dev/null 2>&1 || grep -q '^docker:' /etc/group; then
    if has usermod; then usermod -aG docker "$NAME"; else addgroup "$NAME" docker; fi
  else
    echo "Hinweis: keine Gruppe docker gefunden – Container werden nicht erfasst."
    DOCKER=0
  fi
fi

mkdir -p "$DIR"
install -m 0755 "$TMP/agent" "$BIN.new" && mv -f "$BIN.new" "$BIN"
set -- enroll --url "$URL" --token "$TOKEN" --config "$CONF"
[ "$DOCKER" = 1 ] && set -- "$@" --docker
[ -n "$FINGERPRINT" ] && set -- "$@" --fingerprint "$FINGERPRINT"
"$BIN" "$@"
chown -R "$NAME" "$DIR"
chmod 700 "$DIR"
chmod 600 "$CONF"

if systemd; then
  cat > "$UNIT" <<EOF
[Unit]
Description=NetScope-Agent
After=network-online.target
Wants=network-online.target

[Service]
User=$NAME
ExecStart=$BIN run --config $CONF
Restart=always
RestartSec=10
RestartPreventExitStatus=3
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable netscope-agent >/dev/null 2>&1
  systemctl restart netscope-agent
  echo "Dienst netscope-agent läuft (systemctl status netscope-agent, journalctl -u netscope-agent)."
elif has rc-service; then
  cat > "$INITD" <<EOF
#!/sbin/openrc-run
name="NetScope-Agent"
command="$BIN"
command_args="run --config $CONF"
command_user="$NAME"
supervisor="supervise-daemon"
respawn_delay=10
depend() { need net; }
EOF
  chmod 755 "$INITD"
  rc-update add netscope-agent default >/dev/null 2>&1
  rc-service netscope-agent restart
  echo "Dienst netscope-agent läuft (rc-service netscope-agent status)."
else
  echo "Kein systemd oder OpenRC gefunden. Agent von Hand starten:"
  echo "  su -s /bin/sh $NAME -c '$BIN run --config $CONF'"
fi
echo "Fertig – das System erscheint in NetScope unter Agents und in der Geräteliste."
`
