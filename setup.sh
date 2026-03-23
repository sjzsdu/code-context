#!/bin/bash

# code-memory 安装脚本
# 编译 CLI 并安装到 ~/.local/bin

set -e

echo "========================================"
echo "code-memory 安装脚本"
echo "========================================"
echo ""

# 检查依赖
if ! command -v go &> /dev/null; then
    echo "错误: 未找到 go"
    echo "下载地址: https://go.dev/dl/"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}')
echo "Go 版本: $GO_VERSION"
echo ""

# 编译
echo "构建中..."
go build -o code-memory ./cmd/code-memory
echo "构建完成: $(ls -lh code-memory | awk '{print $5}') binary"
echo ""

# 安装
mkdir -p "$HOME/.local/bin"
cp code-memory "$HOME/.local/bin/"
chmod +x "$HOME/.local/bin/code-memory"

echo "========================================"
echo "安装完成！"
echo "========================================"
echo ""
ls -lh "$HOME/.local/bin/code-memory"
echo ""
echo "使用方法:"
echo "  code-memory index                 # 索引当前项目"
echo "  code-memory search \"Server\"       # 搜索符号"
echo "  code-memory find-def \"NewRouter\"  # 查找定义"
echo "  code-memory stats                 # 查看统计"
echo "  code-memory serve                 # 启动 HTTP 服务"
echo ""
echo "提示: 如需永久生效，将以下内容添加到 ~/.zshrc:"
echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
