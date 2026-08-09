# measure-enforcement Specification

## Purpose
TBD - created by archiving change initial-rgw-ast-cli. Update Purpose after archive.
## Requirements
### Requirement: Workspace root discovery
`rgw-ast` SHALL resolve the workspace root as the nearest ancestor directory
containing a `.git` entry (file or directory), walking from the process working
directory. If none exists, the absolute working directory SHALL be the root.

#### Scenario: Git root
- **GIVEN** the cwd is inside a git repository
- **WHEN** `rgw-ast status` runs
- **THEN** the reported root SHALL be that repository's git root

#### Scenario: Non-git directory
- **GIVEN** the cwd is not inside a git repository
- **WHEN** `rgw-ast status` runs
- **THEN** the reported root SHALL be the absolute cwd

### Requirement: LOC measure
`rgw-ast measure` and the measure embedded in `status` and enforcement checks
SHALL count physical lines of text files under the workspace root that match
global include globs and do not match exclude globs. Files containing a NUL
byte SHALL be skipped.

Results SHALL be cached under the global cache directory using a fingerprint of
the root path, include/exclude policy, and a root content fingerprint (root
directory mtime and git HEAD when present). A cache entry younger than 60
seconds with a matching fingerprint SHALL be reused without walking the tree.
Commands that need enforcement SHALL use the cached path rather than forcing a
full recount on every invocation.

#### Scenario: Exclude node_modules
- **GIVEN** source files exist under `node_modules/`
- **WHEN** measure runs with default excludes
- **THEN** those files MUST NOT contribute to the line count

#### Scenario: Cache hit avoids full walk
- **GIVEN** a successful measure for a root was cached with matching fingerprint
  within 60 seconds
- **WHEN** `rgw-ast status` runs again for the same root and policy
- **THEN** the tool SHALL return the cached loc without requiring a full tree walk

### Requirement: Enforcement decision
Enforcement SHALL be derived only from global config and measured LOC:

- `enforcement.mode = "always"` → enforced is true
- `enforcement.mode = "never"` → enforced is false
- `enforcement.mode = "auto"` → enforced is true iff `loc >= threshold_loc`

#### Scenario: Auto above threshold
- **GIVEN** mode is `auto`, threshold is 5000, and measured loc is 5000 or more
- **WHEN** `rgw-ast status` runs
- **THEN** output SHALL report `enforced: true` (or JSON `enforced: true`)

#### Scenario: Auto below threshold
- **GIVEN** mode is `auto`, threshold is 5000, and measured loc is 4999 or less
- **WHEN** `rgw-ast status` runs
- **THEN** output SHALL report `enforced: false`

### Requirement: Status and measure output
`rgw-ast status` SHALL report at least: root, loc, threshold_loc, mode, and
enforced. `rgw-ast measure` SHALL report at least root and loc. Both SHALL
accept `--json` for a single JSON object on stdout.

#### Scenario: Status JSON
- **WHEN** the user runs `rgw-ast status --json`
- **THEN** stdout SHALL be one JSON object including keys `root`, `loc`,
  `threshold_loc`, `mode`, and `enforced`

### Requirement: Explicit workspace root flag
`rgw-ast` SHALL accept a global `--root <path>` flag that sets the workspace
root to the absolute form of that path (must exist and be a directory) instead
of discovering git root from cwd.

#### Scenario: --root scopes measure
- **GIVEN** a subdirectory of a large monorepo
- **WHEN** the user runs `rgw-ast status --root <subdir>`
- **THEN** reported root SHALL be that subdirectory
- **AND** loc SHALL be measured only under that path

