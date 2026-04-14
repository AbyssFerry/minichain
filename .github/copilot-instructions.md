# Project Guidelines

## Code Style

- 注释必须遵循 Go 官方 godoc 规范：https://go.dev/doc/comment。
- 所有函数必须添加规范化注释；对代码中不清晰的逻辑、分支和关键步骤补充中文注释。
- 模型相关文件（如定义请求体、响应体、领域模型的文件）必须添加完整注释：每个结构体需有用途说明，结构体中的每个字段都需有字段含义说明。

## Architecture

- `.env` 中的变量统一通过 `utils/godotenv.go` 读取。

## Build and Test

- 待补充。

## Conventions

- 待补充。

## Pitfalls

- 待补充。
