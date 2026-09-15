---
name: ctty-demo-recorder
description: Instructions and workflow for recording and updating the official ctty demo GIF (images/ctty.gif) using VHS and mock data.
---

# 🎬 ctty Demo GIF Recording Guide

This skill provides step-by-step instructions for re-recording or updating the official demonstration GIF (`images/ctty.gif`) using [VHS](https://github.com/charmbracelet/vhs).

---

## 🛠️ 1. Prerequisites & Tool Installation

Ensure the required recording tools are installed:

```bash
# macOS via Homebrew
brew install vhs ttyd ffmpeg

# Verify tools
which vhs ttyd ffmpeg
```

---

## ⚙️ 2. Prepare Mock Environment & Workspace

### 2.1 Prepare Local Mock Files (`/tmp/ctty_demo_workspace`)
Create a mock working directory with realistic project files for the SFTP local file browser demonstration:

```bash
mkdir -p /tmp/ctty_demo_workspace && cd /tmp/ctty_demo_workspace
echo '{"app": "ctty-backend", "version": "v2.4.0", "env": "production"}' > config.production.json
echo '#!/usr/bin/env bash\necho "Deploying cluster v2.4.0..."\nexit 0' > deploy_cluster.sh && chmod +x deploy_cluster.sh
echo '-- PostgreSQL Database Dump 2026-08-14\nCREATE TABLE users (id SERIAL PRIMARY KEY, name VARCHAR(100));' > db_backup_20260814.sql
echo 'Release v2.4.0 with Tag Highlighting and Full i18n Localization' > release_notes.md
dd if=/dev/zero of=app_v2.4.0.tar.gz bs=1024 count=256 2>/dev/null
```

### 2.2 Configure English Default Locale
Ensure `ctty` displays in English during the recording:

```bash
mkdir -p ~/.config/ctty
cat << 'EOF' > ~/.config/ctty/config.json
{
  "language": "en",
  "check_for_updates": true,
  "key_bindings": {
    "quit_keys": ["q", "ctrl+c"],
    "disable_esc_quit": false
  }
}
EOF
```

### 2.3 Ensure Mock SSH Hosts Configured
Make sure `~/.ssh/config` contains formatted hosts with `# Tags:` comments before host definitions (including `Netease` for SFTP demo):

```ssh
# Tags: home, netease, nas, local
Host Netease
    HostName 192.168.31.16
    User root

# Tags: prod, api, gateway, us-east
Host prod-api-gateway
    HostName 198.51.100.12
    User deploy

# Tags: hidden, backup, storage, cold
Host backup-vault-cold
    HostName 10.99.0.250
    User backup-admin
```
*(Reference config available at `docs/dev/demo/config`; copy it to `/tmp/ctty_ssh_config` for recording)*

> **Key auth must use an absolute `IdentityFile` path.** The demo runs
> under an isolated `HOME=/tmp/ctty_demo_home`, so `~/.ssh/id_rsa` would
> resolve into the empty demo home and key auth silently falls back to a
> password prompt (this exact failure produced `****` in a past recording
> with no keystroke causing it). Use e.g.
> `IdentityFile /Users/suroy/.ssh/id_rsa` and verify with:
> ```bash
> ssh -o BatchMode=yes -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new \
>     -i ~/.ssh/id_rsa root@192.168.31.16 'echo AUTH_OK'
> ```
> Only key material paths are referenced — no secrets enter fixtures.

### 2.4 Seed Demo HOME (English UI + FTP Sites)
The tape runs with `HOME=/tmp/ctty_demo_home`, so seed it (the real `~/.config/ctty` is untouched):

```bash
mkdir -p /tmp/ctty_demo_home/.config/ctty
cat > /tmp/ctty_demo_home/.config/ctty/config.json <<'EOF'
{
  "language": "en",
  "check_for_updates": false,
  "key_bindings": {
    "quit_keys": ["q", "ctrl+c"],
    "disable_esc_quit": false
  }
}
EOF
cat > /tmp/ctty_demo_home/.config/ctty/ftp.json <<'EOF'
{"sites": [
  {"name": "demo-ftp", "host": "test.rebex.net", "port": 21, "user": "demo", "tags": ["demo", "public"]},
  {"name": "lab-nas", "host": "192.168.31.100", "port": 21, "user": "admin", "tags": ["lab", "nas"]},
  {"name": "mirror", "host": "mirror.example.com", "port": 21, "user": "anonymous", "tags": ["public", "mirror"]}
]}
EOF
```
No passwords are seeded — `demo-ftp` uses anonymous login against the public test.rebex.net server; the FTP demo never types credentials (typing `d` near a password prompt would arm a delete — the tape avoids it by design).

Seed fake serial/telnet devices too. Every demo section ends with the
exact Escapes that return to the host list — an extra `Esc` on the host
list quits the app, so empty device lists are dangerous: with no rows,
`i`/`e` become no-ops and the scripted Escapes overshoot into quit.
Seeded rows keep every `i`/`e` opening a real view:

```bash
cat > /tmp/ctty_demo_home/.config/ctty/serial.json <<'EOF'
{"devices": [
  {"name": "Switch-Console", "device": "/dev/cu.usbserial-1420", "baud_rate": 115200, "data_bits": 8, "parity": "none", "stop_bits": 1, "flow_control": "none"},
  {"name": "Router-Aux", "device": "/dev/cu.usbserial-1421", "baud_rate": 9600, "data_bits": 8, "parity": "none", "stop_bits": 1, "flow_control": "none"}
]}
EOF
cat > /tmp/ctty_demo_home/.config/ctty/telnet.json <<'EOF'
{"hosts": [
  {"name": "core-sw", "host": "192.168.1.10", "port": 23, "tags": ["lab", "core"]},
  {"name": "console-srv", "host": "192.168.1.11", "port": 2001, "tags": ["lab", "console"]}
]}
EOF
```

### 2.5 Launch Wrapper (clean `ctty` on screen)

The tape must not show env prefixes. Use a wrapper so the recording only types `ctty`:

```bash
mkdir -p /tmp/demo-bin && cat > /tmp/demo-bin/ctty <<'EOF'
#!/bin/bash
# Demo launcher: clean `ctty` entry for recordings (no env prefix on screen).
cd /tmp/ctty_demo_workspace
export HOME=/tmp/ctty_demo_home
exec /Users/suroy/Bingo/Projects/ctty/dist/ctty --no-update-check -c /tmp/ctty_ssh_config "$@"
EOF
chmod +x /tmp/demo-bin/ctty
```

Record with the wrapper first on `PATH`:

```bash
PATH=/tmp/demo-bin:$PATH vhs docs/dev/demo/demo.tape
```

---

## 🔨 3. Build & Install Latest Binary

Compile the latest codebase into `$PATH`:

```bash
cd /Users/suroy/Bingo/Projects/ctty
make build
```

The tape sets `PATH` to `dist/` so the recording uses this binary without overwriting a system `ctty`.

---

## 🎥 4. Record Demo GIF

Run VHS against the tape file from the repository root:

```bash
cd /Users/suroy/Bingo/Projects/ctty
vhs docs/dev/demo/demo.tape
```

Output is written directly to:
```
images/ctty.gif
```

---

## 📜 5. Tape Workflow Structure (`docs/dev/demo/demo.tape`)

| Step | Keys / Actions | Feature Highlighted |
|---|---|---|
| **1. Launch** | `ctty` (via `/tmp/demo-bin/ctty` wrapper — no env prefix on screen) | Clean startup in demo directory with colorful tags & status |
| **2. Sort Cycling** | `s` (x4) | 4-column loop: Name ➔ Hostname ➔ Tags ➔ Last Login |
| **3. Tag Search** | `/` ➔ `prod` ➔ `Backspace` ➔ `netease` | Real-time tag and name filtering |
| **4. SFTP Browser** | `o` ➔ `Netease` SFTP | Remote SFTP directory listing (live host) |
| **5. SFTP Dual-Pane** | `Tab` ➔ navigate ➔ `i` ➔ `Tab` ➔ `v` ➔ `v` ➔ `Esc` | Pane switching, file info, single/dual layout toggle |
| **6. Serial Manager** | `t` ➔ select device ➔ `i` ➔ `e` (edit form) ➔ `Esc` | Device info + edit form (baud rate etc.), no save |
| **7. Telnet Manager** | `T` ➔ select device ➔ `i` ➔ `Esc` | Native telnet device list with colored tags & info |
| **8. FTP Manager** | `F` ➔ select site ➔ `i` ➔ `Esc` | FTP site list + details (list-only, no connect) |
| **9. Local Browser** | `b` ➔ navigate ➔ `i` ➔ `Esc` | Standalone local file browser + info |
| **10. Preferences Modal** | `S` ➔ `Down` (x3) ➔ `Right`/`Left` ➔ `Esc` | FTP/SFTP pane layout settings (no save) |
| **11. Details & Hidden Hosts** | `i` ➔ `Esc` ➔ `H` ➔ `H` | Machine configuration modal & hidden cold storage nodes |
| **12. Quit** | `q` | Clean application exit |

### 2.5 Launch Wrapper (clean `ctty` on screen)

The tape must not show env prefixes. Use a wrapper so the recording only types `ctty`:

```bash
mkdir -p /tmp/demo-bin && cat > /tmp/demo-bin/ctty <<'EOF'
#!/bin/bash
# Demo launcher: clean `ctty` entry for recordings (no env prefix on screen).
cd /tmp/ctty_demo_workspace
export HOME=/tmp/ctty_demo_home
exec /Users/suroy/Bingo/Projects/ctty/dist/ctty --no-update-check -c /tmp/ctty_ssh_config "$@"
EOF
chmod +x /tmp/demo-bin/ctty
```

Record with the wrapper first on `PATH`:

```bash
PATH=/tmp/demo-bin:$PATH vhs docs/dev/demo/demo.tape
```

---

## 💡 6. Tips & Troubleshooting

- **Changing Resolution / Font Size**: Modify `Set FontSize`, `Set Width`, or `Set Height` in `demo.tape`.
- **Adjusting Speed**: Adjust `Sleep` intervals or `Set PlaybackSpeed 1.0` in `demo.tape`.
- **Optimizing GIF Size**: Keep recording length under 30-45s and use standard 30fps with palette generation.
