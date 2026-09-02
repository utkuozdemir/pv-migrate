# Governance

This project has a single maintainer, [Utku Özdemir](https://github.com/utkuozdemir).
It is a side project, maintained in spare time.

## Roles

- Maintainer: decides what goes in, reviews and merges pull requests, cuts releases and handles security reports. Also administers the repository, the container registries, the Homebrew tap and the Scoop bucket. Currently one person.
- Contributor: anyone who opens a pull request or an issue. Contributions are welcome, see [CONTRIBUTING.md](../CONTRIBUTING.md).
- Reporter: anyone who reports a bug or a security problem, see [SECURITY.md](SECURITY.md).

## How decisions are made

The maintainer decides, in the open, on GitHub issues and pull requests.
Larger changes are discussed in an issue before the work starts.
The one exception is security reports, which stay private until coordinated disclosure.

There is no voting and no steering committee.
The project is too small for that.

Two things are frozen unless there is an explicit decision to break them: the command line flags, and the public Go API under `pvmigrate`.
Both have an installed base, in scripts, CronJobs and other programs, and a rename breaks them silently.

## Becoming a maintainer

There is no formal process yet.
A contributor with a track record of good pull requests can ask, and the maintainer will be happy to share the load.

## Continuity

There is no second maintainer at the moment.
This is the known weak spot of this setup.
If you depend on this project and want to help with that, open an issue.
