# concurrency101

补上 `errgroup` 之前的地基：goroutine + channel + select + `sync.WaitGroup` /
`sync.Mutex`。5 道题，按顺序做，每道题只改对应的 `NN_xxx.go` 文件，把
`panic("TODO: implement me")` 换成真正的实现——测试已经写好，不用改。

## 顺序

1. **`01_waitgroup.go`** —— `sync.WaitGroup` 基础：跑 N 个任务，等它们全部完成，按原始顺序收集结果。
2. **`02_channel_fanin.go`** —— channel 基础：把多个 channel 合并成一个，全部关闭后自己也要关闭。
3. **`03_select_timeout.go`** —— `select` + 超时：谁先返回就用谁的，这是 `errgroup.WithContext` "一处失败全局取消"的简化版。
4. **`04_mutex_counter.go`** —— 修一个真实的 data race，加 `sync.Mutex`。
5. **`05_mini_errgroup.go`** —— 复刻一个简化版 `errgroup.Group`：只有把 1-4 题吃透了，才写得出来。写完回头对比 [`../../internal/bff/aggregate.go`](../../internal/bff/aggregate.go) 里 `errgroup.WithContext(ctx)` 那一行，应该能看懂它内部大概在干什么。

## 怎么跑

```bash
cd server
go test ./exercises/concurrency101/...          # 跑全部
go test ./exercises/concurrency101/... -run TestRunAllWaitGroup -v   # 只跑第 1 题
go test -race ./exercises/concurrency101/... -run TestCounter        # 第 4 题务必加 -race，不加看不出问题
```

全部题目现在跑起来都是 `panic: TODO: implement me`——这是预期状态，说明测试挂上了、等你实现。

**注意**：Go 的 test 框架遇到没恢复的 panic 会直接中止整个测试进程，不会继续跑后面的测试。所以 `go test ./exercises/...`（不带 `-run`）在你还没实现第 1 题时，只会看到第 1 题 panic 然后整个进程退出，看不到第 2-5 题——这不是它们也挂了，是压根没机会跑到。按顺序一道一道用 `-run` 单独跑、实现一道再解锁下一道，最后全部做完了再跑一次不带 `-run` 的全量测试确认全绿。
