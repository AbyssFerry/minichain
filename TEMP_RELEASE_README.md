# minichain 发布临时说明

本文档只用于发布流程检查，不作为长期项目介绍文档。

## 1. 发布前检查

1. 确认工作区干净：

```bash
git status
```

2. 运行测试：

```bash
go test ./...
```

3. 运行示例入口（需先准备 .env）：

```bash
go run ./cmd/minichain-cli
```

4. 确认 README 中导入路径正确：

- github.com/abyssferry/minichain/llm

## 2. 创建发布版本

1. 提交改动：

```bash
git add .
git commit -m "release: prepare v1.0.0"
```

2. 创建标签：

```bash
git tag v1.0.0
```

3. 推送主分支和标签：

```bash
git push origin main
git push origin v1.0.0
```

## 3. 创建 GitHub Release

在仓库页面基于 v1.0.0 创建 Release，建议说明包括：

- 首个稳定版本
- llm 包提供 ChatModel 与 Agent 能力
- 支持工具注册、流式事件与上下文裁剪

## 4. 外部安装验证

在全新目录执行：

```bash
mkdir demo && cd demo
go mod init demo
go get github.com/abyssferry/minichain@v1.0.0
```

写一个最小代码验证导入：

```go
package main

import "github.com/abyssferry/minichain/llm"

func main() {
	_ = llm.InvokeInput{}
}
```

再执行：

```bash
go run .
```

## 5. 常见问题

1. 拉不到新版本：
- 可能是 Go Proxy 缓存延迟，等待几分钟后重试。

2. 误打标签：
- 本地删除：git tag -d v1.0.0
- 远程删除：git push origin :refs/tags/v1.0.0
- 重新打正确标签后再推送。

3. 需要热修复：
- 在主分支修复后打 v1.0.1，不要重写已发布标签。
