package agents

// Block is the canonical managed AGENTS.md boundary for global rgw-ast policy.
// It creates no project-local config; hosts embed this text into AGENTS.md.
const Block = `<!-- rgw-ast:begin -->
# rgw-ast boundary (enforced large workspaces)

Use the global CLI ` + "`rgw-ast`" + ` as the structure-first inspection and
hash-guarded mutation boundary for large codebases.

## When it applies

Before the first repository file mutation in a workspace (and when unsure):

` + "```sh" + `
rgw-ast status --json
# optional monorepo scope:
rgw-ast --root <path> status --json
` + "```" + `

If ` + "`\"enforced\": true`" + ` (default: auto when LOC ≥ 5000 under
` + "`~/.config/rgw-ast/config.toml`" + `):

1. **Explore** with ` + "`rgw-ast map`" + `, ` + "`show`" + `, ` + "`search`" + ` — not whole-file dumps.
2. **Read** only with ` + "`rgw-ast read <file> --lines START-END`" + ` (any text file).
3. **Edit** only with ` + "`rgw-ast hash <file>`" + ` then
   ` + "`rgw-ast patch <file> --expect-hash <sha> --old <text> --new <text>`" + `
   (or ` + "`--old-file`/`--new-file`/`--ops-file`" + `).
4. **Create** new files with ` + "`rgw-ast create <file> --expect-absent --stdin|--from-file`" + `.
5. **Delete** one verified regular file with ` + "`rgw-ast delete <file> --expect-hash <sha>`" + `;
   add ` + "`--prune-empty`" + ` only when empty ancestor directories should also disappear.
6. **Do not** use direct Write/Edit/` + "`apply_patch`" + `/` + "`search_replace`" + `, ` + "`sed -i`" + `,
   or shell redirection to mutate repo files while enforced.
7. Hosts with PreToolUse hooks **must** pipe events to ` + "`rgw-ast hook`" + ` and
   honor ` + "`\"permissionDecision\":\"deny\"`" + `.

**Trusted generators:** required scaffolding tools (for example
` + "`npm exec -- openspec new|archive`" + `) must run as:

` + "```sh" + `
rgw-ast exec -- npm exec -- openspec <args...>
` + "```" + `

` + "`rgw-ast exec`" + ` allowlists patterns from global config ` + "`generators.allow`" + `,
records before/after git porcelain, and reports created/modified hashes.
Do not run generators as freeform shell mutation outside ` + "`rgw-ast exec`" + `.

If ` + "`\"enforced\": false`" + `, normal project tools are allowed; still prefer bounded
reads on large files.

Project-local ` + "`AGENTS.md`" + ` may be stricter; it cannot waive global enforced mode.

Policy is global only — no per-project ` + "`.rgw-ast.toml`" + `.
Install: ` + "`curl -fsSL https://raw.githubusercontent.com/ryangerardwilson/rgw-ast/main/install.sh | bash`" + `
<!-- rgw-ast:end -->
`
