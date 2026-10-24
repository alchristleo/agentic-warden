# Recording the Claude clips

Step 6 and the second half of step 7 need a logged-in Claude Code, which cannot run in a temp HOME
(`claude -p` there answers "Not logged in"). Record them by hand with your own login. Nothing here
touches `/etc` or needs sudo.

1. Do quickstart steps 1 to 5 in a fresh shell, with `export AW_SYNC_STATE_DIR=~/.aw-try`.
2. Start the recorder: `asciinema rec step6.cast` (100 columns by 28 rows).
3. `cd payments-api && aw claude`, then type: `run curl -sI https://example.com`.
   Expect Claude to refuse and name the managed permission deny (unmeasured; check the wording).
   Exit Claude.
4. `cd ../sandbox && aw claude`, type the same prompt. Expect the request to run.
5. Step 7 plug-pull: edit the YAML, `awd apply examples/quickstart.yaml`, then
   `aw-sync once --state-dir ~/.aw-try --root claude=$HOME/.aw-try/claude`, then stop `awd`
   (`kill %1`), and repeat step 3. Expect the same refusal.
6. Non-interactive alternative for any prompt: `aw claude -p "run curl -sI https://example.com"`
   (arguments after the agent name reach Claude untouched).
7. Save the refusal text to `docs/tapes/capture-step6.log` and convert the cast to a GIF with
   `agg step6.cast docs/assets/06-claude.gif`.
8. Before committing, check the clip shows no account name, no email other than `you@acme.com`,
   and no absolute path under your real home.
