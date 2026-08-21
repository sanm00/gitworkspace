# Git Workspace 实现规范

> 状态：实现基线
> 产品：Git Workspace
> 可执行文件：`gws`
> 插件后端 ID：`git-worktree`
> 英文版本：`GIT_WORKSPACE_IMPLEMENTATION_SPEC.md`

## 1. 产品定义

Git Workspace 是一个有明确策略的 Git worktree 管理器，将开发工作项转换为具名、可检查、可安全清理的 Workspace。

其价值主张是：

> 在 Git worktree 之上提供明确的工作项模型、自动分支策略、安全清理和显式的 apply/finish 成果处理流程。

Git Workspace 同时是：

1. 面向开发者并行工作目录的独立 CLI；
2. 开发环境隔离平台可调用的源码工作空间插件，平台随后可以配置运行时、端口、容器、工具或 Agent。

Git Workspace 本身不是完整的开发环境隔离平台。

## 2. 产品边界

Git Workspace 负责：

- 仓库注册和当前 Project 解析；
- 工作项身份和 Workspace 元数据；
- Git worktree 创建与删除；
- 自动生成无冲突的托管分支；
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

### 3.1 Project

Project 注册一个规范化 Git common directory 及其原始检出目录，包含稳定 ID、展示名称、仓库路径、Git common directory、默认分支、配置路径和时间戳。

属于同一个 Git common directory 的路径属于同一个 Project。`init` 必须幂等，不能重复注册。

### 3.2 工作项

工作项是表示用户意图的可选易读标签，例如 `feature/avatar` 或 `issue-328`。它不是 Git 分支名，不能与分支是否存在耦合。`gws new` 省略名称时，使用 `workspace` 作为自动生成的标签。

规范化后的工作项名称保存在 Workspace 元数据中。路径段不能丢失：`feature/avatar` 应转换为 `feature-avatar`，而不是 `avatar`。

### 3.3 Workspace

Workspace 是一条托管记录，关联：

- 一个 Project；
- 一个工作项和稳定 Workspace ID；
- 一个 Git worktree 路径；
- 一个源 ref 及可选的 base/from ref；
- 一个任务文件；
- 类型、所有权、生命周期状态、错误和时间戳。

生命周期状态为 `creating`、`ready`、`error`、`removing`。

Workspace 类型为 `dev`、`fix`、`review`、`test`。Review 和 test Workspace 使用 detached HEAD，不能 apply。

## 4. 主要工作流

### 4.1 注册

```bash
gws init [--name NAME] [--default-branch BRANCH]
```

`init` 始终把 `cwd` 本身作为 Project 根目录。如果该目录本身还不是 Git 仓库根目录，命令会先在这里初始化仓库；即使 `cwd` 位于父 Git 仓库内也是如此。存在配置或显式指定的默认分支时，将其作为初始分支。Project 名称不添加前后缀；新 Project 的 ID 使用名称 slug 加随机后缀，避免重名和并发注册冲突。对同一 Git common directory 重复执行时必须返回相同 Project ID，只更新显式参数或配置指定的可变字段。

使用 `gws project ls`、`gws project show` 和 `gws project rm` 检查或删除项目注册。删除 Project 前必须确认它没有 Workspace，并且只删除注册信息。

### 4.2 创建

```bash
gws new [NAME] [--from REF]
```

`new` 默认从当前分支创建 dev Workspace，`--from` 可以覆盖起点。命令分配带 `gws/` 前缀的唯一分支，并标记为 Git Workspace 托管。

Project 始终以 `cwd` 为根目录解析；`new` 不接受 `--project`。如果 `cwd` 本身还不是仓库根目录或尚未注册，`new` 会自动完成初始化和注册。仓库没有 commit 时，`new` 在创建 worktree 前自动创建空的 root commit；该操作必须保留现有暂存区，且不得把 staged 内容写入自动生成的 commit。

`NAME` 是工作项标签，因此同名用户分支不能阻塞 `new`。在 detached `cwd` 中必须显式提供 `--from`。

多个 Workspace 可以使用相同工作项名称，但 ID、分支和路径必须唯一；名称 selector 有歧义时必须使用 Workspace ID。

保留高级显式 ref 入口：

```text
gws dev BRANCH [--new] [--from REF]
gws fix BRANCH [--new] [--from REF]
gws review BRANCH [--base REF] [--fetch]
gws test BRANCH [--base REF]
```

只有 `review --fetch` 可以隐式执行网络 fetch。

### 4.3 进入

```bash
gws checkout [WORKSPACE]
```

`checkout` 以 worktree 为工作目录启动用户 Shell，不能修改父 Shell 的目录。启动子 Shell 前必须释放状态锁。

### 4.4 检查

```text
gws ls [--project PROJECT]
gws show [WORKSPACE]
gws status [WORKSPACE]
gws path [WORKSPACE]
```

`show` 展示持久化元数据；`status` 还必须验证文件系统、Git worktree 注册状态、dirty 状态和任务文件是否存在。工作树缺失时，JSON 中的 `git.dirty` 为 `null`；Git 状态检查失败时必须返回错误，不得把失败报告为 clean。

在托管 worktree 中可以省略 Workspace selector，由 `cwd` 解析。

### 4.5 应用和结束

```text
gws apply [WORKSPACE]
gws finish WORKSPACE
```

`apply` 将干净、已提交、attached 的 Workspace 分支合并到注册原始检出目录当前所在分支，并保留 Workspace。

未指定 Workspace selector 时，`apply` 优先选择包含 `cwd` 的 Workspace。如果 `cwd` 不在 Workspace 内，则只在从 `cwd` 解析出的 Project 中查找，并且仅当恰好存在一个 ready、attached Workspace 时自动选择。存在多个候选时必须返回歧义错误，错误消息和结构化 details 都要包含候选 ID。不得隐式选择其他 Project 的 Workspace。

`finish` 执行相同 apply，只有合并成功后才开始清理。它删除 worktree 和任务元数据，仅在元数据明确标记为 Git Workspace 托管时删除源分支。

两个命令都拒绝：

- 包含未提交修改的 Workspace；
- detached review/test Workspace；
- dirty 或 detached 的原始检出目录；
- 从待删除 Workspace 内执行 `finish`。

发生合并冲突时，在原始检出目录保留 Git 冲突状态，用户可正常解决或执行 `git merge --abort`；Workspace 必须保留。

### 4.6 删除与恢复

```text
gws rm WORKSPACE [--project PROJECT] [--force]
gws gc [--dry-run]
gws doctor [--project PROJECT]
```

普通删除拒绝 dirty worktree。`--force` 只跳过 dirty 检查，不能跳过规范路径和所有权验证。

未提供 `--project` 时，`rm` 必须先从 `cwd` 解析 Project，并且只查找该 Project 的 Workspace。只有显式提供 `--project` 才允许删除其他已注册仓库的 Workspace。

GC 只能操作能够证明属于 Git Workspace 的资源。未知资源只报告、不删除。建议始终先运行 `--dry-run`。

## 5. 解析规则

Project 解析顺序：

1. 显式 `--project`；
2. 包含 `cwd` 的 Workspace；
3. 与 `cwd` Git common directory 相同的已注册 Project；
4. 否则返回解析错误，提示运行 `gws init` 或使用 `--project`。

不能因为仅注册了一个 Project 就自动选择它。

`new` 比通用规则更严格：始终使用 `cwd`，并拒绝 `--project`。`rm` 默认使用 `cwd`，仅将 `--project` 作为显式跨仓库操作入口。

Workspace 可通过精确 ID、精确名称，以及在命令明确允许时通过无歧义 ID 前缀解析。歧义必须报错。

## 6. 状态与配置

默认状态根目录：

```text
~/.git-workspace/
├── projects/
├── workspaces/
├── worktrees/
├── tasks/
└── locks/state.lock
```

`GIT_WORKSPACE_HOME` 可覆盖该路径。

仓库配置是可选的，只从注册的原始检出目录读取：

```yaml
version: 1

project:
  name: example
  default_branch: main
```

不接受 agent、editor 或 runtime 配置段。Project 字段在 `init` 时导入，其他命令不会持续用配置隐式修改注册信息。

持久化的 Project 和 Workspace 记录包含 `state_version: 1`。没有版本字段的旧记录按版本 1 解释；版本高于当前程序支持范围时返回 `state_version_unsupported`，并且不得覆盖该记录。

所有状态文件使用同目录原子替换。状态变更持有全局独占 advisory lock，读取持有共享锁。锁等待必须有时限且支持取消。

## 7. 安全不变量

Git Workspace 绝不能：

- 递归删除用户主目录、仓库根目录、状态根目录或 worktree 根目录；
- 删除规范化 worktree 根目录之外的目录；
- 删除无法证明 Workspace 所有权的路径；
- 删除不属于 Git Workspace 的用户分支；
- 用成功清理掩盖 apply 失败；
- 在破坏性操作依赖真实状态时仅相信持久化元数据。

创建流程必须先记录元数据，再执行外部副作用，并按相反顺序回滚。无法完整回滚时保留 `error` 元数据和诊断信息。

## 8. 平台插件协议

插件边界是可执行子进程，使核心不依赖上层平台语言和运行时。

### 8.1 能力发现

```bash
gws capabilities --json
```

响应声明 `protocol_version: 1`、后端 `git-worktree`、功能列表和仅交互命令。

### 8.2 JSON 信封

所有非交互生命周期和查询命令都支持 `--json`：

```json
{
  "schema_version": 2,
  "command": "path",
  "data": {
    "workspace_id": "avatar-8a10",
    "path": "/home/user/.git-workspace/worktrees/example/avatar-8a10"
  },
  "error": null
}
```

失败时向 stdout 输出一个 JSON 信封，stderr 为空，并以非零状态退出。此时 `data` 为 null，`error` 包含稳定的 `code`、`message`、`details` 字段。

人类可读输出不是插件 API，可以演进。

`checkout` 是交互命令，拒绝 `--json`。平台应调用 `path --json`，然后在返回目录中自行启动进程、容器或 Agent。

### 8.3 所有权划分

```text
平台 Task/Runtime ID
        |
        v
Workspace ID ----> worktree path
        |                         |
        |                         +--> 平台运行时及资源
        +--> 分支和元数据
```

平台把 Workspace ID 保存为外键。Git Workspace 不保存平台 Runtime ID。平台必须先停止运行时，再请求 `finish`、`rm` 或破坏性 GC。

## 9. 当前完成标准

- 帮助、提示、任务文件、分支、配置默认值、二进制路径和文档全部使用 Git Workspace 命名；
- `new` 不受同名用户分支影响；
- `apply` 保留 Workspace，`finish` 仅在成功后清理；
- dirty 和不明所有权路径不会被删除；
- 人类输出和 JSON Schema v2 暴露 Project、Workspace、分支、base/from、状态、worktree 和任务信息；
- `capabilities --json` 暴露插件协议版本 1；
- 单元、集成、race、vet 和 build 检查通过。
