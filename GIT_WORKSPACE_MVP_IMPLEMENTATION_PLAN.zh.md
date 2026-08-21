# Git Workspace MVP 实现计划

> 产品：Git Workspace
> 定位：工作项驱动的 Git worktree 管理器、开发环境隔离平台的源码工作空间插件
> 规范：`GIT_WORKSPACE_IMPLEMENTATION_SPEC.zh.md`

## 1. MVP 闭环

MVP 必须让用户不需要理解 Git worktree 的分支占用和目录管理细节，就能完成：

```bash
gws init
gws new feature/avatar
gws checkout feature-avatar
# 修改、测试、提交
gws apply
# 或在退出 Workspace 后
gws finish feature-avatar
```

同时，上层平台必须能够非交互地完成相同的源码工作空间生命周期：

```bash
gws capabilities --json
gws new feature/avatar --json
gws path feature-avatar --json
gws status feature-avatar --json
gws finish feature-avatar --json
```

## 2. MVP 功能范围

### P0：必须完整

- 幂等仓库注册；
- 从当前分支创建工作项 Workspace；
- `new` 仅从当前目录解析仓库，保留 `--from`，不提供 `--project`；
- 唯一托管分支，不受同名用户分支影响；
- Workspace 名称、ID、仓库、分支、起点和状态可查询；
- 子 Shell 进入工作空间；
- dirty 检查和安全删除；
- `rm` 默认限定当前仓库，只有显式 `--project` 才允许跨仓库删除；
- `apply`、`finish` 和失败保留；
- 并发锁、创建回滚和 GC dry-run；
- 所有非交互命令的 JSON Schema v2；
- 能力发现协议。

### P1：保留但不是主入口

- 显式分支 `dev`、`fix`；
- detached `review`、`test`；
- `review --fetch`；
- 任务说明文件；
- Project 多仓库注册和 selector。

### 明确不做

- 启动或配置 Agent、编辑器；
- 管理 Docker Compose 或 devcontainer；
- 分配端口、数据库和网络；
- 监管宿主机进程；
- 安装依赖或执行项目 hook；
- 提供桌面 UI；
- 冒充完整开发环境隔离平台。

## 3. 架构

```text
cmd/gws
      |
      v
internal/cli  -------- JSON protocol v1 / Schema v2
      |
      v
internal/gitworkspace
  |-- service       工作流和安全策略
  |-- git           Git 子进程适配
  |-- storage       JSON 状态与原子写
  |-- lock          跨进程读写锁
  |-- config        .git-workspace.yaml
  |-- task          任务上下文
  +-- paths         状态目录
```

CLI 只负责参数、展示和 JSON 信封。业务规则必须位于核心 service，避免人类 CLI 和平台插件接口产生两套行为。

## 4. 数据与身份

Project 用规范化 `GitCommonDir` 去重。Workspace ID 由规范化工作项名称加随机短后缀组成，例如 `feature-avatar-8a10`。

`new` 使用独立的托管分支：

```text
gws/<workspace-id>
```

Workspace 身份不能使用分支名，因为同一工作项可以创建多个隔离 Workspace。只有 `Source.Managed=true` 的分支允许自动删除。

状态根目录：

```text
~/.git-workspace/
├── projects/<project-id>.json
├── workspaces/<workspace-id>.json
├── worktrees/<project-id>/<workspace-id>/
├── tasks/<workspace-id>.md
└── locks/state.lock
```

## 5. 关键流程

### 5.1 创建

1. 获取全局独占锁；
2. 解析 Project、工作项和起始 ref；
3. 分配唯一 Workspace ID、托管分支和路径；
4. 写入 `creating` 元数据；
5. 创建 Git worktree；
6. 写任务文件；
7. 更新为 `ready`；
8. 失败时按“任务文件、worktree、托管分支”逆序回滚；
9. 无法完整回滚时保留 `error` 元数据。

### 5.2 应用

1. 获取独占锁；
2. 解析并验证 attached Workspace；
3. 确认 Workspace 无未提交修改；
4. 确认原始检出目录为干净 attached 分支；
5. 将 Workspace 源分支 merge 到目标分支；
6. 返回源、目标、Project 和 Workspace 信息；
7. `apply` 到此结束；
8. `finish` 仅在成功后继续安全删除和托管分支清理。

### 5.3 删除

1. 解析真实 worktree 和规范路径；
2. 确认目标严格位于 Git Workspace worktree 根目录之下；
3. 确认 Git 注册和 Workspace 元数据所有权；
4. 非 `--force` 模式拒绝 dirty worktree；
5. 先写 `removing` 状态；
6. 通过 `git worktree remove` 删除；
7. 删除任务和 Workspace 元数据；
8. 仅在明确托管时删除分支。

## 6. 插件协议

插件协议版本与 JSON 数据 Schema 分开演进：

- `protocol_version: 1` 表示调用约定；
- `schema_version: 2` 表示 JSON 信封和字段结构；
- 错误码稳定，错误自然语言允许优化；
- 人类输出不作为平台解析接口；
- `checkout` 不属于机器接口；
- 平台必须把 Workspace ID 保存为外键；
- 平台在删除源码 Workspace 前必须先停止自己拥有的运行时资源。

需要固定测试的 JSON 命令：

```text
capabilities
init
project ls|show|rm
new
dev|fix|review|test
ls|show|status|path
apply|finish
rm
gc
doctor
```

## 7. 命名约束

1. 二进制固定为 `gws`；
2. Go 模块和内部包使用 `gws`；
3. 环境变量只使用 `GIT_WORKSPACE_HOME`；
4. 状态目录只使用 `~/.git-workspace`；
5. 项目配置只读取 `.git-workspace.yaml`；
6. 托管分支前缀固定为 `gws/`；
7. 不探测、不读取、不迁移其他品牌的环境变量、状态目录或配置文件。

## 8. 实施阶段

### 阶段 1：产品和命名收敛

- [x] 产品更名为 Git Workspace；
- [x] CLI、模块、包、分支前缀和任务文本更名；
- [x] README 和中英文规范重写；
- [x] 删除 Agent/编辑器/运行时的平台化承诺。

### 阶段 2：核心 Workspace 生命周期

- [x] Project 注册和解析；
- [x] `new` 自动托管分支；
- [x] `checkout` 子 Shell；
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

## 9. MVP 验收

- [x] `go test ./...` 通过；
- [x] `go test -race ./...` 通过；
- [x] `go vet ./...` 通过；
- [x] `make build` 生成 `bin/gws`；
- [x] 构建产物和运行时提示只使用 Git Workspace 品牌；
- [x] 相同工作项可创建多个唯一 Workspace；
- [x] 同名用户分支不影响 `new`；
- [x] dirty Workspace 默认不能删除或 apply；
- [x] apply 失败不会触发 finish 清理；
- [x] 平台可通过 JSON 完成创建、解析路径、检查、应用和删除闭环。

## 10. MVP 之后的合理方向

后续增强仍应围绕“worktree 生命周期插件”，例如 setup/teardown hook、PR source adapter、合并策略、事件流和更细粒度锁。

Docker、端口、数据库和进程隔离应由独立的上层平台或其他插件实现，通过 Workspace ID 和 worktree path 与 Git Workspace 组合，而不是重新塞回 Git Workspace 核心。
