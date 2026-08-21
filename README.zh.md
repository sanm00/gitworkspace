# Git Workspace

[![CI](https://github.com/sanm00/gitworkspace/actions/workflows/ci.yml/badge.svg)](https://github.com/sanm00/gitworkspace/actions/workflows/ci.yml)
[![Security](https://github.com/sanm00/gitworkspace/actions/workflows/security.yml/badge.svg)](https://github.com/sanm00/gitworkspace/actions/workflows/security.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md)

Git Workspace 是一个具有明确工作项模型、自动分支策略、安全清理和成果应用流程的 Git worktree 管理器。

它既可以独立使用，也可以作为更大的开发环境隔离平台中的“源码工作空间插件”。

> **项目状态：** 当前处于 1.0 之前。项目会谨慎维护 CLI 和 JSON 兼容性，但仍可能在附带迁移说明的版本中引入不兼容调整。

## 产品边界

Git Workspace 只负责源码工作空间的生命周期：

```text
工作项 -> 托管分支 -> Git worktree -> apply 或 finish
```

Git Workspace 不启动编程智能体、编辑器、容器、数据库、宿主机进程或网络服务。上层平台可以解析托管 Workspace，再围绕其 worktree 路径配置这些资源。

文件隔离能力来自 Git worktree。Git Workspace 增加命名、元数据、并发锁、失败回滚、所有权检查、诊断和生命周期策略，但不宣称提供进程、端口、依赖、凭据或运行时隔离。

## 安装

使用 Go 安装最新标签版本：

```bash
go install github.com/sanm00/gitworkspace/cmd/gws@latest
gws --version
```

GitHub Releases 同时提供 macOS、Linux 的 amd64 和 arm64 压缩包。安装前应使用 `SHA256SUMS` 校验下载文件。

## 构建

需要 Go 1.25+、Git，以及 macOS 或 Linux。

```bash
make test
make lint
make build
./bin/gws --help
```

本地构建会自动读取仓库根目录的 `VERSION` 并注入二进制。只有测试其他构建标识时才需要显式覆盖：

```bash
make build VERSION=v0.1.0-test
```

## 快速开始

按需初始化 Git，并将当前目录注册为 Project：

```bash
cd ~/projects/example
gws init --name example --default-branch main
```

`init` 始终把当前目录作为 Project 根目录。如果当前目录本身还不是 Git 仓库根目录，就先在这里执行 `git init`；即使当前目录位于父 Git 仓库内也是如此。通过配置或命令行显式指定的默认分支会作为初始分支。

从当前分支创建 Workspace 并进入子 Shell。工作项名称可以省略：

```bash
gws new feature/avatar
gws checkout feature-avatar
```

直接执行 `gws new` 时使用 `workspace` 作为自动生成的标签；随机化的 Workspace ID 和托管分支仍保持唯一。

`new` 会创建全局唯一的 Git Workspace 托管分支。工作项名称仅是可选的易读标签，不是 Git 分支名，因此仓库中已经存在 `feature/avatar` 分支也不会冲突。

同一个工作项名称可以创建多个 Workspace；名称产生歧义时使用命令输出的 Workspace ID。

`new` 始终把当前目录作为项目根目录，不接受 `--project`；当前目录尚未成为 Git 仓库或尚未注册时会自动完成初始化和注册。仓库没有 commit 时，`new` 会自动创建一个空的首次提交，并且不会提交或清空当前暂存区内容。需要从当前分支以外的 ref 创建时，使用 `--from REF`。

在 Workspace 中提交修改，然后选择成果处理方式：

```bash
gws apply
```

`apply` 将 Workspace 中已提交的分支合并到注册原始仓库当前所在分支，并保留 Workspace。未指定 Workspace 时，优先使用包含当前目录的 Workspace；否则只在当前 Project 中选择唯一可应用的 Workspace。存在多个候选时返回错误并列出它们的 ID。

```bash
exit
gws finish feature-avatar
```

`finish` 先应用成果，只有合并成功后才删除 worktree 和托管分支。

## 核心命令

```text
gws init
gws new
gws checkout
gws apply
gws finish
gws ls|show|status|path
gws rm
gws project ls|show|rm
gws gc
gws doctor
gws capabilities
```

高级命令 `dev`、`review`、`fix`、`test` 用于接入显式分支或创建 detached 检查工作空间。

## 安全模型

- 状态变更由跨进程锁串行化；
- 创建时先记录 `creating` 状态，失败时回滚部分资源；
- 普通删除拒绝包含未提交修改的 worktree；
- `gws rm` 默认只能删除当前仓库的 Workspace，显式提供 `--project` 时才可选择其他仓库；
- `--force` 只跳过 dirty 检查，不跳过路径所有权检查；
- `apply` 失败时，`finish` 不执行清理；
- 只有明确标记为 Git Workspace 托管的分支才会自动删除；
- `gc --dry-run` 可预览回收动作。

## 平台插件接口

插件边界是 `gws` 子进程。平台应先发现能力，而不是假定功能：

```bash
gws capabilities --json
```

所有非交互生命周期和查询命令都支持 `--json`，并输出带版本号的信封结构：

```bash
gws new issue-328 --json
gws show issue-328 --json
gws path issue-328 --json
gws finish issue-328 --json
```

`checkout` 是面向人的交互命令，故意不支持 JSON。上层平台应调用 `path --json` 获取工作目录，再自行启动运行时。具体契约见 [GIT_WORKSPACE_IMPLEMENTATION_SPEC.zh.md](GIT_WORKSPACE_IMPLEMENTATION_SPEC.zh.md)。

`internal/` 下的包不是公共 Go API。受支持的集成入口是带版本的 `gws --json` 子进程协议。

## 配置与状态

仓库可选配置文件为 `.git-workspace.yaml`：

```yaml
version: 1

project:
  name: example
  default_branch: main
```

该文件不是必须的，`gws` 也不会自动生成它。没有配置文件时，Project 名称来自当前目录，默认分支来自 Git。

全局状态默认保存在 `~/.git-workspace`，可通过 `GIT_WORKSPACE_HOME` 覆盖。持久化的 Project 和 Workspace 记录包含状态格式版本；没有版本的旧记录按版本 1 读取，来自未来版本且当前程序不支持的记录会被拒绝，不会被覆盖。

## 贡献与安全

开发和 Pull Request 检查见 [CONTRIBUTING.md](CONTRIBUTING.md)。安全问题请按照 [SECURITY.md](SECURITY.md) 私下报告。本项目采用 [MIT License](LICENSE)。
