# code-context 功能图

## 1. 系统功能全景图

```mermaid
mindmap
  root((code-context))
    核心引擎
      索引功能
        全量索引
        增量索引
        并行解析
        内容哈希检测
      搜索功能
        FTS5 符号搜索
        定义查找
        引用查找
        文本全文搜索
      依赖分析
        构建依赖图
        依赖查询
        反向依赖
        相关性评分
      上下文生成
        LLM 快照
        符号上下文
        文件摘要
        调用链追踪
        变更影响分析
    CLI 命令
      index
      search
      find-def
      files
      imports
      importers
      stats
      map
      explain
      context
      snapshot
      trace
      diff-impact
      serve
    HTTP API
      /api/search
      /api/symbols
      /api/definitions
      /api/references
      /api/text
      /api/imports
      /api/importers
      /api/stats
      /api/index
    MCP 服务器
      index
      search
      find_def
      find_refs
      files
      imports
      importers
      stats
      map
      explain
      context
      snapshot
      diff_impact
      trace
    支持语言
      Go
      TypeScript
      JavaScript
      Python
      Rust
      Java
    技术组件
      tree-sitter 解析
      SQLite 存储
      FTS5 全文索引
      并发 worker 池
      级联删除
```

## 2. 功能模块关系图

```mermaid
graph TD
    subgraph CLI_前端
        A1[index]
        A2[search]
        A3[find-def]
        A4[files]
        A5[imports]
        A6[importers]
        A7[stats]
        A8[map]
        A9[explain]
        A10[context]
        A11[snapshot]
        A12[trace]
        A13[diff-impact]
        A14[serve]
    end
    
    subgraph HTTP_API
        B1[/api/search]
        B2[/api/symbols]
        B3[/api/definitions]
        B4[/api/references]
        B5[/api/text]
        B6[/api/imports]
        B7[/api/importers]
        B8[/api/stats]
        B9[/api/index]
    end
    
    subgraph MCP_Tools
        C1[index]
        C2[search]
        C3[find_def]
        C4[find_refs]
        C5[files]
        C6[imports]
        C7[importers]
        C8[stats]
        C9[map]
        C10[explain]
        C11[context]
        C12[snapshot]
        C13[diff_impact]
        C14[trace]
    end
    
    subgraph Engine_核心
        D1[索引模块]
        D2[搜索模块]
        D3[依赖图模块]
        D4[存储模块]
        D5[解析模块]
    end
    
    A1 --> D1
    A2 --> D2
    A3 --> D2
    A4 --> D4
    A5 --> D4
    A6 --> D4
    A7 --> D4
    A8 --> D4
    A9 --> D4
    A10 --> D4
    A11 --> D2
    A12 --> D3
    A13 --> D3
    
    B1 --> D2
    B2 --> D4
    B3 --> D2
    B4 --> D2
    B5 --> D2
    B6 --> D4
    B7 --> D4
    B8 --> D4
    B9 --> D1
    
    C1 --> D1
    C2 --> D2
    C3 --> D2
    C4 --> D2
    C5 --> D4
    C6 --> D4
    C7 --> D4
    C8 --> D4
    C9 --> D4
    C10 --> D4
    C11 --> D4
    C12 --> D2
    C13 --> D3
    C14 --> D3
    
    D1 --> D5
    D1 --> D4
    D2 --> D4
    D3 --> D4
```

## 3. 索引功能分解

```mermaid
graph TD
    subgraph 索引功能
        A[IndexAll 全量索引]
        B[IndexIncremental 增量索引]
        C[walk 遍历文件]
        D[parseAll 并行解析]
        E[indexOneFile 单文件索引]
        F[内容哈希检测]
    end
    
    A --> C
    A --> D
    A --> E
    A --> F
    
    B --> C
    B --> F
    B --> E
    
    C --> D
    D --> E
```

## 4. 搜索功能分解

```mermaid
graph TD
    subgraph 搜索功能
        A[SearchSymbols FTS5 搜索]
        B[FindDefinition 定义查找]
        C[FindReferences 引用查找]
        D[SearchText 文本搜索]
        E[GetFileSymbols 文件符号]
        F[grepFile 文件 grep]
    end
    
    A --> D
    B --> C
    E --> F
```

## 5. 依赖图功能分解

```mermaid
graph TD
    subgraph 依赖图功能
        A[Build 构建图]
        B[DirectImports 直接依赖]
        C[DirectImporters 直接导入者]
        D[Dependencies 递归依赖]
        E[Dependents 反向依赖]
        F[Related 相关文件]
        G[TraceFiles 路径追踪]
        H[bfs 广度优先搜索]
    end
    
    A --> B
    A --> C
    A --> D
    A --> E
    A --> F
    A --> G
    
    D --> H
    G --> H
```

## 6. 上下文功能分解

```mermaid
graph TD
    subgraph 上下文功能
        A[Map 项目结构映射]
        B[Explain 文件摘要]
        C[Context 符号上下文]
        D[Snapshot LLM 快照]
        E[Trace 调用链追踪]
        F[DiffImpact 变更影响]
    end
    
    A --> B
    A --> C
    B --> C
    C --> D
    D --> E
    E --> F
```

## 7. 解析器功能分解

```mermaid
graph TD
    subgraph 解析模块
        A[Parser 接口]
        B[tree-sitter 解析器]
        C[DetectLanguage 语言检测]
        D[Parse 解析源码]
        E[SymbolQuery 符号查询]
        F[ImportQuery 导入查询]
    end
    
    A --> B
    B --> C
    B --> D
    D --> E
    D --> F
```

## 8. 存储层功能分解

```mermaid
graph TD
    subgraph 存储模块
        A[Store 接口]
        B[sqliteStore 实现]
        C[文件操作]
        D[符号操作]
        E[导入操作]
        F[FTS5 搜索]
    end
    
    A --> B
    B --> C
    B --> D
    B --> E
    D --> F
```

## 9. 语言支持功能分解

```mermaid
graph TD
    subgraph 语言支持
        A[Registry 注册表]
        B[LanguageDef 语言定义]
        C[Go 支持]
        D[TypeScript 支持]
        E[JavaScript 支持]
        F[Python 支持]
        G[Rust 支持]
        H[Java 支持]
    end
    
    A --> B
    B --> C
    B --> D
    B --> E
    B --> F
    B --> G
    B --> H
```

## 10. 功能层级图

```mermaid
graph TB
    subgraph 用户层
        A[CLI 用户]
        B[HTTP 客户端]
        C[MCP 客户端]
    end
    
    subgraph 接口层
        D[CLI 命令]
        E[HTTP API]
        F[MCP Tools]
    end
    
    subgraph 引擎层
        G[Engine 核心引擎]
    end
    
    subgraph 组件层
        H[Indexer 索引器]
        I[Searcher 搜索器]
        J[Graph 依赖图]
        K[Server HTTP服务器]
    end
    
    subgraph 基础层
        L[Parser 解析器]
        M[Store 存储]
        N[Registry 语言注册]
    end
    
    A --> D
    B --> E
    C --> F
    
    D --> G
    E --> G
    F --> G
    
    G --> H
    G --> I
    G --> J
    G --> K
    
    H --> L
    H --> M
    I --> M
    J --> M
    L --> N
    L --> M
```