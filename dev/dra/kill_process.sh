#!/bin/bash

echo "=== 正在查找并杀死所有 npc、dlv、ascend-dra-kubeletplugin 相关进程 ==="
PATTERN="npc|dlv|ascend-dra-kubeletplugin"

# 查找所有匹配到的进程并排除 grep 本身
PIDS=$(ps aux | grep -E "$PATTERN" | grep -v grep | awk '{print $1}')

if [ -n "$PIDS" ]; then
    echo "找到以下进程，准备杀死："
    ps aux | grep -E "$PATTERN" | grep -v grep
    kill -9 $PIDS
else
    echo "未找到任何 npc、dlv 或 ascend-dra-kubeletplugin 相关进程。"
fi

