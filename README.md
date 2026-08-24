# leakviz

`leakviz` is a small, read-only command-line tool for inspecting Go 1.27
`goroutineleak` profiles. It helps identify permanently blocked goroutines from
profile evidence while retaining stack, count, and label information.

Profile labels may contain sensitive data. Review reports before sharing them.

## Requirements

- Go 1.27

## License

Licensed under the [Apache License 2.0](LICENSE).
