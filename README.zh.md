# Git Workspace

[![CI](https://github.com/sanm00/gitworkspace/actions/workflows/ci.yml/badge.svg)](https://github.com/sanm00/gitworkspace/actions/workflows/ci.yml)
[![Security](https://github.com/sanm00/gitworkspace/actions/workflows/security.yml/badge.svg)](https://github.com/sanm00/gitworkspace/actions/workflows/security.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md)

Git Workspace（`gws`）用于从你当前所在的分支切出隔离的 worktree，并在确认安全后合并回去。它为两个时刻而生：一边继续开发、一边排查问题；以及在同一仓库里并行推进多件事。

> **项目状态：** 当前处于 1.0 之前。项目会谨慎维护 CLI 和 JSON 兼容性，但仍可能在附带迁移说明的版本中引入不兼容调整。

## 产品边界

Git Workspace 只负责源码工作空间的生命周期：

```text
基线分支 -> 托管分支（gws/<name>）-> Git worktree -> apply 或 finish
```

它不启动编程智能体、编辑器、容器、数据库、宿主机进程或网络服务。文件隔离能力来自 Git worktree；`gws` 在此基础上增加命名、基线记录、并发锁、失败回滚、所有权检查、诊断和生命周期策略。

## 安装

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

你正在 `feature-x` 上开发，需要同一基线的第二份隔离副本——排查一个 bug，或者在不打扰当前工作的情况下尝试改动：

```bash
cd ~/projects/example
git checkout feature-x
gws new
```

`gws new` 不需要任何参数。它以当前分支为基线创建 Workspace，在该 commit 上建立托管分支 `gws/<name>`，在同级目录（`../example-gws/<id>/`）开出 worktree，把元数据写入 `.git/gws/`，然后直接进入其中的子 Shell。用完 `exit` 退出。

每条命令都会报告所用的基线：

```text
Created Workspace feature-x-9f2c
Type: dev
Based on: feature-x (1a2b3c4d)
Branch: gws/feature-x
Worktree: /Users/you/projects/example-gws/feature-x-9f2c
```

之后要再进入这个 Workspace：

```bash
gws use feature-x-9f2c     # 支持 ID、无歧义前缀或名称
```

在 Workspace 内照常提交修改，然后把成果合回去：

```bash
gws apply                  # 合并进基线分支，保留 Workspace
```

`apply` 必须在 Workspace 内执行，默认目标是它的基线分支（可用 `--into <branch>` 覆盖）。合并前它会逐项校验前置条件——工作区干净、主检出正处于目标分支、无冲突——任一不满足就停下并给出可操作的原因，不做任何半截操作：

```text
! 主检出目录 … 当前在 "main" 分支，不是目标 "feature-x"。
  请执行 git -C … checkout feature-x，或使用 --into main 合并到其他分支。
```

彻底收尾时回到主检出目录执行：

```bash
gws finish feature-x-9f2c  # 合并后删除 worktree 与托管分支
```

`finish` 先应用成果，只有合并成功才执行清理。

## 其他工作空间类型

```bash
gws new --base pull/123/head --type review   # detached，只读检查
gws new --base v0.9.0 --type test            # detached，针对标签复现问题
gws new hotfix --base main --type fix        # 与 dev 相同，基于 main 的托管分支
```

`review` 和 `test` 的 worktree 是 detached 的：它们无法 apply，这正是只读排查的安全保证。`--fetch` 会先刷新远程引用，且仅允许与这两种类型搭配使用。

## 常用 Git 操作封装

`gws` 也把空间里最常敲的 Git 命令封装好了，不必再记基线 commit、也不用重复拼路径：

```bash
gws commit -m "修复头像上传" -a    # 在空间内提交
gws diff                          # 本空间相对基线改了什么
gws diff --uncommitted            # 只看还没提交的部分
gws diff --staged --stat          # 已暂存改动的摘要
gws log -n 5                      # 基线之上的提交
```

`gws diff` 输出的就是普通 `git diff` 内容，并把未跟踪文件单独列出（Git 的 diff 从不包含它们）。`gws commit` 拒绝 detached 的 review/test 空间——它们按设计是只读的。

## 核心命令

```text
gws new [NAME] [--base BRANCH|SHA] [--type dev|fix|review|test] [--detach] [--fetch] [--no-enter] [--json]
gws use [WORKSPACE]
gws commit --message MESSAGE [-a] [--json]
gws diff [WORKSPACE] [--staged] [--stat] [--uncommitted] [--json]
gws log [WORKSPACE] [-n N] [--stat] [--json]
gws apply [--into BRANCH]
gws finish WORKSPACE [--into BRANCH]
gws ls | show | status | path [WORKSPACE]
gws rm WORKSPACE [--force]
gws gc [--dry-run]
gws doctor
gws capabilities [--json]
```

`--help`、`--version`、`capabilities` 在任何目录都可用；其余命令必须在 Git 仓库内执行。

## 安全模型

- 状态变更由跨进程锁串行化；
- 创建时先记录 `creating` 状态，失败时回滚部分资源；
- 托管分支位于 `gws/` 命名空间并带有所有权标记，只有被标记托管的分支才会自动删除；
- `apply`/`finish` 先行全量前置校验并报告阻塞原因，不会半应用；合并失败时 `finish` 不执行清理；
- 普通删除拒绝包含未提交修改的 worktree；`--force` 只跳过 dirty 检查，不跳过路径所有权检查；
- `gc` 清理 worktree 已消失的元数据，但从不删除用户分支。

## 平台插件接口

所有非交互命令都支持 `--json`，输出带版本号的信封结构（schema 版本 2，协议版本 2）：

```bash
gws new issue-328 --no-enter --json
gws show issue-328 --json
gws path issue-328 --json
gws finish issue-328 --json
```

`use` 是面向人的交互命令，故意不支持 JSON。上层平台应调用 `path --json` 获取工作目录，再自行启动运行时。携带 `--json`（或标准输出不是终端）时，`new` 只创建、不进入子 Shell。具体契约见 [GIT_WORKSPACE_IMPLEMENTATION_SPEC.zh.md](GIT_WORKSPACE_IMPLEMENTATION_SPEC.zh.md)。

`internal/` 下的包不是公共 Go API。受支持的集成入口是带版本的 `gws --json` 子进程协议。

## 配置与状态

`gws` 持久化的一切都在仓库内部：

```text
<repo>/.git/gws/config.json          可选配置
<repo>/.git/gws/workspaces/<id>.json Workspace 元数据
<repo>/.git/gws/tasks/<id>.md        供编程智能体使用的任务说明
<repo>/.git/gws/locks/state.lock     跨进程锁
<parent>/<repo>-gws/<id>/            worktree 检出目录
```

元数据存放在 Git common 目录下，因此同一仓库的所有 worktree 共享一份状态；克隆仓库不会带走任何 `gws` 数据。可选配置文件为 `.git/gws/config.json`：

```json
{ "version": 1, "default_branch": "main" }
```

`default_branch` 用于 HEAD 处于 detached 且未指定 `--base` 时的兜底基线。`gws` 不会自动生成该文件。

持久化记录包含状态格式版本；没有版本的旧记录按版本 1 读取，来自未来版本且当前程序不支持的记录会被拒绝，不会被覆盖。

## 贡献与安全

开发和 Pull Request 检查见 [CONTRIBUTING.md](CONTRIBUTING.md)。安全问题请按照 [SECURITY.md](SECURITY.md) 私下报告。本项目采用 [MIT License](LICENSE)。
