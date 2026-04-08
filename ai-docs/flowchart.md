# code-context 流程图

## 1. 索引流程

### 1.1 全量索引流程

```mermaid
flowchart TD
    A[用户执行 index 命令] --> B[创建 Engine]
    B --> C[创建 Indexer]
    C --> D[调用 Indexer.IndexAll]
    
    D --> E[初始化 SQLite Store]
    E --> F[遍历文件系统 walk]
    F --> G[筛选支持的语言文件]
    
    G --> H[创建解析 worker 池]
    H --> I[并行解析每个文件]
    
    I --> J{解析成功?}
    J -->|是| K[计算内容哈希]
    J -->|否| L[标记为失败]
    L --> N[继续处理下一个文件]
    
    K --> M{文件已变更?}
    M -->|是| O[UpsertFile 写入文件信息]
    M -->|否| P[跳过文件]
    P --> N
    
    O --> Q[ReplaceSymbols 写入符号]
    Q --> R[ReplaceImports 写入导入]
    R --> S[更新统计计数器]
    S --> N
    
    N --> T{所有文件处理完成?}
    T -->|否| I
    T -->|是| U[清理已删除文件]
    U --> V[返回索引统计]
    
    V --> W[用户看到索引结果]
```

### 1.2 增量索引流程

```mermaid
flowchart TD
    A[用户执行 index --incremental] --> B[创建 Indexer]
    B --> C[调用 Indexer.IndexIncremental]
    
    C --> D[遍历文件系统获取文件列表]
    D --> E[获取现有索引文件列表]
    
    E --> F[比较文件列表]
    F --> G[计算新文件内容哈希]
    
    G --> H{哈希变化?}
    H -->|是| I[加入更新队列]
    H -->|否| J[跳过文件]
    
    I --> K[并行索引变更文件]
    K --> L[删除已不存在文件]
    L --> M[返回增量统计]
    
    J --> L
```

### 1.3 解析流程

```mermaid
flowchart TD
    A[Indexer 解析单个文件] --> B[读取文件内容]
    B --> C[DetectLanguage 检测语言]
    
    C --> D{语言支持?}
    D -->|是| E[获取语言定义]
    D -->|否| F[返回跳过]
    
    E --> G[创建 tree-sitter Parser]
    G --> H[设置语言]
    
    H --> I[ParseCtx 解析源码]
    I --> J{AST 解析成功?}
    J -->|是| K[执行 SymbolQuery]
    J -->|否| L[返回解析错误]
    
    K --> M[遍历查询结果]
    M --> N{有匹配?}
    N -->|是| O[提取符号名称和位置]
    O --> P[添加到符号列表]
    P --> M
    
    N -->|否| Q[执行 ImportQuery 提取导入]
    Q --> R{有导入?}
    R -->|是| S[提取导入路径]
    S --> T[添加到导入列表]
    T --> U[返回 ParseResult]
    
    R -->|否| U
    L --> U
```

## 2. 搜索流程

### 2.1 符号搜索流程

```mermaid
flowchart TD
    A[用户执行 search 命令] --> B[创建 Engine]
    B --> C[调用 Engine.SearchSymbols]
    
    C --> D[调用 Searcher.SearchSymbols]
    D --> E[调用 Store.SearchSymbols]
    
    E --> F{指定 kind?}
    F -->|是| G[FTS5 查询 + kind 过滤]
    F -->|否| H[纯 FTS5 查询]
    
    G --> I[SQLite FTS5 MATCH 查询]
    H --> I
    
    I --> J[解析查询结果]
    J --> K[返回 Symbol 列表]
    K --> L[格式化输出]
```

### 2.2 定义查找流程

```mermaid
flowchart TD
    A[用户执行 find-def 命令] --> B[创建 Engine]
    B --> C[调用 Engine.FindDef]
    
    C --> D[调用 Searcher.FindDefinition]
    D --> E[调用 Store.FindDefinitions]
    
    E --> F[SQL 查询 name 匹配]
    F --> G[过滤 kind 为 function/method/class/type/interface]
    G --> H[返回 Symbol 列表]
    H --> I[格式化输出]
```

### 2.3 引用查找流程

```mermaid
flowchart TD
    A[用户执行 find-refs 命令] --> B[创建 Engine]
    B --> C[调用 Engine.FindRefs]
    
    C --> D[先查找定义]
    D --> E{找到定义?}
    E -->|否| F[返回空]
    E -->|是| G[调用 Store.FindReferences]
    
    G --> H[查询所有同名符号]
    H --> I[过滤掉定义本身]
    I --> J[返回引用列表]
```

## 3. 依赖图流程

### 3.1 构建依赖图

```mermaid
flowchart TD
    A[Engine.BuildGraph] --> B[获取所有文件列表]
    B --> C[遍历每个文件]
    
    C --> D[获取文件导入]
    D --> E[构建正向映射 forward]
    E --> F[构建反向映射 reverse]
    F --> C
    
    C --> G{所有文件处理完成?}
    G -->|否| C
    G -->|是| H[图构建完成]
```

### 3.2 依赖查询

```mermaid
flowchart TD
    A[Engine.GraphDeps] --> B[调用 Graph.Dependencies]
    
    B --> C[BFS 广度优先搜索]
    C --> D[按深度遍历 forward 映射]
    D --> E[收集所有可达节点]
    E --> F[去重排序返回]
```

### 3.3 变更影响分析

```mermaid
flowchart TD
    A[用户执行 diff-impact] --> B[创建 Engine]
    B --> C[调用 Engine.DiffImpact]
    
    C --> D[获取文件信息验证存在]
    D --> E[BuildGraph 构建依赖图]
    
    E --> F[Graph.DirectImports 获取直接依赖]
    F --> G[Graph.Dependencies 递归依赖]
    G --> H[Graph.Dependents 获取反向依赖]
    
    H --> I[遍历 dependents]
    I --> J{存在对应测试文件?}
    J -->|是| K[加入推荐列表]
    J -->|否| L[跳过]
    
    K --> I
    L --> M[返回 DiffImpact 结果]
```

## 4. 上下文生成流程

### 4.1 快照生成

```mermaid
flowchart TD
    A[用户执行 snapshot 命令] --> B[创建 Engine]
    B --> C[调用 Engine.Snapshot]
    
    C --> D[SearchSymbols 搜索相关符号]
    D --> E[按文件去重]
    
    E --> F[遍历文件]
    F --> G[调用 Explain 获取文件摘要]
    G --> H[获取符号列表]
    H --> I[获取导入列表]
    
    I --> J{文件数达到限制?}
    J -->|否| K[补充文本搜索结果]
    K --> F
    
    J -->|是| L[生成汇总信息]
    L --> M[返回 Snapshot]
```

### 4.2 符号上下文

```mermaid
flowchart TD
    A[用户执行 context 命令] --> B[创建 Engine]
    B --> C[调用 Engine.Context]
    
    C --> D[Store.FindDefinitions 查找定义]
    D --> E{找到?}
    E -->|否| F[返回错误]
    E -->|是| G[获取第一个定义]
    
    G --> H[Store.FindReferences 查找引用]
    H --> I[过滤 Method 类型]
    I --> J[SearchSymbols 搜索相关符号]
    
    J --> K[排除定义本身]
    K --> L[返回 SymbolContext]
```

## 5. 追踪流程

### 5.1 调用链追踪

```mermaid
flowchart TD
    A[用户执行 trace 命令] --> B[创建 Engine]
    B --> C[调用 Engine.Trace]
    
    C --> D[查找 from 符号定义]
    D --> E[查找 to 符号定义]
    
    E --> F{在同一文件?}
    F -->|是| G[直接返回文件内路径]
    F -->|否| H[BuildGraph 构建依赖图]
    
    H --> I[Graph.TraceFiles BFS 追踪]
    I --> J{找到路径?}
    J -->|是| K[收集路径上的符号]
    J -->|否| L[返回空路径]
    
    K --> M[返回 TraceResult]
```

## 6. HTTP API 流程

```mermaid
flowchart TD
    A[启动 HTTP 服务器] --> B[注册路由]
    
    B --> C{收到请求}
    C --> D[/api/search]
    C --> E[/api/symbols]
    C --> F[/api/definitions]
    C --> G[/api/references]
    C --> H[/api/text]
    C --> I[/api/imports]
    C --> J[/api/importers]
    C --> K[/api/stats]
    C --> L[/api/index]
    
    D --> M[调用 Engine.SearchSymbols]
    E --> N[调用 Engine.FileSymbols]
    F --> O[调用 Engine.FindDef]
    G --> P[调用 Engine.FindRefs]
    H --> Q[调用 Engine.SearchText]
    I --> R[调用 Engine.Imports]
    J --> S[调用 Engine.Importers]
    K --> T[调用 Engine.Stats]
    L --> U[调用 Engine.Index/IndexIncremental]
    
    M --> V[JSON 响应]
    N --> V
    O --> V
    P --> V
    Q --> V
    R --> V
    S --> V
    T --> V
    U --> V
```

## 7. MCP 服务器流程

```mermaid
flowchart TD
    A[启动 MCP 服务器] --> B[初始化 Engine]
    B --> C[自动索引代码库]
    
    C --> D[注册所有工具]
    D --> E{收到 MCP 请求}
    
    E --> F[index]
    E --> G[search]
    E --> H[find_def]
    E --> I[find_refs]
    E --> J[files]
    E --> K[imports]
    E --> L[importers]
    E --> M[stats]
    E --> N[map]
    E --> O[explain]
    E --> P[context]
    E --> Q[snapshot]
    E --> R[diff_impact]
    E --> S[trace]
    
    F --> T[调用 Engine.Index]
    G --> U[调用 Engine.SearchSymbols]
    H --> V[调用 Engine.FindDef]
    I --> W[调用 Engine.FindRefs]
    J --> X[调用 Engine.ListFiles]
    K --> Y[调用 Engine.Imports]
    L --> Z[调用 Engine.Importers]
    M --> AA[调用 Engine.Stats]
    N --> AB[调用 Engine.Map]
    O --> AC[调用 Engine.Explain]
    P --> AD[调用 Engine.Context]
    Q --> AE[调用 Engine.Snapshot]
    R --> AF[调用 Engine.DiffImpact]
    S --> AG[调用 Engine.Trace]
    
    T --> AH[MCP CallToolResult]
    U --> AH
    V --> AH
    W --> AH
    X --> AH
    Y --> AH
    Z --> AH
    AA --> AH
    AB --> AH
    AC --> AH
    AD --> AH
    AE --> AH
    AF --> AH
    AG --> AH
```