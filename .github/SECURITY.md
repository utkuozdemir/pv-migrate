# Security policy

## Reporting a problem

If you find a security problem in `pv-migrate`, its container images or the release pipeline, please report it privately instead of opening a public issue.
The preferred way is GitHub's private reporting form: [Report a vulnerability](https://github.com/utkuozdemir/pv-migrate/security/advisories/new).
If that does not work for you, email <utkuozdemir@gmail.com>.

Before reporting, see the [security model](../docs/security-model.md) for what is in scope.
Two things there are documented behavior, not findings on their own:

- the data mover images run as root unless `--non-root` is set,
- `--rsync-extra-args` and `--rclone-extra-args` pass raw flags to the data mover by design.

This is a side project maintained in spare time, so please allow up to two weeks for a first response.
A confirmed report is prioritized by severity, and the fix ships in a release rather than sitting on a branch.
The report is published as an advisory after the release, with credit if you want it.
Please keep the details private until then (coordinated disclosure).

## Supported versions

Only the latest release receives fixes.
There are no maintenance branches for older versions.

## Verifying what you run

The checksums file of every release carries a keyless Sigstore signature, which covers every archive.
The four container images are signed the same way, and everything carries a build provenance attestation.
See [Verifying what you downloaded](../docs/install.md#verifying-what-you-downloaded) for the commands.
