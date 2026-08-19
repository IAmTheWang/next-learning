# 学习笔记

跟 [PLAN.md](./PLAN.md) 不是一个东西——PLAN.md 是项目周期计划，这份是过程中遇到的具体概念/踩坑记录，按主题整理，方便复习。

## Go 后端基础

### `go run` 不是"语法糖"，是编译+运行+清理的便捷封装

**误解**：以为 `go run main.go` 直接执行了你写的代码，跟"语法糖"（语言层面的简写）是一回事。

**实际情况**：`go run` 是 Go 工具链的一个命令，做的事情是：

```
go run main.go
  ≈ 编译出一个临时二进制文件
  → 启动一个"中间人"进程去执行这个临时文件
  → 执行完/被打断后，删掉这个临时文件
```

它简化的是"编译+运行+清理"这一套**操作流程**，不是简化代码写法本身——所以准确说法是"便捷开发工具"，不是"语法糖"。

**为什么这个区别很重要（真实踩坑）**：`go run` 会多出一个"中间人进程"（go 工具自己），你的真正程序是这个中间人启动的子进程：

```
go run main.go
   ↓
中间人进程（go 工具自己，没有任何自定义信号处理逻辑）
   ↓
真正的程序（子进程，写了 signal.NotifyContext 逻辑）
```

按 `Ctrl+C` 时，操作系统把 `SIGINT` 同时发给这两个进程：
- 中间人没有自定义处理逻辑，收到信号立刻死掉，终端马上把控制权还给你（提示符瞬间出现）
- 真正的程序还在按你写的优雅关闭逻辑收尾，但这时候终端已经不再"陪着"它了，它打的日志你很可能看不到，或者看到时已经错过了时机

**教训**：想测试跟"程序自己收到信号/怎么退出"相关的行为，不要用 `go run`，要先 `go build` 出二进制再直接执行：

```bash
go build -o bin/server .
./bin/server
# 这时候只有一个进程，终端会一直陪着它，Ctrl+C 后能看到完整的收尾日志
```

### 常见误解：临时二进制被删除 ≠ 代码没被执行

用 `go run` 时按 Ctrl+C 看不到 `log.Println("收到关闭信号，开始收尾...")` 这行日志，容易误以为是"临时的可执行文件被撕掉了，所以那行代码没机会跑"。

**这是两件不相关的事**，理清因果顺序：

```
你按 Ctrl+C
   ↓
操作系统同时给"中间人"（go run 自己）和"真正的程序"发 SIGINT
   ↓
中间人没写任何信号处理逻辑 → 立刻死掉
   ↓
终端只认中间人是它的"孩子"，中间人一死，终端立刻把提示符还给你
   ↓
（这时候）真正的程序可能还在后台默默执行收尾逻辑，包括打那行日志
   ↓
但你已经拿回终端控制权、注意力移开了 —— 后面打的日志你大概率看不到
```

"临时二进制被删除"发生在**程序已经彻底跑完之后的扫尾阶段**，它不会主动打断正在执行的代码，也不是代码没被执行到的原因。真正的原因是：中间人进程先于真正的程序退出，把终端控制权提前还给了你，让你误以为"什么都没发生"——但那一行代码到底有没有真的执行、有没有打印出来，其实并不确定，只是你看不到了。

这也是为什么调试这类行为要用 `go build` + 直接跑二进制：去掉中间人，终端才会一直等着真正的程序自己退出，你才能准确看到每一行日志有没有被执行、什么时候被执行的。

### OS 信号（Signal）是什么

你在终端按 `Ctrl+C`，不是往程序的输入里"打字"，是操作系统直接给这个进程"发通知"，不经过程序的正常逻辑：

```
你按 Ctrl+C
   ↓
操作系统给这个进程发 SIGINT 信号
   ↓
默认行为：进程立刻终止（除非程序自己拦截了这个信号）
```

常见的几种信号：

| 信号 | 谁会发 | 能不能被拦截 |
|---|---|---|
| `SIGINT` | 终端 `Ctrl+C` | 能 |
| `SIGTERM` | `kill <pid>`、`docker stop`、K8s 下线 Pod | 能 |
| `SIGKILL` | `kill -9` | **不能**，操作系统直接强杀，程序没有机会收尾 |

`signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)` 做的事，就是"拦截" SIGINT/SIGTERM 的默认行为（立刻终止），转换成 `context` 的取消信号，让程序自己决定怎么收尾。SIGKILL 拦不住，这是操作系统设计上留的"最后一道保险"——防止程序耍赖不肯退出。

### `context` 是什么、`signal.NotifyContext` 具体做了什么

如果不用 `signal.NotifyContext`，等价的手写版本是这样（帮助理解它内部做了什么）：

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM) // 让系统把信号投进这个"信箱"
ctx, cancel := context.WithCancel(context.Background())

go func() {
    <-sigCh   // 死等信箱有信
    cancel()  // 收到信就手动取消 ctx
}()
```

`signal.NotifyContext` 把这一整套打包成一次函数调用，返回：
- `ctx`：一个"一旦收到信号就会被取消"的 context，取消后 `ctx.Done()` 这个 channel 会关闭，所有卡在 `<-ctx.Done()` 的代码会被唤醒
- `stop`：清理函数，停止对信号的监听，`defer stop()` 保证资源不泄漏

**为什么要用 context 这套机制，而不是直接操作信号 channel**：因为 Go 里"数据库查询""HTTP 请求""服务器关闭"等等几乎所有可能阻塞的操作，都统一接受一个 `context` 参数来表达"取消"。把系统信号翻译成 context 后，这些函数不需要额外写代码就能对"程序要关闭了"这件事做出反应。

### `time` 包 / 超时是什么时候用的

跟前端 `fetch` 配 `AbortController` 设置请求超时是同一个思路，只是发生在服务端。数据库查询、调外部服务的地方都该包一层超时，防止"下游卡住 = 我也无限期卡住"：

```go
pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
defer cancel()
```

`2*time.Second` 就是"2秒"的写法（`time.Second` 是"1秒"这个时长单位的常量）。

### `os.Exit(1)` 会跳过 `defer`，跟 `return` / `panic` 不是一回事

`os.Exit(n)` 立刻终止进程，`n` 是返回给操作系统的退出码（0 = 成功，非 0 = 异常）。

**最容易踩的坑**：`os.Exit` 不会执行任何 `defer`，也不会触发 `panic/recover`，是"直接拔电源"式的退出：

```go
func main() {
    defer fmt.Println("cleanup") // 永远不会打印
    os.Exit(1)
}
```

三种退出方式对比：

| 方式 | 执行 `defer` | 退出码 |
|---|---|---|
| `return`（从 `main` 正常返回） | 会 | 0 |
| `panic`（未被 `recover`） | 会（沿调用栈逐层执行） | 2 |
| `os.Exit(n)` | **不会** | n |

**实践建议**：需要清理资源（关文件、释放锁、日志落盘）时，尽量把 `os.Exit` 放在最外层（比如 `main` 里根据某个封装函数的返回值决定要不要退出），不要在有 `defer` 的深层函数里直接调用它。

### `http.NewServeMux`（标准库）vs gin：先吃透标准库，再切框架

标准库 `mux` 只提供最基础的路由能力，没有中间件链抽象、没有参数绑定/校验、没有路由分组；gin 在这些方面都更完善，社区插件多，大项目下路由匹配性能也更好（radix tree）。

**学习顺序建议**：先用标准库把 `ServeMux`、`Handler`、中间件模式、`context` 传递这些通用概念吃透，再引入 gin——这样会发现 gin 很多东西（比如 `gin.Context` 本质是包了一层 `http.ResponseWriter` + `*http.Request`）一看就懂，而不是死记 gin 的 API。

选型看场景：内部工具/学习项目/极简 API → 标准库够用；团队协作/需要中间件生态/追求开发效率 → gin。

### Go 1.22（2024-02-06 发布）的几个关键变化

**1. `for` 循环变量作用域修正**（最重要）：1.21 及之前，循环变量是**同一块内存被反复覆写**，导致这种经典 bug：

```go
for _, v := range items {
    go func() { fmt.Println(v) }() // 可能全部打印同一个值（通常是最后一个）
}
```

原因：`go func(){}()` 启动的 goroutine 不是立刻执行，是排队等调度；主循环跑得快，可能等 goroutine 真正被执行时，`v` 已经被后续循环覆盖成最后一个值了。1.22 起每次迭代都会分配**新的变量**，这个坑消失。

**2. `range` 支持整数**：`for i := range 5` 等价于 `for i := 0; i < 5; i++`。

**3. `ServeMux` 路由增强**（标准库拖到 2024 年才做，原因见下）：
- 方法前缀：`mux.HandleFunc("GET /users", h)`，不用在 handler 内部手写 `if r.Method == "GET"`
- 路径参数：`"GET /users/{id}"` + `r.PathValue("id")` 取出实际值，不用手写字符串切割
- 通配符结尾：`"GET /files/{path...}"` 匹配任意深度的子路径（多级路径整体捕获）

**4. `math/rand/v2`**：默认自动用真随机源初始化，不再需要手动 `rand.Seed(time.Now().UnixNano())` 这行历史包袱代码（老版本不手动 seed 的话，每次运行的"随机"序列都完全一样，因为默认种子是固定值）。

**为什么这些"基础功能"拖到 2024 年才加进标准库**：Go 1 兼容性承诺——标准库一旦发布的 API/行为几十年不能随便改，所以团队一贯策略是"复杂/多变的功能交给第三方库（gin/chi/gorilla-mux 早就做得很成熟），标准库只加已经有broad共识、几十年都不会后悔的东西"。路径参数语法（`{id}` vs `:id`）、路由优先级规则这些设计细节讨论了近 8 年才定稿，泛型（1.18）也是类似情况，拖了近 10 年。

### 为什么 handler 签名里 `r` 是 `*http.Request`（指针）

两个原因：
1. **避免拷贝大结构体**：`http.Request` 字段很多（Header、Body、URL、协议版本…），按值传递每次调用都要整份复制，用指针只传一个地址，省内存和 CPU
2. **`Body` 是流，必须共享同一份**：`Body` 底层连着 TCP 连接，是"只能顺序读一次"的数据流（类似直播信号，不能复制两份分别看），必须保证所有代码操作的是同一个 `Request` 实例。这是比"省内存"更硬性的原因——按值传递在语义上就是错的

标准库约定：代表"资源/状态"的类型（如 `*http.Request`）用指针传递；纯数值类型（`int`、`time.Duration`）按值传递就行。`http.HandlerFunc` 的签名是标准库定死的，写成按值传递会直接编译报错。

**"流"和"只能读一次"是两个不同概念**：流的本质是"数据按顺序到达/读取"（比如文件也是流，但文件可以 `Seek` 倒回去重读）；"只能读一次"是 `http.Request.Body` 这种连着实时网络连接的流的**额外**限制——读取游标只会往前走，没有倒带机制。想多次使用 body 内容，得自己先用 `io.ReadAll` 读出来存成 `[]byte`，之后就是普通数据，可以随便复用。

### `context.WithTimeout` 为什么必须搭配 `defer cancel()`

```go
pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
defer cancel()
```

`r.Context()` 是这次 HTTP 请求本身的生命周期（客户端断开连接时会被取消）；`WithTimeout` 在此基础上派生出一个新 context，多加一层"最多 2 秒"的限制——父 context 被取消或 2 秒到了，两者任一发生，`pingCtx` 就被标记为已取消。

**为什么不写 `defer cancel()` 会有问题**：`WithTimeout` 内部启动了一个独立的后台定时器（类似"预定了一个 2 秒后自动响的闹钟"），这个定时器不会因为你的函数提前返回就自动消失。如果操作提前完成（比如 0.1 秒就 ping 成功了）却不调用 `cancel()`，这个闹钟仍然在后台空跑到 2 秒才自动清理——单次问题不大，但高并发下每个请求都这样"忘记关闭"，会导致大量未到期定时器堆积，造成资源泄漏。`defer cancel()` 的作用就是"活干完立刻手动关闹钟"，不依赖超时机制自然到期。

### `&http.Server{...}` 里每个字段是什么、`&` 为什么在这

```go
addr := ":8090"
srv := &http.Server{
    Addr:    addr,
    Handler: handlers.WithLocalCORS(mux),
}
```

这一步只是"组装一个 server 对象"，还没开始监听端口：

- `":8090"`：冒号前面留空表示"监听本机所有网卡"，只指定端口。等价于 Node 里的 `app.listen(8090)`。
- `&http.Server{...}`：创建 `http.Server` 结构体实例并取指针。配置类对象在 Go 里习惯用指针传递，避免复制整个结构体；没写的字段（比如 `ReadTimeout`）就用零值/默认值。
- `Addr`：告诉这个 server 该监听哪个地址。
- `Handler`：真正处理请求的入口，这里传进去的是包了一层中间件的 `mux`（见下面的中间件小节）。

### 为什么等待关闭信号的逻辑必须放进单独的 goroutine

```go
go func() {
    <-ctx.Done() // 等信号
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    srv.Shutdown(shutdownCtx)
}()
```

`srv.ListenAndServe()` 是同步阻塞调用，会一直卡住直到服务器停止才返回。main 函数同时需要"启动服务器"和"等待关闭信号"两件事，但一个 goroutine 同一时刻只能阻塞在一行代码上——谁写前面谁就把后面的代码堵死：先等信号就永远启动不了服务器，先启动服务器就永远等不到信号。所以必须把"等信号→触发 Shutdown"这段拆到独立 goroutine，跟 main goroutine（跑 `ListenAndServe`）并发执行，两者各自阻塞在不同地方，互不影响。等信号真的来了，`Shutdown()` 内部会让卡在 `ListenAndServe()` 的 main goroutine 返回（返回值是 `http.ErrServerClosed`），程序才能继续走到退出逻辑。

对比 Node：Node 的 `server.listen()` 本身不阻塞事件循环（非阻塞 I/O），可以直接在同一个"线程"里接着写 `process.on('SIGINT', ...)`，两者靠事件循环自然并发，不需要手动开一条执行流。Go 的 `ListenAndServe()` 是真正同步到底的调用，"需要并发就必须显式开 goroutine"和 Node "默认异步、隐式并发"是两种语言并发模型的核心区别之一。

### `if err := f(); err != nil {}` 里，`err` 出了 if 就没了

```go
if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
    log.Fatalf("server error: %v", err)
}
// 这里已经出了 if 块，err 不存在了
```

Go 的 `if` 支持"初始化语句"，写法是 `if 初始化语句; 条件 { ... }`。这里 `err := srv.ListenAndServe()` 就是初始化语句，它声明的 `err` 作用域仅限这个 if 语句本身（含 `else`/`else if` 分支），出了花括号就彻底消失。好处是这个局部 `err` 不会污染外层命名空间、不会跟别处的 `err` 冲突。等价于 TS/JS 里 `if (true) { const err = doSomething(); ... }`——都是"变量生命周期被限制在某个块里"，只是 Go 把"声明"和"条件"揉进了同一行语法糖。

判断要不要把 `err :=` 写在 if 外面，看后面还要不要复用它：只在这一次判断里用，就写进 if 的初始化语句（如 `pool.Ping` 那处）；后面还要 `defer` 或者往外传，就得声明在 if 外面（如 `pool, err := db.Connect(...)` 后单独一行 `if err != nil`）。

### `pingCtx` 到底是什么——一个"截止时间 + 取消开关"的信号载体

```go
pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
defer cancel()
```

`context.WithTimeout` 拿一个已有 context（`r.Context()`，这次请求专属）派生出一个新的、带"倒计时"的子 context。`pingCtx` 不装业务数据，它是"遥控器 + 秒表"：秒表 2 秒后自动按下取消键；`pool.Ping(pingCtx)` 内部一边查数据库一边监听这个遥控器，2 秒内没结果就自动放弃、返回超时错误，不会傻等。

### `json.NewEncoder(w).Encode(...)` 在干什么

```go
json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "go-api"})
```

两步：`json.NewEncoder(w)` 创建一个编码器，告诉它"编码完的 JSON 文本写到 `w`（实现了 `io.Writer`）里去"；`.Encode(data)` 把 Go 值转成 JSON 文本并直接写出去。等价于手写 `data, _ := json.Marshal(...)` 再 `w.Write(data)`，`Encode` 只是把"转换"和"写出"合并成一步。

### struct 是值类型，不等于 TS/JS 的 object

struct 不是基础类型（int/float64/bool/string 那种语言内置、不可再拆的），它是**复合类型**（用基础类型拼出来的形状）。但它和 int 共享一个更重要的属性：**都是值类型**，赋值/传参时整体复制：

```go
type Point struct{ X, Y int }
p1 := Point{X: 1, Y: 2}
p2 := p1        // 复制了一份新的，p1、p2 互不影响
p2.X = 100
fmt.Println(p1.X) // 仍是 1
```

JS/TS 的 object 默认是**引用类型**，赋值只是多一个指向同一份数据的名字：

```js
let obj1 = { x: 1 };
let obj2 = obj1;
obj2.x = 100;
console.log(obj1.x); // 100，被共享影响到了
```

一个好记的比喻：**struct 赋值 = 每个人拿到自己独立的一本书，你撕掉你那本，我的完全不受影响；object 赋值 = 几个人合租一间房子，谁把房子点了，所有人一起无家可归**——因为大家手里的"钥匙"（引用）指向的是同一个房子。

这也是为什么函数签名想要"共享同一份数据、互相影响"时，Go 必须显式加 `*` 变成指针（比如 `*http.Request`）——普通 struct 传参默认是"复印一本新书"，不会共享。

（进阶备注：如果 struct 内部字段本身是 slice/map/指针，复制 struct 时这些字段本身也是被复制的"钥匙"，钥匙指向的底层数据还是同一份——所以"复制 struct"只保证最外层独立，内部的引用类型字段依然共享。）

### `http.ResponseWriter` 是接口，为什么不用指针也能改到真正的响应

`http.ResponseWriter` 只是一份方法签名清单（`Header()`、`Write()`、`WriteHeader()`），不存数据，是一种"资格认证"。`net/http` 内部在调用 handler 前，会先造一个满足这份认证的具体对象（一个叫 `*http.response` 的结构体指针），再把这个指针"装进"接口变量 `w` 里传给你。

接口变量在 Go 底层的存储方式可以理解成两格的小盒子：`[ 类型信息 ][ 指向真实数据的地址 ]`。也就是说接口变量的第二格本来就存着一个地址，不管你外面加不加 `*`，里面包着的都已经是"指向同一份数据的地址"了——所以接口类型从不用指针接收，那样是多余的双重间接。这正是为什么在 handler 里调用 `w.WriteHeader(...)`、`w.Write(...)` 能真正改到马上要发给客户端的响应本身，而不是操作一份跟外界无关的复制品：`w` 里的地址和服务器内部持有的 `*http.response` 指向的是同一块内存。

## HTTP 协议基础

### HTTP 状态码是谁定义的、5xx 和 4xx 分别是谁的锅

HTTP 状态码不是 Go 发明的，是 **HTTP 协议标准**的一部分，由 IETF 写进 RFC 文档：HTTP/1.0（RFC 1945，1996 年）最早定义了一批状态码，HTTP/1.1（RFC 2068 1997 → RFC 2616 1999）扩充完善，现行标准是 2022 年重新整理发布的 RFC 9110。任何语言、任何服务器只要遵守 HTTP 协议，用的都是同一套编号，含义全球通用。

状态码按首位数字分类，本质是在回答"这次请求，问题出在谁身上"：

| 范围 | 类别 | 谁的问题 |
|---|---|---|
| 2xx | 成功 | 没问题，比如 200 OK |
| 3xx | 重定向 | 资源换地方了 |
| 4xx | 客户端错误 | **发请求的这一方**——请求本身有问题（格式错、没权限、要的东西不存在……） |
| 5xx | 服务端错误 | **接收请求处理的这一方**——服务器自己出了问题 |

"客户端"在这里就是"发起请求的一方"（浏览器、App、或者另一个调用你 API 的服务），跟"接收并处理请求的服务器"相对。

- **404 Not Found**（4xx，客户端问题）：服务器代码是好的、没崩，只是你请求的这个资源（这个 URL 路径）压根不存在。就像你去便利店问"卫生纸在哪个货架"，店员说"没有第 8 货架"——不是店员业务能力不行，是你问的这个货架编号本身指向了不存在的东西。
- **500 Internal Server Error**（5xx）：最笼统的"服务器内部出错了"，通常是代码本身抛异常/panic，服务器"自己也说不清具体原因"，只能说"我这边炸了"。
- **503 Service Unavailable**（5xx）：服务器知道具体原因——不是代码炸了，是当前暂时没法正常服务（依赖的数据库连不上、正在维护、主动限流），隐含"这是暂时的，晚点再试"。`server/main.go:81` 数据库连不上用 503 而不是 500，就是因为这是更精确的错误归因：handler 代码没 bug，只是外部依赖暂时不可用。

### `http.Handler` 接口和 `ServeHTTP` 方法

`net/http` 的路由/中间件体系都建立在一个只有一个方法的接口上：

```go
type Handler interface {
    ServeHTTP(w ResponseWriter, r *Request)
}
```

`ServeHTTP` 就是"处理一次请求"这件事本身——任何类型只要实现了这个方法，就自动满足 `http.Handler` 这个"能接待请求"的资格认证。`mux`（`*http.ServeMux`）内部就是靠自己的 `ServeHTTP` 方法，按请求的 method+path 去查表，再把请求转交给你用 `HandleFunc` 注册的具体函数。而普通的 `func(w, r)` 函数本身没有 `ServeHTTP` 方法，`http.HandlerFunc` 是标准库提供的一个"适配器"类型，把这种函数包一层就能当 `http.Handler` 用——这也是 `WithLocalCORS` 里 `return http.HandlerFunc(func(w, r) {...})` 这行的用意。

### 中间件模式：`WithLocalCORS(mux)` 这种"函数包函数"在干什么

把整个请求处理流程想象成一栋大楼的门卫系统：

- **客户端的请求** = 有人走到大楼门口，说"我要去 XX 部门办事"（method + path，比如 `GET /health`）。
- **`http.Handler` 接口** = "能接待来访者"的资格认证，谁有 `ServeHTTP` 方法就有资格接人。
- **`mux`（`ServeMux`）** = 前台总机，按你要去的部门（路径/方法）把你分配到正确的房间（具体注册的 handler）。
- **`WithLocalCORS(mux)` 这层中间件** = 大楼门口的安检+登记处，所有人进楼前都先经过这里：
  1. 先给你贴一个通行证（设置 `Access-Control-Allow-Origin` 等响应头）；
  2. 如果你是"来打探路况的侦察兵"（浏览器自动发的 `OPTIONS` 预检请求，问"我能不能访问你"），安检直接说"可以"，发一张 204 通行证就结束，不再往楼里走；
  3. 如果是正常访客，登记完直接把你转交给总机（调用 `next.ServeHTTP(w, r)`），总机再分配到具体部门。

请求真正的流转顺序：

```
请求 → 安检层 WithLocalCORS（贴CORS头 / 拦截OPTIONS） → 总机 mux（按路径分发） → 具体部门 handler（比如 /health）
```

`func(http.Handler) http.Handler` 这种"接收一个 Handler、返回一个新 Handler"的函数签名，就是"安检岗"这个角色的代码形态：它把原来的总机包在里面，对外返回一个新的、多了安检逻辑的入口。最终注册到 `http.Server.Handler` 上的就是这一整条链子的最外层，但从外面看依然只是一个满足 `http.Handler` 接口的普通对象——这就是 Go 标准库里"中间件"的实现手法，等价于 Express 里 `app.use(corsMiddleware)` 再挂路由，只是 Go 没有 `app.use()` 这种链式 API，靠"函数包函数"手写出链条。

### 200 vs 204：区别是"有没有 body"

两个都是 2xx（成功），区别在于响应里有没有实质内容：

- **200 OK**：请求成功，而且服务器返回了内容（比如一段 JSON）。
- **204 No Content**：请求成功，但服务器明确告诉你"没有内容要返回"，body 是空的。

比喻：你去问店员"你们今天营业吗"，店员说"营业"（204，回答完了，没别的要说）；你去问店员"给我拿包薯片"，店员真的把薯片递给你（200，给了实质内容）。两次都是"成功"，区别只是有没有"东西"跟着这个成功一起给你。`WithLocalCORS` 对 `OPTIONS` 预检请求回 204，就是因为这次交互只是"问一下能不能访问"，没有业务数据要给。

### CORS 的本质是"浏览器的同源限制"，不是"前端 vs 后端"

curl、Postman 不受 CORS 限制，准确原因是**它们不是浏览器，不受浏览器的同源策略（Same-Origin Policy）约束**——这条规则是浏览器这个软件自己加的，只有代码跑在浏览器的 JS 引擎里、通过 `fetch`/`XMLHttpRequest` 发请求时才会触发。真正的分界线是"浏览器 vs 非浏览器"，不是"前端 vs 后端"：

- Next.js 的 BFF 代码属于"前端框架"，但它调用 Go API 那部分是跑在 **Node.js 服务器端**，不是浏览器里执行的，所以同样不受 CORS 限制。
- 反过来，任何非浏览器环境（Node 脚本、原生 App、curl）发起的请求，都躲开了 CORS。

### 没有中间件会怎样：服务器照常处理，"拦截"发生在浏览器内部

去掉 `WithLocalCORS`，`mux` 该怎么路由还是怎么路由，服务器"要不要处理"这个请求完全不受影响——中间件只是多加了几个响应头。

真正做拦截判断的是**浏览器**，不是"前端 JS 代码主动判断后拦截"：浏览器发跨域请求前会先自动发一个 `OPTIONS` 预检请求（这一步 JS 代码不知情），检查响应里有没有 `Access-Control-Allow-Origin` 等 header；没有的话，浏览器直接在内部把这次交互短路掉，不把响应内容交给 JS 的 `.then()`，代码根本没机会执行到处理响应那一步，控制台报 "blocked by CORS policy"。更准的说法是"浏览器拦截"，不是"前端拦截"。

### `method + path` 是"组合锁"，匹配要两个条件同时成立

```go
mux.HandleFunc("GET /health", ...)
```

这是一条注册规则："只有当方法**正好是 GET**、路径**正好是 `/health`** 时才用这个函数处理"——method 和 path 两个条件必须**同时**满足，不是"路径对上就算数"。

浏览器发的 `OPTIONS /health` 请求：path 对上了，method 没对上，整条规则算不匹配。**Go 1.22+ 的 `ServeMux` 在这种情况下返回的是 405 Method Not Allowed（并带 `Allow` header 告知支持哪些方法），不是 404**——404 只在"这个路径压根没被任何模式注册过"时才出现；只要路径本身有登记（哪怕方法不对），就是 405。不影响"浏览器会拦截"的结论：不管服务器回 404 还是 405，只要响应里没有正确的 CORS header，浏览器照样不会把内容交给 JS。

比喻：一把需要同时转动两把钥匙才能开的门——"GET 钥匙 + `/health` 钥匙一起转，门才开"。拿着 "OPTIONS 钥匙" 和 "`/health` 钥匙" 过来，`/health` 那把插得进去，`OPTIONS` 那把插不进 GET 的孔——门的反应不是"我没见过这个地方"（404），是"这个地方我知道，但你这把钥匙不对"（405）。

### 为什么 CORS response header 不能由前端自己加

两层原因：

1. **技术上前端没有写响应的权限**：CORS header 是**响应（response）里的 header**，由服务器生成响应时决定放什么进去；前端只是被动接收、读取，没有渠道往"别人已经生成好、正在传回来的响应"里插内容。
2. **安全设计上，批准必须来自被保护的一方**：CORS 存在的目的是防止恶意网站 B 借着用户浏览器里网站 A 的登录状态，偷偷读取 A 上属于用户的数据——"允不允许 B 访问"这个问题只能由 A（服务器）自己回答，不能让发请求的一方（前端/网站 B）自己在请求里声明"我被允许"，那样等于让贼自己写一张"我经过许可"的字条,安检彻底失效。

配套细节：请求里的 `Origin` 头（告诉服务器"我是从哪个网站发出来的"）是**浏览器自动填的**，属于 fetch/XHR 规范里明确规定 JS 代码**不能手动覆盖**的"禁止头"——前端无法伪造自己的身份。所以是两层锁：身份（`Origin`）浏览器自动贴、前端改不了；批准（CORS response header）服务器自己盖章、前端加不了。

### 项目里 `Access-Control-Allow-Origin` 这行到底有没有做"检查"

```go
w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000") // middleware.go:11
```

这行现在是**无条件声明**，不是"检查"：它没有读请求带来的 `Origin` 是什么、没有做任何比较，永远固定回答"我允许 `http://localhost:3000`"。真正会不会放行，是浏览器拿这个声明去跟"当前发请求的页面自己的 origin"做比对——对得上才放行，对不上照样拦截，即使服务器这行代码"看起来"已经批准了。

如果要让服务器真正做检查（比如支持多个白名单来源），得读 `r.Header.Get("Origin")` 再跟白名单比对，命中才把这个值动态塞进响应头，而不是写死一个固定值。

### `vue.config.js` 的 `devServer.proxy` 是怎么"绕开"跨域的

不是"解决了跨域规则"，而是让浏览器**从头到尾都没有跨过域**——一种障眼法：

```
浏览器 → (请求发到 http://localhost:8080/api/xxx，和当前页面同源！)
       → vue-cli-service 自带的开发服务器（本质是个 Node 进程）
       → 开发服务器再把这个请求转发去 http://localhost:8090/xxx（真正的后端）
       → 后端处理完返回给开发服务器
       → 开发服务器再把结果传回浏览器
```

关键点：浏览器**只知道**自己在跟 `localhost:8080` 通信（地址栏 origin 就是这个），完全不知道 `8080` 背后偷偷转发去了 `8090`。中间这一跳转发是**服务器到服务器**的调用（Node 进程发 HTTP 请求给 Go 后端），跟 curl/Postman 不受 CORS 限制是同一个道理——不是浏览器发起的，浏览器管不到，浏览器全程没有意识到发生了跨域。

生产环境常见的同类思路：nginx 反向代理、Next.js 的 `rewrites`，本质都是"把跨域请求伪装成同源请求"。

### 动态白名单版的 CORS 检查代码：读 `Origin` + 查表

```go
origin := r.Header.Get("Origin")
allowed := map[string]bool{"http://localhost:3000": true, "https://staging.example.com": true}
if allowed[origin] {
    w.Header().Set("Access-Control-Allow-Origin", origin)
}
```

逐行拆解：

- `r.Header.Get("Origin")`：读出**这次请求**里浏览器自动带上的 `Origin` 头（比如页面是从 `http://localhost:3000` 发出的，读到的就是这个字符串）。这个头浏览器自动填、前端 JS 改不了，所以读到的是"可信"的真实来源。
- `allowed := map[string]bool{...}`：用 map 模拟一个"集合"（set）——key 是允许的来源字符串，value 全填 `true`，只是为了能用 `allowed[某个字符串]` 快速查"这个 key 在不在表里"。
- `if allowed[origin] { ... }`：**真正的检查**发生在这里——拿这次请求实际的来源去查白名单表，命中才把这个值动态塞进响应头；不命中就什么都不设置，浏览器那边自然会因为缺 header 而拦截。

跟项目现状（[middleware.go:11](server/internal/handlers/middleware.go:11) 无条件写死同一个值）对比：

| | 项目现状（写死一个值） | 动态白名单版 |
|---|---|---|
| 有没有读 `Origin` | 没读，完全不看请求来源 | 读了 `r.Header.Get("Origin")` |
| 是否有真正的判断 | 没有，永远声明允许同一个固定值 | 有，查表决定是否放行、放行谁 |
| 能支持几个来源 | 只能硬编码一个 | 可以配置任意多个（本地 + 预发布环境等） |

一句话：前者是"门口贴死了一张固定名字的通行证"，后者是"门卫真的核对来访者身份，再决定发不发通行证、发给谁"。

## curl / 终端相关

### curl 不是"程序员专用测试工具"，是通用的命令行 HTTP 客户端

用途包括但不限于：本地开发调试、定时任务里的健康检查、CI/CD 部署后的验证脚本、生产环境排查问题。

### 怎么让 curl "一直请求"

curl 本身只发一次请求就结束，"一直请求"是靠 shell 的循环语法：

```bash
while true; do curl -s localhost:8090/health; echo; sleep 1; done
```

直接在终端里敲这一整行回车执行即可，不需要写成文件。`Ctrl+C` 停止这个循环本身。
