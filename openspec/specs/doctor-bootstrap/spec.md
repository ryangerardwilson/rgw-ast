# doctor-bootstrap Specification

## Purpose
TBD - created by archiving change resolve-remaining-bugs. Update Purpose after archive.
## Requirements
### Requirement: Doctor and agents-block
`rgw-ast doctor` SHALL report version, config path, root measure, enforcement,
and evidence-based readiness flags. An AGENTS block SHALL be current only when
it matches the canonical managed block. Hook readiness SHALL require a parsed
command field whose argv begins with `rgw-ast hook`; prose mentions are not
readiness evidence. `rgw-ast agents-block` SHALL print the canonical managed
AGENTS boundary without writing files.

#### Scenario: Agents block is pure output
- **WHEN** agents-block runs
- **THEN** stdout SHALL contain `<!-- rgw-ast:begin -->`
- **AND** no project-local config file SHALL be created


#### Scenario: Superficial agents block is stale
- **GIVEN** AGENTS.md has managed markers but omits canonical instructions
- **WHEN** doctor inspects the workspace
- **THEN** `agents_block_ready` SHALL be false
- **AND** `agents_block_status` SHALL be `stale`

#### Scenario: Prose hook mention is not configured
- **GIVEN** a supported host settings file mentions `rgw-ast hook` only in prose
- **WHEN** doctor inspects the workspace
- **THEN** `hook_ready` SHALL be false
- **AND** `hook_status` SHALL be `not_checked`
