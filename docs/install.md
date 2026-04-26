# Installation

The recommended install path is Homebrew on macOS/Linux and Scoop on Windows. Other options below.

## Homebrew (macOS/Linux)

If you use Homebrew:

```bash
brew tap utkuozdemir/pv-migrate
brew install pv-migrate
```

## Scoop (Windows)

If you use [Scoop package manager](https://scoop.sh) on Windows,
run:

```powershell
scoop bucket add pv-migrate https://github.com/utkuozdemir/scoop-pv-migrate.git
scoop install pv-migrate/pv-migrate
```

## krew

1. Install [krew](https://krew.sigs.k8s.io/).
2. Install the pv-migrate plugin:

```bash
$ kubectl krew update
$ kubectl krew install pv-migrate
```

## Release binaries (macOS/Linux/Windows)

1. Go to the [releases](https://github.com/utkuozdemir/pv-migrate/releases) and download
   the latest release archive for your platform.
2. Extract the archive.
3. Move the binary to somewhere in your `PATH`.

Sample steps for macOS:

```bash
$ VERSION=<VERSION_TAG>
$ wget https://github.com/utkuozdemir/pv-migrate/releases/download/${VERSION}/pv-migrate_${VERSION}_darwin_x86_64.tar.gz
$ tar -xvzf pv-migrate_${VERSION}_darwin_x86_64.tar.gz
$ mv pv-migrate /usr/local/bin
$ pv-migrate --help
```

## Docker

You can also run the
[official Docker image](https://hub.docker.com/repository/docker/utkuozdemir/pv-migrate):

```bash
docker run --rm -it utkuozdemir/pv-migrate:<IMAGE_TAG> --source <source-pvc> --dest <dest-pvc> ...
```

## Shell completion

If you install `pv-migrate` using Homebrew, completions for bash,
zsh and fish are installed for you.

Completions are not supported when `pv-migrate` is installed using krew. See
[kubernetes-sigs/krew#543](https://github.com/kubernetes-sigs/krew/issues/543).

If you have installed `pv-migrate` by directly downloading the binaries,
run `pv-migrate completion --help` and follow the instructions.

```
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
