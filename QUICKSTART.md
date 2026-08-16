# Quickstart

This fork adds a **Career Dashboard** desktop GUI (Wails) and a Listings/Preferences
screen to the TUI, on top of upstream [career-ops](https://github.com/santifer/career-ops).

Work on the **`gui`** branch. `main` is a clean upstream mirror.

## 1. Prerequisites

- Node.js **22.5+** (below that, tracker DB indexing is disabled)
- Go **1.25+** — only for the dashboard
- An AI coding CLI, e.g. [Claude Code](https://claude.ai/code)

## 2. Install

```bash
git clone -b gui https://github.com/7medeh/career-ops.git
cd career-ops
npm install          # also downloads the Playwright browser for PDF rendering
node doctor.mjs      # confirms everything is wired up
```

## 3. Make it yours

Open the repo in your AI CLI and say **"set me up"**. It walks you through creating
the four files that make the system yours, all git-ignored so they never get
committed or overwritten by an update:

| File | What it holds |
|---|---|
| `cv.md` | Your CV — the source of truth for every generated resume |
| `config/profile.yml` | Name, location, target roles, comp range |
| `modes/_profile.md` | Who you are: archetypes, deal-breakers, narrative |
| `portals.yml` | Which companies and keywords to scan |

Prefer to do it by hand? Copy `config/profile.example.yml`,
`modes/_profile.template.md`, and `templates/portals.example.yml` into place,
then write your own `cv.md`.

## 4. Career Dashboard GUI (macOS)

```bash
dashboard/desktop/build-app.sh
```

Builds the app and installs it to `/Applications/Career Dashboard.app`. Launch it
from Spotlight. Three tabs: **Pipeline** (your tracker), **Listings** (scanned roles
not yet evaluated), **Preferences** (edits `config/profile.yml`).

The repo path is baked into the binary at build time, so **re-run the script if you
move the repo**. First launch may ask permission to control Terminal — that is the
Scan and Apply buttons handing off to your AI CLI.

## 5. Dashboard TUI (any platform)

```bash
npm run serve:dashboard
```

Keys: `L` listings · `P` preferences · `p` progress · `Enter` report · `q` quit.

## One rule

**Do not run `node update-system.mjs apply`.** It treats `dashboard/` as
system-owned and would overwrite the GUI. Pull upstream with git instead:

```bash
git fetch upstream && git merge upstream/main
```

Only six files differ from upstream, so conflicts are rare and small. See
`modes/_custom.md` for the full house rules.

---

Everything else — commands, modes, PDF rendering, troubleshooting — is in
[`docs/SETUP.md`](docs/SETUP.md) and [`README.md`](README.md).
