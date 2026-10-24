# Recording the docs GIFs

Each tape writes one GIF to `docs/assets/`, and the GIF names are the slots in `docs/site/quickstart.md`
and `docs/site/index.md`.

| Tape | GIF | Rendered by |
| --- | --- | --- |
| `01-build.tape` to `05-doctor.tape` | `01-build.gif` to `05-doctor.gif` | CI and you |
| `07-reapply.tape` (CLI part of step 7) | `07-reapply.gif` | CI and you |
| `manual/06-claude.tape` | `06-claude.gif` | you, with your Claude login |
| `manual/hero.tape` | `hero.gif` | you, with your Claude login |

CI (`.github/workflows/docs.yml`) renders every tape in this directory and uploads the GIFs as the
`docs-gifs` artifact. It does not compare GIF bytes. It fails only if a tape fails to render.
Download the artifact and commit the GIFs you want.

Each CI tape runs the quickstart commands, as written, in a fresh temp `HOME`. Steps before the
visible one run hidden through `lib/prep.sh`. The clone in step 1 comes from your checkout, not from
GitHub. `aw doctor` prints its `file:` line only when `claude` is on `PATH`, so CI puts a stub
`claude` first on `PATH`.

## Render locally

You need [vhs](https://github.com/charmbracelet/vhs) (`go install github.com/charmbracelet/vhs@v0.12.1`),
ffmpeg and ttyd. From the repository root, with changes committed (the clone reads your checkout):

```sh
mkdir -p /tmp/stub && printf '#!/bin/sh\nexit 0\n' > /tmp/stub/claude && chmod +x /tmp/stub/claude
export AW_REPO=$PWD AW_STUB_BIN=/tmp/stub
vhs docs/tapes/01-build.tape
vhs validate docs/tapes/*.tape docs/tapes/manual/*.tape
```

## The two Claude tapes

`manual/06-claude.tape` and `manual/hero.tape` need a logged-in Claude Code, which a temp `HOME`
does not have (`claude -p` there answers "Not logged in"). CI does not render them. Render them by
hand with your own login, without the stub:

```sh
export AW_REPO=$PWD AW_MANUAL=1
vhs docs/tapes/manual/06-claude.tape
vhs docs/tapes/manual/hero.tape
```

`AW_MANUAL=1` keeps your real `HOME`, so the trial state lands in `~/.aw-try` and
`~/.cache/agent-wrapper`. Remove both when you are done (`rm -rf ~/.aw-try ~/.cache/agent-wrapper`).
Each tape runs the step 6 `aw claude -p` command with the prompt before `--allowedTools`. The hero
shows it refused in `payments-api` and then running in `sandbox`. Claude's wording varies per run.

Before committing a Claude GIF, check it shows no account name, no email other than `you@acme.com`,
and no absolute path under your real home.
