---
name: rewind-sh
description: Use when performing multi-step file operations that may need partial rollback, bulk file modifications like rm -rf or mass renames, running programs/subprocesses that edit multiple files, or experimental refactoring where you might want to undo intermediate steps
---

# rewind-sh

A rewindable shell environment for AI agents with command-level rollback via filesystem snapshots.

## Overview

**Core principle:** Every command is automatically snapshotted. Restore to any point with `rewind restore <id>`.

```
Command 1 → snapshot created
Command 2 → snapshot created
Command 3 → snapshot created (oops, mistake!)
rewind restore 3 → state before command 3
```

## When to Use

- Multi-step file operations where you might want to undo partway through
- Bulk modifications: `rm -rf`, `find -delete`, mass renames
- Running programs/subprocesses that edit multiple files (changes automatically tracked)
- Experimental refactoring where intermediate steps may break things

## When NOT to Use

- Single file edits (git handles this)
- Writing new code (nothing to undo)
- Running tests (no file modifications)
- Read-only operations

## Quick Start

```bash
# Build (if needed)
go build -o rewind-sh ./cmd/rewind-sh

# Start shell with protected paths
rewind-sh shell --paths /workspace --backend auto

# One-off execution
rewind-sh exec --paths /workspace "rm -rf build/"
```

## Tmux Integration (Recommended)

For persistent sessions across multiple interactions:

```bash
# Create session with rewind-sh
tmux new-session -d -s rewind 'rewind-sh shell --paths /workspace --backend auto'

# Send command
tmux send-keys -t rewind 'mkdir -p src/components' Enter

# Capture output (wait for command to complete)
tmux capture-pane -t rewind -p | tail -20

# Restore from mistake
tmux send-keys -t rewind 'rewind restore 5' Enter
```

## Shell Built-in Commands

Inside the shell:

| Command | Description |
|---------|-------------|
| `rewind list` | Show command history with IDs |
| `rewind restore <id>` | Restore to state BEFORE command `<id>` |
| `rewind scope` | Show monitored paths and watch usage |
| `cd <dir>` | Change directory (extends scope if needed) |
| `pwd` | Print working directory |

## Restore Semantics

**Critical:** `rewind restore 5` restores to the state **BEFORE** command 5 ran.

```
ID  Command
1   echo "hello" > file.txt    ← file.txt created
2   echo "world" >> file.txt   ← file.txt has "hello\nworld"
3   rm file.txt                ← file.txt deleted
4   echo "new" > other.txt

rewind restore 3  → file.txt restored (state before rm)
rewind restore 2  → file.txt has only "hello" (state before append)
```

## Scope Management

- `--paths` defines protected roots (recursive watching)
- `cd` outside roots triggers automatic scope extension
- Watch limits exist (kernel inotify limits); large scopes may fail
- Check scope with `rewind scope` after startup

```bash
# Multiple paths
rewind-sh shell --paths /workspace,/home/user/data

# Check what's monitored
rewind-sh> rewind scope
roots=/workspace,/home/user/data watch_used=42 watch_limit=524288
```

## Blocked Commands

The shell blocks for safety:
- `exit`, `quit` - prevents escaping the controlled environment
- Interactive shells: `bash`, `zsh`, `sh` - maintains audit trail
- TUI tools: `vim`, `top`, `less`, `man` - not compatible with non-interactive mode

## Linux Requirement

Current backend (`watch-diff`) is Linux-only (uses inotify). Check availability:

```bash
rewind-sh backend-check --backend auto
```

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Using `checkpoint-*` commands | Use shell mode with `rewind restore` for automatic snapshots |
| Restoring wrong ID | `rewind restore N` = state BEFORE command N, not after |
| Forgetting to start shell | Commands outside shell aren't tracked |
| Scope too large | Reduce `--paths` or split into sessions |
