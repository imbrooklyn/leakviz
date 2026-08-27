# 变更日志

本文档记录 `leakviz` 对用户可见的重要变更。

## v0.1.0

有关用法和重要限制，请参阅 [v0.1.0 release notes](releases/v0.1.0.zh-CN.md)。

### 新增

- 以只读方式分析来自 HTTP(S)、本地文件或标准输入的 Go 1.27 二进制
  `goroutineleak` pprof snapshot。
- 提供确定性的文本报告和 JSON schema v1，其中包含 stack、count、blocker、
  fingerprint、label、用户 frame 和 finding 数据。
- 在单个 snapshot 中执行 exact grouping，并在两个 snapshot 之间执行 semantic
  比较。
- Diff 输出提供 `NEW`、`INCREASED`、`DECREASED`、`RESOLVED` 和
  `UNCHANGED` status，同时在 JSON 中保留每个 exact site。
- 使用标准的“flag 位于 operand 之前”语法，支持 `--app`、`--timeout`、
  `--json`、help 和 version。

### 安全与兼容性

- 报告会保留 profile label，其中可能包含敏感数据。
- Runtime reachability 可能产生 false negative；空报告不能证明应用不存在
  goroutine leak。
- 普通 goroutine profile、delta profile、symbolization、root-cause proof 和
  自动修复不属于 v0.1 范围。
