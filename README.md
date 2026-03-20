# rewind-sh

[中文说明](README.zh-CN.md)

> **A lightweight, rewindable shell environment designed for AI agents**
>
> ⚠️ **Project status**
> - This repository is in an early development stage.
> - A large part of implementation was produced with vibe coding.
> - Expect rough edges; use in isolated environments and keep backups for critical paths.

## Motivation

`rewind-sh` is not a general-purpose interactive shell. It is an agent-oriented execution wrapper that focuses on:
- Command-level rollback (filesystem + env snapshots)
- Output control and readable summaries
- Process observability for spawned command trees
- Sparse file tracking with watch + subtree diff

## Build & Start

```bash
go build -o rewind-sh ./cmd/rewind-sh
rewind-sh shell --paths ~ --history-size 500 --backend auto
```

Each shell start creates a separate session store under `~/.rewind-sh/.../sessions/<session-id>`.  
You can reuse one explicitly:

```bash
rewind-sh shell --paths ~ --session-id 1710912345-abcdef01
```

## Core Usage

### Rewind Basics

`rewind-sh` snapshots files under protected roots and environment variables before each command.

```bash
rewind-sh> rm -rf src/
rewind-sh> rewind restore 1
rewind-sh> rewind scope
```

### Safety Model

- Blocks `exit`/`quit` in shell mode.
- Blocks interactive TTY-oriented commands (e.g. `vim/top/less/man`).
- If command output indicates interactive prompts (`[y/N]`, `yes/no`, `password:`), execution is interrupted with guidance.
- During restore, the currently running `rewind-sh` binary path is protected to avoid `text file busy`.

### Scope Model

- Use `--paths` to define protected roots (recursive).
- Shell starts in the startup directory; `--paths` defines protection scope only.
- `cd` outside current roots triggers a dynamic scope extension attempt.
- If required watch count exceeds kernel limits, startup/extension fails with a scope-too-large error.

Examples:

```bash
# Protect home
rewind-sh shell --paths ~ --backend auto

# Protect current directory
rewind-sh shell --paths . --backend auto

# Protect workspace + mount path
rewind-sh shell --paths ~/workspace,/mnt/data --backend auto
```

## Linux Compatibility

Current backend is Linux-only (`watch-diff`, inotify based).  
`backend-check` helps inspect runtime availability.

```bash
rewind-sh backend-check --backend auto
```

## Tests

```bash
go test -v ./internal/snapshot
```

See [TEST_FLOW.md](TEST_FLOW.md) for manual integration checks.

## Roadmap

- [x] Validate core rewind behavior in Kubernetes Linux containers
- [ ] Validate output pagination flow in Kubernetes Linux containers
- [ ] Validate behavior in common Linux distributions
- [ ] Complete background process management implementation and tests
- [ ] Add macOS compatibility
- [ ] Explore high-performance overlayfs backend in privileged Linux environments