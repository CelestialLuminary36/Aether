# Aether 核心设计

- **日期**: 2026-09-03
- **状态**: 待审阅
- **目标版本**: v1（桌面客户端）

## 背景与目标

Aether 是一款全新设计的代理软件。动机：xray 过度背负 v2ray 遗产、代码重且难以取用；sing-box 依赖 sagernet 自家 fork、配置频繁 breaking change、无法单独嵌入。Aether 要综合两者优点，确立差异化。经确认，用户的三项核心痛点为：**协议与生态取舍、代码架构与可维护性、配置与心智模型**。

用户是代理软件开发新手。因此设计优先保证：抽象清晰、可渐进实现、每个阶段的产出可验证、配置错误在启动时暴露并指向修法。

## 核心决策摘要

1. **架构方案**: 显式管道（方案 A）。core 是叶子包（零依赖，只 import 标准库 + common），实现全在子包，core 反向依赖注入。无 init() 自注册，用显式装配。
2. **v1 形态**: 桌面客户端，本地起 SOCKS5/HTTP 入站 + 规则路由 + DNS + 真加密出站协议，命令行运行，配置文件驱动。不碰 TUN（留 v2）。
3. **UDP 进 v1**: core 从第一天就同时建模流（TCP）与包（UDP）两套对等抽象。
4. **buffer**: 使用 `packet.Buffer`（带预留头部空间的字节切片），UDP 路径零额外拷贝。
5. **路由**: 两阶段路由 + 显式 `resolve` 动作（详见 2.2 节）。
6. **配置**: 结构化 YAML + 强校验 + 版本契约（`version: 1` 向后兼容承诺）。
7. **协议/传输**: 明确区分 `TransportPluggable`（可组合传输，如 VLESS/Trojan/SS）与 `SelfTransported`（自带传输，如 Hysteria2/TUIC），配置校验器据此报错而非静默忽略。
8. **协议交付顺序**: Shadowsocks-2022 与 Trojan 优先，VLESS/REALITY/Hysteria2/TUN 留 v2。
9. **测试策略**: core 各块走 TDD；协议实现走"对着真实服务端跑集成测试"。

---

## 第一段：core 的契约

`core` 包是叶子包、零依赖。只含类型与接口，不含实现。实现全在子包。

### 1.1 Metadata —— 替代现有 Session

现有 `core.Session` 嵌入 `context.Context`（Go 反模式），且无法随便复制、塞日志、缓存。改为纯数据：

```go
package core

type Network uint8

const (
	NetworkTCP Network = iota
	NetworkUDP
)

// Addr 统一表达"域名或 IP + 端口"，避免每次用 net.SplitHostPort 重新解析。
type Addr struct {
	ip     netip.Addr // 有效时表示 IP 目标
	domain string     // 非空时表示域名目标
	port   uint16
}

func (a Addr) IsDomain() bool
func (a Addr) String() string // "example.com:443" / "1.2.3.4:443" / "[::1]:443"

// Metadata 是一次代理请求的全部已知事实。纯数据，可复制，无行为。
type Metadata struct {
	Network Network

	Source      Addr   // 客户端
	Destination Addr   // 客户端请求的目标（可能是域名）

	InboundTag string // 哪个入站收到的
	User       string // 认证用户名，未认证为空

	// ---- 以下由管道逐步填充，入站不负责 ----

	SniffedProtocol string     // "http" / "tls" / "quic" / "dns"，嗅探后填
	SniffedDomain   string     // SNI 或 Host，嗅探后填
	ResolvedIPs     []netip.Addr // DNS 解析后填，未解析为 nil
}
```

关键：**分层填充**。入站只填前 4 个字段，其余由管道各阶段填。任何字段"是空的"都有明确含义，无需猜测。

### 1.2 流与包 —— 对等抽象

`net.Conn` 直接用（标准库）。UDP 侧新增对等抽象 `PacketConn`：

```go
// PacketConn 是面向数据包的连接。每个包自带地址。
type PacketConn interface {
	// ReadPacket 读一个包。dest 是该包的目标地址（UDP 一条"连接"上
	// 客户端可以往多个目标发包）。
	ReadPacket(buf *packet.Buffer) (dest Addr, err error)
	WritePacket(buf *packet.Buffer, dest Addr) error

	Close() error
	LocalAddr() net.Addr
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
}

// Buffer(在 common/packet) —— 带预留头部空间的字节切片。
type Buffer struct {
	data  []byte // 底层数组
	start int    // 有效数据起点，前面是预留头部空间
	end   int
}

func (b *Buffer) ExtendHeader(n int) []byte // 向前扩展，返回可写入头部的区域
```

`Buffer` 的取舍理由：Shadowsocks、Trojan 的 UDP 都要在包前加地址/nonce 头。若用朴素 `[]byte` 需每次分配新 buffer + 拷贝一次。`Buffer` 几十行代码换 UDP 路径零额外拷贝。

### 1.3 四个核心接口

```go
// Dispatcher —— 唯一的调度中心。入站只认识它。
type Dispatcher interface {
	DispatchStream(ctx context.Context, md *Metadata, conn net.Conn) error
	DispatchPacket(ctx context.Context, md *Metadata, conn PacketConn) error
}

// Inbound —— 只负责说好自己那门协议。
type Inbound interface {
	Tag() string  // 配置里的标签，如 "socks-in"
	Type() string // 协议名，如 "socks"
	Start(ctx context.Context) error
	Close() error
}

// Outbound —— 只负责建连。
type Outbound interface {
	Tag() string
	Type() string
	Networks() []Network // 支持哪些网络，配置校验时用

	DialStream(ctx context.Context, md *Metadata) (net.Conn, error)
	DialPacket(ctx context.Context, md *Metadata) (PacketConn, error)
}

// Router —— 纯函数：元数据进，出站标签出。
type Router interface {
	Route(md *Metadata) (Decision, error)
}

// 语义化错误（core/errors.go）—— 协议无关，各入站自行翻译成本协议错误码。
var (
	ErrNetworkUnreachable  = errors.New("network is unreachable")
	ErrHostUnreachable     = errors.New("host is unreachable")
	ErrConnectionRefused   = errors.New("connection refused")
	ErrBlockedByRule       = errors.New("blocked by rule")
	ErrNetworkNotSupported = errors.New("network not supported")

	// ErrNeedMoreData 由 Sniffer 返回，表示需要更多字节才能判定。
	ErrNeedMoreData = errors.New("need more data")
)
```

三处刻意设计：

- **Inbound 无 `Handler` 参数**。现有 `inbound.Handler` 返回 outConn、由 inbound 自己 relay，把选路+relay 责任摊到每个 inbound。
- **Outbound 同时有 DialStream 与 DialPacket**，不支持的返回 `ErrNetworkNotSupported`。配合 `Networks()` 让**配置加载时**就报"把 UDP 流量路由到了不支持 UDP 的出站"，而非运行时静默失败。
- **Router.Route 是纯函数**。只输入 Metadata、只输出 Decision，可用零网络 IO 的单元测试覆盖。

---

## 第二段：Dispatcher 管道

**固定顺序**，五步：

```
                      ┌─────────────────────────────────────┐
  Inbound ──md,conn──> │ ① 嗅探  ② 路由  ③ 解析  ④ 建连  ⑤ 中继 │ ──> Outbound
                      └─────────────────────────────────────┘
                               ▲                          │
                               └──── Resolve 时重路由 ────┘
```

固定顺序意味着任何一次连接的日志都是同构、可对比的。这是对 xray 那种"到处可插"的反向取舍——灵活性让位给可读性。

唯一的多趟循环在 ②↔③ 之间（两阶段路由，见 2.2），且每趟循环至多发生一次（第二遍不允许再 Resolve）。其余步骤严格单向。

### 2.1 ① 嗅探：非破坏性

问题：客户端用 IP 直连时看不到域名，无法按域名路由。

做法（`CachedConn`）：读进来的字节先存 cache，嗅探读 cache；之后 relay 从 cache 开始读，客户端无感。

```go
type CachedConn struct {
	net.Conn
	cache *packet.Buffer
}

type Sniffer interface {
	Name() string
	Sniff(header []byte) (*SniffResult, error) // ErrNeedMoreData = 再多给点
}
```

v1 实现三个：TLS ClientHello（取 SNI）、HTTP（取 Host）、DNS（识别查询用于劫持）。带超时上限（如 200ms / 最多 4KB），超时放弃嗅探继续走——不得因嗅探卡住连接。

### 2.2 ②③ 路由与解析：两阶段，解决先后矛盾

两难：按 GeoIP 路由需先知道 IP → 要知道 IP 需先 DNS → 但若域名最终走代理，本地解析是错的（DNS 污染给假 IP，且解析本身泄露访问意图）。

方案——显式两阶段路由：

```go
type Action uint8

const (
	ActionProxy  Action = iota
	ActionDirect
	ActionBlock
	ActionResolve
)

type Decision struct {
	Action   Action
	Outbound string // Action==Proxy 时有效
}
```

- **阶段一**：只用不需 IP 的信息匹配——域名、端口、网络类型、入站标签、用户、嗅探结果。
- 命中 `ActionResolve` → 执行 DNS 解析，填 `md.ResolvedIPs`，然后**重跑路由**（第二遍才能匹配 GeoIP/CIDR）。第二遍不允许再 Resolve，避免死循环。
- 命中 `ActionProxy` → 直接走，域名原样交给出站，本地一次 DNS 不查。

"要不要本地解析"变成一个**配置里看得见的规则**：

```yaml
route:
  rules:
    - domain_suffix: [".cn", ".alibaba.com"]
      action: resolve      # 想按 GeoIP 判国内，先解析
    - geoip: ["cn", "private"]
      action: direct
    - default: proxy
```

对比 xray 的 `domainStrategy: IPIfNonMatch`（一个全局开关暗中改变所有规则语义）——本方案清楚得多。

### 2.3 ④ 建连：错误映射回协议

出站失败时，SOCKS5 需回具体 REP 码。现有 [inbound/socks/socks.go] 有 `TODO: Map the outbound error to the appropriate SOCKS5 REP code`，全部回 `0x01 general failure`——这是抽象缺失的信号。

出站产出 `core/errors.go` 的语义错误，各入站自己翻译：SOCKS5 → REP，HTTP → 502/403。用户能从客户端报错看出"被规则挡了"还是"目标连不上"。

### 2.4 ⑤ 中继：现有实现的改进

现有 `core.Relay` 结构正确，但三处要改：

1. **不响应 ctx 取消**。`ctx.Done()` 时强控 `Close()` 双端，否则程序退出时连接挂到 TCP 超时。
2. **不统计字节数**。流量统计、按用户限速、`aether stats` 全依赖。`io.CopyBuffer` 返回值现被丢弃。
3. **`dst` 不支持 `CloseWrite` 时无兜底**。加密协议包装后的 conn 常不支持，半关闭语义会丢失。

**关于现有 `common/bufpool`**：当前 TCP↔TCP 场景下它实际没被用上。`io.CopyBuffer` 会先检查 `dst` 是否实现 `io.ReaderFrom`，而 `*net.TCPConn` 实现了 → 走 `dst.ReadFrom(src)`，在 Linux 用 `splice(2)` 零拷贝，传的 buffer 被忽略。这是好事，但也意味着 bufpool 在纯 TCP 场景是备而不用。接上加密出站（conn 不再实现 ReaderFrom）后才真正生效。别删，但别以为它现在在起作用。

### 2.5 UDP 的 NAT 表 —— 放在 Dispatcher 层

UDP 无连接，但代理必须伪造连接概念：客户端 `10.0.0.1:5000` → `8.8.8.8:53`，服务器回的包必须找回那个客户端端口。

```
key: (源地址, 目标地址) → value: 出站 PacketConn + 最后活跃时间
```

**NAT 表归 Dispatcher 管，不归任何单个协议管**。否则有 N 份实现、N 套超时策略、N 个内存泄漏可能。放 Dispatcher：

- 一份实现、一套超时（默认 5 分钟 idle，DNS 查询单独 10 秒）；
- 一个全局会话上限，防 UDP flood 打爆内存（xray 早年吃过这个亏）；
- `aether stats` 能一次看到所有 UDP 会话。

---

## 第三段：配置层与版本契约

目标：**让配置错误在启动时暴露，且错误信息直接指向修法**。而非启动成功、用起来才发现某功能静默不生效。

### 3.1 三层分离：语法 → 语义 → 实例

```
config.yaml
   │  ① 语法层：YAML/JSON → AST，记录每个节点的行号
   ▼
Document (带位置信息)
   │  ② 语义层：校验器。检查引用、类型、协议约束——纯函数，零 IO
   ▼
Config (已校验的强类型结构)
   │  ③ 实例层：按 Config 构造 Inbound/Outbound/Router 实例
   ▼
运行中的 App
```

收益：

- **② 是纯函数，可大量单元测试覆盖**。校验逻辑是"用户体验"的主要来源，必须最好测。
- **`aether check config.yaml` 只跑 ①②**，不监听、不建连、不需 root。可进 CI。
- **行号能带到错误信息里**（① 保留位置）：

```
config.yaml:23:7: 出站 "hy2-jp" 不支持 UDP，但路由规则 #2 把 UDP 流量指向了它
  23 │       outbound: hy2-jp
     │                 ^^^^^^
  提示：该出站的 network 为 [tcp]。若需 UDP，请改用支持 UDP 的出站，
        或在规则中加 network: [tcp] 限定。
```

对比 sing-box 的 `json: cannot unmarshal ...`。此差距即"心智模型"痛点的具体形态。

### 3.2 校验器检查什么

| 类别 | 例子 |
|---|---|
| **引用完整性** | 路由规则指向的 outbound tag 存在；tag 不重复；DNS server tag 存在 |
| **能力匹配** | UDP 规则不能指向纯 TCP 出站；`transport: ws` 不能配给自带传输的协议 |
| **可达性** | 有无规则永远匹配不到（被前面完全覆盖）→ 警告而非错误 |
| **常见陷阱** | 监听 `0.0.0.0` 但没设认证 → 警告；DNS 服务器地址是域名但没有 bootstrap → 错误 |

第三、四类是 warning 不是 error，**但默认打印**。这是 xray 完全没做、sing-box 做得很少的——恰是新手最易踩的坑。

### 3.3 版本契约

```yaml
version: 1 # 必填。缺失 = 错误，不猜
```

三条硬规则：

1. **`version: 1` 的配置，在所有 1.x 版本上行为一致**。不是"能启动"，是行为一致——不允许靠改默认值偷偷改行为。
2. **字段只增不减、不改语义**。淘汰走"标记 deprecated → 启动时警告并说明替代方案 → 至少保留到下个大版本"。sing-box 不这样做，直接删，所以升级踩雷。
3. **新增字段必须有默认值，且默认值等于旧版本行为**。这是第 1 条的执行保障。

工具兜底：`aether migrate config.yaml` 升级旧版本配置，输出 diff。

### 3.4 配置骨架

```yaml
version: 1

log:
  level: info
  format: text          # text | json

inbounds:
  - tag: local
    type: mixed         # socks5 + http 共用一个端口
    listen: 127.0.0.1:1080

outbounds:
  - tag: proxy
    type: shadowsocks
    server: example.com:8388
    method: 2022-blake3-aes-128-gcm
    password: "..."
  - tag: direct
    type: direct
  - tag: block
    type: block

dns:
  servers:
    - tag: local
      address: 223.5.5.5
      detour: direct    # 这台 DNS 走哪个出站查
    - tag: remote
      address: https://1.1.1.1/dns-query
      detour: proxy
  rules:
    - domain_suffix: [".cn"]
      server: local
    - default: remote

route:
  rules:
    - domain_suffix: [".cn"]
      action: resolve
    - geoip: [cn, private]
      action: direct
    - default: { action: proxy, outbound: proxy }
```

刻意的选择：

- **`detour` 让 DNS 走哪个出站是显式的**。DNS 泄露是经典问题，把它变成看得见的一行。
- **路由规则里 `action` 与 `outbound` 分开**。`direct/block/resolve` 是内建语义，`proxy` 才需 `outbound`。比 sing-box 那种"outbound 字段混着 `direct`/`block` 特殊值"清楚。
- **无嵌套 `settings` / `streamSettings`**（v2ray protobuf 的历史包袱）。

---

## 第四段：协议与传输的正交性

### 4.1 两类协议，接口上明说

```go
// TransportPluggable：协议只管认证与封装，底层连接由外部传输层提供。
// VLESS / Trojan / Shadowsocks / VMess 属此类。
type TransportPluggable interface {
	Outbound
	Handshake(ctx context.Context, md *Metadata, under net.Conn) (net.Conn, error)
}

// SelfTransported：协议自带传输（通常 QUIC），不接受外部注入。
// Hysteria2 / TUIC / WireGuard 属此类。
type SelfTransported interface {
	Outbound
	selfTransported()
}
```

**唯一目的：让配置校验器能报错。** 给 `hysteria2` 写 `transport: ws`，校验器报：

```
config.yaml:31:5: 协议 "hysteria2" 自带 QUIC 传输，不支持 transport 配置
  提示：若需 WebSocket 传输，请改用 vless / trojan / shadowsocks
```

而非像现在的 sing-box 静默忽略——用户以为自己配了 ws，实际没有，然后花几小时排查"为什么过不了 CDN"。

### 4.2 传输层的接口

对 `TransportPluggable`，传输是两个正交维度（学 xray，这点它做对了）：

```go
// Transport：负责"怎么建立一条字节流"
type Transport interface {
	Name() string // "tcp" / "ws" / "grpc" / "httpupgrade"
	Dial(ctx context.Context, addr Addr) (net.Conn, error)
}

// Security：负责"怎么加密这条流"，作用于已建立连接之上
type Security interface {
	Name() string // "none" / "tls" / "reality"
	Client(ctx context.Context, conn net.Conn, serverName string) (net.Conn, error)
}
```

组合顺序固定：`Transport.Dial → Security.Client → Protocol.Handshake`。加新传输不需碰任何协议代码——保留 xray 正交设计的价值，只是不假装它对所有协议成立。

### 4.3 显式装配，不用 init 魔法

```go
// presets/presets.go —— 唯一一处把具体实现接进来的地方
func Register(r *registry.Registry) {
	r.Inbound("socks",       socks.New)
	r.Inbound("http",        http.New)
	r.Inbound("mixed",       mixed.New)

	r.Outbound("direct",      direct.New)
	r.Outbound("block",       block.New)
	r.Outbound("socks",       socksout.New)
	r.Outbound("shadowsocks", shadowsocks.New)
	r.Outbound("trojan",      trojan.New)

	r.Transport("tcp", tcp.New)
	r.Transport("ws",  ws.New)
	r.Security("none", none.New)
	r.Security("tls",  tls.New)
}
```

一个文件一眼看完这个二进制支持什么；想裁剪出精简版，复制此文件删几行即可。对比 sing-box 散落各处的 `init()` 自注册。

### 4.4 v1 协议清单，按依赖顺序

| # | 内容 | 说明 |
|---|---|---|
| 1 | core 契约 + Dispatcher + relay | 地基，纯 TDD |
| 2 | direct / block 出站，socks 入站改造 | 打通新管道，现有代码迁移 |
| 3 | 路由引擎（两阶段） | 纯函数，重点测试 |
| 4 | DNS 子系统 | 依赖路由（detour） |
| 5 | UDP 全链路（PacketConn + NAT 表） | 最容易出 bug，独立阶段 |
| 6 | http / mixed 入站 | 简单，巩固管道 |
| 7 | socks/http 上游出站 | 第一个"转发到另一代理"的出站，用于验证模型 |
| 8 | Shadowsocks-2022 | 第一个加密协议 |
| 9 | Trojan + TLS/ws 传输层 | 验证传输正交性 |
| 10 | 可观测性（stats / 连接列表 / API） | 收尾 |

阶段 5（UDP）排在加密协议之前是刻意的：UDP 会反咬整个模型，越早验证越好。代价是阶段 1-7 只能用 socks 上游出站凑合测。用户已确认维持此顺序（慢工出细活）。

阶段 10 之后为 v2 范围：VLESS、REALITY、Hysteria2、TUN。

---

## 测试策略

- **core 契约 / Dispatcher / 路由引擎 / DNS / UDP NAT 表**：纯 TDD，零网络依赖的单元测试为主。
- **协议实现**：对着真实服务端跑集成测试（如用真实 Shadowsocks-2022 服务端测试客户端握手与中继）。
- **配置校验器**：表驱动测试，给坏配置断言具体错误信息。
- **两阶段路由 & 传输/安全正交性**：单元测试覆盖（纯函数/纯装配）。

## 明确的非目标（v1）

- 不做 TUN / 透明代理 / 平台 VPN 权限。
- 不做 VLESS / REALITY / Hysteria2 / TUIC。
- 不做移动端。
- 不承诺"兼容 sing-box / clash 配置格式"（v2 可考虑转换器）。
