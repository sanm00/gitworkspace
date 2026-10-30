# Git Workspace 实现规范

> 状态：实现基线
> 产品：Git Workspace
> 可执行文件：`gws`
> 插件后端 ID：`git-worktree`
> 英文版本：`GIT_WORKSPACE_IMPLEMENTATION_SPEC.md`

## 1. 产品定义

Git Workspace 是一个有明确策略的 Git worktree 管理器：从你当前所在的分支一步切出隔离的 Workspace，并在确认安全后合并回去，让你一边继续开发、一边排查问题，或并行推进多件事而不干扰主检出。

其价值主张是：

> 在 Git worktree 之上提供一步式创建、基线追踪、安全清理、前置条件校验的合并回退，以及带版本的机器接口。

Git Workspace 同时是：

1. 面向开发者并行工作目录的独立 CLI；
2. 开发环境隔离平台可调用的源码工作空间插件，平台随后可以配置运行时、端口、容器、工具或 Agent。

Git Workspace 本身不是完整的开发环境隔离平台。

## 2. 产品边界

Git Workspace 负责：

- 仓库范围内的 Workspace 元数据与解析；
- 每种类型 Workspace 的基线 ref/commit 记录；
- Git worktree 创建与删除；
- `gws/` 命名空间内自动无冲突的托管分支；
- detached review/test 工作空间；
- 任务上下文文件；
- 状态锁、失败回滚、真实状态验证和垃圾回收；
- 应用已提交成果并安全结束 Workspace；
- 带版本号的进程级 JSON 接口。

Git Workspace 不负责：

- 选择或启动编辑器、编程智能体；
- Docker Compose、devcontainer、虚拟机或沙箱引擎；
- 宿主机进程监管；
- 端口、数据库、网络、密钥或凭据隔离；
- 依赖安装或 setup hook；
- 远程 PR 生命周期或 CI 编排。

上层平台可以围绕 Git Workspace 返回的 worktree 路径实现这些能力。

## 3. 领域模型

### 3.1 仓库范围

没有注册表。`cwd` 所在仓库（以 Git common directory 标识）就是每条命令的作用域。所有 Workspace、元数据和锁都位于 `<git-common-dir>/gws/` 之下：同一仓库的所有 linked worktree 共享一份状态，互不相关的仓库永远看不到彼此的 Workspace。在 Git 仓库之外执行命令返回 `repository_not_found`。

### 3.2 工作项

工作项是表示用户意图的可选易读标签，例如 `feature/avatar` 或 `issue-328`。它不是 Git 分支名，不能与分支是否存在耦合。省略名称时，实现从基线分支 slug 派生标签，兜底为 `workspace`；review/test 类型额外加 `review-`/`test-` 前缀。

规范化后的标签保留在 Workspace 元数据中。路径组件在规范化中被保留：`feature/avatar` 变成 `feature-avatar`，而不是只剩 `avatar`。

### 3.3 Workspace

Workspace 是一条受管记录，关联：

- 一个工作项和稳定的 Workspace ID；
- 一个 Git worktree 路径；
- 一个源 ref，以及记录的 `base_ref` 与 `base_commit`；
- 一个任务文件；
- 类型、所有权、生命周期状态、错误和时间戳。

生命周期状态为 `creating`、`ready`、`error`、`removing`。

Workspace 类型为 `dev`、`fix`、`review`、`test`。review 和 test Workspace 是 detached 的，无法 apply。`--detach` 可以让任何类型进入 detached 状态。

托管分支位于 `gws/` 命名空间并带有所有权标记；只有被标记的分支才会自动删除，`gc` 从不删除分支。

## 4. 主工作流

### 4.1 创建

```bash
gws new [NAME] [--base BRANCH|SHA] [--type dev|fix|review|test] [--detach] [--fetch] [--no-enter] [--json]
```

`new` 的基线按以下顺序解析：

1. 显式 `--base <branch|sha>`；否则
2. `cwd` 处于本仓库附着分支上时，使用当前分支；否则
3. `.git/gws/config.json` 中的 `default_branch`；否则
4. 返回 `git_ref_not_found` 错误。

所有类型都会同时记录基线 ref 和解析出的基线 commit，检查输出与任务文件始终展示确切基线（`Based on: <base> (<sha>)`）。

`dev`/`fix` 在基线 commit 上分配唯一的托管分支 `gws/<name>`。`review`/`test`（或 `--detach`）在解析出的基线 commit 上建立 detached worktree。`--fetch` 先刷新远程引用，且只允许与 review/test 搭配。

创建后，除非使用 `--json`、`--no-enter` 或标准输出不是终端，`new` 会在 worktree 中启动用户的子 Shell；否则打印 `Next: gws use <id>`。多个 Workspace 可以共用同一个标签；它们的 ID、分支、路径必须保持唯一，歧义的名称选择器要求使用 Workspace ID。

不支持直接在已有的用户分支上开 Workspace：Git 禁止两个 worktree 附着同一分支，所以每个开发 Workspace 都有自己的托管分支。

### 4.2 进入

```bash
gws use [WORKSPACE]
```

`use` 以 worktree 为工作目录启动用户的 Shell，无法改变父 Shell 的目录。子 Shell 启动前必须释放状态锁。选择器接受精确 ID、精确名称或无歧义的 ID 前缀；省略选择器时按 `cwd` 解析。`use` 只做交互，拒绝 `--json`。

### 4.3 检查

```text
gws ls
gws show [WORKSPACE]
gws status [WORKSPACE]
gws path [WORKSPACE]
gws diff [WORKSPACE] [--staged] [--stat] [--uncommitted]
gws log [WORKSPACE] [--limit N] [--stat]
gws commit --message MESSAGE [--all]
```

`diff`、`log`、`commit` 是薄封装：由 `cwd`（或显式选择器）解析 Workspace，并在其 worktree 内执行 Git，调用方无需重复拼路径或查询基线 commit：

- `diff` 输出 `git diff <base_commit>`，即本空间相对基线的全部改动（含已提交部分）。`--uncommitted` 改为对比 `HEAD`；`--staged` 对比索引；`--stat` 输出摘要。Git 的 diff 从不包含未跟踪文件，因此这些文件单独列出。
- `log` 输出 `git log --oneline <base_commit>..HEAD`，`--limit` 限制条数。
- `commit` 在空间内执行 `git commit -m`，`--all` 对应 `-a`（仅已跟踪文件）。它拒绝 detached 的 review/test 空间，其余情况保持只读语义，空消息或工作区干净都作为错误返回而不进入交互提示。

除 `commit` 外这些命令只读取 Git 状态；`commit` 从不修改 Workspace 元数据。

`show` 展示持久化元数据，包含基线 ref 与 commit。`status` 还会校验文件系统、Git worktree 注册、dirty 状态和任务文件是否存在，并展示基线信息。worktree 缺失时，JSON 中 `git.dirty` 为 `null`；Git 状态检查失败作为错误返回，而不是报告为 clean。

在托管 worktree 内，省略 Workspace 选择器时按 `cwd` 解析。

### 4.4 apply 与 finish

```text
gws apply [--into BRANCH]
gws finish WORKSPACE [--into BRANCH]
```

`apply` 必须在 Workspace 内执行；选择器总是由 `cwd` 解析，位置参数会被拒绝。合并目标默认是记录的基线分支，可用 `--into <branch>` 覆盖。

`finish` 在主检出（Workspace 之外）执行，使用相同的目标规则完成 apply，只有合并成功后才开始清理：删除 worktree、任务元数据，以及在记录所有权时删除托管分支。

两条命令在执行任何改动之前完成全部前置校验，任一不满足即停止并返回结构化的阻塞原因：

- 不在 Workspace 内（apply）/ 仍在被移除的 Workspace 内（finish）；
- Workspace 状态不是 `ready`；
- detached review/test Workspace；
- Workspace worktree dirty、缺失或未注册；
- 无目标：基线是纯 SHA 且未给 `--into`；
- 源分支与目标分支相同；
- 主检出处于 detached HEAD，或不在目标分支上（绝不自动切换）；
- 主检出存在未提交修改；
- 合并冲突——冲突保留在主检出中供常规 Git 解决或 `git merge --abort`，Workspace 不被清理。

阻塞错误包含 `workspace_id`、`source_branch` 结构化详情，以及指明具体 Git 命令或 `--into` 备选的可操作消息。

### 4.5 删除与恢复

```text
gws rm WORKSPACE [--force]
gws gc [--dry-run]
gws doctor
```

普通删除拒绝包含未提交修改的 worktree，并在存在所有权记录时删除托管分支。`--force` 只跳过 dirty 检查，绝不跳过规范化路径与所有权校验。

垃圾回收只作用于 Git Workspace 所有权可被证明的资源；未知资源只报告、不删除；`gc` 从不删除分支。`--dry-run` 是推荐的第一步。

`doctor` 对当前仓库自检：Git 可用性、配置文件、状态目录、Workspace/worktree 一致性。

## 5. 解析规则

仓库解析：对 `cwd` 执行 `git rev-parse --show-toplevel` 与 `--git-common-dir`。没有选择器、没有注册表、没有跨仓库隐式搜索。

Workspace 解析接受精确 ID、精确名称，以及在显式启用时的无歧义 ID 前缀。歧义是错误，其结构化详情包含候选 Workspace。

## 6. 状态与配置

全部状态位于仓库内：

```text
<repo>/.git/gws/
├── config.json          可选，{"version":1,"default_branch":"main"}
├── workspaces/<id>.json
├── tasks/<id>.md
└── locks/state.lock

<parent>/<repo>-gws/<id>/    worktree 检出目录（同级目录）
```

元数据挂在 Git common 目录下，因此所有 linked worktree 共享一份状态；克隆仓库不会带走任何 `gws` 数据。`config.json` 可选且永不自动生成，唯一字段是兜底的 `default_branch`（HEAD detached 且未给 `--base` 时使用）。

持久化 Workspace 记录携带 `state_version: 1`。无版本的旧记录按版本 1 解释；更高版本的记录以 `state_version_unsupported` 拒绝，不会被覆盖。

所有状态文件使用同目录原子替换。变更持有仓库级排他建议锁；读取持有共享锁。锁获取有界且感知取消。

## 7. 安全不变量

Git Workspace 绝不能：

- 递归删除用户主目录、仓库根、仓库父目录、状态根或 worktree 根；
- 删除规范化配置的 worktree 根之外的目录；
- 删除 Workspace 所有权无法证明的路径；
- 删除非托管的用户分支；
- 在 apply/finish 期间自动切换主检出的分支；
- 用成功的清理掩盖失败的 apply；
- 在破坏性操作依赖真实 Git/文件系统状态时盲信持久化状态。

创建时在外部副作用之前落盘元数据，并按逆序回滚。回滚不完整时留下带诊断信息的 `error` 元数据。

## 8. 平台/插件协议

插件边界是可执行文件进程，使核心与平台的语言和运行时解耦。

### 8.1 发现

```bash
gws capabilities --json
```

响应声明 `protocol_version: 2`、后端 `git-worktree`、特性列表与仅交互命令（`use`，以及不带 `--json`/`--no-enter` 的 `new`）。`capabilities` 可以在仓库外运行。

### 8.2 JSON 信封

所有非交互生命周期与检查命令接受 `--json`：

```json
{
  "schema_version": 2,
  "command": "path",
  "data": {
    "workspace_id": "avatar-8a10",
    "path": "/home/user/projects/example-gws/avatar-8a10"
  },
  "error": null
}
```

失败时向 stdout 写入一个 JSON 信封、stderr 无输出、退出码非零。`data` 为 null，`error` 包含稳定的 `code`、`message`、`details` 字段。Workspace 对象暴露 `source.base_ref`、`source.base_commit`、`detached`、`managed`、`from_ref`。

人类可读输出不是插件 API，可以继续演化。

`use` 是交互命令，拒绝 `--json`。平台应调用 `path --json` 拿到目录后自行启动进程/容器/Agent。`new --json` 只创建、不进入 Shell。

### 8.3 所有权切分

```text
平台任务/运行时 ID
        |
        v
Workspace ID ----> worktree 路径
        |                         |
        |                         +--> 平台运行时/资源
        +--> 分支 + 元数据
```

平台把 Workspace ID 作为外键保存。Git Workspace 不保存平台运行时 ID。平台必须在请求 `finish`、`rm` 或破坏性 GC 之前完成运行时拆除。

## 9. 当前完成标准

- 所有帮助、提示、任务文件、分支、配置默认值、二进制路径与文档统一使用 Git Workspace 命名。
- 每个 Workspace 都记录并展示基线 ref 与 base commit。
- `new` 不受同名用户分支影响。
- `apply` 保留 Workspace；`finish` 只在成功后清理；两者都不自动切换主检出分支。
- dirty 路径与非托管路径受到删除保护。
- 人类检查输出与 JSON Schema v2 暴露 Workspace、分支、基线、状态、worktree、任务数据。
- `capabilities --json` 暴露插件协议版本 2。
- 单元、集成、race、vet、构建检查全部通过。
