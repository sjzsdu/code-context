# code-context 产品需求文档

## 1. 项目概述

### 1.1 项目背景

code-context 是一个纯 Go 语言实现的代码上下文系统，旨在为 AI 智能体和大型语言模型（LLM）提供高效的程序代码理解与检索能力。该项目通过结构化解析技术（基于 tree-sitter）建立代码库的语义索引，支持多种主流编程语言的符号提取与依赖分析。

### 1.2 项目目标

- 为 AI 智能体提供代码理解能力，使其能够准确理解代码结构、符号定义和依赖关系
- 提供高效的代码检索功能，支持按符号名称、文件路径、文本内容等多种方式搜索
- 支持增量索引，仅重新索引变更的文件，提高索引效率
- 提供 HTTP API 和 MCP（Model Context Protocol）服务器接口，便于与各种 AI 工具集成

### 1.3 目标用户

- AI 智能体开发者（Claude Desktop、Cursor 等）
- 大型语言模型开发者
- 需要代码理解与分析工具的开发者

## 2. 功能需求

### 2.1 核心功能

#### 2.1.1 代码索引

- **结构化解析**：使用 tree-sitter AST 解析，而非正则表达式匹配
- **符号提取**：提取函数、方法、类、类型、接口、变量、常量、包、导入等符号信息
- **增量索引**：基于内容哈希（SHA-256）检测文件变更，仅重新索引变更的文件
- **多语言支持**：支持 Go、TypeScript、JavaScript、Python、Rust、Java 六种语言

#### 2.1.2 符号搜索

- **FTS5 全文搜索**：使用 SQLite FTS5 全文索引加速符号名称搜索
- **定义查找**：查找符号的定义位置
- **引用查找**：查找符号的所有引用位置
- **类型过滤**：支持按符号类型（function、method、class、type、interface 等）过滤搜索结果

#### 2.1.3 依赖图分析

- **导入图构建**：基于文件导入关系构建有向依赖图
- **依赖查询**：查询文件的直接依赖和递归依赖
- **反向依赖**：查询依赖该文件的所有文件
- **相关性评分**：基于共同导入计算文件相关性

#### 2.1.4 上下文生成

- **LLM 上下文快照**：为指定查询生成相关的代码上下文包
- **符号详情**：获取符号的定义、方法、相关符号等信息
- **文件摘要**：获取文件的符号、导入、导入者等信息

#### 2.1.5 影响分析

- **变更影响分析**：分析修改文件可能影响的下游文件
- **调用链追踪**：追踪两个符号之间的调用路径
- **测试建议**：推荐可能需要运行的测试文件

### 2.2 CLI 命令

| 命令 | 功能描述 |
|------|----------|
| `index` | 索引代码库 |
| `search` | 按名称搜索符号 |
| `find-def` | 查找符号定义 |
| `files` | 列出已索引的文件 |
| `imports` | 显示文件的导入 |
| `importers` | 显示导入指定源的文件 |
| `stats` | 显示索引统计 |
| `map` | 显示项目架构概览 |
| `explain` | 显示文件摘要 |
| `context` | 显示符号详情 |
| `snapshot` | 生成 LLM 上下文 |
| `trace` | 追踪调用链 |
| `diff-impact` | 分析变更影响 |
| `serve` | 启动 HTTP 服务器 |

### 2.3 HTTP API

| 方法 | 端点 | 参数 | 描述 |
|------|------|------|------|
| GET | `/api/search` | `q`, `kind?`, `limit?` | 按名称搜索符号 |
| GET | `/api/symbols` | `file` | 列出文件的符号 |
| GET | `/api/definitions` | `name` | 查找符号定义 |
| GET | `/api/references` | `name` | 查找符号引用 |
| GET | `/api/text` | `q`, `file?`, `limit?` | 全文搜索源码 |
| GET | `/api/imports` | `file` | 获取文件的导入 |
| GET | `/api/importers` | `source` | 查找导入指定源的文件 |
| GET | `/api/stats` | - | 获取索引统计 |
| POST | `/api/index` | `incremental?` | 触发重新索引 |

### 2.4 MCP 服务器

支持作为 Model Context Protocol 服务器运行，提供以下工具：

- `index`：索引代码库
- `search`：搜索符号
- `find_def`：查找定义
- `find_refs`：查找引用
- `files`：列出文件
- `imports`：显示导入
- `importers`：显示导入者
- `stats`：统计信息
- `map`：项目架构
- `explain`：文件摘要
- `context`：符号详情
- `snapshot`：上下文快照
- `diff_impact`：影响分析
- `trace`：调用链追踪

## 3. 数据模型

### 3.1 核心数据类型

#### Symbol（符号）

```go
type Symbol struct {
    Name      string     // 符号名称
    Kind      SymbolKind // 符号类型
    FilePath  string     // 所在文件
    Line      int        // 行号
    EndLine   int        // 结束行号
    Signature string     // 函数签名
    Parent    string     // 父级类/结构体
}
```

支持的符号类型：function、method、class、type、interface、variable、constant、module、import、package

#### FileInfo（文件信息）

```go
type FileInfo struct {
    Path        string   // 文件路径
    Language    Language // 语言类型
    ContentHash string   // 内容哈希
    Size        int64    // 文件大小
}
```

#### ImportEdge（导入边）

```go
type ImportEdge struct {
    FromFile string // 源文件
    ToSource string // 导入的源
    Line     int    // 导入语句所在行
}
```

#### IndexStats（索引统计）

```go
type IndexStats struct {
    TotalFiles   int     // 总文件数
    IndexedFiles int     // 已索引文件数
    SkippedFiles int     // 跳过的文件数
    FailedFiles  int     // 失败的文件数
    TotalSymbols int     // 总符号数
    TotalImports int     // 总导入数
    Duration     float64 // 耗时（秒）
}
```

## 4. 非功能性需求

### 4.1 性能需求

- 支持并行解析，默认使用 CPU 核心数（最大 16）的 worker 数量
- 增量索引仅处理变更文件，全量索引支持跳过未变更文件
- 使用 FTS5 全文索引加速搜索

### 4.2 存储需求

- 使用纯 Go SQLite（modernc.org/sqlite），无需外部数据库
- 默认存储路径：`<项目根目录>/.code-context/index.db`
- 支持级联删除，删除文件时自动删除其关联的符号和导入

### 4.3 可用性需求

- 单一二进制文件，无运行时依赖
- 提供 CLI 和 HTTP API 两种交互方式
- 支持 MCP 协议，便于与 AI 工具集成

## 5. 技术栈

- **语言**：Go 1.25+
- **解析器**：tree-sitter（通过 smacker/go-tree-sitter）
- **数据库**：modernc.org/sqlite（纯 Go SQLite）
- **CLI 框架**：spf13/cobra
- **MCP SDK**：github.com/modelcontextprotocol/go-sdk

## 6. 支持的语言

| 语言 | 文件扩展名 |
|------|------------|
| Go | .go |
| TypeScript | .ts, .tsx |
| JavaScript | .js, .jsx, .mjs |
| Python | .py |
| Rust | .rs |
| Java | .java |