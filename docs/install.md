# Installation

`pv-migrate` is a single static binary.
Pick whichever of the options below fits your machine, they all install the same thing.

- [Homebrew](#homebrew-macos-and-linux) (macOS and Linux)
- [krew](#krew) (as a kubectl plugin, any OS)
- [Release archives](#release-archives-linux-macos-and-windows) (Linux, macOS and Windows)
- [Scoop](#scoop-windows) (Windows)
- [Docker](#docker)
- [Shell completion](#shell-completion)
- [Verifying what you downloaded](#verifying-what-you-downloaded)

## Homebrew (macOS and Linux)

```bash
brew tap utkuozdemir/pv-migrate
brew install pv-migrate
```

The tap installs the binary and the completions for bash, zsh and fish.
Upgrades come with `brew upgrade`.

## krew

With [krew](https://krew.sigs.k8s.io/) installed:

```bash
kubectl krew update
kubectl krew install pv-migrate
```

The tool is then available as `kubectl pv-migrate`.
Shell completion is not available for krew plugins, see [kubernetes-sigs/krew#543](https://github.com/kubernetes-sigs/krew/issues/543).

## Release archives (Linux, macOS and Windows)

Every [release](https://github.com/utkuozdemir/pv-migrate/releases) ships an archive per platform: `linux`, `darwin` (macOS) and `windows`, for `x86_64`, `arm64` and, on Linux, `armv7`.

1. Download the archive for your platform.
2. Extract it.
3. Move the `pv-migrate` binary to a directory in your `PATH`.

For example, on an Intel Mac:

```bash
VERSION=<VERSION_TAG>
curl -fsSLO https://github.com/utkuozdemir/pv-migrate/releases/download/${VERSION}/pv-migrate_${VERSION}_darwin_x86_64.tar.gz
tar -xzf pv-migrate_${VERSION}_darwin_x86_64.tar.gz
mv pv-migrate /usr/local/bin
pv-migrate --help
```

The macOS binaries are not notarized, so macOS may refuse to start a downloaded one.
Remove the quarantine attribute with `xattr -d com.apple.quarantine pv-migrate`, or install with Homebrew, which does that for you.

The archives also contain the shell completion files, see [Shell completion](#shell-completion).

## Scoop (Windows)

With [Scoop](https://scoop.sh) installed:

```powershell
scoop bucket add pv-migrate https://github.com/utkuozdemir/scoop-pv-migrate.git
scoop install pv-migrate/pv-migrate
```

## Docker

The CLI is also published as a container image, on [Docker Hub](https://hub.docker.com/r/utkuozdemir/pv-migrate) and on GHCR as `ghcr.io/utkuozdemir/pv-migrate`.
It needs a kubeconfig, so mount one in and point `KUBECONFIG` at it:

```bash
docker run --rm -it \
  -v "$HOME/.kube/config:/kubeconfig:ro" -e KUBECONFIG=/kubeconfig \
  utkuozdemir/pv-migrate:<IMAGE_TAG> --source <source-pvc> --dest <dest-pvc>
```

The image is built from scratch and has no shell, so run the binary directly as shown.
A kubeconfig pointing at `localhost` (e.g., a kind cluster) additionally needs `--network host`.

This is the image to use in a Kubernetes `CronJob` for scheduled backups, see [Bucket backup and restore](backup-restore.md#scheduled-backups).

## Shell completion

Homebrew installs the completions for bash, zsh and fish.
Otherwise, run `pv-migrate completion --help` and follow the instructions for your shell:

```text
To load completions:

Bash:

  $ source <(pv-migrate completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ pv-migrate completion bash > /etc/bash_completion.d/pv-migrate
  # macOS:
  $ pv-migrate completion bash > /usr/local/etc/bash_completion.d/pv-migrate

Zsh:

  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:

  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ pv-migrate completion zsh > "${fpath[1]}/_pv-migrate"

  # You will need to start a new shell for this setup to take effect.

fish:

  $ pv-migrate completion fish | source

  # To load completions for each session, execute once:
  $ pv-migrate completion fish > ~/.config/fish/completions/pv-migrate.fish

PowerShell:

  PS> pv-migrate completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> pv-migrate completion powershell > pv-migrate.ps1
  # and source this file from your PowerShell profile.

Usage:
  pv-migrate completion [bash|zsh|fish|powershell]

Flags:
  -h, --help   help for completion
```

## Verifying what you downloaded

Everything the release pipeline publishes is signed, so you can check that a file or an image came from this project's release workflow and from nothing else.
The signatures are keyless (Sigstore): there is no project key to fetch.
Instead, every signature is tied to the identity of the release workflow, which is what the commands below check.

This applies to releases made after signing was introduced.
Older releases have checksums only.

### Release archives

Each release ships a `checksums.txt` with the sha256 sums of every archive, and a signature bundle `checksums.txt.sigstore.json` that covers it.
Check the signature, then the checksum of what you downloaded:

```bash
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  --certificate-identity-regexp='^https://github\.com/utkuozdemir/pv-migrate/\.github/workflows/release\.yml@refs/tags/v.*$' &&
sha256sum --ignore-missing -c checksums.txt
```

The `&&` keeps a failed signature check from being followed by a passing checksum line.
On macOS, `shasum -a 256 --ignore-missing -c checksums.txt` replaces the last line.

### Container images

The CLI image and the three data mover images (`pv-migrate-rsync`, `pv-migrate-sshd`, `pv-migrate-rclone`) are signed on both Docker Hub and GHCR.
Verify one like this:

```bash
cosign verify \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  --certificate-identity-regexp='^https://github\.com/utkuozdemir/pv-migrate/\.github/workflows/release\.yml@refs/tags/v.*$' \
  utkuozdemir/pv-migrate:vX.Y.Z
```

### Build provenance

Every archive listed in `checksums.txt` and every image also carry a build provenance attestation.
It is a signed statement, made by GitHub during the release workflow, that ties the artifact's digest to this repository, the exact commit, the workflow and the run that produced it.
It answers a different question than the signatures above: not only "is this the project's release", but "was this built by the release workflow from that commit".

Verify a downloaded file with the GitHub CLI.
The two extra flags pin the attestation to the release workflow and to the release tag, so an attestation made by anything else in the repository is rejected:

```bash
gh attestation verify pv-migrate_vX.Y.Z_linux_x86_64.tar.gz \
  --repo utkuozdemir/pv-migrate \
  --signer-workflow github.com/utkuozdemir/pv-migrate/.github/workflows/release.yml \
  --source-ref refs/tags/vX.Y.Z
```

Images are verified by reference:

```bash
gh attestation verify oci://docker.io/utkuozdemir/pv-migrate:vX.Y.Z \
  --repo utkuozdemir/pv-migrate \
  --signer-workflow github.com/utkuozdemir/pv-migrate/.github/workflows/release.yml \
  --source-ref refs/tags/vX.Y.Z
```

The output names the commit and the workflow run, so you can follow it back to the source and the build log.
`checksums.txt` itself is not attested, it is covered by its signature bundle.

### Reproducing a release

The archives of releases made by the current pipeline are built reproducibly: the same commit gives the same bytes, on any machine. Earlier releases were not.
The release workflow itself rebuilds every release on a second runner and fails if the checksums differ.
You can do the same:

1. Clone the repository and check out the release tag.
2. Install the Go version `go.mod` declares and the goreleaser version `hack/dev.Dockerfile` pins (the `GORELEASER_VERSION` line).
3. Build the release files without publishing anything, and compare:

```bash
PRIVATE_ACCESS_TOKEN=placeholder goreleaser release --clean --skip=publish,sign,sbom,docker,announce
diff dist/checksums.txt <(curl -fsSL https://github.com/utkuozdemir/pv-migrate/releases/download/vX.Y.Z/checksums.txt)
```

An empty diff means every archive you built is byte for byte the one that was published.
The container images are not covered by this: the data mover images install packages with `apk`, and the package index moves.
