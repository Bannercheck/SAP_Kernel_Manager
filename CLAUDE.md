# KernelMan — SAP Kernel Manager · Claude working rules

(Türkçe özet: her oturumda önce STATE.md oku, sadece NEXT maddesini yap, bitince STATE.md güncelle, commit+push. Mimariyi tekrar anlatma.)

## Session protocol (token budget matters)
1. Read `STATE.md` first. Do NOT scan the repo tree, re-read `docs/ARCHITECTURE.md`, or re-derive
   decisions unless the NEXT item explicitly points to a section.
2. Work ONLY on the item marked **NEXT** in `STATE.md` (one step per session unless the user says otherwise).
3. Before changing a package, read only the files you will change and their tests. Nothing else.
4. Finish every session with: `make check` (build + vet + test) → update `STATE.md`
   (move item to done, set new NEXT, log decisions) → commit → `git push -u origin <branch>`.
5. **Show, don't tell.** The user cannot run kernelman (macOS, no SAP host). After every step regenerate
   `docs/examples/*.txt` transcripts (fake-runner scenarios or the real binary) and their PNGs with
   `make examples`, commit them, and send the PNGs to the user with SendUserFile. New behaviour
   without a screenshot is not done.
6. Reply to the user in Turkish, ≤ 15 lines: what was done, what is NEXT, open questions (if any).
   Never re-explain the architecture; reference `docs/ARCHITECTURE.md §n` instead.
7. If a step turns out too big for one session, split it in `STATE.md` and finish the first part cleanly.

## Code rules
- Go 1.27.x (latest patch, `toolchain` line in go.mod), stdlib-first. Allowed deps: `gopkg.in/yaml.v3`, `golang.org/x/sys`,
  `golang.org/x/term`, `golang.org/x/crypto` (hub SSH only). Keep them at the latest release; `make vuln` (govulncheck) must report
  no vulnerability the code is affected by. Any new dependency needs a line in `STATE.md` → "Karar günlüğü" with the reason.
- `CGO_ENABLED=0`. The agent (`cmd/kernelman`) is Unix only: linux/amd64, linux/ppc64le, aix/ppc64 (`make cross`).
  The hub (`cmd/kernelman-hub`, `internal/hub`) also runs on Windows; it reaches SAP hosts only over SSH and runs the agent there.
- Hub UI (`internal/hub/static`): no third-party JS/CSS/fonts, every interpolated value goes through the escaping `html` template,
  no inline styles (CSP). Every state-changing API needs the `X-KernelMan` header; host secrets never leave the hub unsealed.
- OS-specific code lives only in `internal/platform` behind build tags. Everything else is OS-agnostic.
- Never call SAP binaries (sapcontrol, SAPCAR, disp+work, …) directly from business logic;
  go through `internal/exec.Runner` so it is testable with a fake.
- `internal/ops` operations report through `ops.Progress`, never print; every step is numbered `[i/n]` and shows ok/fail.
- Never delete or overwrite the running kernel without a verified backup.
- Errors: wrap with context (`fmt.Errorf("stop system %s: %w", sid, err)`). No panics in library code.
- Tests: table-driven; parsers use golden files in `testdata/`. Keep files under ~300 lines.
- Docs in Turkish (`docs/`, `STATE.md`, `README.md`); code, comments, commit messages in English.

## Never
- Don't open a PR unless asked. Don't force-push. Don't rewrite whole files; use targeted edits.
- Don't put AI model names into any repository artifact.
- Don't block on questions for reversible work: pick the default, note the assumption in `STATE.md`.

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

Rules:
- When graphify-out/graph.json exists, run `graphify query "<question>"` before your first grep, search or source-file read for a codebase question, and put this rule in every subagent prompt that explores code. Read raw files only for lines the graph or an earlier read already located. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).
- A committed graphify-out/graph.json is rewritten whole on every build, so a large diff to it in a pull request is expected. Do not flag it as churn or suggest skipping the graph rebuild or its hooks.
