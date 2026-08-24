# leakviz

`leakviz` 是一个小型只读命令行工具，用于检查 Go 1.27 `goroutineleak`
profile。它根据 profile 证据帮助识别永久阻塞的 goroutine，同时保留栈、计数和
label 信息。

Profile label 可能包含敏感数据。分享报告前请先检查内容。

## 环境要求

- Go 1.27

## 许可证

本项目采用 [Apache License 2.0](../../LICENSE)。
