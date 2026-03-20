# rewind-sh 测试流程

## 目标

- 验证 watch-diff 后端在大范围监控下的命令级 checkpoint。
- 验证子进程写入文件后可被恢复。
- 验证 `--paths` 保护范围、`cd` 动态扩展监控和 `rewind scope` 输出。
- 验证 watch 上限保护：范围过大时给出可读报错。

## 1. 单元测试

```bash
go test -v ./internal/snapshot
```

对照点：

- `TestDiagnoseAuto` 输出 `resolved=watch-diff`
- `TestWatchDiffCreateRestore` 输出 `step=after_restore content=origin`
- 所有 snapshot 测试 `PASS`

## 2. 后端自检

```bash
rewind-sh backend-check --backend auto
rewind-sh backend-check --backend watch-diff --json
```

对照点：

- 输出包含 `requested=` 与 `resolved=watch-diff`
- JSON 输出中只包含 `watch-diff` 状态项

## 3. 手动集成（基础回滚）

```bash
go build -o rewind-sh ./cmd/rewind-sh
rm -rf /tmp/rewind_demo
mkdir -p /tmp/rewind_demo
echo origin >/tmp/rewind_demo/a.txt

rewind-sh shell --paths /tmp/rewind_demo --backend auto
```

在 shell 中执行：

```bash
echo changed > a.txt; /bin/sh -lc 'echo child > child.txt'
rewind list
rewind restore 1
```

对照点：

- shell 启动时输出 `monitor scope` 和 `monitor scope log`
- 恢复后 `a.txt` 回到 `origin`
- 恢复后 `child.txt` 不存在

## 4. 手动集成（追加监控路径）

```bash
mkdir -p /tmp/rewind_extra
rewind-sh shell --paths /tmp/rewind_demo,/tmp/rewind_extra --backend auto
```

对照点：

- 启动日志中的 roots 同时包含 `/tmp/rewind_demo` 与 `/tmp/rewind_extra`
- scope 日志文件可查看完整监控目录列表

## 5. 目录切换与动态扩展

```bash
rewind-sh shell --paths /tmp/rewind_demo --backend auto
```

在 shell 中执行：

```bash
rewind scope
cd /tmp
rewind scope
```

对照点：

- 第一次 `rewind scope` 只包含 `/tmp/rewind_demo`
- `cd /tmp` 后提示尝试动态扩展监控范围
- 第二次 `rewind scope` 包含 `/tmp`

## 6. watch 上限保护

可在目录层级很多的路径启动：

```bash
rewind-sh shell --paths /very/large/tree --backend auto
```

对照点：

- 若目录数超过 inotify watch 上限，启动失败并提示：
  `监控范围目录过多，watch数量=... 超过上限=...，请缩小监控范围`
