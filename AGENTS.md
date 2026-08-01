# pv-migrate — agent guide

This is the project's knowledge base for humans and agents working in this repository.
Maintain it as you go: whenever you learn something durable about how this repo works, add it here.
It is not append-only, so fix or delete anything that becomes wrong or outdated.

Keep it timeless.
Two rules keep it from rotting: **prefer pointers over copies**, and **prefer invariants over inventories**.
Never restate a flag list, a strategy table, a chart value, a file count, a version number or the current state of some in-flight work.
Those live in the code and the docs, they change constantly, and a stale copy here is worse than no copy.
What belongs here is the map, and the reasoning that is not written down anywhere else.

`CLAUDE.md`, `GEMINI.md` and `.github/copilot-instructions.md` point at this file, so this is the one place to edit.
The Copilot file additionally inlines a few of the rules below, because its consumer injects that file verbatim instead of following the pointer; keep those copies in sync.

Keep one sentence per line, and the first line as the single top-level heading.

## What this repo is

A CLI and kubectl plugin that moves the *contents* of a Kubernetes PersistentVolumeClaim.

The premise is that a PVC is two things: an API object, which is trivial to recreate, and data in a storage backend, which is not.
Everything here exists to move the second one, and it does that by running someone else's proven data mover inside the cluster rather than reimplementing one.
There are two workflows: PVC to PVC directly over rsync, and PVC to and from bucket storage over rclone.

The tool itself is a one-shot client that owns no state.
It renders an embedded Helm chart, waits, and uninstalls.
That shape is the reason for most of what follows: everything it needs to say to the cluster has to survive being written into chart values, and everything it learns back it learns by reading pod logs.

`README.md` says what the project is for; `docs/` carries the user-facing reference.

## The two workflows, and the strategy ladder

Direct migration is the original workflow and the more intricate one, because "can these two volumes see each other" has several different answers.
So there is a ladder of strategies, tried in order, each of which declines the job when it cannot do it, and the migration succeeds on the first one that works.
`docs/migrate.md` lists them; the invariants worth knowing are:

- Declining is not failing.
  A strategy that cannot apply returns `ErrUnaccepted` and the next one is tried; a strategy that fails logs and the next one is *also* tried.
  Only an exhausted ladder is an error.
  Adding a strategy therefore means being precise about which of those two a given condition is, because getting it wrong turns a real failure into a silent fallback.
- Order is the user's, and the default order is cheapest-first.
  Mounting both volumes in one pod needs no network at all, so it comes first; everything after it is rsync over SSH and differs only in how the two sides find each other.
- The SSH key pair is generated per attempt and never reused.
  It exists for one transfer and is thrown away with the release.
- Two strategies are opt-in because they have costs the others do not: one depends on node reachability, and one routes the whole transfer through the machine running the CLI.

Bucket backup and restore is the simpler workflow: one release, one rclone job, no networking between clusters.
It has its own identity concept, because a backup has to be findable again later, which a migration does not.
`docs/backup-restore.md` carries the object layout.

## Code map

- `cmd/pv-migrate`: the binary. Thin: signals, exit code, and the version strings goreleaser stamps.
- `pvmigrate`: the public Go API, and the only supported way to drive this as a library.
  It is a stable surface with defaulting and validation, and it converts into the internal request types.
  Everything under `internal/` may be rearranged freely; this may not.
- `internal/app`: the cobra command tree, flags, completion and logger setup.
- `internal/migrator`: runs the strategy ladder, and the pre-flight checks that apply to every strategy.
- `internal/strategy`: the strategies themselves, plus the topology that decides which side runs sshd and which runs rsync.
- `internal/bucketstorage`: the backup and restore workflow end to end.
- `internal/rsync`, `internal/rclone`: build the data mover command strings, and the rclone config.
- `internal/progresslog`: tails a log stream, parses progress and drives the progress bar, for either data mover.
  `internal/rsync/progress` and `internal/rclone/progress` are just the two line parsers; `internal/jobprogress` picks between them by job name.
- `internal/helm`: the embedded chart and its loader.
- `internal/k8s`: client construction, and the waiting: for a pod, a job, a service address, a port-forward.
- `internal/pvc`: resolves a claim into what the strategies need to know about it.
- `internal/opid`: the operation identifier, generated and validated in one place.
- `internal/shell`: quoting for the command strings. Small and load-bearing, see below.
- `integration/`: the end-to-end suite, behind build tags, against real clusters.

## The command values are shell strings

This is the invariant most easily broken by accident, and the one with the worst failure modes.

The chart's rsync and rclone Jobs run `sh -c` on a script assembled by a Go template, and the actual data mover invocation arrives as a chart *value* that is interpolated into that script.
So a string built in `internal/rsync` or `internal/rclone` is read three times: as a Helm value, as YAML inside a block scalar, and as shell.
Three consequences, each of which was a real bug:

- **Every interpolated path, host and spec is quoted as one shell word.**
  `internal/shell` does that and is used by both builders.
  A path is allowed to contain a space, because `--source-path` and `--dest-path` are user input, and an unquoted one splits into two arguments and copies the wrong thing.
- **A character YAML cannot carry on one line is rejected rather than quoted.**
  Quoting one is valid shell, but the value sits in a YAML block scalar, so the chart stops rendering and the user gets a parse error naming a template line instead of their own flag.
  That is a wider set than a newline, and the rule that matters is the one the YAML reader Helm uses actually applies rather than what the current spec says.
  It permits no control character there except tab, treats one C1 control and two Unicode separators as line breaks of their own, stops its printable set below the last two non-characters of the basic plane, and is defined over characters rather than bytes, so it also rejects a value that is not valid UTF-8.
  Reject at the builder, where the flag name is still known.
- **`extraArgs` is the one deliberate exception**, documented as raw rsync or rclone flags, appended unquoted, at the user's own risk.
  Do not "fix" it, and do not add a second one.

Two related rules:

- A value templated with Sprig's `quote` ends up in shell *double* quotes, where a command substitution still runs.
  Anything user-derived must reach the script through an environment variable instead, which is what the metadata upload path does.
- A user-supplied path is resolved against the container's mount point and checked for staying inside it, before anything touches the cluster.
  Without that, `--dest-path ../` named the container's root, which rsync would then write to and, with the delete flag, delete from.
  The check is lexical: a symlink already inside the volume can still lead rsync out of it, which is a different problem and one that needs write access to the volume to set up.
  The resolver preserves a trailing slash, and rsync's other spelling of it, because rsync reads `dir` as the directory and `dir/` as its contents.

## Operation identifiers and the name budget

Every operation has an identifier, supplied with `--id` or generated, and it is what `status` and `cleanup` are given to find the operation again.
It is also embedded in the Helm release name and, through that, in the name of every resource the chart creates, which is what constrains it.

Two different limits apply and the tighter one is not the obvious one: Kubernetes caps a Service name and a label value at 63 characters, but **Helm refuses a release name over 53**.
Helm's is binding, and exceeding it fails partway through the install with a message about a generated name the user never typed.
The length limit lives in `internal/opid`, and `pvmigrate`'s derived-name test composes the worst-case names and hands them to Helm's own validator, so that arithmetic is checked rather than asserted.
Adding a strategy with a longer name will fail that test rather than a user's migration, because the strategy names come from the code.
The chart's component suffixes do not: the test spells those out, so a longer one has to be added there too.

## Progress reporting

Neither data mover offers a progress API, so progress is scraped from the job pod's log, which is the only channel available to a client that owns no state.

The parsers therefore read whatever the data mover, the image, or the user's extra arguments cause to be printed.
An update that leaves a parser goes two places without further checking: into what `pv-migrate status` prints, and into the progress bar.
Both need the same things to be true, so both parsers guarantee them: a percentage between 0 and 100, and a total no smaller than what has already been transferred.
Where the guarantee is about a number the data mover only estimates, the parsers correct it: the total is raised to at least what has been transferred, since both movers revise their totals as they go.
Where it is about a number the data mover states outright, they refuse the line instead of repairing it, because a fabricated 100% reads as a completed transfer.
The two refuse different things, because they read different formats: a percentage above 100 simply does not match rsync's pattern, while a negative byte count is something only rclone's typed JSON can express in the first place.

Some specifics that surprise people:

- rsync overwrites its progress line in place with a carriage return, so one log chunk holds many updates and the log splitter splits on CR as well as LF.
- rsync prints no total, so the total is inferred by scaling the transferred count by the percentage. That multiplication is skipped when it would overflow, because an out-of-range float-to-int conversion is implementation-defined in Go and would otherwise give a different answer on each CI architecture.
- rclone prints JSON and revises its total upward as it walks, so a stats line can legitimately show more transferred than total.
- Both `FindLast` functions share one implementation in `internal/progresslog`. Keep it that way: the previous duplicate drifted and one copy went stale.
- While a transfer is being followed, the progress logger owns the writer, and it retries an ended stream until it is cancelled.
  Anything that wants to print to that writer has to cancel the tail and join the goroutine first, or it interleaves with the bar's redraws.
  The same retry is why a data mover result that is being treated as a success must still reach the logger's completion signal rather than returning early: the goroutine would otherwise never finish.
- A retried follow resumes at the log's tail instead of replaying it, the bar is created once per transfer rather than once per stream attempt, and updates that move the transferred count backward are dropped.
  All three exist because a replayed log used to re-drive a fresh bar to completion on every retry, stranding a finished bar line each time with a nonsense transfer rate.

## The embedded chart

The chart lives in `internal/helm/pv-migrate` and is compiled into the binary, so the released CLI and the chart it installs always match.
Two things follow:

- The chart is not published for standalone use and is not versioned independently. Its in-repo version is a placeholder that the CLI overwrites with its own at load time.
- Changing a template changes the binary, so chart edits are code changes and need the Go tests, not only `helm lint`.
- The Job scripts decide the container's exit code, and the client reads it back and attaches the data mover's own documented meaning for it.
  So the retry loops capture the code rather than collapsing it, a condition the script decides to treat as a success announces itself with a line the client scans for, and the interpretation tables live next to the command builders they describe.
  These scripts are covered by rendering the chart in a Go test and running the script under a real shell with a stand-in data mover, with the retry values zeroed so nothing sleeps.

The chart README is generated from the comments in `values.yaml` by helm-docs and CI fails if it is stale, so never edit it directly.
`docs/cli-reference.md` is likewise generated, from the commands' own help output.
`task generate-all` regenerates both, and CI runs it and fails on a dirty tree, so a change that touches either has to be regenerated in the same commit.

Network policies are off by default and opt-in per component, because most clusters do not need them.
When they are needed and missing, the SSH connection simply never establishes, so the strategy fails and the ladder moves on, and what the user sees is every strategy having failed rather than anything about network policy.

## Testing

`go test ./...` is the whole unit suite and needs nothing installed.
The integration suites are behind build tags and need real clusters, so they run in CI and, deliberately, are not part of the default suite.

- The migration suite wants two clusters, since cross-cluster migration is a first-class case, and it points at the second one through an environment variable.
- The backup suite wants a bucket, and CI stands up MinIO for it. The cloud-provider tests are additionally gated so they fail rather than skip only when that provider's credentials are present.
- Both suites create and delete namespaces in whatever cluster they are pointed at, so point them at a throwaway one.

### Fuzzing

The parsers and generators here read text this project does not control: a data mover's output, a user's flag, a credential arriving through an environment variable.
Fuzz targets cover the shapes nobody thought to write a case for.

Every target asserts a property the callers already rely on rather than just the absence of a panic, and they are deliberately few.
The value is in the parser-shaped code, so that is where they are: config generation, both progress parsers, and the path validators.
`task test:fuzz` drives the search; `task test` replays the seed corpora, so a fixed bug stays fixed without anyone running a fuzzer.

Two rules, both learned by getting them wrong first:

- **Assert at the layer that owns the invariant.** A resolved path is required to stay inside its volume where the mount root is known, not further down where it is just a string.
- **Use an exact oracle, or don't fuzz.** The shell quoting was going to be fuzzed against a hand-written POSIX splitter, until it was clear that a splitter sharing a blind spot with the quoter would pass. The exact oracle is a real shell, which is too slow for a fuzz loop but perfect for a table, and the interesting inputs there are a small enumerable set of metacharacters rather than a deep state space. Table test, no fuzzer.

The same reasoning rules out fuzzing a closed regex, a fixed-field serializer, or standard-library glue: enumerate those instead, and if the property is arithmetic, assert it against the real validator rather than restating the arithmetic.

## The README recordings

The two GIFs in the README are generated rather than screen-captured.
`demo/` holds the tape files that drive them along with the cluster fixtures they run against, and `task demo:record` produces them.

Recording is headless and needs no window or keyboard, but it does need a cluster, since what it records is a real migration.
A demo assembled from invented output would go stale the same way a hand-made capture does, with nothing to catch it.

Things worth knowing before changing them:

- Both tapes source `demo/common.tape`, so the two recordings cannot drift apart in size or colour. The dimensions there are twice what the README displays, because GitHub renders the image at about half that width and the spare pixels are what keep the text sharp.
- A tape waits for the shell prompt to come back rather than for a phrase in the output, since the phrase can scroll off screen before the command exits. When a tape ends, VHS kills whatever is still running, which for this tool means a half-installed release left behind on the cluster.
- The transfer needs to be worth watching. Uniform file sizes keep the progress bar moving at a steady rate, and the destination is emptied first, since rsync copies only what differs and a second run against a full destination is over before the bar moves.
- Both tapes pin a strategy. Left alone the ladder picks the mount strategy, which copies locally in a second or two and is too fast to see. The failure tape pins mount for the opposite reason, to keep that recording short, and doing so assumes one pod can mount both claims. On a cluster that provisions them in different zones the strategy declines instead, and the recording shows the wrong failure.
- The workflow runs the same steps as the local task, from the same tapes. Only the recorder differs. CI runs VHS from its own image, whose fonts include the colour emoji this tool prints and which a bare runner may not have.

Regenerate them by hand or by dispatching the workflow, never on a schedule.
Every run brings its own timestamps and generated names, so a periodic re-record would commit another megabyte of GIF showing nothing new.

## Building and releasing

`Taskfile.yml` is the entry point and mirrors what CI runs; `task lint` covers Go, chart, shell and the release config.

Releases are cut by pushing a version tag, which `task release` derives and pushes.
The pipeline publishes considerably more than the CLI binary, including the three data mover images the chart pulls, and it does so across several registries and package managers.
`.goreleaser.yml` and `.github/workflows/release.yml` are the authority on what goes where; do not keep a second list of it.

Things that have bitten before:

- The data mover images are built by a separate job from the CLI, but they carry the same tag, and the CLI stamps that tag into the chart values it installs. A released CLI therefore pulls data mover images of exactly its own version, and a release that publishes one without the other leaves the CLI pulling a tag that does not exist.
- goreleaser already strips binaries and stamps the version by default. Setting `ldflags` at all silently replaces those defaults, so the binary would report version `dev`, and the image tag and chart version are derived from that string.
- A snapshot or dev build deliberately produces an *empty* image tag rather than a wrong one, which makes the chart fall back to its own default. That is why the version string is inspected rather than used directly.
- CI runs a goreleaser snapshot on every change, which validates the packaging inputs before release day rather than during it.

## Conventions

- Commits follow the conventional-commit format: a `type(scope): imperative description` title plus a body explaining what changed and why.
  The changelog is generated by grouping commits on that type, so the type is functional, not decorative: a behaviour fix belongs in a `fix:` commit even when it arrives alongside the tests that found it.
- One commit per pull request is strongly preferred, and its title and body are then the pull request's title and description verbatim, with only the sign-off trailer left out.
  Keeping them identical means there is one account of the change rather than two that drift apart, and the commit's is the one that outlives the pull request, in the history and in the generated changelog.
  Use more than one commit only when the work genuinely contains separable atomic changes, and then give the pull request a description of its own.
- Commits carry a DCO sign-off, and are deliberately not cryptographically signed.
  A signature does not survive the rebase or squash that lands a change here, so requiring one buys nothing and puts a hardware key in the way of routine work.
  Do not add `-S`, and do not leave a pull request in draft on account of a commit being unsigned.
- Nothing carries AI attribution: no "generated with" lines, no AI co-author trailers, in commits, pull requests or anywhere else.
- Linting is `golangci-lint` with linters enabled by default and a short, individually-justified disable list.
  New code is expected to satisfy it rather than accumulate suppressions, and a complexity finding is usually telling you to extract a function.
- Dependencies are updated by Renovate; `.renovaterc.json` is the authority on what qualifies.
  Pinned tool versions in CI and the Taskfile carry `# renovate:` comments so they are updated too.

## Reference pointers

- `README.md`: what the project is and who it is for.
- `docs/usage.md`: the entry point to the user-facing docs.
- `docs/migrate.md`: strategies and direct-migration examples.
- `docs/backup-restore.md`: backends, object layout and raw rclone config mode.
- `docs/install.md`, `docs/cli-reference.md`: install options, and the generated flag reference.
- `internal/helm/pv-migrate/README.md`: the chart's values reference.
- `CONTRIBUTING.md`: the contributor-facing process and the task targets.
