# rewind-sh

> **专为 Agent 设计的轻量级可回滚 Shell 容器环境**

> ⚠️ **项目状态提示**
> - 当前仓库处于早期开发阶段，功能和接口可能发生较大变化。
> - 项目开发过程中大量使用 vibe coding，潜在缺陷相对较多。
> - 建议在隔离环境中使用，并在关键操作前自行做好额外备份。

## 🎯 动机与亮点

在 AI Agent 自动化执行任务的场景中，传统的交互式 Shell 面临诸多挑战：Agent 难以处理超长输出、缺乏执行高风险命令后的安全保障、难以精确追踪和控制后台进程。`rewind-sh` 并非通用的交互式 Shell，而是专门为 Agent 接管命令执行路径而设计的轻量级方案。

**核心亮点：**
- **命令级可逆 (Time Travel)**：自动为每条命令创建轻量级 Checkpoint，支持一键将文件系统和环境变量无缝回滚至历史状态。
- **进程树追踪 (Process Observability)**：监控 Shell 启动的后台进程树，在回滚时可选择性终止进程，防止环境污染。
- **大范围稀疏监控 (Watch+Diff)**：按用户指定监控 roots，通过 inotify 事件感知有变动的路径，仅对改动子树做差异扫描。

## 🚀 安装与初始化

### 1. 编译安装

建议将编译好的二进制文件所在目录加入到系统的 `PATH` 环境变量中，以便在任意目录调用。

```bash
go build -o rewind-sh ./cmd/rewind-sh

# 将当前目录加入到 PATH，并写入 ~/.bashrc 以便永久生效
echo "export PATH=\"\$PWD:\$PATH\"" >> ~/.bashrc

# 同时在当前 Shell 环境中立即生效（无需重启终端）
export PATH="$PWD:$PATH"

# 备选：如果你想全局安装，也可以使用 sudo 放入系统目录
# sudo mv rewind-sh /usr/local/bin/
```

### 2. 启动 Agent 沙盒容器

Agent 每次执行任务前，应直接通过命令行启动 `rewind-sh` 容器：

```bash
# 请确保 rewind-sh 已经存在于你的 PATH 中，或者采用绝对路径调用
rewind-sh shell --paths ~ --history-size 500 --backend auto
```

每次启动会生成独立 `session id`，并写入独立的 state 目录，避免多个并发 shell 记录串台。
如需显式复用已有会话，可使用 `--session-id <id>`。

## 💡 核心特性与用法

### 🔄 命令执行与回滚 (Rewind)

`rewind-sh` 会在每条命令执行前，自动快照当前**受保护范围（roots）**内的文件与**环境变量**。命令输出直接显示在控制台。

**安全与防越狱机制：**
- **不可退出**：在 `shell` 交互模式下，禁用了 `exit`、`quit`，也拦截了嵌套调用 `bash`、`sh` 的行为，防止 Agent 越狱逃出受控的快照环境。
- **广域快照**：通过 `--paths` 显式指定监控范围，启动有文件操作的 python 或其他脚本也会追踪。
- **自保护恢复**：执行 `rewind restore` 时会跳过当前正在运行的 `rewind-sh` 可执行文件，避免 `text file busy` 导致回滚中断。

**回滚能力边界：**
- **能回滚的**：实际监控 roots 范围内的文件修改/删除/新增（`paths` 中所有路径及子目录）、当前 Shell 的环境变量。
- **不能回滚的**：监控 roots 之外的文件修改（如 `/etc` 或未纳入 roots 的其他绝对路径）、系统级状态（如网络配置、全局服务），后台进程的状态等。
- *容错机制*：元数据默认隔离在用户目录 `~/.rewind-sh/`，且支持防损坏自动恢复，避免用户误改导致回滚链路崩溃。

**使用示例：**
```bash
# 推荐！启动交互式容器（保留 500 条历史），并将整个家目录纳入快照保护范围
rewind-sh shell --paths ~ --history-size 500 --backend auto

# 显式复用某个会话
rewind-sh shell --paths ~ --history-size 500 --backend auto --session-id 1710912345-abcdef01

# 在环境内执行高危操作，或运行第三方脚本
rewind-sh> rm -rf src/
rewind-sh> python3 dangerous_script.py

# 尝试退出（会被拦截并警告）
rewind-sh> exit
权限拒绝: 当前是受限容器，不允许使用 exit 或 quit 退出当前 Shell。请继续执行任务。

# 发现误操作，一键回滚到 ID=1 执行前的状态
rewind-sh> rewind restore 1

# 切换执行目录（内建命令）
rewind-sh> cd ~/0319a

# 查看当前监控范围与 watch 占用
rewind-sh> rewind scope
```

提示：
- 提示符会显示当前目录（如 `rewind-sh:~/0319a>`）。
- 禁用交互式 TTY 程序（如 `vim/top/less/man`）。
- 若命令输出中出现常见交互提示（如 `[y/N]`、`yes/no`、`password:`），系统会中断并提示改用非交互参数。

### ⚙️ 监控范围与后端策略

`rewind-sh` 会根据启动时的 `--paths` 参数来决定监控与保护的目录范围。所有的文件系统变动（包括 Agent 产生的临时文件、输出日志、代码修改等）只要发生在这些 roots 内，就可以被完整回滚。

**指定保护范围：**
- **单路径模式**：`./rewind-sh shell --paths .`（当前目录及子目录）
- **多路径模式**：`--paths /path/a,/path/b`（多个 roots 同时监控）
- **执行目录规则**：Shell 启动后默认保持在启动时目录，`paths` 仅定义受保护范围；后续可通过 `cd` 切换。
- **上限保护**：若监控根下目录数量超过 inotify watch 上限，启动会直接报错并提示缩小范围。
- **目录切换行为**：`cd` 到 roots 外路径时会提示并尝试动态扩展监控范围（若超出 watch 上限则报错并要求缩小范围）。
- **启动提示**：会输出实际监控 roots、watch 使用量和 scope 日志文件。

**监控范围示例**
```bash
# 监控整个 Home
./rewind-sh shell --paths ~ --backend auto

# 监控当前工具目录
./rewind-sh shell --paths . --backend auto

# 监控工作目录 + 某个挂载目录
./rewind-sh shell --paths ~/workspace,/mnt/data --backend auto
```

## 🧪 测试

> 当前仅支持 Linux 运行与验证。

```bash
go test -v ./internal/snapshot
```

*注：由于不涉及复杂外部依赖，Go 原生测试均采用按包就地 (colocate) 模式。完整手动集成测试与预期对照见 [TEST_FLOW.md](TEST_FLOW.md)。*

## 🗺️ Roadmap

- [x] 在 Kubernetes Linux 容器中完成回滚的核心功能的可用性验证
- [ ] 在常用 Linux 环境中测试
- [ ] 完成后台进程管理能力开发与测试
- [ ] 完成 macOS 兼容性适配与测试
- [ ] 在权限足够的 Linux 环境探索 overlayfs 高性能后端方案
