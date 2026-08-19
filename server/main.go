package main

import (
	// context: Go 用来传递"取消信号/超时/请求范围内的值"的标准机制。
	// 几乎所有会阻塞的操作（网络请求、数据库查询）都接受一个 context，
	// 用来告诉它"如果我不需要你了，请尽快停下来"。
	"context"

	// encoding/json: 把 Go 的 map/struct 编码成 JSON 写入 http.ResponseWriter。
	"encoding/json"

	// log: 标准库自带的日志包，log.Printf 打印信息，log.Fatalf 打印后直接退出进程。
	"log"

	// net/http: Go 标准库自带的 HTTP 服务器/客户端实现，不需要额外框架。
	"net/http"

	// os: 用来读环境变量（os.Getenv）。
	"os"

	// os/signal: 用来"监听"操作系统发给这个进程的信号（比如 Ctrl+C）。
	"os/signal"

	// syscall: 定义了具体的信号常量，比如 SIGINT、SIGTERM。
	"syscall"

	// time: 用来设置超时时长（2*time.Second 之类）。
	"time"

	// 下面两个是本项目自己写的包，路径前缀 "next-learning/server" 来自 go.mod 里的 module 名。
	"next-learning/server/internal/db"
	"next-learning/server/internal/handlers"
)

func main() {
	// 1. 读取数据库连接字符串。
	//    优先用环境变量 DATABASE_URL（生产/docker环境会设置它），
	//    本地没设的话就退回一个默认值，方便直接跑起来不用配置任何东西。
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://bff:bff@localhost:5432/bff?sslmode=disable"
	}

	// 2. 创建一个"会在收到 Ctrl+C 或 kill 信号时自动被取消"的 context。
	//    ctx 本身现在还没被取消；一旦操作系统发来 SIGINT/SIGTERM，
	//    ctx.Done() 这个 channel 就会被关闭，所有"在等它"的代码都会收到通知。
	//    stop() 是用来提前解除这个信号监听的清理函数，defer 保证 main 退出前一定调用它。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 3. 连接数据库。db.Connect 内部带重试逻辑（因为 docker compose 刚启动时
	//    Postgres 可能还没就绪）。把上面那个 ctx 传进去，
	//    意味着"如果程序在重试连接的过程中就收到了关闭信号，也能提前退出"。
	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		// 重试完还是连不上，直接让整个进程退出（Fatalf 会调用 os.Exit(1)）。
		// 数据库都连不上，这个服务活着也没有意义。
		log.Fatalf("failed to connect to database: %v", err)
	}
	// defer：无论 main 函数是怎么结束的（正常走完/panic），
	// 都保证最后执行 pool.Close()，把数据库连接池里的连接都关掉，不留悬空连接。
	defer pool.Close()

	// 4. 创建一个路由器（Go 1.22+ 的标准库自带 mux，不需要额外引入 gin/chi 之类的框架）。
	mux := http.NewServeMux()

	// 5. 注册第一个路由：GET /health
	//    "GET /health" 这种"方法+路径"写法是 Go 1.22 才支持的新语法，
	//    旧版本的 ServeMux 只能匹配路径、匹配不了方法。
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		// r.Context() 是"这一次 HTTP 请求专属"的 context，
		// 如果客户端提前断开连接，这个 context 会被自动取消。
		// 这里再包一层 2 秒超时：即使客户端不断开，
		// 如果数据库 2 秒内没回应，也主动放弃，不让这个请求无限期卡住。
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel() // 提前释放这个 timeout context 关联的资源

		// pool.Ping 真正去数据库那边验证连接是不是通的
		// （不只是"进程还活着"，而是"进程依赖的数据库也还活着"）。
		if err := pool.Ping(pingCtx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable) // 503
			json.NewEncoder(w).Encode(map[string]string{"status": "db unreachable"})
			return // 提前返回，不再往下执行成功分支
		}

		// 数据库也正常，返回 200（默认状态码）+ JSON body
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "go-api"})
	})

	// 6. 组装 http.Server。
	//    Handler 不是直接传 mux，而是 handlers.WithLocalCORS(mux)——
	//    这是"中间件"的写法：用一个函数把 mux 包起来，
	//    所有请求会先经过 WithLocalCORS 加 CORS 响应头，再被转发给 mux 做真正的路由匹配。
	addr := ":8090"
	srv := &http.Server{
		Addr:    addr,
		Handler: handlers.WithLocalCORS(mux),
	}

	// 7. 开一个独立的 goroutine，专门"等待"关闭信号。
	//    <-ctx.Done() 会一直阻塞，直到步骤2里的 ctx 被取消（也就是收到 Ctrl+C/kill）。
	//    一旦收到，就调用 srv.Shutdown()：
	//    这是"优雅关闭"——不是直接掐断所有连接，而是给正在处理的请求最多 5 秒把活干完，
	//    5 秒后还没完的才会被强制中断。
	go func() {
		<-ctx.Done()
		log.Println("收到关闭信号，开始收尾...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	// 8. 真正启动服务器，开始监听 8090 端口。
	//    这一行会"阻塞"在这里，程序的主流程就停在这儿，
	//    直到服务器出错，或者上面第7步里的 srv.Shutdown() 被调用。
	log.Printf("go-api listening on %s", addr)

	// srv.Shutdown() 被调用后，ListenAndServe() 会返回一个固定的错误值 http.ErrServerClosed，
	// 这代表"是被正常关闭的，不是真的出错了"，所以要把这种情况从错误处理里排除掉，
	// 否则每次你按 Ctrl+C 停服务都会被当成"服务器挂了"打一条 Fatal 日志。
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
