# Git Workspace MVP 实现计划

> 产品：Git Workspace
> 定位：基于当前分支一步切出隔离工作空间的 Git worktree 管理器、开发环境隔离平台的源码工作空间插件
> 规范：`GIT_WORKSPACE_IMPLEMENTATION_SPEC.zh.md`

## 1. MVP 闭环

MVP 必须让用户不需要理解 Git worktree 的分支占用和目录管理细节，就能完成：

```bash
gws new                      # 基于当前分支创建并直接进入
# 修改、测试、提交
gws apply                    # 在 Workspace 内合并回基线分支
# 或在退出 Workspace 后
gws finish <workspace-id>
```

以及排查场景的第二入口：

```bash
gws use <workspace-id>       # 再次进入已有 Workspace
```

同时，上层平台必须能够非交互地完成相同的源码工作空间生命周期：

```bash
gws capabilities --json
gws new issue-328 --no-enter --json
gws path issue-328 --json
gws status issue-328 --json
gws finish issue-328 --json
```

## 2. MVP 功能范围

### P0：必须完整

- 仓库即作用域：状态存于 `<repo>/.git/gws/`，无注册表、无全局 home；
- `new` 无参可用：默认基线 = 当前分支，`--base <branch|sha>` 覆盖，detached HEAD 用 `config.json` 的 `default_branch` 兜底；
- 所有类型记录并展示基线 ref + base commit；
- 唯一托管分支（`gws/<name>`），不受同名用户分支影响；
- 创建后默认进入子 Shell，`--json`/`--no-enter`/非 TTY 只创建；
- `use` 进入已有 Workspace（ID/名称/无歧义前缀/按 cwd）；
- Workspace 名称、ID、分支、基线、状态可查询；
- 常用 Git 操作封装：`diff`（相对基线 / `--uncommitted` / `--staged` / `--stat` + 未跟踪文件列表）、`log`（`-n`、`--stat`）、`commit`（`-m`、`-a`，拒绝 detached 空间）；
- dirty 检查和安全删除；
- `apply` 必须在 Workspace 内，目标默认基线分支、`--into` 覆盖；先全量校验、阻塞即止、绝不自动切换主检出；
- `finish` 在主检出处执行，成功后才清理；
- 并发锁、创建回滚和 GC dry-run；
- 所有非交互命令的 JSON Schema v2；
- 能力发现协议（protocol v2）。

### P1：保留但不是主入口

- `--type dev|fix|review|test` 与 `--detach`；
- `review/test --fetch`；
- 任务说明文件（含 Base/Base commit）；
- `gc`/`doctor` 自检。

### 明确不做

- 启动或配置 Agent、编辑器；
- 管理 Docker Compose 或 devcontainer；
- 分配端口、数据库和网络；
- 监管宿主机进程；
- 安装依赖或执行项目 hook；
- 提供桌面 UI；
- 冒充完整开发环境隔离平台；
- Project 注册表与 `--project` selector（已移除）；
- 直接在已存在用户分支上开 worktree（Git 限制）。

## 3. 架构

```text
cmd/gws
      |
      v
internal/cli  -------- JSON protocol v2 / Schema v2
      |
      v
internal/gitworkspace
  |-- service       工作流、基线解析、阻塞校验和安全策略
  |-- git           Git 子进程适配（worktree/main worktree/ref 解析）
  |-- storage       JSON 状态与原子写
  |-- lock          跨进程读写锁
  |-- config        .git/gws/config.json
  |-- task          任务上下文
  +-- paths         仓库内状态目录 + 同级 worktree 目录
```

CLI 只负责参数、展示和 JSON 信封。业务规则必须位于核心 service，避免人类 CLI 和平台插件接口产生两套行为。

## 4. 数据与身份

仓库身份 = 规范化 `GitCommonDir`。Workspace ID 由规范化名称（或基线 slug）加随机短后缀组成，例如 `feature-x-9f2c`。

开发型 Workspace 使用独立的托管分支：

```text
gws/<name>            碰撞时追加随机后缀
```

Workspace 身份不能使用分支名，因为同一基线可以创建多个隔离 Workspace。只有 `Source.Managed=true` 的分支允许自动删除。

状态布局：

```text
<repo>/.git/gws/
├── config.json                 可选 {"version":1,"default_branch":"main"}
├── workspaces/<workspace-id>.json
├── tasks/<workspace-id>.md
└── locks/state.lock

<parent>/<repo-name>-gws/<workspace-id>/    worktree 检出目录
```

元数据挂在 common dir 下，所有 linked worktree 共享；worktree 放在仓库同级目录，避免嵌套进 `.git` 或污染工作区。克隆仓库不携带任何 gws 数据。

## 5. 关键流程

### 5.1 创建

1. 获取仓库级独占锁；
2. 解析基线（`--base` → 当前分支 → `config.default_branch` → 报错）并解析 base commit；
3. 分配唯一 Workspace ID、托管分支和路径；
4. 写入 `creating` 元数据；
5. 创建 Git worktree；
6. 写任务文件（含 Base 与 Base commit）；
7. 更新为 `ready`；
8. 失败时按“任务文件、worktree、托管分支”逆序回滚；
9. 无法完整回滚时保留 `error` 元数据；
10. 依 TTY/旗标决定是否进入子 Shell。

### 5.2 应用

1. 获取独占锁；
2. 由 `cwd` 解析 Workspace（不在 Workspace 内即阻塞）；
3. 全量前置校验：状态 `ready`、非 detached、worktree 干净且注册、目标可确定（基线非纯 SHA 或有 `--into`）、源≠目标、主检出附着且在目标分支上、主检出干净；任一失败 → 返回结构化阻塞原因，不做任何改动；
4. 将 Workspace 源分支 merge 到目标分支；冲突则保留冲突现场、Workspace 不清理；
5. 返回源、目标、仓库根和 Workspace 信息；`apply` 到此结束；
6. `finish` 仅在成功后继续安全删除与托管分支清理。

### 5.3 删除

1. 解析真实 worktree 和规范路径；
2. 确认目标严格位于本仓库 worktree 根之下，且不等于仓库根/父目录/worktree 根；
3. 确认 Git 注册和 Workspace 元数据所有权；
4. 非 `--force` 模式拒绝 dirty worktree；
5. 先写 `removing` 状态；
6. 通过 `git worktree remove` 删除；
7. 删除任务和 Workspace 元数据；
8. 仅在明确托管时删除分支。

## 6. 插件协议

插件协议版本与 JSON 数据 Schema 分开演进：

- `protocol_version: 2` 表示调用约定（命令面重构）；
- `schema_version: 2` 表示 JSON 信封和字段结构（新增 `source.base_commit`）；
- 错误码稳定，错误自然语言允许优化；
- 人类输出不作为平台解析接口；
- `use` 不属于机器接口；`new --json` 只创建不进入；
- 平台必须把 Workspace ID 保存为外键；
- 平台在删除源码 Workspace 前必须先停止自己拥有的运行时资源。

需要固定测试的 JSON 命令：

```text
capabilities
new
ls|show|status|path
apply|finish
rm
gc
doctor
```

## 7. 命名约束

1. 二进制固定为 `gws`；
2. Go 模块和内部包使用 `gws`；
3. 状态目录固定 `<repo>/.git/gws/`，无环境变量覆盖；
4. worktree 根固定 `<parent>/<repo>-gws/`；
5. 项目配置只读取 `.git/gws/config.json`；
6. 托管分支前缀固定为 `gws/`；
7. 不探测、不读取、不迁移其他品牌的环境变量、状态目录或配置文件；旧 `~/.git-workspace` 与 `.git-workspace.yaml` 不迁移（pre-1.0）。

## 8. 实施阶段

### 阶段 1：产品和命名收敛

- [x] 产品更名为 Git Workspace；
- [x] CLI、模块、包、分支前缀和任务文本更名；
- [x] README 和中英文规范重写；
- [x] 删除 Agent/编辑器/运行时的平台化承诺。

### 阶段 2：核心 Workspace 生命周期

- [x] Project 注册和解析（后续已被移除，见阶段 5）；
- [x] `new` 自动托管分支；
- [x] 子 Shell 进入（原 `checkout`，后被 `use` 取代）；
- [x] `apply` 和 `finish`；
- [x] 安全 `rm` 和 `gc`；
- [x] `ls/show/status/path/doctor`。

### 阶段 3：平台插件接口

- [x] `capabilities --json`；
- [x] 非交互命令 JSON 输出；
- [x] 稳定错误信封；
- [x] `path --json` 作为运行时交接点；
- [ ] 为所有成功 JSON 输出增加字段级 golden tests；
- [ ] 发布插件协议兼容性测试工具。

### 阶段 4：可靠性

- [x] 跨进程锁；
- [x] 原子状态写入；
- [x] 创建失败回滚；
- [x] dirty 和路径所有权保护；
- [ ] 故障注入覆盖每个创建/删除步骤；
- [ ] Git Workspace 后续版本的迁移测试夹具。

### 阶段 5：极简化重构（本轮）

- [x] 移除 Project 注册表：`init`、`project ls|show|rm`、`--project`、`GIT_WORKSPACE_HOME`；
- [x] 状态迁入 `<repo>/.git/gws/`，worktree 改为同级 `<parent>/<repo>-gws/<id>/`；
- [x] 配置改读 `.git/gws/config.json`（JSON，替换 YAML）；
- [x] `dev/review/fix/test` 并入 `new --type/--detach`；`checkout` 改为 `use`；
- [x] `new` 无参即用：默认基线 = 当前分支，创建后默认进入子 Shell；
- [x] 全类型记录 base ref + base commit，检查输出与任务文件展示基线；
- [x] `apply/finish` 目标 = 基线分支，`--into` 覆盖；先校验后执行，阻塞原因结构化、可操作；取消自动切换主检出；
- [x] `--force` 收敛为 `rm` 专属布尔旗标；
- [x] 协议升为 version 2。

## 9. MVP 验收

- [x] `go test ./...` 通过；
- [x] `go test -race ./...` 通过；
- [x] `go vet ./...` 通过；
- [x] `make build` 生成 `bin/gws`；
- [x] 构建产物和运行时提示只使用 Git Workspace 品牌；
- [x] 相同基线可创建多个唯一 Workspace；
- [x] 同名用户分支不影响 `new`；
- [x] dirty Workspace 默认不能删除或 apply；
- [x] apply 阻塞或失败不会触发 finish 清理；
- [x] 每个 Workspace 展示基线 ref 与 commit；
- [x] 平台可通过 JSON 完成创建、解析路径、检查、应用和删除闭环。

## 10. MVP 之后的合理方向

后续增强仍应围绕“worktree 生命周期插件”，例如 setup/teardown hook、PR source adapter、合并策略、事件流和更细粒度锁。

Docker、端口、数据库和进程隔离应由独立的上层平台或其他插件实现，通过 Workspace ID 和 worktree path 与 Git Workspace 组合，而不是重新塞回 Git Workspace 核心。
