# AI 学习笔记

记录日常使用 AI 工具（Claude Code 等）过程中收集到的实用知识点、技巧。

---

## Claude Code：不打断进程就能安心下班回家的命令

- 来源: [Confluence - Claude codeのプロセスを中断させずにお家に帰るコマンド](https://welby-dev-team.atlassian.net/wiki/spaces/jtoHg5GOshUc/pages/98140200/Claude+code)
- 作者: Hiroshi Imai

**场景**：用 Claude Code 跑任务，处理时间比预期长，但想合盖下班，又不希望电脑休眠中断正在运行的任务。

**做法**：另开一个终端窗口执行：

```bash
caffeinate -i -w $(pgrep -f claude) &
```

- 效果：在 `claude` 进程结束前，Mac 不会进入休眠，且不影响正在运行的 Claude Code 进程本身。
- 之后即可合盖离开。

**注意事项**：

1. 合盖后 Wi-Fi 会断开，如果任务需要联网，建议改用手机热点（蜂窝网络）。
2. 如果 `pgrep -f claude` 返回多个 PID 导致报错，先用以下命令确认具体进程：

   ```bash
   pgrep -f claude | xargs ps -o pid,ppid,start,command
   ```

   确认目标 PID 后，单独执行：

   ```bash
   caffeinate -i -w <PID> &
   ```
