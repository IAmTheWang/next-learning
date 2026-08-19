# Next.js BFF + Go/Postgres 真实后端 学习计划（4-5周）

## 进度

- [x] Week 1: `web/` Next.js 骨架 + `/api/health`；`server/` Go module + docker-compose Postgres + `/health`
- [ ] Week 2: Go 建表 + 三个只读接口 + 流式接口（`server/internal/db/schema.sql` 已建好起点表结构，handler 待写）
- [ ] Week 3: Next.js BFF 聚合/缓存/DTO裁剪/SSE代理/JWT middleware
- [ ] Week 4: 前端页面 + 边界场景验证
- [ ] Week 5: README 架构文档 + 部署

## Context

用户前端能力扎实，但简历上的 BFF 经历（聚合、SSE代理、middleware、缓存）严格说是"维护/改过公司代码"级别，不是"从零设计"级别。面试临近，需要独立搭建一个真实可跑、每个技术点都能被面试官深挖而不虚的 BFF demo 项目，把简历上的 bullet 从"改过"升级成"独立架构过"。

最初方案是 Next.js BFF 聚合免费公开 API（Open-Meteo/Frankfurter/Quotable），3周冲刺，零后端依赖。用户看过一份 Gemini 的评估后，决定**升级为更贴近真实场景的双层架构**：自己用 **Go + Postgres** 写一个真实后端服务，Next.js BFF 完全替换掉对公开 API 的调用，改为聚合/代理这个自建后端。这样面试时讲的就是"BFF 层如何对接真实内部服务"，而不是"套壳调用第三方免费接口"，说服力更强。

**已与用户确认的关键决策：**
- Go+Postgres **完全替换**公开 API 聚合（不是混用）
- 用户已写过一些 Go，语法没问题，需要补的是 web 框架 + Postgres 连接/建表这部分
- 时间线从 3 周**延长到 4-5 周**，两边（Go 后端 / Next.js BFF）都做扎实，不因为加了新技术栈而在任一侧偷工减料
- 沿用之前的决定：前端做一个简单展示页面（不只是 Postman 演示），部署到 Vercel
- **Week 2 起的核心逻辑（建表、CRUD、流式接口、BFF聚合）由用户自己写**，Claude 只在卡住时讲解/review，不代写

## 架构总览

```
浏览器
  │
  │ (登录页 / 仪表盘页 / 流式输出页)
  ▼
Next.js BFF (web/)
  │  - middleware.ts: JWT 校验（jose，Edge Runtime 兼容）
  │  - /api/dashboard: 聚合 Go 后端 3 个接口 + 缓存 + DTO裁剪 + 超时/降级
  │  - /api/stream: 代理 Go 后端的真实流式接口（不再是纯mock，是真代理）
  ▼
Go 后端 (server/)
  │  - GET /accounts, /transactions/recent, /budgets/summary
  │  - GET /reports/stream  （http.Flusher 实现真实分块流，用来被BFF代理）
  ▼
Postgres（docker-compose 本地起）
```

**领域选型**：个人财务小仪表盘 —— `accounts`（账户）/ `transactions`（交易）/ `budgets`（预算），3张表，足够练"聚合 + 裁剪"但不需要复杂业务逻辑。

**SSE 代理的关键升级**：不再是 Next.js 自己 mock 一个 `ReadableStream`，而是 **Go 后端真实产出一个分块流**（例如模拟"报表生成进度"，用 `http.Flusher` 每隔一段时间 `Write` 一行 + `Flush()`），Next.js 的 `/api/stream` 去代理这个真实的上游流并转成 SSE 格式给浏览器。这才是简历上"SSE代理"真正对应的场景：BFF 是转发者，不是流的生产者。

## 简历 bullet 对应关系

| 简历 bullet | 对应本项目模块 |
|---|---|
| API 聚合 | `/api/dashboard` 聚合 Go 后端 3 个接口 |
| SSE 代理 | `/api/stream` 代理 Go 后端 `/reports/stream` 的真实分块流 |
| Auth middleware | `middleware.ts` JWT 校验 + 跳转（认证逻辑完全在 Next 侧，Go 后端是纯数据服务，不管认证——这本身就是一个可讲的架构选择） |
| 缓存 | 聚合结果加内存 TTL 缓存 |
| 数据脱敏/DTO裁剪 | Go 返回的原始表结构裁剪成前端专用 DTO |
| timeout/fallback | 聚合请求 `AbortController` 超时 + 单个上游失败时降级（`Promise.allSettled`） |

## 部署策略（提前定好，避免 Week 5 抓瞽）

Vercel 只能跑 Next.js（无状态 serverless），跑不了长连接的 Go 服务 + Postgres。为了不把"给 Go+Postgres 找云主机"这件事变成额外一整周的运维学习，**默认方案**：
- Go + Postgres 用 `docker-compose` 只在**本地**跑，面试时用"起一个 `docker-compose up`，本机演示完整链路"的方式展示（这是合理且常见的 portfolio 展示方式，说明"分层架构"比"是否上云"更重要）
- Next.js 部署到 Vercel，README 里说明"生产环境下 Go 服务会部署在内网/容器平台，这里为演示简化为本地"
- Week 5 若时间充裕，可选加分项：把 Go+Postgres 部署到 Railway/Fly.io（有免费额度），Next.js 生产环境指向那个地址——这是可选项，不是必须项

## 四到五周节奏

### Week 1（双线打基础）✅
- **Next.js 侧**：App Router / Route Handlers / Middleware 心智模型（官方 Learn 教程 + 文档），起 `pnpm create next-app`，写通 `/api/health`
- **Go 侧**：选 web 层方案（推荐标准库 `net/http` + Go 1.22+ 的路由 pattern，不额外引入框架，保持够用不臃肿——这本身也是可讲的选型理由），选 Postgres 驱动（推荐 `pgx`，比 `lib/pq` 更现代），写 `docker-compose.yml` 起 Postgres，写通一个 `/health` 端点连上数据库

**这周要能讲清楚**：Route Handler vs 传统后端路由的区别；为什么 middleware 跑在请求匹配之前；`pgx` 连接池的基本概念（不需要深挖，能讲清楚"为什么用连接池而不是每次开新连接"即可）

### Week 2（Go 后端建模）← 当前
- 设计并建表：`accounts` / `transactions` / `budgets`（3表，带一份 seed SQL）
- 写 `GET /accounts`、`GET /transactions/recent`、`GET /budgets/summary` 三个只读接口，返回 JSON
- `pgxpool` 初始化时加简单重试逻辑（每隔1秒重试连DB，最多5次），或在 `docker-compose.yml` 给 Go 服务加 `depends_on: postgres: condition: service_healthy`——`docker compose up` 时 Postgres 起得比 Go 慢，Go 一启动就强连会直接 crash
- 路由最外层加一个极简本地 CORS 中间件（允许 `localhost:3000`），方便 Week2 用浏览器直接调 Go 接口做对比验证时不被跨域挡住（生产场景下 BFF 走 S2S 不需要这个，纯粹是本地调试用）
- 写 `GET /reports/stream`：用 `http.Flusher` 做真实分块输出（模拟"报表逐步生成"），这是给 Week3 的 SSE 代理准备的真实上游。**总推送时长控制在 5-8 秒内**（例如每200ms推一行，累计20-30行）——Vercel Hobby 版 Serverless 函数单次执行有 10-15秒超时限制，流跑太久线上代理会被强制切断，控制在这个区间既能完整展示效果又不会触发超时
- **在推送循环里监听 `r.Context().Done()`**（`select { case <-ctx.Done(): return ... }`），当下游（Next.js BFF）中途取消请求时，Go 侧能立刻停止查询/推送并释放资源——这是和 Week3 的 `request.signal` abort 处理相对应的"全链路取消传播"，两端都要做才算完整闭环
- 基本错误处理（查询失败返回合理的 HTTP 状态码，不是裸的 500）

**这周要能讲清楚**：`http.Flusher` 是怎么让响应体分块发送而不是攒完再发；为什么这三个接口分开而不是一个大而全的接口（贴合"BFF 才做聚合，后端保持单一职责"的分层理由）；取消信号是怎么从浏览器一路传播到 Go 的数据库查询层的（前端 abort → Next `request.signal` → fetch 到 Go 时的 `AbortController` → Go 收到连接关闭 → `ctx.Done()`）

### Week 3（Next.js BFF 核心）
- `lib/upstream/{accounts,transactions,budgets}.ts`：各自封装对 Go 后端的调用，独立超时（`AbortController`）。注意这是**服务端对服务端通信**（Route Handler 在 Node/Edge 运行时里直接 fetch Go 服务，不经过浏览器），完全不受 CORS 限制，`GO_API_BASE_URL` 直接配置成 Go 服务地址即可，不需要额外处理跨域
- `/api/dashboard`：`Promise.allSettled` 并发调用三者，单个失败给降级占位值，整体裁剪成前端专用 DTO，包一层内存 TTL 缓存
- `/api/stream`：读 Go 的 `/reports/stream` 响应体（`ReadableStream`），转发成 `text/event-stream` 格式给浏览器；**监听 `request.signal` 的 `abort` 事件，浏览器中断时主动关闭上游连接和 controller**（这是 Gemini 建议里提到的加分点，也是真实场景必须处理的边界）
- `middleware.ts`：用 `jose`（不是 `jsonwebtoken`——`jose` 是 Web Crypto API 兼容，能跑在 Edge Runtime，`jsonwebtoken` 依赖 Node 原生 crypto 跑不了）校验 cookie 里的 JWT，未登录跳转登录页；`/api/auth/login` 签发 JWT（校验逻辑可以先简化成硬编码/环境变量里的测试账号，重点不在做完整用户系统）

**这周要能讲清楚**：为什么用 `Promise.allSettled` 而不是 `Promise.all`；DTO 裁剪前后字段/体积对比；`request.signal` 中断处理防止的是什么问题（服务端悬挂连接/资源泄漏）；为什么 middleware 里的 JWT 库要考虑 Edge Runtime 限制

### Week 4（前端页面 + 边界场景打磨）
- `login/page.tsx`、`dashboard/page.tsx`、`stream-demo/page.tsx`（哪怕样式简陋，能交互即可）
- 故意停掉 Go 容器验证 `/api/dashboard` 的降级路径确实生效（不是全盘 500）
- 故意在流式输出中途关闭浏览器标签/中断请求，验证 Go 侧和 Next 侧的连接都被正确清理（可以在 Go handler 里打日志观察 context 是否被取消）

### Week 5（收尾 + 面试武装，含可选加分）
- README 写成"架构设计文档"而不是"怎么跑起来"说明书，至少包含一节 **"Why BFF Architecture?"**，用 bullet 讲清楚：
  - 为什么聚合用 `Promise.allSettled`
  - DTO 裁剪前后的字段/体积对比
  - 为什么认证放在 BFF 层而不是 Go 后端
  - SSE 代理里 abort 处理解决的问题
- Vercel 部署 Next.js；Go+Postgres 走 docker-compose 本地演示（默认方案）
- 可选加分：把 Go+Postgres 部署到 Railway/Fly.io，打通线上端到端
- 自己过一遍三个经典面试话术："最难调优的 bug"、"为什么做 DTO 裁剪"、"降级策略怎么设计的"

## 项目骨架

```
next-learning/
├── web/                              # Next.js BFF
│   ├── app/
│   │   ├── api/
│   │   │   ├── health/route.ts       # ✅
│   │   │   ├── dashboard/route.ts
│   │   │   ├── stream/route.ts       # 代理 Go 的 /reports/stream
│   │   │   └── auth/login/route.ts
│   │   ├── login/page.tsx
│   │   ├── dashboard/page.tsx
│   │   ├── stream-demo/page.tsx
│   │   └── layout.tsx
│   ├── middleware.ts
│   ├── lib/
│   │   ├── upstream/{accounts,transactions,budgets}.ts
│   │   ├── cache.ts
│   │   └── jwt.ts
│   ├── .env.example                  # ✅ GO_API_BASE_URL=http://127.0.0.1:8090, JWT_SECRET
│   └── package.json
├── server/                           # Go 后端
│   ├── main.go                       # ✅ /health 已跑通
│   ├── go.mod                        # ✅
│   ├── internal/
│   │   ├── handlers/
│   │   │   ├── middleware.go         # ✅ 本地 CORS
│   │   │   └── {accounts,transactions,budgets,stream}.go   # 待写
│   │   └── db/
│   │       ├── pool.go               # ✅ pgx 连接池初始化 + 重试
│   │       └── schema.sql            # ✅ 建表 + seed data（起点，可自行调整）
│   ├── docker-compose.yml            # ✅ postgres 服务，healthy
│   └── .env.example                  # ✅ DATABASE_URL
└── README.md                         # 架构设计文档（Week5）
```

## 验证方式

- Go 侧：`curl localhost:8090/accounts` 等验证 JSON 结构；`curl -N localhost:8090/reports/stream` 观察分块到达而非一次性返回
- Next 侧：`curl -N localhost:3000/api/stream` 验证代理后仍然是逐块到达；浏览器手动测试未登录访问 `/dashboard` 是否被 middleware 跳转
- 降级场景：`docker compose stop` 掉 Go 容器后请求 `/api/dashboard`，确认返回降级值而非 500
- 中断场景：在 `stream-demo` 页面播放中途刷新/关闭标签，检查 Go 和 Next 两端日志确认连接被清理
- 最终验证：`pnpm build`（web）+ `go build`（server）本地都过；Vercel 部署后 Next.js 页面可访问（指向本地或已部署的 Go 服务视 Week5 选择而定）
