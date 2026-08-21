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

## BFF 并发聚合（errgroup + context）—— 面试项目实现

这是真实落地进本项目的代码，不是纸面上的 PoC 片段，而且已经打通了完整的 Next.js + Go 双层架构（跟 `middleware.go` 注释里说的"Next.js BFF 服务端到服务端调用 Go 服务"是同一个模式）：

- [`internal/bff/aggregate.go`](server/internal/bff/aggregate.go)：核心并发聚合逻辑
- [`internal/bff/aggregate_test.go`](server/internal/bff/aggregate_test.go)：单元测试 + benchmark
- [`main.go:103-135`](server/main.go)：Go 侧真实路由 `/bff/aggregate`（并发）与 `/bff/aggregate-serial`（串行），`go run .` 之后可以直接 curl
- [`web/app/api/bff-aggregate/route.ts`](web/app/api/bff-aggregate/route.ts)：Next.js 侧的 Route Handler，服务端并发调用上面两个 Go 接口并各自计时，浏览器只跟这个 route 打交道（同源，不涉及 CORS）
- [`web/app/bff-demo/page.tsx`](web/app/bff-demo/page.tsx)：一个可视化对比页面，点"运行对比"按钮，实时显示并发版 vs 串行版的真实耗时和百分比差异；首页（`web/app/page.tsx`）也加了入口链接

### 和最初那版 PoC 代码相比，做了两个关键调整

1. **框架从 Gin 换成了标准库 `net/http`**——因为这个项目从一开始就是纯标准库 mux（见 `main.go` 里 `http.NewServeMux()`），没有引入 Gin。如果简历上写"Go / Gin / errgroup"，但代码库里根本没有 Gin，面试官一旦细问或要求过一遍代码就会露馅。所以简历这一行建议改成 **"Go（标准库 net/http）/ errgroup"**，除非你在别的项目里确实用过 Gin，那是另一码事，不要混在同一段描述里。
2. **`errgroup` 补上了 panic recovery**——最初的 PoC 代码问答里已经指出"errgroup 不会自动 recover panic"这个坑，这次直接把它变成了代码而不是停留在回答里：`aggregate.go` 里的 `safeGo()` 包了一层 `defer recover()`，把某个上游 panic 转换成普通 error，不会拖垮整个进程。测试 `TestSafeGoRecoversPanic` 直接验证了这一点。

### 用真实 benchmark 数字替换"约 60%+"这种估算

```bash
go test ./internal/bff/... -bench=. -benchtime=20x -run=^$
```

在这台机器（Apple M5）上，3 路上游各 150ms 延迟的场景下跑出来的真实数字：

```
BenchmarkAggregateSerial-10      20    453998433 ns/op   # ≈454ms/次
BenchmarkAggregateParallel-10    20    151346025 ns/op   # ≈151ms/次
```

454ms → 151ms，**延迟降低约 66.7%**，比最初简历里写的"60%+"还要好看一点，而且是真跑出来的、可复现的数字，不是估算——面试被追问"这个数字怎么来的"，可以直接甩一句 `go test -bench=.` 现场跑给对方看。

同时接进了两个真实路由，可以直接对比：

```bash
go run .
curl -w '\n%{time_total}s\n' localhost:8090/bff/aggregate         # 并发版，≈0.15-0.2s
curl -w '\n%{time_total}s\n' localhost:8090/bff/aggregate-serial  # 串行版，≈0.45-0.5s
```

### 简历技术描述（修订版）

> **BFF 层并发聚合 API（Go 标准库 net/http / errgroup）**
> 针对多上游服务调用场景下的接口响应延迟问题，设计并实现基于 `errgroup` + `context` 的并发聚合方案：将原本串行调用 3 个上游 API 的 I/O 密集型逻辑改造为 goroutine 并发执行，通过 `errgroup.WithContext` 实现"一处失败、全局取消"的错误传播机制，并利用 `context.WithTimeout` 对整体聚合请求设定统一超时边界。相比串行方案，在 3 路上游、单路 150ms 延迟的 benchmark 场景下，端到端延迟从 ≈454ms 降至 ≈151ms，降低约 **66.7%**（`go test -bench=.` 可复现）。方案同时用单元测试验证了 panic 恢复（防止单个上游异常拖垮整个进程）与超时熔断（context 取消能让请求快速失败而非傻等）两个生产级并发安全要点。

### 面试问答演练（已对照真实代码校准）

**Q1：如果某个上游 goroutine panic 了，会发生什么？如何处理？**
`errgroup` 本身不会自动 recover panic——一个 goroutine panic 会直接让整个进程崩溃。本项目里 `aggregate.go` 的 `safeGo()` 函数专门包了一层 `defer recover()`，把 panic 转换成普通 error 交给 `g.Wait()`，测试 `TestSafeGoRecoversPanic` 直接验证：故意让一个任务 panic，断言 `g.Wait()` 拿到的是 error 而不是进程崩溃。

**Q2：`context.WithTimeout` 超时后，那些还在跑的 goroutine 会立刻停止吗？**
不会立刻停。`context` 取消只是关闭 `ctx.Done()` channel，通知 goroutine "该退出了"，真正能不能停下来取决于内部有没有用支持 context 的调用。本项目里 `fetch()` 用的是 `http.NewRequestWithContext`，所以底层网络连接会在超时那一刻真正被中断——这也是 `TestAggregate_TimeoutCancelsSiblings` 这个测试要专门验证的点：给 3 个 200ms 慢的上游只留 20ms 预算，断言真实耗时远小于 200ms（证明是靠 context 取消提前失败，不是傻等上游自然结束）。

**Q3：这种并发聚合模式下，goroutine 泄露最常见的场景是什么？怎么规避？**
最常见的场景是某个 `g.Go` 里的调用没有正确响应 `ctx.Done()`（不支持 context 的阻塞调用、无缓冲 channel 发送卡住等），即使 `g.Wait()` 已经因超时提前返回，那个 goroutine 依然挂着。规避方式：① 所有 I/O 强制走 context-aware 的 API（本项目全程用 `http.NewRequestWithContext`）；② 用 pprof 的 goroutine profile 观测生产环境的 goroutine 数量趋势。

**Q4：Goroutine 本身的开销有多大？是不是并发路数越多越好？**
单个 goroutine 初始栈约 2KB，开销远小于 OS 线程，但不是零成本——尤其是每一路都带来独立网络连接、GC 压力和调度负担。本项目场景是"固定 3 路上游"，直接开 3 个 goroutine 没问题；如果扇出路数不固定、可能很大（比如遍历一个不定长列表逐条请求），就要用 `errgroup.SetLimit(n)` 限制并发上限，防止打爆下游或本机连接数——这是当前代码**没有覆盖**的场景，如果面试官顺着问下去，要老实说"当前实现是固定 3 路，没有做限流，量大就得加 `SetLimit`"，不要不懂装懂。

## AbortController / fetch —— 前端这边的"取消机制"，对应 Go 的 `context`

### AbortController 是谁封装的

浏览器（以及 Node.js）自己内置的，不是第三方库、也不是某个程序员写的 JS 代码。它跟 `fetch`、`document`、`window`、`setTimeout` 是同一级别的东西——浏览器厂商（Chrome/Firefox/Safari）在引擎底层用 C++ 实现好，暴露一个接口给 JS 直接用。不需要 `import`、不需要装 npm 包，因为它是 **WHATWG**（浏览器厂商联合组成的标准组织）制定的官方标准的一部分，所有浏览器都必须内置实现。

### 为什么要拆成 `controller` 和 `controller.signal` 两半

`AbortController` 本体拥有"发起取消"的权力——调用 `controller.abort()` 才能触发取消。如果把整个 `controller` 传给 `fetch`，`fetch` 内部理论上也能自己调用 `abort()`，把不该由它决定的事情也决定了。所以设计上把权力拆开：

|            | `controller` 本体            | `controller.signal`                  |
| ---------- | ----------------------------- | ------------------------------------- |
| 谁拿着     | 发起取消的一方（自己的代码）   | 传给需要"被取消"的操作（比如 `fetch`）|
| 能干什么   | 有权按下 `abort()`             | 只能"听"，不能主动触发取消            |
| 类比       | 遥控器                         | 遥控器发出的红外线信号                |

**员工/老板类比**：`fetch` 是员工，干活干到一半，自己没有权力说"老子不干了"——叫停的权力只在老板手里，这权力本身就是 `controller`；员工能做的是"听到老板喊停之后主动停下来"，这个"能听懂命令并做出反应"的能力就是 `signal`。员工手里没戴"对讲机"（没接 `signal`）的话，老板喊破喉咙也听不见，会一直闷头干到自然结束——这正是"老库不支持 context/signal"时的处境。

### 为什么叫 "signal"

这个词照搬现实里"信号"的概念——红绿灯、消防警报铃：一方广播出状态变化，所有"正在监听"的人自己决定要不要反应，发信号的人不用挨个通知每一个人。`fetch` 拿到 `signal` 后，官方实现里主动加了一个监听（`signal.addEventListener('abort', ...)`），一旦触发就中断底层网络连接——这段监听逻辑是浏览器帮你写好的，自己写的一个不接 `signal` 的 `setTimeout` 包装函数，就不会被取消，因为没人帮它写那段监听逻辑。

### 和 Go 的 `ctx.Done()` 完全对得上

| JS | Go | 作用 |
| --- | --- | --- |
| `AbortController` 本体 | `context.WithCancel(...)` 返回的东西 | 拥有"发起取消"权力的一方 |
| `controller.abort()` | 调用 `cancel()`（或超时自动触发） | 按下取消开关 |
| `controller.signal` | `ctx` 本身（传给下游函数的那个） | 只读的"监听凭证" |
| `signal.addEventListener('abort', ...)` | `select { case <-ctx.Done(): ... }` | 主动监听"信号响了没" |
| `signal.aborted`（布尔值） | `ctx.Err()`（非 nil 就是已取消/超时） | 查一下现在触发了没 |

一句话总结：`signal` 这个名字本身就在提示——它是"广播出去的状态"，不是"强制命令"，不会主动打断任何代码，只是安静待在那儿，谁写了代码去"听"，谁才会反应。

## fetch 是谁写的、fetch vs axios

### fetch 分两层

- **标准/规范层**：由 WHATWG（Google、Mozilla、Apple、Microsoft 等厂商组成）制定"fetch 该长什么样"的规范文档，类似"红绿灯该是几种颜色"这种交通规则。
- **实现层**：每家浏览器厂商照着规范用自己的引擎语言（大多 C++）真正写出来——Chrome/Edge 是 Google 的 V8+Blink 团队写的，Firefox 是 Mozilla 写的，Safari 是 Apple 写的，Node.js 从 v18 开始内置了基于 `undici` 库的实现。所以在 Chrome 和 Firefox 里敲同一行 `fetch(...)`，跑的是两家公司各自写的代码，但因为都照着同一份规范，JS 代码不用改，行为看起来一致。

### fetch vs axios 对比

| | fetch | axios |
| --- | --- | --- |
| 出身 | 浏览器/Node 官方内置，不用装包 | 第三方 npm 库（Matt Zabriskie 最早写的，后转社区维护） |
| 请求失败判定 | 大坑：只有网络层彻底失败（断网/DNS 挂了）才 reject；404/500 这类 fetch 认为"请求本身成功"，不会自动 reject，得自己判断 `response.ok` | 自动把 4xx/5xx 当 error 抛出来，更符合直觉 |
| 好用的特性 | 原生支持 `AbortController`、streaming，Next.js 官方还给它加了缓存能力 | 自带请求/响应拦截器、自动 JSON 序列化、超时配置更简单 |

新项目/轻量项目现在（2026）主流建议优先用原生 `fetch`；老项目/企业级项目大量还在用 `axios`，主要是历史惯性 + 拦截器（"所有请求自动带 token"、"所有 401 自动跳登录页"这类全局逻辑）比原生 fetch 顺手。

### 时间线：为什么 axios 感觉"火了很久"

- **2017 年之前，`AbortController`/`signal` 压根不存在**：Firefox 2017 年底、Chrome 2018 年、Safari 2019 年才落地。在这之前 fetch 发出去的请求真的没法取消——2015 年 fetch 规范刚出来时只顾着用 Promise 替代回调，没设计"取消"，业内骂了两年才补上。（但更早的 `XMLHttpRequest`，约 2005-2006 年就有，自带专属的 `.abort()` 方法，只是写法啰嗦。）
- **axios 约 2014 年底发布，2015-2016 年火起来**，到 2026 年已经 10+ 年——它出生时正好补上 fetch 当年一堆坑：自动 JSON 转换、4xx/5xx 自动抛错、拦截器、自己的 `CancelToken` 取消机制（比 fetch 官方拿到 `AbortController` 支持早了好几年）。
- **Node.js 原生 `fetch` 直到 2022 年 Node 18 才出现**，这之前 Node 后端场景下 `axios`（或更早的 `request` 库）几乎是唯一选择长达将近 8 年，进一步巩固了它的地位。
- 现在 axios 依然大量存在，主要不是技术上还领先（fetch 已经追平甚至反超大部分场景），而是十年积累的存量代码 + 拦截器这类场景依然更顺手，企业级项目缺乏动力迁移。

## panic / recover / defer 机制精讲

### `panic(...)` 是干什么的：占坑代码里的用法

`panic(...)` 是 Go 内置的"立刻中断当前执行、开始退栈"机制，类似 JS 的 `throw`，但默认更暴力——没人 `recover()` 的话，整个进程直接崩溃退出，并打印传进去的值 + 调用栈。[`server/exercises/concurrency101/01_waitgroup.go`](server/exercises/concurrency101/01_waitgroup.go) 里的 `panic("TODO: implement me")` 就是故意用它当"占坑符"：比起 `return nil`（会让测试报出"结果不对"这种含糊错误），`panic` 一调用就炸出清楚的文字 + 精确的文件行号，消除"没实现"和"实现错了"之间的歧义。

### `defer` 到底是不是"立即执行函数"

关键看**结尾有没有紧跟着的 `()`**：

```go
func() { ... }()      // 结尾有 ()，这是"定义完立刻调用"，IIFE
g.Go(func() { ... })  // 结尾没有 ()，只是把函数值当参数传出去，由 g.Go 自己决定何时调
```

`defer func(){...}()` 里那对 `()` 让它在语法上确实是一次函数调用；但 `defer` 关键字劫持了这次调用的**执行时机**——登记的动作立刻发生，真正执行被推迟到外层函数返回前(不管是正常 `return` 还是 panic 退栈)。

`defer` 后面也不是必须跟匿名函数——它要求的是"一个函数调用表达式"，可以调用已经存在的具名函数，真实的 `errgroup.Go` 源码里 `defer g.done()` 就是调用一个现成方法，不是当场定义匿名函数。`recover()` 场景习惯写 `defer func(){...}()`，只是因为需要一段"这里专属、别处用不到"的逻辑，只能当场写。

### 为什么 `recover()` 能接住 panic——退栈机制 + 图书馆比喻

`panic()` 触发后，Go 不是瞬间让程序消失，而是开始"退栈"：停止当前函数继续往下执行，但会把**已经登记好**的 `defer` 清单挨个执行一遍，再把这个"爆炸"往上一层调用者传递，一层层向上，直到某一层的 `defer` 里调用了 `recover()`（爆炸在这层被接住，不再上传，函数正常返回），或者一路传到最顶没人接住（进程真的崩溃）。

**核心限制**：`defer` 必须在风险代码执行**之前**就已经登记好，不能事后补——如果把 `defer` 写在会 panic 的那行代码后面，panic 一响，代码根本执行不到那一行 `defer`，它连"登记"这一步都没发生，退栈时清单上没有它，接不住。这跟"图书馆管理员"的比喻完全对应：这个指令必须在"出事之前"先交代好("不管等会发生什么，关门前必须做 XX")，不能等出事以后才现场补交代——补交代的时候已经来不及了，程序已经在往外退的路上，不会再执行后面新写的指令。

### `recover` 是谁定义的、什么时候有的

`recover` 和 `panic`、`defer` 一样，是 Go **语言内置**的东西（跟 `len`/`cap`/`make`/`append` 同一类），不需要 `import`，由编译器 + 运行时联合实现（编译器识别"这个 `recover()` 是不是直接写在 defer 函数里"，运行时维护"现在是不是正在退栈"这个状态）。这是 **Go 语言规范**（Go Spec，"Handling panics"一节）定义的机制，由 Go 设计团队（Robert Griesemer、Rob Pike、Ken Thompson）在 2009 年 Go 首次公开亮相时就已经定好，比 Go 1.0（2012 年）还早，属于语言"从第一天就有、之后再没大改过"的地基特性。

### `recover()` 抓到的 `r` 到底是什么、怎么跟命名返回值配合——安全网比喻

`r` 就是当初 `panic(v)` 里那个 `v` 原封不动地被交还——手写 `panic("boom")`，`r` 就是字符串 `"boom"`；Go 运行时自己触发的 panic（数组越界、空指针），`r` 是运行时自己拼的一个描述错误的值。类型是 `any`，拿到手通常要判断/转换。

用"走钢丝 + 安全网"理解 [`safeGo`](server/internal/bff/aggregate.go)：

```go
g.Go(func() (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("panic recovered: %v", r)
        }
    }()
    return fn()
})
```

- `fn()` 是走钢丝的人，正常情况下稳稳走到对岸（正常 `return`）
- 摔下去（`fn()` panic 了）时，**提前架好**的安全网（`defer` 必须在他开始走之前就架好）接住他，`recover()` 就是"接住"这个动作
- `func() (err error)` 里的 `err` 是提前印好、挂在安全网旁边的"事故报告表"（命名返回值）——正常情况下空白（`nil`）；真摔了，现场工作人员（`defer` 里的代码）直接在这张表上填"panic recovered: xxx"，这张表本身就是最终交出去的返回值
- 交出去之后，调用方（`errgroup`）拿到手里的永远只是这张"事故报告表"，**分不清**这次到底是"正常业务失败"还是"真的摔下去被网救回来"——两种情况长得一模一样，都只是"这个任务返回了个 error"。这正是 `safeGo` 的意义：把"进程级灾难"伪装成"普通业务失败"

### `g.Go` / `WithContext` / `errOnce`——用项目实际锁定的 `errgroup@v0.17.0` 真源码讲清楚

真实源码（`/Users/welby/go/pkg/mod/golang.org/x/sync@v0.17.0/errgroup/errgroup.go`）：

```go
type Group struct {
	cancel  func(error)
	wg      sync.WaitGroup
	sem     chan token
	errOnce sync.Once
	err     error
}

func WithContext(ctx context.Context) (*Group, context.Context) {
	ctx, cancel := context.WithCancelCause(ctx)
	return &Group{cancel: cancel}, ctx
}

func (g *Group) Go(f func() error) {
	if g.sem != nil {
		g.sem <- token{}
	}
	g.wg.Add(1)
	go func() {
		defer g.done()
		if err := f(); err != nil {
			g.errOnce.Do(func() {
				g.err = err
				if g.cancel != nil {
					g.cancel(g.err)
				}
			})
		}
	}()
}
```

- **`g.Go`**：`Group` 结构体上的一个方法，不是语言内置，是第三方（严格说是"Go 官方扩展库"）代码。内部就是 `wg.Add(1)` → 起新 goroutine → 跑 `f()` → 有 error 就记下来。
- **`WithContext`**：创建 `Group` 的同时，用 `context.WithCancelCause` 派生一个新 `ctx`，把取消函数存进 `Group.cancel`——以后任何任务失败，自动调用这个 `cancel`，让所有拿着这个派生 `ctx` 的兄弟任务能感知到"该收工了"。
- **`errOnce`**：**不是方法，是一个字段**，类型是标准库 `sync.Once`。`.Do(func(){...})` 才是方法，这个方法由 **Go 标准库 `sync` 包**定义，`errgroup` 只是用它保证"多个 goroutine 同时出错时，只有第一个 error 会被真正记录"，线程安全。

**官方源码里明确写了"故意不做 panic 自动恢复"的理由**（`errgroup.go:81-91` 的注释）：把 panic 自动转成 error 会让 bug 被延迟发现、把清晰的崩溃堆栈藏成一个普通值（监控工具抓不到）、还可能造成死锁掩盖真正的 panic——所以 Go 团队**主动选择不做**这层防护，把"要不要拦 panic"这个决定完全留给使用者，这正是为什么本项目要自己加一层 `safeGo`。

### errgroup 像不像 `Promise.all`——像，但取消机制是关键差异

相似点：都是"并发跑几个任务、等结果、第一个失败的被特殊对待"。

不同点：
- `Promise.all` 一个 reject 就立刻返回，**但其他 promise 该跑还跑**——JS 原生 Promise 没有真正的取消能力
- `errgroup.Wait()` **不会提前返回**，内部 `g.wg.Wait()` 必须等所有 goroutine 真正结束才往下走；它做的是把"取消信号"广播出去（等价于我们聊过的 `AbortController`/`signal` 那套广播模型），但信号能不能让任务真正提前停，取决于任务代码有没有认真监听 `ctx.Done()`
- 更精确地说：errgroup "等所有人结束"这个行为更像 `Promise.allSettled`；"只关心第一个 error"这一点像 `Promise.all`；而它带的**真正取消传播**（`ctx` 广播），是 JS 原生 Promise 完全没有的东西

### `error` 和 `panic` 不是同一维度的东西，不能都叫"structure"

- **`error` 是接口(interface)**，不是 struct：`type error interface { Error() string }`，任何实现了 `Error() string` 方法的类型都自动满足这个接口。真正装数据的是各种**实现了这个接口的 struct**（如 `errors.New` 返回的 `*errors.errorString`）
- **`panic` 是内置函数**，不是数据结构——是"发起一次退栈"这个动作本身。它接收的参数 `v` 可以是任意类型（字符串、`error`、自定义 struct），`v` 才是数据，`panic` 只是拿着 `v` 去触发退栈的那个动词

### Go 的"库分层"体系——回答"是不是都得用第三方库"

| 层级 | 例子 | 要不要额外 `go get` |
|---|---|---|
| 语言内置 | `panic`/`recover`/`defer` | 不用，写代码就有 |
| 标准库（装完 Go 自带） | `fmt`、`net/http`、`context`、`sync` | 不用，`import` 直接用 |
| Go 官方"扩展库"（`golang.org/x/...`） | `errgroup`、`x/text`、`x/net` | 要，但维护方是 Go 团队自己 |
| 真正第三方（社区维护） | `gin`、`gorm` | 要，维护方跟 Google/Go 团队无关 |

`errgroup` 属于第三档：技术上要显式拉依赖（这个项目 `go.mod` 里能看到），但严格说不算"第三方"——是 Go 团队自己维护，只是设计还在演进、还没到"进标准库后几十年不能改"的稳定程度（跟前面"[为什么这些基础功能拖到 2024 年才加进标准库](NOTES.md)"是同一套逻辑），所以故意放在 `x/` 底下单独发版。

### `safeGo` 是谁写的

[`safeGo`](server/internal/bff/aggregate.go) 是这个项目自己的代码，不属于 Go 语言、标准库，也不属于 `errgroup`——是在搭这个 BFF demo 时专门写的、小写字母开头（未导出）的辅助函数，只在 `bff` 包内部使用，因为 `errgroup` 官方明确决定不做 panic 兜底，所以项目自己在它外面补了这一层。

## `sync.Once` 内部机制、结构化类型、TS 擦除、错误链——概念答疑合集

### `sync.Once.Do(fn)`：为什么不能"第一个人吃到一半就广播说没了"

`sync.Once` 内部大致是一个 `done uint32`（原子标记）+ 一个 `m sync.Mutex`：

```go
func (o *Once) Do(f func()) {
    if atomic.LoadUint32(&o.done) == 0 {
        o.doSlow(f)
    }
}

func (o *Once) doSlow(f func()) {
    o.m.Lock()
    defer o.m.Unlock()
    if o.done == 0 {
        defer atomic.StoreUint32(&o.done, 1)
        f()
    }
}
```

后来者不是"排队等一个通知",是**卡在 `o.m.Lock()` 这把锁上**——第一个人进 `doSlow` 时已经把锁攥在手里,直到 `f()` 完全跑完（连同 `defer` 里标记 `done=1`）才释放。所以"第一个人吃到一半"这个阶段,后面的人**压根还没资格被通知"没了"**,因为他们此刻正卡在锁外面,连查 `done` 的机会都没有。

这不是设计疏忽,是**故意的保证**：`Do()` 返回 ⇒ `f()` 已经彻底跑完。`sync.Once` 最常见的用途是"懒加载单例"（比如"第一次用到才初始化一个全局连接池"），如果允许"半路广播'别等了'",所有后来者会在 `f()` 还没初始化完时就以为"已经好了"直接往下用,读到一个还没初始化完的半成品——这比"多等一下"严重得多。

`errgroup` 里 `errOnce.Do(func(){ g.err = err; g.cancel(g.err) })` 能做到"第一个失败的任务几乎瞬间通知所有兄弟 goroutine 退出",靠的不是 `sync.Once` 本身变快了,而是**在这个"只跑一次"的函数体内部,手动调用了 `cancel()`**——`cancel()` 走的是完全独立的通道（关闭 `ctx.Done()` 这个 channel）,不受 `Once` 内部那把锁影响。这是 Go 的一贯设计哲学：`sync.Once`（保证"恰好一次"）和 `context`（广播取消信号）是两个各自专注、正交的小工具，组合起来用，而不是让一个复杂原语同时干两件事。

### 结构化类型（Go、TS）vs 名义类型（Java、C#）——两条不同的轴，别混在一起比

Go 和 TypeScript 都允许**隐式满足接口**：不用写 `implements`，只要方法/属性的"形状"对上就算数。Java/C# 必须显式声明 `implements Xxx`。

但这只是**其中一条轴**,还有第二条轴容易被混在一起：

| | 显式声明轴：要不要写 `implements` | 运行时存在轴：这个"接口"运行时还在不在 |
|---|---|---|
| Go | 不需要（结构化） | **在**——可以用 `x.(SomeInterface)` 类型断言、反射查 |
| TypeScript | 不需要（结构化） | **完全不在**——`tsc` 编译后 `interface` 100% 被擦除，JS 里查无此物 |
| Java / C# | 需要（名义化） | 在——可以用 `instanceof`/`is` 查 |

所以准确说法是：**Go 和 TS 在"要不要显式声明"这条轴上站在一边，Java/C# 是这条轴上的异类；但在"运行时存不存在"这条轴上，TS 才是真正的异类，Go 反而和 Java/C# 站一边**。不能笼统地说"TS 是异类，Go 常规"——要看比的是哪条轴。

**两者选结构化类型的动机完全不同，不是"Go 向 TS 看齐"**（Go 语言设计在 2007-2009 年就定型，TypeScript 2012 年才发布，时间线上 Go 不可能借鉴还不存在的 TS）：

- **Go 的动机**：允许"事后补充满足关系"——不用改动已有类型（尤其是第三方包里的类型）就能让它满足一个新接口，这对写测试（消费方自己定义一个只含所需方法的最小接口）、解耦调用方与实现方极其重要；同时契合 Go"没有 class、没有继承"的整体设计哲学，避免 Java 式的大量样板代码。
- **TS 的动机**：JS 里早就存在大量没有 class 的原生对象字面量（`{x:1,y:2}`），TS 的任务是"给已经存在的、无类型的 JS 代码打类型补丁"——如果用名义类型，这些字面量根本无类型可归属，结构化类型是唯一行得通的方案。

"Java/C# 才是真正的后端语言，Go 反而向前端看齐"这个前提本身也不成立：后端/前端属性和名义/结构化类型选择没有任何因果关系，只是四种语言各自独立做出的设计取舍，凑巧 Go 和 TS 在其中一条轴上重合了而已。

### TypeScript 编译时擦除——基本正确，有一个例外

`interface`、类型标注、泛型、type-only import 这些 TS 专属语法，被 `tsc` 编译后**在生成的 JS 里完全不存在**——这个理解是对的。具体带来的好处，比"培养编码习惯"更精确：

1. **编译期抓 bug**：类型不匹配在写代码的时候就报错，不用等运行时炸出来。
2. **IDE 工具能力**：编辑器能基于静态的"形状"信息做自动补全、跳转定义、重构安全检查。
3. **不会腐烂的文档**：类型标注本身会被编译器强制检查，代码改了但注释忘记同步这种"文档撒谎"的情况，类型标注不会发生（改了实现但类型没跟着改，编译直接报错）。

**唯一的例外**：非 `const` 的 `enum` 不会被完全擦除，会生成一段真实的、运行时存在的 JS 代码（一个双向映射对象）：

```ts
enum Color { Red, Green }
```

编译后大致变成：

```js
var Color;
(function (Color) {
    Color[Color["Red"] = 0] = "Red";
    Color[Color["Green"] = 1] = "Green";
})(Color || (Color = {}));
```

所以"TS 专属语法运行时都不存在"不是 100% 绝对——`const enum` 才会被完全内联擦除，普通 `enum` 是个例外。

### 错误链：`wrapError` / `Unwrap()` / `errors.Is` / `errors.As` 到底怎么顺藤摸瓜

**场景**：数据库层返回一个哨兵错误，repository 层包一层，service 层再包一层：

```go
func queryDB() error { return sql.ErrNoRows }

func getUserRepo() error {
    if err := queryDB(); err != nil {
        return fmt.Errorf("query user: %w", err) // 第一层包装
    }
    return nil
}

func getUserService() error {
    if err := getUserRepo(); err != nil {
        return fmt.Errorf("get profile: %w", err) // 第二层包装
    }
    return nil
}

// errors.Is(getUserService(), sql.ErrNoRows) // true —— 三层包装之后依然能查到最初的错误
```

**`fmt.Errorf(..., %w, err)` 真实生成的类型**（`fmt` 包源码）：

```go
type wrapError struct {
    msg string // 给人看的、已经拼好的完整文字
    err error  // 给程序判断用的、指向"下一环"的真实值（指针）
}

func (e *wrapError) Error() string { return e.msg }
func (e *wrapError) Unwrap() error { return e.err } // 关键就这一个方法
```

三层包装后的链条，画出内存结构：

```
err（最外层）
  → *wrapError{ msg: "get profile: query user: sql: no rows in result set",
                err: → *wrapError{ msg: "query user: sql: no rows in result set",
                                    err: → sql.ErrNoRows（链条终点：没有 Unwrap 方法）
                }
    }
```

`msg` 是拼死的字符串，本身没有"层"的概念，查不了；真正能"顺藤摸瓜"的是每一层的 `err` 指针字段——这是"给人看的描述"（`msg`）和"给程序判断身份用的引用"（`err`）两条独立通道，Go 故意把它们拆开。

**类型断言基础，`x.(T)` 是什么**：`.( )` 是 Go 内置的专用语法（不是方法调用，和 `x.Method()` 只是长得像），问"`x` 背后具体存的东西，是不是满足 `T`（可以是具体类型，也可以是接口）"：

```go
s, ok := i.(string)                          // 问：i 背后是不是 string
u, ok := err.(interface{ Unwrap() error })   // 问：err 除了 error 接口本身保证的 Error() string 之外，
                                              // 是不是"额外"还有一个 Unwrap() error 方法
```

准确说法：`err` 已经保证有 `Error() string`，是因为它的**静态类型声明就是 `error`**，不是这句断言"查出来"的；这句断言只检查 `Unwrap() error` 这一个额外方法有没有。

**`errors.Is`/`errors.As` 内部循环**（简化版，抓住核心机制）：

```go
func Is(err, target error) bool {
    for {
        if err == target {
            return true
        }
        u, ok := err.(interface{ Unwrap() error })
        if !ok {
            return false // 没有 Unwrap 方法 = 链条到头，没找到
        }
        err = u.Unwrap() // 顺着指针走到下一环，继续循环
    }
}
```

真实源码还多两个分支（自定义 `Is(error) bool` 方法、`Unwrap() []error` 支持 `errors.Join` 多重包装），但核心"循环调用 Unwrap 直到匹配或到头"没变。

**`%w` vs `%v`——为什么 `%v` 会让链条直接断掉**：两者打印出来的文字一模一样，区别在 `fmt.Errorf` 背后造出来的类型：

```go
errV := fmt.Errorf("query user: %v", sql.ErrNoRows) // 只把 Error() 返回的文字抄一遍，原 error 对象本身用完就扔了
errW := fmt.Errorf("query user: %w", sql.ErrNoRows) // 文字抄一遍的同时，额外把原 error 对象的指针也存下来

errors.Is(errV, sql.ErrNoRows) // false —— errV 背后的类型没有 Unwrap()，链条到此断了
errors.Is(errW, sql.ErrNoRows) // true
```

`%v` 是"只留遗言"，`%w` 是"遗言 + 本体指针都留下，还告诉你怎么通过 `Unwrap()` 找到下一环"。

**`sql.ErrNoRows` 长什么样（链条终点为什么没有 `Unwrap`）**：

```go
// database/sql
var ErrNoRows = errors.New("sql: no rows in result set")

// errors 包
func New(text string) error { return &errorString{text} }
type errorString struct{ s string }
func (e *errorString) Error() string { return e.s }
```

背后是 `*errors.errorString`，只有一个字段（文字）、只实现 `Error()`，没有指向"更下一层"的字段/方法——它本来就是链条起点，没有更底层可指。

### 方法"签名"完全匹配是什么意思

签名 = **方法名 + 参数类型列表 + 返回值类型列表**，四样东西合在一起才算数：

```go
type Reader interface {
    Read(p []byte) (n int, err error)
}
```

要满足这个接口，某个类型必须有一个**方法名叫 `Read`**、**接收一个 `[]byte` 参数**、**返回 `(int, error)`** 的方法——缺一不可。方法名对了但参数/返回值类型不对，不算满足这个接口。

### `errors.New(...)` 在干什么、`errorString` 为什么包一层 struct + 指针

```go
var ErrNoRows = errors.New("sql: no rows in result set")
```

`errors.New` 是标准库函数：给一段文字，造一个 error 值。这是包级变量声明，包被加载时执行**一次**，造出**唯一一份** error 值，之后整个程序反复复用这同一份（不是每次现造）。`database/sql` 内部凡是"查询没返回任何行"，都返回这个**同一个**预造值，调用方可以拿它跟 `sql.ErrNoRows` 比较（`errors.Is`）来判断是不是这种特定情况。

`errorString` 为什么不是直接把 error 定义成字符串类型（例如 `type MyErr string`，同样能满足 `error` 接口），而要包一层 struct 再取指针：

```go
func New(text string) error { return &errorString{text} } // 返回的是指针
type errorString struct{ s string }
func (e *errorString) Error() string { return e.s }
```

**原因不是"字段类型不能写 string"**（`struct{ s string }` 完全合法），而是**指针能保证每次调用产生独一无二的身份**：

```go
err1 := errors.New("boom")
err2 := errors.New("boom")
err1 == err2 // false —— 文字一样，但这是两个不同的指针，身份不同
```

如果用值类型字符串实现（`type MyErr string`），内容相同就会 `==` 相等——这对哨兵错误（sentinel error，靠"是不是特指这一个"做判断）是致命的：任何人手写一段相同文案的错误，都会被误判成"就是 `ErrNoRows`"。所以标准库故意选"struct + 指针"，用指针地址而不是文字内容来定义"是不是同一个错误"。

`sql.ErrNoRows` 的静态类型是 `error`（接口），背后具体存的是 `*errorString`（指针），**不是字符串本身**——正因为不是裸字符串，才"有指针可挂"，才能用指针相等做精确判断；如果它真的"本来就是字符串"，反而没有这套身份机制了。

### Go 的 `type` 关键字不等同于 TS 的 `interface`

`type` 是 Go 里**通用的类型声明关键字**，什么类型都能声明，不专属某一种：

```go
type Point struct{ X, Y int }    // 声明一个 struct
type Reader interface{ Read() } // 声明一个 interface
type Handler func(w, r) error   // 声明一个函数类型
type MyInt int                  // 给已有类型起新名字
```

真正对应 TS `interface` 的，是 `type X interface{...}` 这**一种特定写法**（`type` + `interface` 两个词组合），不是 `type` 这个关键字本身。`type X struct{...}`（比如 `errorString`）更接近 TS 里定义一个具体对象形状/`class`，跟 TS 的 `interface`（契约/形状描述）不是一回事。

### Go 多返回值怎么写——`(数据, error)` 是最常见的惯用法

```go
func Divide(a, b int) (int, error) {
    if b == 0 {
        return 0, errors.New("division by zero") // 出错:第一个给零值,第二个给具体错误
    }
    return a / b, nil // 正常:第一个给真实结果,第二个给 nil
}

result, err := Divide(10, 2)
if err != nil { /* 处理错误 */ }
```

Go 没有 try/catch，几乎所有"可能失败"的操作都靠"最后一个返回值是不是 `error`"表达成功/失败，调用方必须手动检查 `err != nil`。其他常见形式：`(min, max int)`（都不是 error）、`(int, bool)`（常见于 `v, ok := m[key]` 查 map）、`(context.Context, context.CancelFunc)`。

**命名返回值**：提前在签名里给返回值起好名字并声明类型：

```go
func Divide(a, b int) (result int, err error) {
    if b == 0 {
        err = errors.New("division by zero") // 直接赋值,不用 return 0, err
        return                                // 光秃秃的 return,自动带出 result、err 当前的值
    }
    result = a / b
    return
}
```

关键点：
- `result`、`err` 是函数体内提前声明好的局部变量，函数一开始就自动初始化成各自类型的零值（`result=0`, `err=nil`），跟普通变量声明不赋值时行为一致——所以 `b==0` 分支没碰 `result`，返回的 `result` 依然是自动填上的零值 `0`，不是没定义。
- 函数体内其他变量**没有资格**被光秃秃的 `return` 带出去，只有签名里声明过的这两个名字才算数——不是"选择性忽略别的变量"，是语法上只认这两个名字。
- 命名返回值最大的价值是配合 `defer` 修改最终返回值（`recover()` 场景），普通函数体较长时，社区更推荐显式 `return result, err`，光秃秃的 `return` 会让人看不出这次到底返回什么，得往上翻代码找赋值语句。

### `errors.Is` 判断结果"准不准"和"是不是 true"是两回事

"因为 `sql.ErrNoRows` 是提前造好的唯一一份，所以只要是这个 error，`errors.Is` 就是 true"——这句话本身没错，但不能引申成"从 `database/sql` 出来的 error，`errors.Is` 一般倾向于 true"。

准确说法：**只有这次真的发生了"没查到行"这个具体情况，`errors.Is(err, sql.ErrNoRows)` 才会是 true**；如果这次是别的失败（连接超时、语法错、约束冲突……），返回的根本不是这个预造值，`errors.Is` 依然是 false。"提前造好、独一无二"这件事保证的是**判断的精确性**（不会被文字相似的其他错误误判，也不会被 `%w` 包了几层影响），不是让判断结果"偏向 true"。
