# rewind-sh Test Flow

[中文版本](TEST_FLOW.zh-CN.md)

## Goals

- Verify command-level checkpoint/restore with the watch-diff backend.
- Verify rollback for subprocess-created file changes.
- Verify `--paths` scope, dynamic scope extension on `cd`, and `rewind scope` output.
- Verify watch-limit guardrails with readable scope-too-large errors.

## 1. Unit Tests

```bash
go test -v ./internal/snapshot
```

Checks:

- `TestDiagnoseAuto` resolves to `watch-diff`
- `TestWatchDiffCreateRestore` logs `step=after_restore content=origin`
- All snapshot tests pass

## 2. Backend Diagnostics

```bash
rewind-sh backend-check --backend auto
rewind-sh backend-check --backend watch-diff --json
```

Checks:

- Output contains both `requested=` and `resolved=watch-diff`
- JSON contains only watch-diff status entries

## 3. Manual Integration (Basic Rollback)

```bash
go build -o rewind-sh ./cmd/rewind-sh
rm -rf /tmp/rewind_demo
mkdir -p /tmp/rewind_demo
echo origin >/tmp/rewind_demo/a.txt

rewind-sh shell --paths /tmp/rewind_demo --backend auto
```

Inside shell:

```bash
echo changed > a.txt; /bin/sh -lc 'echo child > child.txt'
rewind list
rewind restore 1
```

Checks:

- Startup shows `monitor scope` and `monitor scope log`
- `a.txt` returns to `origin` after restore
- `child.txt` does not exist after restore

## 4. Manual Integration (Multiple Scope Roots)

```bash
mkdir -p /tmp/rewind_extra
rewind-sh shell --paths /tmp/rewind_demo,/tmp/rewind_extra --backend auto
```

Checks:

- Startup roots include both `/tmp/rewind_demo` and `/tmp/rewind_extra`
- Scope log contains full monitored roots list

## 5. Directory Switch & Dynamic Scope Extension

```bash
rewind-sh shell --paths /tmp/rewind_demo --backend auto
```

Inside shell:

```bash
rewind scope
cd /tmp
rewind scope
```

Checks:

- First `rewind scope` includes only `/tmp/rewind_demo`
- `cd /tmp` triggers dynamic scope extension prompt
- Second `rewind scope` includes `/tmp`

## 6. Watch Limit Guardrail

Start on a large directory tree:

```bash
rewind-sh shell --paths /very/large/tree --backend auto
```

Checks:

- When directory count exceeds inotify watch limit, startup fails with:
  `监控范围目录过多，watch数量=... 超过上限=...，请缩小监控范围`
