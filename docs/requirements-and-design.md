# Gopad — 需求与技术方案文档

> Go 实现的实时协同代码编辑器,对标 [rustpad](https://github.com/ekzhang/rustpad),并在其基础上增强文档生命周期管理、只读分享和管理控制台能力。

- 版本:v1.0
- 日期:2026-08-14
- 状态:方案评审中

---

## 第一部分:需求文档

### 1. 背景与目标

rustpad 是一个基于 Rust + OT(Operational Transformation)的极简协同代码编辑器:无需注册,打开一个随机 URL 即可与他人实时共同编辑一份代码文本。本项目用 Golang 实现同类应用,保持其"单二进制、零依赖、开箱即用"的部署体验,同时补齐 rustpad 缺失的几个实用能力:

- 文档可自定义过期时间(rustpad 固定一天)
- 只读分享链接
- 带鉴权的管理控制台(查看/删除全部文档)
- 重启不丢数据(SQLite 落盘)

### 2. 用户与场景

- **临时协作**:面试写码、结对调试、会议中共享代码片段——打开链接即用,用完即弃。
- **短期共享**:把一段配置/脚本以链接形式发给同事,设置几天的保留期。
- **运维人员**:通过管理控制台掌握实例上的文档总量,清理不需要的文档。

### 3. 功能需求

#### 3.1 协同编辑(核心)

| 编号 | 需求 | 说明 |
|---|---|---|
| F1 | 多人实时协同编辑 | 基于 OT,任意人数(单文档建议 ≤ 64 连接)同时编辑,所有副本最终收敛一致 |
| F2 | 断线重连 | 客户端断线后自动重连,重连后基于 revision 增量同步,不丢本地未确认的编辑 |
| F3 | 远程光标与选区 | 实时显示其他用户的光标位置和选区,以用户颜色区分 |
| F4 | 在线用户列表 | 侧栏显示当前文档在线用户;用户可自定义昵称和颜色(hue) |
| F5 | 语法高亮切换 | 用户可选择编程语言,选择结果全员同步;预置常用语言集合 |

#### 3.2 文档生命周期

| 编号 | 需求 | 说明 |
|---|---|---|
| F6 | 自动过期 | 文档默认在最后一次编辑后 24 小时过期删除(滚动续期) |
| F7 | 自定义 TTL | 任一编辑者可修改文档 TTL,范围 1 分钟 ~ 100 年;修改结果全员同步并持久化 |
| F8 | 落盘与恢复 | 文档内容定期写入 SQLite;服务重启后未过期文档可继续访问 |

#### 3.3 分享与访问

| 编号 | 需求 | 说明 |
|---|---|---|
| F9 | 免认证访问 | 知道文档 URL 即可读写;文档 ID 为高熵随机串,URL 即凭证 |
| F10 | 只读分享链接 | 每个文档有一条独立的只读链接;通过只读链接进入的用户能实时看到内容变化,但不能编辑、不能改语言/TTL,且无法反推出可写链接 |
| F11 | 裸文本接口 | `GET /api/text/{id}` 返回文档纯文本,便于 curl/脚本取用(rustpad 兼容) |

#### 3.4 管理控制台

| 编号 | 需求 | 说明 |
|---|---|---|
| F12 | Basic Auth 鉴权 | `/admin` 及其 API 受 HTTP Basic Auth 保护,凭证由环境变量配置;未配置则整个管理端不开放 |
| F13 | 文档列表 | 分页展示全部文档:ID、大小、语言、当前连接数、创建/更新/过期时间 |
| F14 | 删除文档 | 管理员可删除任意文档:清除 SQLite 记录与内存状态,并断开该文档的所有活跃连接(客户端收到"文档已删除"提示) |

### 4. 非功能需求

| 项 | 要求 |
|---|---|
| 部署 | 单个静态编译的 Go binary(前端资源内嵌),除一个 SQLite 文件外无外部依赖;支持 Docker |
| 规模 | 单实例;目标 100~500 并发 WebSocket 连接,数千份存量文档 |
| 文档上限 | 单文档 1 MB(UTF-8 字节数),超限的编辑被拒绝 |
| 可靠性 | 优雅关停时全量刷写内存中的脏文档,不丢已确认的编辑 |
| 延迟 | 局域网/同区域下编辑广播端到端 < 100ms |
| 浏览器 | 现代 evergreen 浏览器(Chrome/Edge/Firefox/Safari 最新两个大版本) |

### 5. 明确不做(v1 范围外)

- 多实例水平扩展、跨实例状态同步
- 账号体系、细粒度权限(仅"可写链接/只读链接"两级)
- 文档目录、搜索、标签
- 富文本、图片、多文件/多标签页
- 历史版本回放

---

## 第二部分:技术方案

### 6. 总体架构

```
┌────────────────────────── 浏览器 ──────────────────────────┐
│  CodeMirror 6 编辑器 + OT 客户端状态机 (TypeScript, Vite)  │
└──────────────┬────────────────────────────┬────────────────┘
               │ WebSocket (JSON)           │ HTTP
┌──────────────▼────────────────────────────▼────────────────┐
│                    gopad (单 Go binary)                     │
│  ┌───────────┐  ┌──────────────┐  ┌──────────────────────┐ │
│  │ HTTP 路由  │  │ WS 会话管理   │  │ 管理端 API (Basic)   │ │
│  │ net/http   │  │coder/websocket│ │                      │ │
│  └─────┬─────┘  └──────┬───────┘  └──────────┬───────────┘ │
│        │        ┌──────▼────────────────────▼───────────┐  │
│        │        │ Document Registry(活跃文档表)         │  │
│        │        │  每文档:文本 + OT 历史 + 用户 + 光标   │  │
│        │        └──────────────────┬────────────────────┘  │
│  ┌─────▼─────┐  ┌─────────────────▼─────────────────────┐  │
│  │ embed.FS  │  │ Store (SQLite via ncruces/go-sqlite3) │  │
│  │ 前端静态资源│  │ 快照落盘 / 装载 / 过期清理             │  │
│  └───────────┘  └───────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

技术选型及理由:

| 组件 | 选型 | 理由 |
|---|---|---|
| HTTP 路由 | Go 1.22+ `net/http`(含方法路由与路径参数) | 标准库足够,零框架依赖 |
| WebSocket | `github.com/coder/websocket` | 维护活跃、支持 context、API 现代(原 nhooyr.io/websocket) |
| SQLite 驱动 | `github.com/ncruces/go-sqlite3` | 无 CGO(内嵌 SQLite WASM,经 wazero 运行),单 binary 可任意交叉编译 |
| 前端构建 | Vite + TypeScript | 轻量,产物由 `go:embed` 内嵌 |
| 编辑器 | CodeMirror 6 | 模块化、包体小、`ChangeSet` 模型与 OT 天然契合 |

### 7. OT 引擎(`internal/ot`)

移植 rustpad 所用的 [operational-transform](https://github.com/spebern/operational-transform-rs) / ot.js 模型:

- `TextOperation`:`Retain(n)`、`Insert(s)`、`Delete(n)` 的有序序列。
- **计数单位为 Unicode code point**(非字节、非 UTF-16 code unit),前后端必须一致;前端负责在 CM6 的 UTF-16 偏移与 code point 偏移之间换算。
- 核心 API:

```go
func (op *Op) Apply(doc string) (string, error)       // 应用到文本
func Compose(a, b *Op) (*Op, error)                   // 合并连续操作
func Transform(a, b *Op) (a2, b2 *Op, err error)      // TP1: b∘a2 ≡ a∘b2
func (op *Op) TransformIndex(pos uint32) uint32       // 光标位置随操作变换
```

- JSON 线格式与 rustpad 一致:正整数=Retain,负整数=Delete,字符串=Insert,如 `[5, " hello", -2]`。
- 测试:常规单元测试 + property test——随机生成文档与并发操作对,断言 `Transform` 收敛(TP1)、`Compose` 与顺序应用等价。

### 8. 文档会话模型(`internal/document`)

每个活跃文档对应一个 `Document` 对象,由 `sync.Mutex` 保护(单文档操作串行化,文档间并行):

```go
type Document struct {
    mu        sync.Mutex
    ID        string
    Text      string          // 当前全文
    Ops       []UserOp        // 自 revision 0 起的历史(revision = len(Ops))
    Language  string
    TTL       time.Duration
    ExpiresAt time.Time
    users     map[uint64]UserInfo    // socketId → 昵称/hue
    cursors   map[uint64]CursorData
    conns     map[uint64]*conn       // 活跃连接,含只读标记
    dirty     bool                   // 是否有未落盘修改
}
```

**Edit 处理流程**(与 rustpad 服务端一致):客户端带 `revision R` 提交操作 → 服务端校验 `R ≤ len(Ops)` → 将该操作依次与 `Ops[R:]` 中每个历史操作 `Transform` → 校验变换后操作的基长度与当前文本一致、且应用后不超过 1MB → `Apply` 到文本、追加历史 → 向该文档所有连接广播 `History{start: R', operations: [...]}`。

**Registry**:`map[docID]*Document` + 全局锁。文档装载路径:内存命中 → SQLite 命中(装载,历史从空开始但文本完整,revision 从 0 计——客户端首次连接总是全量拿 `History`,不受影响)→ 都未命中则新建空文档。

**内存换出**:文档连接数为 0 且已落盘、且静默超过阈值(默认 10 分钟)时,从 Registry 移除;历史操作随之丢弃(快照即真相,与 rustpad 行为一致)。

### 9. WebSocket 协议

单端点双向 JSON 消息,外层为 `{"<类型>": <负载>}` 的单键对象(rustpad 风格)。

**客户端 → 服务端**

| 消息 | 负载 | 说明 |
|---|---|---|
| `Edit` | `{revision, operation}` | 提交编辑;只读连接发送则被服务端断开 |
| `SetLanguage` | `"go"` | 切换语法高亮语言 |
| `ClientInfo` | `{name, hue}` | 设置昵称与颜色 |
| `CursorData` | `{cursors: [..], selections: [[a,b],..]}` | 上报本端光标/选区(code point 偏移) |
| `SetExpiry` | `{ttlSeconds}` | 修改文档 TTL,范围 [60, 100年];只读连接不可用 |

**服务端 → 客户端**

| 消息 | 负载 | 说明 |
|---|---|---|
| `Identity` | `socketId` | 连接建立后分配的会话 ID |
| `History` | `{start, operations: [{id, operation}]}` | 从 revision `start` 起的操作序列;新连接先收到全量 |
| `Language` | `"go"` | 语言变更广播 |
| `UserInfo` | `{id, info | null}` | 用户加入/更新/离开(null 表示离开) |
| `UserCursor` | `{id, data}` | 某用户光标更新 |
| `Expiry` | `{ttlSeconds, expiresAt}` | TTL 变更广播(含首次连接时的当前值) |
| `Killed` | `{reason}` | 文档被管理员删除或已过期,客户端置为只读并提示 |

只读连接:服务端在握手阶段即打上 readonly 标记,收到 `Edit`/`SetLanguage`/`SetExpiry` 一律拒绝;`ClientInfo`/`CursorData` 允许(只读用户也出现在用户列表,光标可见)。

### 10. HTTP 路由

| 方法/路径 | 说明 |
|---|---|
| `GET /*` | 前端 SPA(embed.FS);文档 ID 在 URL hash 中:`/#<id>` 可写、`/#view/<roId>` 只读 |
| `GET /api/socket/{id}` | WebSocket 升级,可写会话;文档不存在则创建 |
| `GET /api/readonly/{roId}` | WebSocket 升级,只读会话;按 `readonly_id` 查找,不存在返回 404(**只读链接不隐式建文档**) |
| `GET /api/text/{id}` | 裸文本(rustpad 兼容) |
| `GET /admin` | 管理控制台页面(Basic Auth) |
| `GET /api/admin/documents?page=&size=` | 分页文档列表(Basic Auth),按 `updated_at` 倒序 |
| `DELETE /api/admin/documents/{id}` | 删除文档(Basic Auth) |

Basic Auth 校验使用 `crypto/subtle` 常数时间比较;`ADMIN_USER`/`ADMIN_PASSWORD` 任一未设置时,`/admin` 与 `/api/admin/*` 返回 404。

### 11. 只读分享设计

- 文档首次持久化时生成 `readonly_id`:与主 ID 同强度的随机串(见 §14),二者存于同一行、互相独立不可推导。
- 可写页面侧栏提供"复制只读链接"(向服务端查询本文档的 `readonly_id`,仅可写连接可查)。
- 只读页面:CodeMirror 置 `EditorState.readOnly`,隐藏语言/TTL 控件;安全性不依赖前端——服务端按连接的 readonly 标记强制拦截写消息。

### 12. 持久化与生命周期(`internal/store`)

**Schema**

```sql
CREATE TABLE documents (
    id          TEXT PRIMARY KEY,
    readonly_id TEXT NOT NULL UNIQUE,
    text        TEXT NOT NULL,
    language    TEXT NOT NULL DEFAULT 'plaintext',
    ttl_seconds INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,   -- unix 秒
    updated_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL
);
CREATE INDEX idx_documents_expires ON documents(expires_at);
CREATE INDEX idx_documents_updated ON documents(updated_at DESC);
```

连接串启用 `_pragma=journal_mode(WAL)` 与 `busy_timeout`。

**落盘时机**(快照式,只存全文不存历史):

1. 编辑后防抖 3 秒静默落盘;
2. 周期兜底:每 60 秒扫描 dirty 文档刷写;
3. 文档最后一个连接断开时;
4. 收到 SIGINT/SIGTERM 优雅关停时,全量刷写后退出。

**过期语义**:`expires_at = updated_at + ttl_seconds`,即**自最后一次编辑起滚动续期**;只读访问不续期。默认 TTL 24h(可由 `DEFAULT_TTL` 配置),`SetExpiry` 修改 TTL 后立即按当前 `updated_at` 重算并落盘。

**清理**:后台 goroutine 每 60 秒执行 `DELETE FROM documents WHERE expires_at < now`,同时检查内存 Registry 中已过期的文档——断开其连接(发 `Killed{reason:"expired"}`)并移除。

### 13. 前端方案(`frontend/`)

Vite + TypeScript,无 UI 框架(或按需引入极轻量方案),构建产物输出到 `internal/server/dist` 并 `go:embed`。

**OT 客户端状态机**(ot.js 经典三态,与 rustpad 前端等价):

- `Synchronized`:本地编辑 → 立即发 `Edit{revision, op}`,进入 `AwaitingConfirm`;
- `AwaitingConfirm(outstanding)`:期间的本地编辑 `Compose` 进 buffer,进入 `AwaitingWithBuffer`;收到含自己操作的 `History` 即确认;
- `AwaitingWithBuffer(outstanding, buffer)`:确认后立即发送 buffer;
- 收到他人操作时,与 outstanding/buffer 做 `Transform` 后再应用到编辑器。

**CM6 集成要点**:

- `updateListener` 将 `ChangeSet` 转为 `TextOperation`(注意 UTF-16 → code point 偏移换算);远程操作经 `dispatch` 应用时打上标记,避免回环。
- 远程光标/选区:`StateField` + `Decoration`(widget 光标 + mark 选区),按用户 hue 着色;收到远程操作时对已知光标位置做 `TransformIndex`,收到 `UserCursor` 时整体替换。
- 语言包:`@codemirror/lang-*` 官方集合(javascript/typescript、go、python、rust、java、cpp、html、css、json、markdown、sql、xml、yaml…),`Compartment` 动态切换。
- 断线重连:指数退避;重连后以本地 revision 请求增量,并重发未确认操作(协议与 rustpad 相同,天然支持)。

**页面结构**:

- 编辑页:顶栏(文档 ID、复制可写/只读链接、连接状态)+ 编辑器 + 侧栏(在线用户列表、昵称/颜色、语言选择、TTL 设置)。
- 只读页:同布局,隐藏写控件,显示"只读"徽标。
- 管理页 `/admin`:独立轻量页面(同一构建产物内),表格 + 分页 + 删除确认。

### 14. ID 与安全

- 文档 ID 与 `readonly_id`:`crypto/rand` 生成,base58/自定义字母表编码,长度 ≥ 16 字符(> 90 bit 熵),URL 即凭证。
- WebSocket 握手校验 `Origin` 同源,防跨站 WebSocket 劫持。
- 服务端对所有入站消息做尺寸限制(单消息 ≤ 2MB)与格式校验;操作基长度不匹配即断开连接。
- 管理端仅 Basic Auth,建议部署于 HTTPS 反代之后。

### 15. 配置(环境变量)

| 变量 | 默认 | 说明 |
|---|---|---|
| `PORT` | `3030` | 监听端口 |
| `SQLITE_PATH` | `gopad.db` | SQLite 文件路径 |
| `ADMIN_USER` / `ADMIN_PASSWORD` | 空 | 管理端凭证;任一为空则管理端关闭 |
| `DEFAULT_TTL` | `24h` | 新文档默认 TTL(Go duration 格式) |
| `MAX_DOC_SIZE` | `1048576` | 单文档字节上限 |
| `BASE_PATH` | 空 | 子路径挂载前缀(如 `/gopad`),用于反代场景 |

子路径挂载:服务端只接收带前缀的请求(前缀外一律 404),并在启动时把构建产物
中的 HTML 改写一次——注入 `<base href="<prefix>/">`、把根绝对的 assets URL 加上
前缀;前端所有 API / WebSocket / 分享链接都由 `<base>` 解析出的前缀拼出
(`frontend/src/base.ts`)。因此反代只需原样透传路径,不要剥掉前缀。

### 16. 项目结构

```
gopad/
├── cmd/gopad/main.go         # 装配、优雅关停
├── internal/ot/              # TextOperation:Apply/Compose/Transform + property tests
├── internal/document/        # Document、Registry、会话广播
├── internal/server/          # 路由、WS handler、admin API、embed 静态资源
├── internal/store/           # SQLite 读写、过期清理
├── frontend/                 # Vite + TS + CodeMirror 6
│   ├── src/ot/               # 客户端 OT 与状态机
│   ├── src/editor/           # CM6 集成、远程光标
│   └── src/admin/            # 管理页
├── docs/requirements-and-design.md
├── Dockerfile                # 多阶段:node 构建前端 → go 构建 → distroless
└── Makefile                  # build/test/dev
```

### 17. 测试策略

- `internal/ot`:property test(随机操作对的 TP1 收敛、Compose 等价性)+ 边界用例(空操作、纯 Unicode、越界)。
- `internal/document`:并发 Edit 的竞态测试(`-race`),revision 乱序/过期提交处理。
- 集成测试:启动 httptest 服务,两个真实 WS 客户端并发随机编辑数百次,断言最终文本一致;断线重连场景;只读连接写入被拒;admin 删除后活跃连接收到 `Killed`。
- 前端:OT 状态机与偏移换算的单元测试(vitest)。

### 18. 里程碑

| # | 里程碑 | 验收标准 |
|---|---|---|
| M1 | OT 引擎 | 全部单元/property 测试通过 |
| M2 | WS 服务端 + 内存文档 | 集成测试:双客户端并发编辑收敛 |
| M3 | CM6 前端 | 浏览器双窗口实时协同,含光标/用户列表/语言切换 |
| M4 | 持久化 + 生命周期 | 重启恢复;TTL 修改生效;过期自动清理 |
| M5 | 只读链接 + 管理控制台 | 只读强制生效;admin 分页/删除可用 |
| M6 | 打包与加固 | 单 binary + Docker 镜像;优雅关停不丢数据;`-race` 全绿 |

### 19. 风险与对策

| 风险 | 对策 |
|---|---|
| OT Transform 实现出错导致文本发散 | property test 覆盖 + 服务端对每个操作校验基长度,不匹配即断开让客户端全量重同步 |
| UTF-16 / code point 偏移换算错位 | 前端封装统一换算层并单测;集成测试覆盖 emoji/中文混排 |
| 历史操作数组无限增长占用内存 | 内存换出时丢弃历史;活跃文档可在无连接时截断历史(快照即真相) |
| SQLite 写放大(每 3 秒全文快照) | 文档 ≤ 1MB、WAL 模式下可接受;必要时提高防抖阈值 |
