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
`docs/security-model.md` says what the tool promises security-wise and where the trust boundaries are, and `docs/roadmap.md` says what is planned and what is deliberately not.

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
- `internal/helm`: the embedded chart, its loader, and the adjustments to its values that depend on what the cluster allows.
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

Each component that uses the network gets an allow-all network policy selecting only its own pod, on by default.
Where no other policy exists it changes nothing, and in a namespace isolated with Kubernetes network policies it is the difference between a transfer and every network strategy failing with a connection timeout that names nothing about network policy.
A deny rule in a CNI's own policy type still wins, as it should, so the claim in the docs is limited to Kubernetes network policies.
The permission to create them is checked with a `SelfSubjectAccessReview` before the install, and an account that is not allowed, or whose check cannot be answered, gets the release without the policies and a warning, rather than a failed install over an object that would have been a no-op for most.
The check only ever removes a policy from the tool's own values, so a policy the user asks for explicitly is still attempted, and the install then reports the real permission problem.
The `mount` strategy's pod opens no connection, so its values switch the policy off and the check has nothing to ask.

## Testing

`go test ./...` is the whole unit suite and needs nothing installed.
The integration suites are behind build tags and need real clusters, so they run in CI and, deliberately, are not part of the default suite.

- The migration suite wants two clusters, since cross-cluster migration is a first-class case, and it points at the second one through an environment variable.
- The backup suite wants a bucket, and CI stands up MinIO for it. The cloud-provider tests additionally fail rather than skip when that provider's credentials are present, and only then.
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
- The recorder honours a setting only where nothing precedes it, and otherwise warns and carries on with its own defaults, so a tape can record at the wrong size and colours while reporting success. The settings therefore come first in every tape, anything that is not a setting comes after them, and `task demo:record` fails on that warning rather than leaving it to be noticed.
- That image also brings its own locale and fonts, and both matter. Without a UTF-8 locale the shell mangles the prompt the tapes wait for, and a missing font is substituted silently, which moves every wrap point. The tapes therefore set the locale and name the font instead of relying on the recorder's defaults, and recording manually on a machine without that font produces a picture that wraps differently.
- The font weight is carried in the family name, because the recorder sets a family and a size and nothing else. Matching what GitHub renders is not available: its stack resolves to a font that ships with macOS and not with the recorder's image, and the one member of that stack the image does carry is lighter still.

Regenerate them manually or by dispatching the workflow, never on a schedule.
Every run brings its own timestamps and generated names, so a periodic re-record would commit another megabyte of GIF showing nothing new.
Dispatching it against a branch rather than the default one records from that branch's code and proposes the recordings back into it, which is how a change to a tape arrives together with the picture it produces.
Doing that on a change to how the recordings are made is deliberate rather than automatic: recording on every push would spend a cluster on each intermediate attempt, and the commit it pushed back would land without checks and race the next push.

## Building and releasing

`Taskfile.yml` is the entry point and mirrors what CI runs; `task lint` covers Go, chart, shell and the release config.

Releases are cut by pushing a version tag, which `task release` derives, signs and pushes.
The tag is signed, and the release workflow's first job checks it before anything is built or published.
It refuses a tag that GitHub does not report as verified, a tag that does not name the release or does not point at the commit being built, and a commit that is not on the main branch.
A tag ruleset makes release tags immutable (no update, no deletion), so a signature cannot be reused by re-pointing a tag.
The ruleset does not check the signature itself, the workflow check is the enforcement.
The publishing jobs run in the `release` environment, which admits only version tags and waits for the maintainer's approval.
This way, a workflow on any other ref cannot read the publishing secrets.
The pipeline publishes considerably more than the CLI binary, including the three data mover images the chart pulls, and it does so across several registries and package managers.
`.goreleaser.yml` and `.github/workflows/release.yml` are the authority on what goes where; do not keep a second list of it.

Things that have bitten before:

- The data mover images are built by a separate job from the CLI, but they carry the same tag, and the CLI stamps that tag into the chart values it installs. A released CLI therefore pulls data mover images of exactly its own version, and a release that publishes one without the other leaves the CLI pulling a tag that does not exist.
- goreleaser's default `ldflags` are spelled out in the release config rather than relied on, because the defaults stamp the build time into the binary and that breaks reproducibility. Setting `ldflags` at all replaces every default, so each one the binary reads has to stay listed. Dropping the version one makes the binary report version `dev`, and the image tag and chart version are derived from that string.
- The release files are reproducible: no build paths in the binaries, the commit time as every timestamp, the generated completions in the archives included, and a fixed owner and mode on every archive entry. The owner and mode matter because an archive entry otherwise records the builder's uid, user name and umask. The same commit yields the same bytes on any machine. A workflow checks that on every change by building on two runners, and the release workflow checks it again by rebuilding the published release on a second runner. Both rebuild without the Go cache on purpose. The cache key is only the `go.sum` hash, so a rebuild would otherwise restore the compiled packages of the build it is meant to check and compare only the packaging. Neither check can catch a dependence on the user or the umask, because every runner builds as the same user. Therefore, the fixed owner and mode on the archive entries must not be dropped as redundant. Keep new build steps deterministic. The CLI image is reproducible the same way (commit time as the image timestamp, buildkit's own attestations off, layer timestamps rewritten on push). The data mover images are not: they install packages from the Alpine repositories.
- Releases are signed keyless with cosign, tied to the release workflow's identity: a signature bundle over the checksums file, which covers every archive, and a signature per image digest. There is no signing key to store or rotate. GitHub build provenance attestations are attached to the archives and the images in separate jobs, after publishing. This way, a failure there never leaves a published release unsigned, and the job can be retried on its own.
- A snapshot or dev build deliberately produces an *empty* image tag rather than a wrong one, which makes the chart fall back to its own default. That is why the version string is inspected rather than used directly.
- CI runs a goreleaser snapshot on every change, which validates the packaging inputs before release day rather than during it.
- A release that fails after its archives are uploaded is finished by tagging the next patch version, never by re-running it over the top.
  goreleaser refuses to upload over existing archives, and the release tag cannot be moved anyway.
  The jobs after publishing (the attestations, the reproducibility check, krew) can be re-run on their own.
- The package manager updates are the last stage, after the release and the images, so a credential that lapsed since the previous release takes down only those, and it does so on a release that otherwise looks complete.

## Conventions

- Commits follow the conventional-commit format: a `type(scope): imperative description` title plus a body explaining what changed and why.
  The changelog is generated by grouping commits on that type, so the type is functional, not decorative: a behaviour fix belongs in a `fix:` commit even when it arrives alongside the tests that found it.
- One commit per pull request is strongly preferred, and its title and body are then the pull request's title and description verbatim, with only the sign-off trailer left out.
  Keeping them identical means there is one account of the change rather than two that drift apart, and the commit's is the one that outlives the pull request, in the history and in the generated changelog.
  Use more than one commit only when the work genuinely contains separable atomic changes, and then give the pull request a description of its own.
- Commits carry a DCO sign-off, and are deliberately not cryptographically signed.
  A signature does not survive the rebase or squash that lands a change here, so requiring one buys nothing and puts a hardware key in the way of routine work.
  Do not add `-S`, and do not leave a pull request in draft on account of a commit being unsigned.
  Release tags are the exception: they are signed, see the release section.
- Nothing carries AI attribution: no "generated with" lines, no AI co-author trailers, in commits, pull requests or anywhere else.
- Log messages open with an emoji, and it has to be one a terminal gives two columns to, which is not every emoji that looks like one.
  A test walks the source and rejects the narrow ones, with the reasoning in its comment; warnings all use the same marker, so pick that one rather than a new shade of orange.
- Linting is `golangci-lint` with linters enabled by default and a short, individually-justified disable list.
  New code is expected to satisfy it rather than accumulate suppressions, and a complexity finding is usually telling you to extract a function.
- Dependencies are updated by Renovate; `.renovaterc.json` is the authority on what qualifies.
  Pinned tool versions in CI and the Taskfile carry `# renovate:` comments so they are updated too.
  Actions are pinned by commit and base images by digest, and Renovate keeps both fresh.
  Renovate merges qualifying updates as branches without opening pull requests.
  That only works while the main branch has no rule requiring a pull request before merging, so if dependency pull requests start appearing for passing updates, that rule is the thing to look for.
- The main branch ruleset requires the three build workflow jobs to pass and has no bypass, not even for admins.
  The build workflow therefore has no `paths-ignore`: a change that skipped the required jobs could never merge.
  Work lands through pull requests, including the maintainer's.
- All three test suites upload coverage to Codecov, each under a flag naming the suite, and Codecov merges them per commit.
  The unit suite alone covers about half of the module, so a coverage number from one suite is not the project's coverage.
- `.bestpractices.json` at the repository root holds the answers to the OpenSSF Best Practices badge questionnaire.
  Their site reads it from the default branch and proposes the answers in the form.
  Therefore, a change to how the project works (tests, releases, reporting) is reflected there, not in the web form.
  The live entry and this file must be updated together.

## Reference pointers

- `README.md`: what the project is and who it is for.
- `docs/usage.md`: the entry point to the user-facing docs.
- `docs/migrate.md`: strategies and direct-migration examples.
- `docs/backup-restore.md`: backends, object layout and raw rclone config mode.
- `docs/install.md`, `docs/cli-reference.md`: install options, and the generated flag reference.
- `internal/helm/pv-migrate/README.md`: the chart's values reference.
- `CONTRIBUTING.md`: the contributor-facing process and the task targets.
- `.github/SECURITY.md`, `.github/GOVERNANCE.md`: how to report a security problem, and how the project is run.
- `docs/security-model.md`: what the tool promises, the trust boundaries and how they are checked.
- `docs/roadmap.md`: what is planned and what is deliberately not.
