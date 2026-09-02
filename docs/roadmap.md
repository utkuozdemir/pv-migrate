# Roadmap

What the project intends to do, and not do, in the next year or so.
This is a side project, so there are no dates.
Last reviewed 2026-09-02.

## Doing

- Keep the two workflows solid across Kubernetes distributions, storage classes and network setups. Most of the work here is reacting to reports, so a good bug report with the cluster details is the most useful contribution.
- Keep the released artifacts verifiable: signed, attested and reproducible, and keep the checks that prove it in CI.

## Looking into

- Migrating PVCs in `Block` volume mode ([#221](https://github.com/utkuozdemir/pv-migrate/issues/221)). rsync cannot do it, so this needs another data mover.
- A strategy that combines the local tunnel with a `LoadBalancer` service ([#235](https://github.com/utkuozdemir/pv-migrate/issues/235)), for clusters where only one side can expose a service.
- A Tailscale-based strategy ([#219](https://github.com/utkuozdemir/pv-migrate/issues/219)), for clusters that cannot reach each other directly.
- Documenting the exact permissions the tool needs ([#152](https://github.com/utkuozdemir/pv-migrate/issues/152)), so a restricted service account can be set up without trial and error.

## Not doing

- A backup platform. Retention, backup catalogs, restore checks, scheduling policies and application-consistent snapshots are out of scope. `pv-migrate backup` is a data mover you can put in a `CronJob`, and it stays that.
- A controller or operator. The tool is a one-shot client that owns no state, and that is what keeps it simple to run and to reason about.
- Reimplementing a data mover. rsync and rclone do the copying, and the project stays a thin layer around them.
- Renaming or removing established flags. Scripts and CronJobs in the wild depend on them. New behavior gets new flags.
