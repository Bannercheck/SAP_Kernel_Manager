# skm — SAP Kernel Manager · Claude working rules

(Türkçe özet: her oturumda önce STATE.md oku, sadece NEXT maddesini yap, bitince STATE.md güncelle, commit+push. Mimariyi tekrar anlatma.)

## Session protocol (token budget matters)
1. Read `STATE.md` first. Do NOT scan the repo tree, re-read `docs/ARCHITECTURE.md`, or re-derive
   decisions unless the NEXT item explicitly points to a section.
2. Work ONLY on the item marked **NEXT** in `STATE.md` (one step per session unless the user says otherwise).
3. Before changing a package, read only the files you will change and their tests. Nothing else.
4. Finish every session with: `make check` (build + vet + test) → update `STATE.md`
   (move item to done, set new NEXT, log decisions) → commit → `git push -u origin <branch>`.
5. **Show, don't tell.** The user cannot run skm (macOS, no SAP host). After every step regenerate
   `docs/examples/*.txt` transcripts (fake-runner scenarios or the real binary) and their PNGs with
   `make examples`, commit them, and send the PNGs to the user with SendUserFile. New behaviour
   without a screenshot is not done.
6. Reply to the user in Turkish, ≤ 15 lines: what was done, what is NEXT, open questions (if any).
   Never re-explain the architecture; reference `docs/ARCHITECTURE.md §n` instead.
7. If a step turns out too big for one session, split it in `STATE.md` and finish the first part cleanly.

## Code rules
- Go, stdlib-first. Allowed deps: `gopkg.in/yaml.v3`, `golang.org/x/sys`, `golang.org/x/term`.
  Any new dependency needs a line in `STATE.md` → "Karar günlüğü" with the reason.
- `CGO_ENABLED=0`. Must cross-compile for linux/amd64, linux/ppc64le, aix/ppc64, windows/amd64 (`make cross`).
- OS-specific code lives only in `internal/platform` behind build tags. Everything else is OS-agnostic.
- Never call SAP binaries (sapcontrol, SAPCAR, disp+work, …) directly from business logic;
  go through `internal/exec.Runner` so it is testable with a fake.
- Every `internal/workflow` step is idempotent, journaled, and has `Check/Do/Undo`.
- Never delete or overwrite the running kernel without a verified backup.
- Errors: wrap with context (`fmt.Errorf("stop system %s: %w", sid, err)`). No panics in library code.
- Tests: table-driven; parsers use golden files in `testdata/`. Keep files under ~300 lines.
- Docs in Turkish (`docs/`, `STATE.md`, `README.md`); code, comments, commit messages in English.

## Never
- Don't open a PR unless asked. Don't force-push. Don't rewrite whole files; use targeted edits.
- Don't put AI model names into any repository artifact.
- Don't block on questions for reversible work: pick the default, note the assumption in `STATE.md`.
