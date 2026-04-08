# code-context 架构设计文档

## 1. 系统概述

code-context 是一个纯 Go 语言实现的代码上下文系统，采用模块化架构设计，通过结构化解析技术（tree-sitter）为 AI 智能体和大型语言模型提供代码理解与检索能力。

### 1.1 设计原则

- **模块化**：各组件职责明确，通过接口解耦
- **可扩展**：支持新增语言和搜索功能
- **高性能**：并行解析、增量索引、FTS5 加速
- **自包含**：单一二进制，无外部依赖

## 2. 系统架构

```
┌─────────────────────────────────────────────────────────────────┐
│                         CLI / HTTP Server                        │
│                   (cmd/code-context / cmd/mcp)                  │
└─────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Engine (协调层)                           │
│    整合 Parser、Store、Indexer、Search、Graph 五大子系统         │
└─────────────────────────────────────────────────────────────────┘
         │            │            │              │            │
         ▼            ▼            ▼              ▼            ▼
┌────────────┐ ┌────────────┐ ┌────────────┐ ┌──────────┐ ┌──────────┐
│  Parser    │ │  Store    │ │ Indexer   │ │ Searcher │ │  Graph   │
│ (解析器)    │ │ (存储层)   │ │ (索引器)   │ │ (搜索)   │ │ (依赖图) │
└────────────┘ └────────────┘ └────────────┘ └──────────┘ └──────────┘
         │            │            │              │            │
         ▼            ▼            ▼              ▼            ▼
┌────────────┐ ┌────────────┐ ┌────────────┐ ┌──────────┐ ┌──────────┐
│  Lang      │ │  SQLite    │ │  文件系统  │ │ SQLite   │ │  Store   │
│ (语言定义)  │ │  数据库    │ │           │ │ FTS5     │ │          │
└────────────┘ └────────────┘ └────────────┘ └──────────┘ └──────────┘
```

## 3. 模块划分

### 3.1 cmd/

#### cmd/code-context/main.go

CLI 入口点，使用 cobra 框架实现所有命令。负责解析命令行参数、初始化 Engine、调用相应功能。

#### cmd/mcp/main.go

MCP 服务器入口点，将 Engine 功能暴露为 MCP 工具，供 Claude Desktop、Cursor 等 AI 客户端使用。

### 3.2 internal/api/

定义核心数据类型和接口：

- **Symbol**：代码符号（函数、方法、类、类型等）
- **FileInfo**：索引文件信息
- **ImportEdge**：导入依赖关系
- **IndexStats**：索引统计信息
- **SymbolKind**：符号类型枚举
- **Language**：编程语言枚举

### 3.3 internal/parser/

代码解析模块，基于 tree-sitter 实现：

- **Parser 接口**：定义 Parse 和 DetectLanguage 方法
- **treeSitterParser**：tree-sitter 实现
- **ParseResult**：解析结果（符号列表 + 导入列表）
- **execSymbolQuery**：执行符号提取查询
- **execImportQuery**：执行导入提取查询

### 3.4 internal/lang/

语言定义模块，支持六种编程语言：

- **Registry**：语言注册表，管理所有语言定义
- **LanguageDef**：语言定义结构
- **SymbolQuery**：符号查询定义
- 各种语言定义：go.go、typescript.go、javascript.go、python.go、rust.go、java.go

每种语言定义包括：
- 文件扩展名
- tree-sitter 语言对象
- 符号提取查询（tree-sitter S-expression）
- 导入提取查询

### 3.5 internal/store/

数据存储模块，抽象存储接口：

- **Store 接口**：定义所有数据操作方法
- **sqliteStore**：SQLite 实现
- **schema.sql**：数据库 schema 定义

数据库表结构：

```
files ──────┐
     ───────┼──► symbols (CASCADE DELETE)
     ───────┼──► imports (CASCADE DELETE)
     ───────┘
     
symbols_fts (FTS5 全文索引)
```

### 3.6 internal/indexer/

索引模块，负责代码库的索引：

- **Indexer**：索引器主类
- **IndexAll**：全量索引
- **IndexIncremental**：增量索引
- **walk**：遍历文件系统
- **parseAll**：并行解析所有文件
- **indexOneFile**：索引单个文件

索引流程：
1. 遍历文件树，筛选支持的语言文件
2. 并行解析（worker 池）
3. 内容哈希检测变更
4. 写入 SQLite（符号 + 导入）
5. 清理已删除文件

### 3.7 internal/search/

搜索模块，提供多种搜索能力：

- **Searcher**：搜索器
- **SearchSymbols**：FTS5 符号搜索
- **FindDefinition**：查找符号定义
- **FindReferences**：查找符号引用
- **SearchText**：全文文本搜索（grep 风格）
- **GetFileSymbols**：获取文件的所有符号

### 3.8 internal/graph/

依赖图模块：

- **Graph**：依赖图结构
- **Build**：从导入数据构建图
- **DirectImports**：直接依赖
- **Dependencies**：递归依赖（BFS）
- **Dependents**：反向依赖（被谁依赖）
- **Related**：相关性文件
- **TraceFiles**：追踪文件间的调用路径

### 3.9 internal/engine/

核心协调层，封装所有子系统：

- **Engine**：主引擎类
- **Index/IndexIncremental**：索引操作
- **SearchSymbols/FindDef/FindRefs**：搜索操作
- **FileSymbols/Imports/Importers**：文件操作
- **Map**：项目结构映射
- **Explain**：文件摘要
- **Context**：符号上下文
- **Snapshot**：LLM 上下文生成
- **Trace**：调用链追踪
- **DiffImpact**：变更影响分析
- **Stats**：统计信息

### 3.10 internal/server/

HTTP API 服务器：

- **Server**：HTTP 服务器
- **Handler**：返回 http.Handler
- 9 个 API 端点（详见 HTTP API 章节）

## 4. 数据流

### 4.1 索引数据流

```
用户执行 index 命令
         │
         ▼
Engine.Index() 
         │
         ▼
Indexer.IndexAll()
         │
         ├── walk() 遍历文件树
         │
         ├── parseAll() 并行解析
         │      │
         │      └── Parser.Parse() → tree-sitter AST
         │                    │
         │                    └── SymbolQuery 执行
         │
         └── 写入 SQLite (UpsertFile, ReplaceSymbols, ReplaceImports)
```

### 4.2 搜索数据流

```
用户执行 search 命令
         │
         ▼
Engine.SearchSymbols()
         │
         ▼
Searcher.SearchSymbols()
         │
         ▼
Store.SearchSymbols() → FTS5 查询
         │
         ▼
返回 Symbol 列表
```

### 4.3 依赖图数据流

```
用户执行 diff-impact 命令
         │
         ▼
Engine.DiffImpact()
         │
         ├── BuildGraph() 构建依赖图
         │      │
         │      └── 从 Store 获取所有文件的导入
         │
         ├── Dependencies() 递归依赖
         │
         ├── Dependents() 反向依赖
         │
         └── 推荐测试文件
```

## 5. 并发模型

### 5.1 解析并行

Indexer 使用 worker 池模型：

```go
sem := make(chan struct{}, workers)  // 并发限制
wg := sync.WaitGroup                   // 等待组

for _, f := range files {
    wg.Add(1)
    go func(path string) {
        defer wg.Done()
        sem <- struct{}{}      // 获取令牌
        defer func() { <-sem }() // 释放令牌
        // 解析文件
    }(f)
}
wg.Wait()
```

### 5.2 写入模型

- 解析：并行（read-only）
- 写入：串行（SQLite 写锁）

## 6. 存储设计

### 6.1 SQLite Schema

```sql
-- 文件表
CREATE TABLE files (
    id           INTEGER PRIMARY KEY,
    path         TEXT UNIQUE,
    language     TEXT,
    content_hash TEXT,
    size         INTEGER,
    indexed_at   INTEGER
);

-- 符号表
CREATE TABLE symbols (
    id        INTEGER PRIMARY KEY,
    file_id   INTEGER REFERENCES files(id) ON DELETE CASCADE,
    name      TEXT,
    kind      TEXT,
    line      INTEGER,
    end_line  INTEGER,
    signature TEXT,
    parent    TEXT
);

-- 导入表
CREATE TABLE imports (
    id      INTEGER PRIMARY KEY,
    file_id INTEGER REFERENCES files(id) ON DELETE CASCADE,
    source  TEXT,
    line    INTEGER
);

-- FTS5 全文索引
CREATE VIRTUAL TABLE symbols_fts USING fts5(
    name, signature,
    content=symbols, content_rowid=id
);

-- 触发器保持 FTS 同步
CREATE TRIGGER symbols_ai AFTER INSERT ON symbols ...
CREATE TRIGGER symbols_ad AFTER DELETE ON symbols ...
CREATE TRIGGER symbols_au AFTER UPDATE ON symbols ...
```

### 6.2 索引策略

- **增量索引**：基于 SHA-256 内容哈希判断文件是否变更
- **级联删除**：删除文件自动清理关联的符号和导入
- **FTS 同步**：通过触发器保持 FTS5 索引与 symbols 表同步

## 7. 扩展性设计

### 7.1 新增语言

在 `internal/lang/` 下新增语言定义文件，实现以下内容：

1. 导入 `github.com/smacker/go-tree-sitter/xxx`
2. 定义 LanguageDef：
   - 文件扩展名
   - tree-sitter 语言对象
   - 符号提取查询（SymbolQuery 数组）
   - 导入提取查询
3. 在 allLanguageDefs() 中注册

### 7.2 新增搜索功能

在 `internal/search/` 中实现新的 Searcher 方法，调用 Store 接口。

## 8. 技术选型理由

- **tree-sitter**：业界标准的 AST 解析库，支持多种语言
- **modernc.org/sqlite**：纯 Go 实现，无 CGo 依赖，单一二进制
- **FTS5**：SQLite 内置全文搜索，性能足够
- **cobra**：成熟的 Go CLI 框架
- **MCP SDK**：标准化的 AI 工具协议