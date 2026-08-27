# leakviz

`leakviz` 是一个小型只读命令行工具，用于检查 Go 1.27
`goroutineleak` profile。它将 runtime 关于永久阻塞 goroutine 的证据转换为
确定性的文本或 JSON 报告，并且可以在不修改目标应用或 profile 的情况下比较
两个 snapshot。

## 环境要求

- Go 1.27.x
- 来自 Go 1.27 应用的二进制 `goroutineleak` pprof snapshot

## 安装或构建

在带 tag 的 v0.1.0 module release 可用后，可使用以下命令安装：

```bash
go install github.com/imbrooklyn/leakviz/cmd/leakviz@v0.1.0
```

要构建当前 source checkout：

```bash
mkdir -p bin
go build -o ./bin/leakviz ./cmd/leakviz
./bin/leakviz --version
```

从 source checkout 构建会报告 `leakviz devel`。从 `v0.1.0` module tag
安装的构建通过 Go build information 报告 `leakviz v0.1.0`。

## 分析 snapshot

### HTTP

裸 `HOST:PORT`，或路径为空或根路径的 HTTP(S) URL，会使用
`/debug/pprof/goroutineleak`：

```bash
leakviz localhost:6060
leakviz https://service.example
```

带有明确非根路径的 URL 会按原样请求该路径：

```bash
leakviz https://service.example/internal/profiles/goroutineleak
```

### 文件

```bash
leakviz ./leak.pprof
```

### 标准输入

```bash
cat ./leak.pprof | leakviz -
```

## 输出与分析选项

Flag 采用 Go 标准的“flag 位于 operand 之前”顺序。

| 选项 | 行为 |
| --- | --- |
| `--json` | 写出确定性的 JSON schema v1，而不是文本。 |
| `--app prefix` | 优先选择位于该 package 或 module prefix 中的第一个用户 frame。它只改变展示选择，不改变 fingerprint。 |
| `--timeout duration` | 设置 HTTP request timeout；默认值为 `30s`，并且该值必须大于零。 |

```bash
leakviz --json ./leak.pprof
leakviz --app github.com/acme/service ./leak.pprof
leakviz --timeout 2m localhost:6060
```

文本和 JSON 报告包含 stack 证据、exact 与 semantic fingerprint、计数、blocker
分类、用户 frame 选择、label 和 finding。未知 blocker 仍可报告，并且不会被赋予
猜测的原因。

## 比较 snapshot

`diff` 通过 semantic fingerprint bucket 比较 exact snapshot group，因此 source
line 移动后仍可匹配，同时每个 exact site 都会保留在 JSON 输出中。

```bash
leakviz diff ./before.pprof ./after.pprof
leakviz diff --json ./before.pprof ./after.pprof
```

Diff status 包括 `NEW`、`INCREASED`、`DECREASED`、`RESOLVED` 和
`UNCHANGED`。最多一个 diff 输入可以使用标准输入。每个 HTTP 输入都有独立的
timeout。发现 leak 或数量增加不会改变成功退出码；运行错误返回 `1`，用法错误
返回 `2`。

## 解读与限制

### Runtime 证据与 false negative

Go 1.27 `goroutineleak` profile 中的一条记录表示 runtime 已确定该 goroutine
无法解除阻塞。这是永久阻塞的有力证据，但不能证明应用的 root cause。

Runtime 的判断依赖垃圾回收器的 reachability。如果同步对象仍可从全局变量或
runnable goroutine 到达，真实 leak 可能不会进入 profile。因此，空报告不能证明
应用不存在 goroutine leak。

### 敏感 label

Profile label 可能包含 tenant 名称、标识符或其他敏感数据。`leakviz` 默认会在
文本和 JSON 报告中包含 label key 与 value。分享报告文件前请先审查并妥善保护。
本工具不会上传 profile、启动 server 或发送 telemetry。

### 输入范围

v0.1 接受 gzip 压缩或未压缩的二进制 pprof 数据，其中必须恰好包含一个
`goroutineleak/count` sample type。它会拒绝普通 `goroutine` profile、delta
profile 和具有正 count 但未 symbolized 的 sample，而不会猜测 leak 或 symbol
信息。

## v0.1 非目标

v0.1 不提供：

- symbolization；
- root-cause proof；
- 自动修复；
- watch、poll 或 daemon 模式；
- plugin 或 rule DSL；
- configuration file 或 environment variable 设置；
- public Go library API；
- 跨 fingerprint version 的自动迁移；或
- 从普通 goroutine profile 推断 leak。

## 许可证

本项目采用 [Apache License 2.0](../../LICENSE)。
