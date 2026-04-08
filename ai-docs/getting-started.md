# code-context 入门指南

## 1. 快速开始

### 1.1 安装

```bash
# 使用 go install
go install github.com/sjzsdu/code-context/cmd/code-context@latest

# 或者从源码构建
git clone https://github.com/sjzsdu/code-context.git
cd code-context
go build -o code-context ./cmd/code-context
```

### 1.2 索引代码库

```bash
# 进入项目目录
cd your-project

# 全量索引
code-context index

# 增量索引（仅索引变更文件）
code-context index --incremental

# 详细输出
code-context index -v
```

索引完成后，您将看到类似输出：

```
Done: 42 indexed, 15 skipped, 0 failed — 318 symbols, 156 imports (2.3s)
```

## 2. 核心功能示例

### 2.1 项目结构概览

```bash
# 查看项目架构
code-context map
```

输出示例：

```
root
  internal/
    api/ (2 files, 5 symbols)
    engine/ (1 files, 15 symbols)
    store/ (3 files, 20 symbols)
    parser/ (4 files, 30 symbols)
    ...
  cmd/ (2 files, 10 symbols)
```

### 2.2 符号搜索

```bash
# 搜索名为 "Server" 的符号
code-context search "Server"

# 限定类型为 function
code-context search "Handler" --kind function

# 限制结果数量
code-context search "parse" --limit 20
```

输出示例：

```
Engine                            function  internal/engine/engine.go:70
New                               function  internal/server/server.go:25
ServeHTTP                         method    internal/server/server.go:30

3 results
```

### 2.3 查找定义

```bash
# 查找符号定义位置
code-context find-def "NewRouter"
```

### 2.4 文件操作

```bash
# 列出所有已索引的文件
code-context files

# 按语言过滤
code-context files --lang go

# 查看文件导入
code-context imports internal/server/server.go

# 查找导入指定源的文件
code-context importers "fmt"
```

### 2.5 索引统计

```bash
# 查看索引统计
code-context stats
```

输出：

```
Files:   42
Symbols: 318
Imports: 156
```

### 2.6 文件摘要

```bash
# 查看文件详情
code-context explain internal/engine/engine.go
```

输出：

```
File: internal/engine/engine.go
Language: go

Symbols (15):
  Engine                            function  internal/engine/engine.go:19
  New                              function  internal/engine/engine.go:29
  ...

Imports (8):
  fmt (line 3)
  context (line 4)
  ...

Importers (3):
  cmd/code-context/main.go (line 12)
  ...
```

### 2.7 符号上下文

```bash
# 查看符号详情
code-context context Engine
```

### 2.8 LLM 上下文生成

```bash
# 生成上下文快照
code-context snapshot "authentication"

# 限制文件数量
code-context snapshot "parser" --limit 3
```

### 2.9 调用链追踪

```bash
# 追踪两个符号之间的调用路径
code-context trace "main" "Engine"
```

### 2.10 变更影响分析

```bash
# 分析修改文件的影响
code-context diff-impact internal/store/sqlite.go

# 调整依赖深度
code-context diff-impact internal/store/sqlite.go --depth 2
```

## 3. HTTP API 使用

### 3.1 启动服务器

```bash
# 默认端口 9090
code-context serve

# 自定义端口
code-context serve --port 8080
```

### 3.2 API 调用示例

```bash
# 搜索符号
curl "http://localhost:9090/api/search?q=Server&limit=10"

# 获取文件符号
curl "http://localhost:9090/api/symbols?file=internal/engine/engine.go"

# 查找定义
curl "http://localhost:9090/api/definitions?name=Engine"

# 获取导入
curl "http://localhost:9090/api/imports?file=internal/engine/engine.go"

# 获取统计
curl "http://localhost:9090/api/stats"

# 触发索引
curl -X POST "http://localhost:9090/api/index"
```

## 4. MCP 服务器使用

### 4.1 构建 MCP 服务器

```bash
go build -o code-context-mcp ./cmd/mcp
```

### 4.2 配置 AI 客户端

**Claude Desktop** (`~/Library/Application Support/Claude/claude_desktop_config.json`)：

```json
{
  "mcpServers": {
    "code-context": {
      "command": "/path/to/code-context-mcp",
      "args": ["--root", "."]
    }
  }
}
```

**Cursor** (`~/.cursor/mcp.json`)：

```json
{
  "mcpServers": {
    "code-context": {
      "command": "/path/to/code-context-mcp",
      "args": ["--root", "."]
    }
  }
}
```

### 4.3 MCP 工具使用

在 AI 客户端中可以直接调用：

```
# 索引代码库
code-context:index

# 搜索符号
code-context:search { "query": "Server" }

# 项目结构
code-context:map

# 上下文快照
code-context:snapshot { "query": "authentication" }

# 变更影响
code-context:diff_impact { "file": "internal/store/sqlite.go" }

# 调用链追踪
code-context:trace { "from": "main", "to": "Engine" }
```

## 5. 进阶用法

### 5.1 自定义数据库路径

```bash
code-context index --db /custom/path/index.db
code-context search "Handler" --db /custom/path/index.db
```

### 5.2 指定项目根目录

```bash
code-context index --root /path/to/project
code-context search "Handler" --root /path/to/project
```

### 5.3 增量索引脚本

创建一个定期执行的脚本：

```bash
#!/bin/bash
# incremental-index.sh

while true; do
    code-context index --incremental
    sleep 300  # 每 5 分钟执行一次
done
```

### 5.4 CI/CD 集成

在 GitHub Actions 中使用：

```yaml
- name: Index code
  run: go install github.com/sjzsdu/code-context/cmd/code-context@latest
- run: code-context index
```

### 5.5 编程接口

```go
package main

import (
    "context"
    "fmt"
    "github.com/sjzsdu/code-context/internal/engine"
)

func main() {
    eng, err := engine.New(".", "")
    if err != nil {
        panic(err)
    }
    defer eng.Close()

    // 索引
    stats, err := eng.Index(context.Background(), false)
    fmt.Printf("Indexed: %d files\n", stats.IndexedFiles)

    // 搜索
    results, _ := eng.SearchSymbols(context.Background(), "Engine", nil, 10)
    for _, s := range results {
        fmt.Printf("%s at %s:%d\n", s.Name, s.FilePath, s.Line)
    }

    // 文件摘要
    summary, _ := eng.Explain(context.Background(), "internal/engine/engine.go")
    fmt.Printf("File: %s, Symbols: %d\n", summary.Path, len(summary.Symbols))
}
```

## 6. 常见问题

### Q: 索引很慢怎么办？

A: 使用增量索引 `code-context index --incremental`，只索引变更的文件。

### Q: 搜索不到新添加的符号？

A: 需要重新索引：`code-context index`。

### Q: 如何清理索引数据？

A: 删除 `.code-context/index.db` 文件，然后重新索引。

### Q: 支持哪些语言？

A: Go、TypeScript、JavaScript、Python、Rust、Java。

### Q: 如何查看详细索引过程？

A: 使用 `-v` 参数：`code-context index -v`

## 7. 性能优化建议

1. **增量索引**：开发时使用 `--incremental` 避免全量索引
2. **合理限制**：搜索时使用 `--limit` 限制结果数量
3. **深度控制**：变更影响分析时使用 `--depth` 控制递归深度
4. **定期清理**：删除不再需要的索引数据库文件

## 8. 进阶功能

更多高级功能请参考：

- [架构设计文档](architecture.md)
- [功能图](feature-diagram.md)
- [流程图](flowchart.md)