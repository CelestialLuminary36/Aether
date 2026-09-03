# Aether v1 — Plan 1：Core 契约与新管道 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Aether 从「inbound 自持 handler、自己 relay」的旧结构迁到方案 A：core 是叶子包，Dispatcher 是唯一调度中心，inbound 只认识 Dispatcher。行为不变——依然是 SOCKS5 in → direct out——但下面的所有骨架都换成新契约，为后续 plan（路由、DNS、UDP、加密协议）铺路。

**Architecture:** `core/` 只放类型和接口（零第三方依赖，只 import 标准库 + `common/*`）。`dispatcher/` 实现 `core.Dispatcher`，Plan 1 里只做「选唯一出站 + 建连 + relay」，不含嗅探/路由/DNS（后续 plan 加）。协议实现（inbound/outbound）反向依赖 core，不允许 core 反向 import。

**Tech Stack:** Go 1.27.1，标准库为主（`net`、`context`、`log/slog`、`io`、`errors`），零第三方依赖。

**Spec:** `docs/superpowers/specs/2026-09-03-aether-core-design.md`

## Global Constraints

- Go 1.27.1（`go.mod` 已声明）
- Module path: `github.com/CelestialLuminary36/Aether`
- `core/` 包只 import 标准库 + `common/*`；不得 import 任何 `inbound/*`、`outbound/*`、`dispatcher/*`
- 不允许 `init()` 自注册；具体实现的装配写在 `cmd/aether/main.go`（后续 plan 引入 `presets` 包）
- 每个 core 类型/接口必须能被零网络 IO 的单元测试覆盖（或明确说明无需测试，如纯 `var` 声明）
- 所有面向 core 契约的 outbound 都需实现 `DialStream` 与 `DialPacket` 两个方法；不支持某网络的返回 `core.ErrNetworkNotSupported`（Plan 1 里所有 outbound 的 UDP 侧都返回此错误，Plan 4 再补 UDP 实现）
- 与 spec 的一处偏差：`Dispatcher.DispatchStream` 签名新增 `onDialed func() error` 参数，让 inbound 有机会在 dial 完成、relay 开始之前发送本协议的成功回复（如 SOCKS5 REP=0x00）。spec 未考虑此需求，本计划做此偏差。

---

## File Structure

**新增：**
- `common/packet/buffer.go` — 带头部预留空间的字节缓冲（UDP 零拷贝所需，Plan 4 用；本 plan 只定义与测试）
- `common/packet/buffer_test.go`
- `core/addr.go` — 统一的「域名或 IP + 端口」类型
- `core/addr_test.go`
- `core/metadata.go` — `Network`、`Metadata`、`Decision`、`Action`
- `core/metadata_test.go`
- `core/interfaces.go` — `Dispatcher`、`Inbound`、`Outbound`、`Router`、`PacketConn`
- `core/errors.go` — 语义化错误
- `core/relay_test.go` — Relay v2 的测试
- `dispatcher/dispatcher.go` — `core.Dispatcher` 的最小实现（只调唯一出站 + Relay）
- `dispatcher/dispatcher_test.go`
- `outbound/block/block.go` — 阻断出站
- `outbound/block/block_test.go`
- `outbound/direct/direct_test.go` — direct 的单元测试
- `inbound/socks/errmap.go` — 语义错误 → SOCKS5 REP 码的映射（可独立测试的纯函数）
- `inbound/socks/errmap_test.go`
- `inbound/socks/socks_test.go` — 集成测试（起真 SOCKS5 → 真 direct → httptest 后端）

**修改：**
- `core/relay.go` — 重写：ctx 感知、字节数返回、`CloseWrite` 无支持时兜底关闭
- `core/app.go` — 更新包注释（现有内容一行注释，加一句说明新的契约层）
- `outbound/direct/direct.go` — 迁移到 `core.Outbound`：`DialStream`/`DialPacket`；使用 `core.Metadata` 而非旧 `Session`
- `inbound/socks/socks.go` — 迁移到 `core.Inbound`：移除 `Handler`，改成持有 `core.Dispatcher`；错误经 `errmap` 翻译为 REP
- `cmd/aether/main.go` — 用新契约装配

**删除：**
- `core/context.go` — 旧 `Session`，被 `core.Metadata` 替代
- `inbound/interface.go` — 旧 `Inbound`/`Handler`，接口进 `core/interfaces.go`
- `outbound/interface.go` — 旧 `Outbound`，进 `core/interfaces.go`

---

## Task 1: `common/packet.Buffer` — 带头部预留的字节缓冲

**Rationale:** UDP 加密协议（SS/Trojan）要在包**前面**加头（地址、nonce）。用朴素 `[]byte` 就得「分配新 buffer + 拷贝一次」。`Buffer` 底层是固定大小数组，用 `start/end` 两个游标表示有效区，允许向前扩展占用头部预留空间。Plan 1 定义 + 测试，Plan 4 才真正用。

**Files:**
- Create: `common/packet/buffer.go`
- Test: `common/packet/buffer_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `type Buffer struct { ... }`
  - `func New(size int) *Buffer` — 新建 buffer，`size` 为底层数组总容量
  - `func (b *Buffer) Cap() int`
  - `func (b *Buffer) Len() int`
  - `func (b *Buffer) Bytes() []byte` — 当前有效数据（`data[start:end]`）
  - `func (b *Buffer) Reset()` — 清空，游标回到中位（保留前后各半空间）
  - `func (b *Buffer) Advance(n int) []byte` — 从尾部预留 n 字节可写区，返回该 slice；若剩余尾部空间不足则 panic
  - `func (b *Buffer) ExtendHeader(n int) []byte` — 向前扩展 n 字节头部空间，返回该 slice；若头部空间不足则 panic
  - `func (b *Buffer) Truncate(n int)` — 把 `Len()` 截到 n（不改 start）

- [ ] **Step 1: 写测试**

Create `common/packet/buffer_test.go`:

```go
package packet

import (
	"bytes"
	"testing"
)

func TestBuffer_NewIsEmpty(t *testing.T) {
	b := New(64)
	if b.Cap() != 64 {
		t.Fatalf("Cap()=%d want 64", b.Cap())
	}
	if b.Len() != 0 {
		t.Fatalf("Len()=%d want 0", b.Len())
	}
	if len(b.Bytes()) != 0 {
		t.Fatalf("Bytes() not empty: %v", b.Bytes())
	}
}

func TestBuffer_AdvanceWritesTail(t *testing.T) {
	b := New(64)
	p := b.Advance(4)
	copy(p, []byte{1, 2, 3, 4})
	if !bytes.Equal(b.Bytes(), []byte{1, 2, 3, 4}) {
		t.Fatalf("Bytes()=%v want [1 2 3 4]", b.Bytes())
	}
	if b.Len() != 4 {
		t.Fatalf("Len()=%d want 4", b.Len())
	}
}

func TestBuffer_ExtendHeaderPrepends(t *testing.T) {
	b := New(64)
	tail := b.Advance(2)
	copy(tail, []byte{9, 9})
	head := b.ExtendHeader(3)
	copy(head, []byte{1, 2, 3})
	if !bytes.Equal(b.Bytes(), []byte{1, 2, 3, 9, 9}) {
		t.Fatalf("Bytes()=%v want [1 2 3 9 9]", b.Bytes())
	}
}

func TestBuffer_ExtendHeaderInsufficientPanics(t *testing.T) {
	b := New(8)
	// Reset 把 start 放到中位。size=8 → start=4，头部空间 4 字节。
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on header exhaustion")
		}
	}()
	b.ExtendHeader(100)
}

func TestBuffer_AdvanceInsufficientPanics(t *testing.T) {
	b := New(8)
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on tail exhaustion")
		}
	}()
	b.Advance(100)
}

func TestBuffer_ResetRestoresMidpoint(t *testing.T) {
	b := New(64)
	b.Advance(10)
	b.Reset()
	if b.Len() != 0 {
		t.Fatalf("post-Reset Len()=%d want 0", b.Len())
	}
	// After Reset we can extend header again.
	head := b.ExtendHeader(4)
	if len(head) != 4 {
		t.Fatalf("post-Reset ExtendHeader len=%d want 4", len(head))
	}
}

func TestBuffer_Truncate(t *testing.T) {
	b := New(64)
	tail := b.Advance(10)
	for i := range tail {
		tail[i] = byte(i)
	}
	b.Truncate(4)
	if !bytes.Equal(b.Bytes(), []byte{0, 1, 2, 3}) {
		t.Fatalf("post-Truncate Bytes()=%v want [0 1 2 3]", b.Bytes())
	}
}
```

- [ ] **Step 2: 跑测试验证失败**

Run: `go test ./common/packet/... -v`
Expected: 编译失败——包不存在。

- [ ] **Step 3: 实现 Buffer**

Create `common/packet/buffer.go`:

```go
// Package packet 提供 UDP 与其它面向包场景使用的带头部预留字节缓冲。
package packet

// Buffer 是带头部预留空间的字节缓冲。
//
// 底层是固定大小数组。有效数据是 data[start:end]。
// Reset 时把游标放到中位，让 ExtendHeader 与 Advance 都有空间可用。
type Buffer struct {
	data  []byte
	start int
	end   int
}

// New 分配一个总容量为 size 的 Buffer。
func New(size int) *Buffer {
	b := &Buffer{data: make([]byte, size)}
	b.Reset()
	return b
}

func (b *Buffer) Cap() int   { return cap(b.data) }
func (b *Buffer) Len() int   { return b.end - b.start }
func (b *Buffer) Bytes() []byte { return b.data[b.start:b.end] }

// Reset 清空数据并把游标放到中位，让前后各半都能扩展。
func (b *Buffer) Reset() {
	mid := cap(b.data) / 2
	b.start = mid
	b.end = mid
}

// Advance 从尾部占用 n 字节可写区并返回该 slice。
// 若尾部剩余空间不足则 panic。
func (b *Buffer) Advance(n int) []byte {
	if b.end+n > cap(b.data) {
		panic("packet.Buffer: Advance exceeds capacity")
	}
	old := b.end
	b.end += n
	return b.data[old:b.end]
}

// ExtendHeader 从头部向前占用 n 字节并返回该 slice。
// 若头部剩余空间不足则 panic。
func (b *Buffer) ExtendHeader(n int) []byte {
	if b.start-n < 0 {
		panic("packet.Buffer: ExtendHeader exceeds head room")
	}
	b.start -= n
	return b.data[b.start : b.start+n]
}

// Truncate 把有效数据的长度截到 n。
func (b *Buffer) Truncate(n int) {
	if n < 0 || b.start+n > cap(b.data) {
		panic("packet.Buffer: Truncate out of range")
	}
	b.end = b.start + n
}
```

- [ ] **Step 4: 跑测试验证通过**

Run: `go test ./common/packet/... -v`
Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add common/packet/
git commit -m "feat(packet): add Buffer with head reservation"
```

---

## Task 2: `core.Addr` — 统一的域名或 IP 地址

**Rationale:** 现在 `Session` 里 target 是 string，每次要 `SplitHostPort` 重新解析（`core/context.go:88`）。合并成一个类型，一次解析多次读。区分「域名目标」与「IP 目标」是路由与 DNS 的基础。

**Files:**
- Create: `core/addr.go`
- Test: `core/addr_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `type Addr struct { ip netip.Addr; domain string; port uint16 }`
  - `func AddrFromIPPort(ip netip.Addr, port uint16) Addr`
  - `func AddrFromDomain(domain string, port uint16) Addr`
  - `func ParseAddr(s string) (Addr, error)` — 解析 "example.com:443" / "1.2.3.4:443" / "[::1]:443"；数字 IP 走 IP 分支，其它走域名分支
  - `func (a Addr) IsDomain() bool`
  - `func (a Addr) IsIP() bool`
  - `func (a Addr) IP() netip.Addr`
  - `func (a Addr) Domain() string`
  - `func (a Addr) Port() uint16`
  - `func (a Addr) String() string` — 输出 "host:port" 形式，IPv6 加中括号
  - `func (a Addr) IsZero() bool` — 未设置的 Addr

- [ ] **Step 1: 写测试**

Create `core/addr_test.go`:

```go
package core

import (
	"net/netip"
	"testing"
)

func TestParseAddr_IPv4(t *testing.T) {
	a, err := ParseAddr("1.2.3.4:443")
	if err != nil {
		t.Fatalf("ParseAddr error: %v", err)
	}
	if !a.IsIP() || a.IsDomain() {
		t.Fatalf("expected IP, got domain=%v ip=%v", a.IsDomain(), a.IsIP())
	}
	if a.IP() != netip.MustParseAddr("1.2.3.4") {
		t.Fatalf("IP()=%v", a.IP())
	}
	if a.Port() != 443 {
		t.Fatalf("Port()=%d want 443", a.Port())
	}
	if a.String() != "1.2.3.4:443" {
		t.Fatalf("String()=%q want 1.2.3.4:443", a.String())
	}
}

func TestParseAddr_IPv6(t *testing.T) {
	a, err := ParseAddr("[2001:db8::1]:443")
	if err != nil {
		t.Fatalf("ParseAddr error: %v", err)
	}
	if !a.IsIP() {
		t.Fatalf("expected IP")
	}
	if a.String() != "[2001:db8::1]:443" {
		t.Fatalf("String()=%q", a.String())
	}
}

func TestParseAddr_Domain(t *testing.T) {
	a, err := ParseAddr("example.com:443")
	if err != nil {
		t.Fatalf("ParseAddr error: %v", err)
	}
	if !a.IsDomain() || a.IsIP() {
		t.Fatalf("expected domain")
	}
	if a.Domain() != "example.com" {
		t.Fatalf("Domain()=%q", a.Domain())
	}
	if a.Port() != 443 {
		t.Fatalf("Port()=%d", a.Port())
	}
	if a.String() != "example.com:443" {
		t.Fatalf("String()=%q", a.String())
	}
}

func TestParseAddr_Errors(t *testing.T) {
	cases := []string{"", "no-port", "example.com", "example.com:", "example.com:abc", "example.com:99999"}
	for _, s := range cases {
		if _, err := ParseAddr(s); err == nil {
			t.Errorf("ParseAddr(%q) expected error, got nil", s)
		}
	}
}

func TestAddr_ZeroValueIsZero(t *testing.T) {
	var a Addr
	if !a.IsZero() {
		t.Fatalf("zero value should IsZero")
	}
}

func TestAddrFromIPPort(t *testing.T) {
	a := AddrFromIPPort(netip.MustParseAddr("1.2.3.4"), 80)
	if a.String() != "1.2.3.4:80" {
		t.Fatalf("String()=%q", a.String())
	}
}

func TestAddrFromDomain(t *testing.T) {
	a := AddrFromDomain("x.io", 8080)
	if !a.IsDomain() {
		t.Fatalf("expected domain")
	}
	if a.String() != "x.io:8080" {
		t.Fatalf("String()=%q", a.String())
	}
}
```

- [ ] **Step 2: 跑测试验证失败**

Run: `go test ./core/... -v -run TestParseAddr`
Expected: 编译失败——`addr.go` 不存在。

- [ ] **Step 3: 实现 Addr**

Create `core/addr.go`:

```go
package core

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// Addr 统一表达"域名或 IP + 端口"。
//
// 一个 Addr 要么持有 ip（IP 目标），要么持有 domain（域名目标），
// 不会同时持有。zero 值表示未设置，通过 IsZero 判定。
type Addr struct {
	ip     netip.Addr
	domain string
	port   uint16
}

// AddrFromIPPort 构造一个 IP 目标。
func AddrFromIPPort(ip netip.Addr, port uint16) Addr {
	return Addr{ip: ip, port: port}
}

// AddrFromDomain 构造一个域名目标。
func AddrFromDomain(domain string, port uint16) Addr {
	return Addr{domain: domain, port: port}
}

// ParseAddr 解析 "host:port" 字符串。若 host 是数字 IP，走 IP 分支；否则走域名分支。
func ParseAddr(s string) (Addr, error) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return Addr{}, fmt.Errorf("parse addr %q: %w", s, err)
	}
	if host == "" {
		return Addr{}, fmt.Errorf("parse addr %q: empty host", s)
	}
	p64, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return Addr{}, fmt.Errorf("parse addr %q: bad port: %w", s, err)
	}
	if p64 == 0 {
		return Addr{}, errors.New("parse addr: port must be > 0")
	}
	port := uint16(p64)
	if ip, err := netip.ParseAddr(host); err == nil {
		return AddrFromIPPort(ip, port), nil
	}
	return AddrFromDomain(host, port), nil
}

func (a Addr) IsDomain() bool { return a.domain != "" }
func (a Addr) IsIP() bool     { return a.ip.IsValid() }
func (a Addr) IsZero() bool   { return a.domain == "" && !a.ip.IsValid() && a.port == 0 }
func (a Addr) IP() netip.Addr { return a.ip }
func (a Addr) Domain() string { return a.domain }
func (a Addr) Port() uint16   { return a.port }

func (a Addr) String() string {
	if a.IsDomain() {
		return net.JoinHostPort(a.domain, strconv.Itoa(int(a.port)))
	}
	if a.IsIP() {
		return netip.AddrPortFrom(a.ip, a.port).String()
	}
	return "<invalid>"
}
```

- [ ] **Step 4: 跑测试验证通过**

Run: `go test ./core/... -v -run TestParseAddr`
Then: `go test ./core/... -v -run TestAddr`
Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add core/addr.go core/addr_test.go
git commit -m "feat(core): add Addr for domain-or-IP + port"
```

---

## Task 3: `core.Metadata`, `core.Network`, `core.Decision`, `core.Action`

**Rationale:** 替代旧 `Session`（`core/context.go`）。新类型是纯数据——不嵌 `context.Context`，可复制，可塞日志、可路由缓存。Decision/Action 是 Router 的输出（Plan 2 才用 Router，但类型现在就定，避免后续把它嵌进 Router 里循环依赖）。

**Files:**
- Create: `core/metadata.go`
- Test: `core/metadata_test.go`

**Interfaces:**
- Consumes: `core.Addr`
- Produces:
  - `type Network uint8` with constants `NetworkTCP`, `NetworkUDP`, method `String()`
  - `type Metadata struct { Network Network; Source Addr; Destination Addr; InboundTag string; User string; SniffedProtocol string; SniffedDomain string; ResolvedIPs []netip.Addr }`
  - `type Action uint8` with constants `ActionProxy`, `ActionDirect`, `ActionBlock`, `ActionResolve`, method `String()`
  - `type Decision struct { Action Action; Outbound string }`

- [ ] **Step 1: 写测试**

Create `core/metadata_test.go`:

```go
package core

import "testing"

func TestNetwork_String(t *testing.T) {
	cases := map[Network]string{
		NetworkTCP: "tcp",
		NetworkUDP: "udp",
	}
	for n, want := range cases {
		if got := n.String(); got != want {
			t.Errorf("Network(%d).String()=%q want %q", n, got, want)
		}
	}
}

func TestAction_String(t *testing.T) {
	cases := map[Action]string{
		ActionProxy:   "proxy",
		ActionDirect:  "direct",
		ActionBlock:   "block",
		ActionResolve: "resolve",
	}
	for a, want := range cases {
		if got := a.String(); got != want {
			t.Errorf("Action(%d).String()=%q want %q", a, got, want)
		}
	}
}

func TestMetadata_ZeroValueIsUsable(t *testing.T) {
	var m Metadata
	if m.Network != NetworkTCP {
		t.Fatalf("zero Network want NetworkTCP, got %v", m.Network)
	}
	if !m.Source.IsZero() || !m.Destination.IsZero() {
		t.Fatalf("zero Metadata addresses should IsZero")
	}
	if m.ResolvedIPs != nil {
		t.Fatalf("zero ResolvedIPs should be nil")
	}
}

func TestMetadata_IsCopyable(t *testing.T) {
	// Metadata 必须是纯数据，可以按值复制而不共享状态（ResolvedIPs 底层 slice 除外，那是有意的）。
	m := Metadata{Network: NetworkUDP, InboundTag: "in-a"}
	m2 := m
	m2.InboundTag = "in-b"
	if m.InboundTag != "in-a" {
		t.Fatalf("copy modified original: %q", m.InboundTag)
	}
}
```

- [ ] **Step 2: 跑测试验证失败**

Run: `go test ./core/... -v -run 'TestNetwork|TestAction|TestMetadata'`
Expected: 编译失败——`metadata.go` 不存在。

- [ ] **Step 3: 实现**

Create `core/metadata.go`:

```go
package core

import "net/netip"

// Network 表示一次请求的网络类型。
type Network uint8

const (
	NetworkTCP Network = iota
	NetworkUDP
)

func (n Network) String() string {
	switch n {
	case NetworkTCP:
		return "tcp"
	case NetworkUDP:
		return "udp"
	default:
		return "unknown"
	}
}

// Metadata 是一次代理请求的全部已知事实。纯数据，可复制，无行为。
//
// 入站负责填 Network、Source、Destination、InboundTag、User。
// 其它字段由 Dispatcher 管道各阶段填。
type Metadata struct {
	Network Network

	Source      Addr
	Destination Addr

	InboundTag string
	User       string

	// 以下字段由 Dispatcher 管道填。

	SniffedProtocol string
	SniffedDomain   string
	ResolvedIPs     []netip.Addr
}

// Action 是 Router 决策的动作。
type Action uint8

const (
	ActionProxy Action = iota
	ActionDirect
	ActionBlock
	ActionResolve
)

func (a Action) String() string {
	switch a {
	case ActionProxy:
		return "proxy"
	case ActionDirect:
		return "direct"
	case ActionBlock:
		return "block"
	case ActionResolve:
		return "resolve"
	default:
		return "unknown"
	}
}

// Decision 是 Router.Route 的输出。Outbound 仅在 Action==ActionProxy 时有效。
type Decision struct {
	Action   Action
	Outbound string
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./core/... -v -run 'TestNetwork|TestAction|TestMetadata'`
Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add core/metadata.go core/metadata_test.go
git commit -m "feat(core): add Metadata, Network, Decision, Action"
```

---

## Task 4: `core` interfaces + 语义化错误

**Rationale:** 一次把 `Dispatcher`、`Inbound`、`Outbound`、`Router`、`PacketConn` 全部接口和语义化错误放齐。这些是纯类型/`var` 声明，没有可测行为，一次提交即可。后续 task 会开始实现。

**Files:**
- Create: `core/interfaces.go`, `core/errors.go`
- Modify: `core/app.go`（更新包注释）
- Delete: `inbound/interface.go`, `outbound/interface.go`, `core/context.go`

**Interfaces:**
- Consumes: `core.Addr`, `core.Metadata`, `packet.Buffer`
- Produces:
  - `type Dispatcher interface { DispatchStream(ctx, md, conn, onDialed) error; DispatchPacket(ctx, md, conn) error }`
  - `type Inbound interface { Tag() string; Type() string; Start(ctx) error; Close() error }`
  - `type Outbound interface { Tag() string; Type() string; Networks() []Network; DialStream(ctx, md) (net.Conn, error); DialPacket(ctx, md) (PacketConn, error) }`
  - `type Router interface { Route(md *Metadata) (Decision, error) }`
  - `type PacketConn interface { ReadPacket(buf *packet.Buffer) (Addr, error); WritePacket(buf *packet.Buffer, dest Addr) error; Close() error; LocalAddr() net.Addr; SetReadDeadline(time.Time) error; SetWriteDeadline(time.Time) error }`
  - `var Err{NetworkUnreachable, HostUnreachable, ConnectionRefused, BlockedByRule, NetworkNotSupported, NeedMoreData, NoRouteMatched}` — 七个语义错误

- [ ] **Step 1: 建立 `core/interfaces.go`**

Create `core/interfaces.go`:

```go
package core

import (
	"context"
	"net"
	"time"

	"github.com/CelestialLuminary36/Aether/common/packet"
)

// Dispatcher 是唯一的调度中心。入站只认识它。
//
// DispatchStream 的 onDialed 回调（可为 nil）在出站 dial 成功、relay 开始
// 之前被调用一次。入站可在此发送本协议的"成功回复"（如 SOCKS5 REP=0x00、
// HTTP 200 Connection Established）。onDialed 返回错误则中止整次调度。
//
// 判断错误发生在哪个阶段的方法：调用方在闭包外维护一个 bool，闭包内置为
// true；调用返回后，若 err != nil 且 bool 仍为 false，说明是 dial 阶段失
// 败（可翻译成本协议的失败码）；否则是 relay 阶段错误（连接已经成功回复
// 过，只记日志即可）。
type Dispatcher interface {
	DispatchStream(ctx context.Context, md *Metadata, conn net.Conn, onDialed func() error) error
	DispatchPacket(ctx context.Context, md *Metadata, conn PacketConn) error
}

// Inbound 是一个监听中的协议服务端。
type Inbound interface {
	Tag() string  // 配置里的实例名，如 "socks-in"
	Type() string // 协议名，如 "socks"
	Start(ctx context.Context) error
	Close() error
}

// Outbound 是一个建连器。
//
// Networks() 声明它支持的网络类型（用于配置校验）。不支持的网络在
// DialStream/DialPacket 里必须返回 ErrNetworkNotSupported。
type Outbound interface {
	Tag() string
	Type() string
	Networks() []Network

	DialStream(ctx context.Context, md *Metadata) (net.Conn, error)
	DialPacket(ctx context.Context, md *Metadata) (PacketConn, error)
}

// Router 是纯函数：元数据进，决策出。
type Router interface {
	Route(md *Metadata) (Decision, error)
}

// PacketConn 是面向数据包的连接。
//
// 与 net.Conn 的本质区别：一条"连接"上客户端可以往多个目标发包，每个
// 包自带地址。
type PacketConn interface {
	ReadPacket(buf *packet.Buffer) (dest Addr, err error)
	WritePacket(buf *packet.Buffer, dest Addr) error

	Close() error
	LocalAddr() net.Addr
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}
```

- [ ] **Step 2: 建立 `core/errors.go`**

Create `core/errors.go`:

```go
package core

import "errors"

// 语义化错误。出站产出这些错误，各入站自行翻译成本协议的错误码。
var (
	ErrNetworkUnreachable  = errors.New("network is unreachable")
	ErrHostUnreachable     = errors.New("host is unreachable")
	ErrConnectionRefused   = errors.New("connection refused")
	ErrBlockedByRule       = errors.New("blocked by rule")
	ErrNetworkNotSupported = errors.New("network not supported")
	ErrNoRouteMatched      = errors.New("no route matched")

	// ErrNeedMoreData 由 Sniffer 返回，表示需要更多字节才能判定。Plan 2 才用。
	ErrNeedMoreData = errors.New("need more data")
)
```

- [ ] **Step 3: 更新 `core/app.go` 的包注释**

Edit `core/app.go`（现有内容只有注释和 package 行）：

```go
// Package core 定义 Aether 的核心契约：类型（Addr、Metadata、Decision）
// 与接口（Dispatcher、Inbound、Outbound、Router、PacketConn），以及跨协议
// 的语义化错误。
//
// core 是叶子包：只 import 标准库与 common/*，不允许反向依赖任何具体
// 实现（inbound、outbound、dispatcher）。
package core
```

- [ ] **Step 4: 删除旧文件**

```bash
git rm core/context.go inbound/interface.go outbound/interface.go
```

（此步会让项目暂时编译不过——旧文件被 inbound/socks 和 outbound/direct 依赖。后续 task 里会重写它们。）

- [ ] **Step 5: 验证 core 包能独立编译**

Run: `go build ./core/... ./common/...`
Expected: 成功。

Run: `go build ./...`
Expected: 失败（inbound/socks/direct 还没迁移）。这是预期的，下一 task 会修。

- [ ] **Step 6: 提交**

```bash
git add core/ inbound/interface.go outbound/interface.go
git commit -m "feat(core): add contracts (interfaces, errors); remove old Session/Handler"
```

---

## Task 5: `core.Relay` v2 — ctx 感知 / 字节计数 / 半关闭兜底

**Rationale:** 现有 `core.Relay`（`core/relay.go`）三处要改（spec 2.4）：
1. 不响应 ctx 取消 → 加 goroutine 在 ctx.Done 时强关双端
2. 丢弃字节数 → 返回 `(uplink, downlink uint64, err error)`
3. `dst` 不支持 `CloseWrite` 时无兜底 → 那种情况直接 `Close()` 整个连接（能让对端也感知半边关了）

**Files:**
- Modify: `core/relay.go`
- Create: `core/relay_test.go`

**Interfaces:**
- Consumes: `net.Conn`, `bufpool`
- Produces:
  - `func Relay(ctx context.Context, left, right net.Conn) (uplink, downlink uint64, err error)` — 阻塞直到两方向都结束或 ctx 取消；err 是遇到的第一个非 EOF 错误（若两方向都正常结束则为 nil）

- [ ] **Step 1: 写测试**

Create `core/relay_test.go`:

```go
package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// pipePair 用 net.Pipe 造两对内存中的双向 net.Conn。
//   client <-> a  b <-> server
// Relay(a, b) 会把 client 和 server 桥起来。
func pipePair() (client, a, b, server net.Conn) {
	client, a = net.Pipe()
	b, server = net.Pipe()
	return
}

func TestRelay_BidirectionalCopy(t *testing.T) {
	client, a, b, server := pipePair()
	defer client.Close()
	defer server.Close()

	done := make(chan struct {
		up, down uint64
		err      error
	}, 1)
	go func() {
		up, down, err := Relay(context.Background(), a, b)
		done <- struct {
			up, down uint64
			err      error
		}{up, down, err}
	}()

	// client → server
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := client.Write([]byte("hello")); err != nil {
			t.Errorf("client write: %v", err)
		}
		client.(interface{ CloseWrite() error }).CloseWrite()
	}()
	go func() {
		defer wg.Done()
		if _, err := server.Write([]byte("world!")); err != nil {
			t.Errorf("server write: %v", err)
		}
		server.(interface{ CloseWrite() error }).CloseWrite()
	}()

	// 读方向
	clientBuf, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	serverBuf, err := io.ReadAll(server)
	if err != nil {
		t.Fatalf("server read: %v", err)
	}

	wg.Wait()
	res := <-done

	if !bytes.Equal(clientBuf, []byte("world!")) {
		t.Errorf("client got %q want %q", clientBuf, "world!")
	}
	if !bytes.Equal(serverBuf, []byte("hello")) {
		t.Errorf("server got %q want %q", serverBuf, "hello")
	}
	// Relay(a, b): a 侧从 client 收 "hello" 转发到 b（uplink），b 侧从 server 收 "world!" 转发到 a（downlink）。
	if res.up != 5 {
		t.Errorf("uplink=%d want 5", res.up)
	}
	if res.down != 6 {
		t.Errorf("downlink=%d want 6", res.down)
	}
	if res.err != nil && !errors.Is(res.err, io.EOF) {
		t.Errorf("unexpected err: %v", res.err)
	}

	// Relay 返回后应已关闭 a/b。二次 Close 应无 panic，且不阻塞。
	if err := a.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Errorf("a.Close err=%v", err)
	}
	if err := b.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Errorf("b.Close err=%v", err)
	}
}

func TestRelay_ContextCancelClosesBothSides(t *testing.T) {
	client, a, b, server := pipePair()
	defer client.Close()
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, _, err := Relay(ctx, a, b)
		done <- err
	}()

	// 双方都不发数据；等一小段确保 Relay 阻塞在读上。
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Relay did not return after ctx cancel")
	}

	// a/b 已被 Relay Close，client/server 侧的读应立刻返回 EOF 或错误。
	client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := io.ReadAll(client); err != nil && !isClosedErr(err) && err != io.EOF {
		// 允许 EOF / closed / deadline
		t.Logf("client read after cancel: %v (acceptable)", err)
	}
}

func isClosedErr(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrClosedPipe)
}
```

- [ ] **Step 2: 跑测试验证失败**

Run: `go test ./core/... -v -run TestRelay`
Expected: FAIL — 签名不匹配（现在的 Relay 是 `func Relay(left, right net.Conn)`，无返回值也无 ctx）。

- [ ] **Step 3: 重写 Relay**

Replace `core/relay.go`:

```go
package core

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"github.com/CelestialLuminary36/Aether/common/bufpool"
)

// halfCloser 匹配支持单向关写的连接（如 *net.TCPConn）。
type halfCloser interface {
	CloseWrite() error
}

// Relay 在 left 与 right 之间双向拷贝，直到两方向都结束或 ctx 取消。
//
// 返回值：
//   uplink   —— 从 left 拷贝到 right 的字节数
//   downlink —— 从 right 拷贝到 left 的字节数
//   err      —— 拷贝过程中遇到的第一个非 EOF 错误；两方向都自然结束时为 nil
//
// 行为保证：
//   - ctx 取消时，两端连接都会被 Close，返回值可能已计入部分字节。
//   - 函数返回时，left 与 right 都会被 Close。调用方不应再使用或再次 Close。
func Relay(ctx context.Context, left, right net.Conn) (uplink, downlink uint64, err error) {
	// ctx 取消时强关双端，让阻塞的 io.Copy 立刻返回。
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = left.Close()
			_ = right.Close()
		case <-stop:
		}
	}()

	var (
		up, down atomic.Uint64
		firstErr atomic.Value // stores error
		wg       sync.WaitGroup
	)
	wg.Add(2)

	pipe := func(dst, src net.Conn, counter *atomic.Uint64) {
		defer wg.Done()

		bufPtr := bufpool.Get()
		defer bufpool.Put(bufPtr)

		n, err := io.CopyBuffer(dst, src, bufPtr[:])
		counter.Add(uint64(n))

		if err != nil && !errors.Is(err, io.EOF) && !isBenignCloseErr(err) {
			// 只记录第一个错误。
			firstErr.CompareAndSwap(nil, err)
		}

		// 半关闭：让 dst 侧发 FIN，让对端知道我们不再发数据。
		if hc, ok := dst.(halfCloser); ok {
			_ = hc.CloseWrite()
		} else {
			// 无 CloseWrite 支持（很多加密连接），退化为整关；对端会立刻感知。
			_ = dst.Close()
		}
	}

	go pipe(right, left, &up)   // uplink: left → right
	go pipe(left, right, &down) // downlink: right → left
	wg.Wait()

	// 双向拷贝结束后，接管并关闭两端。调用方不再负责。
	// 这里用独立 goroutine 避免某一方 Close 阻塞导致 Relay 挂起。
	go func() { _ = left.Close() }()
	go func() { _ = right.Close() }()

	if v := firstErr.Load(); v != nil {
		err = v.(error)
	}
	return up.Load(), down.Load(), err
}

func isBenignCloseErr(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrClosedPipe)
}
```

- [ ] **Step 4: 跑测试验证通过**

Run: `go test ./core/... -v -run TestRelay`
Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add core/relay.go core/relay_test.go
git commit -m "feat(core): rewrite Relay with ctx cancel, byte counts, close fallback"
```

---

## Task 6: `outbound/direct` 迁移到 `core.Outbound`

**Rationale:** direct 是最简单的出站——用 `net.Dialer` 直连。把它迁到新接口是打通新契约的第一个真实用例。

**Files:**
- Modify: `outbound/direct/direct.go`
- Create: `outbound/direct/direct_test.go`

**Interfaces:**
- Consumes: `core.Outbound`, `core.Metadata`, `core.ErrNetworkNotSupported`
- Produces:
  - `type Direct struct { tag string }`
  - `func New(tag string) *Direct`
  - `func (*Direct) Tag() string`
  - `func (*Direct) Type() string` → `"direct"`
  - `func (*Direct) Networks() []core.Network` → `[]core.Network{core.NetworkTCP}` (Plan 4 再加 UDP)
  - `func (*Direct) DialStream(ctx, md) (net.Conn, error)` — 用 md.Destination 建 TCP
  - `func (*Direct) DialPacket(ctx, md) (core.PacketConn, error)` — Plan 1 一律返回 `nil, core.ErrNetworkNotSupported`

- [ ] **Step 1: 写测试**

Create `outbound/direct/direct_test.go`:

```go
package direct

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"testing"

	"github.com/CelestialLuminary36/Aether/core"
)

func TestDirect_Metadata(t *testing.T) {
	d := New("out-direct")
	if d.Tag() != "out-direct" {
		t.Errorf("Tag()=%q", d.Tag())
	}
	if d.Type() != "direct" {
		t.Errorf("Type()=%q", d.Type())
	}
	nets := d.Networks()
	if len(nets) != 1 || nets[0] != core.NetworkTCP {
		t.Errorf("Networks()=%v", nets)
	}
}

func TestDirect_DialStream_ToLoopback(t *testing.T) {
	// 起个回声监听器。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c)
	}()

	ap := ln.Addr().(*net.TCPAddr)
	md := &core.Metadata{
		Network:     core.NetworkTCP,
		Destination: core.AddrFromIPPort(netip.MustParseAddr(ap.IP.String()), uint16(ap.Port)),
	}

	d := New("out")
	conn, err := d.DialStream(context.Background(), md)
	if err != nil {
		t.Fatalf("DialStream: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf) != "ping" {
		t.Errorf("got %q want ping", buf)
	}
}

func TestDirect_DialPacket_NotSupported(t *testing.T) {
	d := New("out")
	_, err := d.DialPacket(context.Background(), &core.Metadata{Network: core.NetworkUDP})
	if !errors.Is(err, core.ErrNetworkNotSupported) {
		t.Fatalf("want ErrNetworkNotSupported, got %v", err)
	}
}
```

- [ ] **Step 2: 跑测试验证失败**

Run: `go test ./outbound/direct/... -v`
Expected: 编译失败（旧 direct 依赖已删的 outbound.Outbound / core.Session）。

- [ ] **Step 3: 重写 direct**

Replace `outbound/direct/direct.go`:

```go
// Package direct 实现一个直连出站：不经过任何上游代理。
package direct

import (
	"context"
	"net"

	"github.com/CelestialLuminary36/Aether/core"
)

// Direct 是一个直连出站。
type Direct struct {
	tag string
}

// New 构造一个带标签的直连出站。
func New(tag string) *Direct {
	return &Direct{tag: tag}
}

func (d *Direct) Tag() string             { return d.tag }
func (d *Direct) Type() string            { return "direct" }
func (d *Direct) Networks() []core.Network { return []core.Network{core.NetworkTCP} }

// DialStream 用系统 net.Dialer 直连 md.Destination。
func (d *Direct) DialStream(ctx context.Context, md *core.Metadata) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "tcp", md.Destination.String())
}

// DialPacket 在 Plan 1 尚未实现。Plan 4 会加 UDP 支持。
func (d *Direct) DialPacket(ctx context.Context, md *core.Metadata) (core.PacketConn, error) {
	return nil, core.ErrNetworkNotSupported
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./outbound/direct/... -v`
Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add outbound/direct/
git commit -m "feat(outbound/direct): migrate to core.Outbound"
```

---

## Task 7: `outbound/block` — 阻断出站

**Rationale:** 路由决策会在 Plan 2 里出现 `ActionBlock`。此时 dispatcher 会调用一个统一的"block outbound"，它两个 Dial 都直接返回 `ErrBlockedByRule`。写 Plan 1 就把它做齐，简单且用得上（cmd/aether 用它做兜底）。

**Files:**
- Create: `outbound/block/block.go`
- Create: `outbound/block/block_test.go`

**Interfaces:**
- Consumes: `core.Outbound`, `core.ErrBlockedByRule`
- Produces:
  - `type Block struct { tag string }`
  - `func New(tag string) *Block`
  - `func (*Block) Tag/Type/Networks` — Type="block"，Networks 声明 TCP+UDP
  - `func (*Block) DialStream/DialPacket` — 都返回 `nil, core.ErrBlockedByRule`

- [ ] **Step 1: 写测试**

Create `outbound/block/block_test.go`:

```go
package block

import (
	"context"
	"errors"
	"testing"

	"github.com/CelestialLuminary36/Aether/core"
)

func TestBlock_AllDialsRejected(t *testing.T) {
	b := New("blocked")
	if b.Type() != "block" {
		t.Errorf("Type=%q", b.Type())
	}
	if _, err := b.DialStream(context.Background(), &core.Metadata{}); !errors.Is(err, core.ErrBlockedByRule) {
		t.Errorf("DialStream err=%v want ErrBlockedByRule", err)
	}
	if _, err := b.DialPacket(context.Background(), &core.Metadata{}); !errors.Is(err, core.ErrBlockedByRule) {
		t.Errorf("DialPacket err=%v want ErrBlockedByRule", err)
	}
}

func TestBlock_NetworksBoth(t *testing.T) {
	b := New("blocked")
	nets := b.Networks()
	got := map[core.Network]bool{}
	for _, n := range nets {
		got[n] = true
	}
	if !got[core.NetworkTCP] || !got[core.NetworkUDP] {
		t.Errorf("Networks()=%v want both TCP and UDP", nets)
	}
}
```

- [ ] **Step 2: 跑测试验证失败**

Run: `go test ./outbound/block/... -v`
Expected: 编译失败——包不存在。

- [ ] **Step 3: 实现**

Create `outbound/block/block.go`:

```go
// Package block 实现一个阻断出站：任何 Dial 都直接返回 ErrBlockedByRule。
package block

import (
	"context"
	"net"

	"github.com/CelestialLuminary36/Aether/core"
)

// Block 是一个阻断出站。
type Block struct {
	tag string
}

// New 构造一个带标签的阻断出站。
func New(tag string) *Block {
	return &Block{tag: tag}
}

func (b *Block) Tag() string             { return b.tag }
func (b *Block) Type() string            { return "block" }
func (b *Block) Networks() []core.Network { return []core.Network{core.NetworkTCP, core.NetworkUDP} }

func (b *Block) DialStream(ctx context.Context, md *core.Metadata) (net.Conn, error) {
	return nil, core.ErrBlockedByRule
}

func (b *Block) DialPacket(ctx context.Context, md *core.Metadata) (core.PacketConn, error) {
	return nil, core.ErrBlockedByRule
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./outbound/block/... -v`
Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add outbound/block/
git commit -m "feat(outbound/block): add block outbound"
```

---

## Task 8: `dispatcher` — 最小 Dispatcher 实现

**Rationale:** Plan 1 的 Dispatcher 不含嗅探、路由、DNS——那些都是后续 plan 的事。这里只做「选唯一出站 → dial → 调用 onDialed → Relay」。核心价值是**接口**已经和后续 plan 一致，后面加功能是往内部加阶段，不改签名。

**Files:**
- Create: `dispatcher/dispatcher.go`
- Create: `dispatcher/dispatcher_test.go`

**Interfaces:**
- Consumes: `core.Dispatcher`, `core.Outbound`, `core.Relay`
- Produces:
  - `type StaticDispatcher struct { primary core.Outbound; block core.Outbound }`
  - `func New(primary core.Outbound, block core.Outbound) *StaticDispatcher`
  - `func (*StaticDispatcher) DispatchStream(ctx, md, conn, onDialed) error` — 一律走 primary；dial 失败返回语义错误
  - `func (*StaticDispatcher) DispatchPacket(ctx, md, conn) error` — Plan 1 一律返回 `core.ErrNetworkNotSupported`

- [ ] **Step 1: 写测试**

Create `dispatcher/dispatcher_test.go`:

```go
package dispatcher

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/CelestialLuminary36/Aether/core"
)

// stubOutbound 是一个可控 outbound，用来断言 Dispatcher 的行为。
type stubOutbound struct {
	tag         string
	dialErr     error
	dialedCount int
	server      net.Conn // Dial 成功时返回此 conn
}

func (s *stubOutbound) Tag() string              { return s.tag }
func (s *stubOutbound) Type() string             { return "stub" }
func (s *stubOutbound) Networks() []core.Network { return []core.Network{core.NetworkTCP, core.NetworkUDP} }
func (s *stubOutbound) DialStream(ctx context.Context, md *core.Metadata) (net.Conn, error) {
	s.dialedCount++
	if s.dialErr != nil {
		return nil, s.dialErr
	}
	return s.server, nil
}
func (s *stubOutbound) DialPacket(ctx context.Context, md *core.Metadata) (core.PacketConn, error) {
	return nil, core.ErrNetworkNotSupported
}

func TestDispatcher_DialSuccessInvokesOnDialedThenRelays(t *testing.T) {
	// server ← Relay → client（Relay 通过 out.server 和 inConn 连起来）
	inLocal, inRemote := net.Pipe()  // Dispatcher 拿 inLocal
	outLocal, outRemote := net.Pipe() // stub 通过 outLocal 交给 Dispatcher

	out := &stubOutbound{tag: "out", server: outLocal}
	d := New(out, nil)

	onDialedCalled := false
	relayErr := make(chan error, 1)
	go func() {
		relayErr <- d.DispatchStream(context.Background(),
			&core.Metadata{
				Network:     core.NetworkTCP,
				Destination: core.AddrFromIPPort(netip.MustParseAddr("1.2.3.4"), 80),
			},
			inLocal,
			func() error { onDialedCalled = true; return nil },
		)
	}()

	// 从 inbound 侧写 "hi"，应该出现在 outbound 侧
	go func() {
		_, _ = inRemote.Write([]byte("hi"))
		inRemote.(interface{ CloseWrite() error }).CloseWrite()
	}()

	buf := make([]byte, 2)
	if _, err := io.ReadFull(outRemote, buf); err != nil {
		t.Fatalf("read from outbound: %v", err)
	}
	if string(buf) != "hi" {
		t.Errorf("got %q want hi", buf)
	}
	_ = outRemote.Close()
	_ = inRemote.Close()

	select {
	case err := <-relayErr:
		if err != nil {
			t.Logf("Dispatch returned: %v (may be OK)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Dispatch did not return")
	}

	if !onDialedCalled {
		t.Error("onDialed was not invoked")
	}
	if out.dialedCount != 1 {
		t.Errorf("dialedCount=%d want 1", out.dialedCount)
	}
}

func TestDispatcher_DialFailurePropagatesError(t *testing.T) {
	out := &stubOutbound{tag: "out", dialErr: core.ErrHostUnreachable}
	d := New(out, nil)

	inLocal, inRemote := net.Pipe()
	defer inRemote.Close()

	onDialedCalled := false
	err := d.DispatchStream(context.Background(),
		&core.Metadata{Network: core.NetworkTCP},
		inLocal,
		func() error { onDialedCalled = true; return nil },
	)
	if !errors.Is(err, core.ErrHostUnreachable) {
		t.Fatalf("err=%v want ErrHostUnreachable", err)
	}
	if onDialedCalled {
		t.Error("onDialed should not be invoked when dial fails")
	}
}

func TestDispatcher_OnDialedErrorAbortsRelay(t *testing.T) {
	outConn, outPeer := net.Pipe()
	defer outPeer.Close()
	out := &stubOutbound{tag: "out", server: outConn}
	d := New(out, nil)

	inLocal, inRemote := net.Pipe()
	defer inRemote.Close()

	sentinel := errors.New("client bailed")
	err := d.DispatchStream(context.Background(),
		&core.Metadata{Network: core.NetworkTCP},
		inLocal,
		func() error { return sentinel },
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v want sentinel", err)
	}
}

func TestDispatcher_PacketAlwaysUnsupportedInPlan1(t *testing.T) {
	out := &stubOutbound{tag: "out"}
	d := New(out, nil)
	err := d.DispatchPacket(context.Background(), &core.Metadata{Network: core.NetworkUDP}, nil)
	if !errors.Is(err, core.ErrNetworkNotSupported) {
		t.Fatalf("err=%v want ErrNetworkNotSupported", err)
	}
}
```

- [ ] **Step 2: 跑测试验证失败**

Run: `go test ./dispatcher/... -v`
Expected: 编译失败——包不存在。

- [ ] **Step 3: 实现**

Create `dispatcher/dispatcher.go`:

```go
// Package dispatcher 实现 core.Dispatcher。
//
// Plan 1 版本是 StaticDispatcher：不含嗅探、路由、DNS，一律走 primary 出站。
// 后续 plan 会替换/扩展成完整管道。
package dispatcher

import (
	"context"
	"fmt"
	"net"

	"github.com/CelestialLuminary36/Aether/core"
)

// StaticDispatcher 把每一次 DispatchStream 都送到 primary 出站。
type StaticDispatcher struct {
	primary core.Outbound
	// block 目前未用；Plan 2 引入路由后 ActionBlock 会走此出站。保留字段以稳
	// 定 New 的签名，避免 Plan 2 重打字。
	block core.Outbound
}

// New 构造一个 StaticDispatcher。block 可为 nil。
func New(primary core.Outbound, block core.Outbound) *StaticDispatcher {
	return &StaticDispatcher{primary: primary, block: block}
}

// DispatchStream 走固定管道：dial primary → onDialed → Relay。
//
// 注意：outConn 的所有权在调用 core.Relay 后转移给 Relay；Relay 返回时会
// 同时关闭 inConn 与 outConn。因此 dispatcher 不得再 Close outConn。
func (d *StaticDispatcher) DispatchStream(
	ctx context.Context,
	md *core.Metadata,
	inConn net.Conn,
	onDialed func() error,
) error {
	outConn, err := d.primary.DialStream(ctx, md)
	if err != nil {
		return fmt.Errorf("dial primary: %w", err)
	}

	if onDialed != nil {
		if err := onDialed(); err != nil {
			_ = outConn.Close()
			return fmt.Errorf("onDialed: %w", err)
		}
	}

	_, _, err = core.Relay(ctx, inConn, outConn)
	return err
}

// DispatchPacket 在 Plan 1 尚未实现。Plan 4 会补上。
func (d *StaticDispatcher) DispatchPacket(
	ctx context.Context,
	md *core.Metadata,
	inConn core.PacketConn,
) error {
	return core.ErrNetworkNotSupported
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./dispatcher/... -v`
Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add dispatcher/
git commit -m "feat(dispatcher): add StaticDispatcher (dial-only, Plan 1)"
```

---

## Task 9: `inbound/socks` 语义错误→REP 映射（纯函数）

**Rationale:** Spec 2.3 要求各入站把 `core/errors.go` 的语义错误翻译成本协议错误码。这个映射是纯函数，独立文件独立测试，Task 10 里 socks 主循环直接调用。

**Files:**
- Create: `inbound/socks/errmap.go`
- Create: `inbound/socks/errmap_test.go`

**Interfaces:**
- Consumes: `core.Err*`
- Produces:
  - `func mapErrToRep(err error) byte` — 语义错误 → SOCKS5 REP 码

**SOCKS5 REP 码参考（RFC 1928 §6）:**
- 0x00 succeeded
- 0x01 general SOCKS server failure
- 0x02 connection not allowed by ruleset
- 0x03 Network unreachable
- 0x04 Host unreachable
- 0x05 Connection refused
- 0x08 Address type not supported

- [ ] **Step 1: 写测试**

Create `inbound/socks/errmap_test.go`:

```go
package socks

import (
	"errors"
	"fmt"
	"testing"

	"github.com/CelestialLuminary36/Aether/core"
)

func TestMapErrToRep(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want byte
	}{
		{"nil is succeeded", nil, 0x00},
		{"network unreachable", core.ErrNetworkUnreachable, 0x03},
		{"host unreachable", core.ErrHostUnreachable, 0x04},
		{"connection refused", core.ErrConnectionRefused, 0x05},
		{"blocked by rule", core.ErrBlockedByRule, 0x02},
		{"wrapped blocked", fmt.Errorf("dial primary: %w", core.ErrBlockedByRule), 0x02},
		{"unknown error", errors.New("boom"), 0x01},
		{"wrapped unknown", fmt.Errorf("wrap: %w", errors.New("boom")), 0x01},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mapErrToRep(c.err)
			if got != c.want {
				t.Errorf("mapErrToRep(%v)=0x%02x want 0x%02x", c.err, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试验证失败**

Run: `go test ./inbound/socks/... -v -run TestMapErrToRep`
Expected: FAIL — 函数不存在（且 socks.go 尚未迁移，包大概率不编译；如需先跳过整包编译，把测试用 build tag 隔离——不必；下一 task 会一起改）。

若整包编译失败，暂时把 socks.go 的旧引用注释掉/或先跳过 Step 2 并直接进入 Task 10 一起改。**推荐**：把 Task 9 与 Task 10 视为同一次工作，只是提交分开——先按 Task 10 的 Step 1 把 socks.go 骨架切换成用 core，再回来跑 Task 9 的测试。

- [ ] **Step 3: 实现映射**

Create `inbound/socks/errmap.go`:

```go
package socks

import (
	"errors"

	"github.com/CelestialLuminary36/Aether/core"
)

// SOCKS5 REP 码，参考 RFC 1928 §6。
const (
	repSucceeded             byte = 0x00
	repGeneralFailure        byte = 0x01
	repNotAllowedByRuleset   byte = 0x02
	repNetworkUnreachable    byte = 0x03
	repHostUnreachable       byte = 0x04
	repConnectionRefused     byte = 0x05
)

// mapErrToRep 把 core 层语义错误翻译成 SOCKS5 REP 码。
// 未识别的错误统一为 general failure。
func mapErrToRep(err error) byte {
	switch {
	case err == nil:
		return repSucceeded
	case errors.Is(err, core.ErrNetworkUnreachable):
		return repNetworkUnreachable
	case errors.Is(err, core.ErrHostUnreachable):
		return repHostUnreachable
	case errors.Is(err, core.ErrConnectionRefused):
		return repConnectionRefused
	case errors.Is(err, core.ErrBlockedByRule):
		return repNotAllowedByRuleset
	default:
		return repGeneralFailure
	}
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./inbound/socks/... -v -run TestMapErrToRep`
Expected: PASS（若 socks.go 尚未迁移导致整包不编译，先做 Task 10 步骤 1-3 打通再回来跑）。

- [ ] **Step 5: 提交**

（若与 Task 10 一起做，可合并提交；分开提交更符合 bite-sized。）

```bash
git add inbound/socks/errmap.go inbound/socks/errmap_test.go
git commit -m "feat(socks): map core semantic errors to SOCKS5 REP codes"
```

---

## Task 10: `inbound/socks` 迁移到 `core.Inbound` + `core.Dispatcher`

**Rationale:** 这是 Plan 1 里改动最大的文件。核心变化：
- 移除 `inbound.Handler` 参数，构造时注入 `core.Dispatcher`
- 不再自己 relay，改成 `dispatcher.DispatchStream(..., onDialed)`
- 用 `core.Metadata` 替代 `core.Session`
- 用 Task 9 的 `mapErrToRep` 把出站错误翻成 REP

**Files:**
- Modify: `inbound/socks/socks.go`（重写关键部分）
- Create: `inbound/socks/socks_test.go`（端到端集成测试）

**Interfaces:**
- Consumes: `core.Inbound`, `core.Dispatcher`, `core.Metadata`, `mapErrToRep`
- Produces:
  - `func New(tag, addr string, auth AuthMethod, users map[string]string, dispatcher core.Dispatcher) *Server`
  - `type AuthMethod uint8`（保留旧的 `AuthNone`/`AuthPassword`）
  - `func (*Server) Tag/Type/Start/Close` — 实现 `core.Inbound`

- [ ] **Step 1: 集成测试骨架（先写，虽然会 fail）**

Create `inbound/socks/socks_test.go`:

```go
package socks_test

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/CelestialLuminary36/Aether/core"
	"github.com/CelestialLuminary36/Aether/dispatcher"
	"github.com/CelestialLuminary36/Aether/inbound/socks"
	"github.com/CelestialLuminary36/Aether/outbound/direct"
)

// TestSOCKS_EndToEnd 起：
//   loopback httptest 后端  ←  SOCKS5 server（socks + direct + dispatcher）  ←  客户端连 SOCKS
// 客户端走 SOCKS5 CONNECT，读到后端的 "hello"。
func TestSOCKS_EndToEnd(t *testing.T) {
	// 后端
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		c, err := backend.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write([]byte("hello"))
	}()

	// SOCKS 服务器
	out := direct.New("out")
	disp := dispatcher.New(out, nil)
	s := socks.New("in", "127.0.0.1:0", socks.AuthNone, nil, disp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	// 等 listener 就绪
	time.Sleep(50 * time.Millisecond)
	socksAddr := s.ListenAddr() // 需要 socks.Server 暴露此方法

	// 手动跑一次 SOCKS5 CONNECT
	backendAP := backend.Addr().(*net.TCPAddr)
	client, err := net.Dial("tcp", socksAddr.String())
	if err != nil {
		t.Fatalf("Dial socks: %v", err)
	}
	defer client.Close()

	// 方法协商：VER=5, NMETHODS=1, METHOD=0(NoAuth)
	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatal(err)
	}
	if buf[0] != 0x05 || buf[1] != 0x00 {
		t.Fatalf("bad method reply: %x", buf)
	}

	// CONNECT: VER=5, CMD=1, RSV=0, ATYP=1(IPv4), IP(4), PORT(2)
	req := []byte{0x05, 0x01, 0x00, 0x01}
	req = append(req, netip.MustParseAddr(backendAP.IP.String()).As4()[:]...)
	req = append(req, byte(backendAP.Port>>8), byte(backendAP.Port&0xff))
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}

	// Reply: VER, REP, RSV, ATYP, BND.ADDR(4), BND.PORT(2) = 10 bytes
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0x00 {
		t.Fatalf("CONNECT REP=0x%02x want 0x00", reply[1])
	}

	// 读后端应答
	data, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q want hello", data)
	}
}
```

- [ ] **Step 2: 跑测试验证失败**

Run: `go test ./inbound/socks/... -v -run TestSOCKS_EndToEnd`
Expected: 编译或运行失败（新签名尚未实现）。

- [ ] **Step 3: 迁移 socks.go**

Modify `inbound/socks/socks.go`（关键改动，非全文重写；文件较长，下面列改动要点，具体保留原有的 SOCKS5 协议解析细节）：

**改 imports & 移除的东西：**
- 删除 `github.com/CelestialLuminary36/Aether/inbound`（旧接口不存在了）
- 保留 `core` 相关 import

**改 Server struct：**

```go
type Server struct {
	tag        string
	listenAddr string
	listener   net.Listener

	authMethod AuthMethod
	users      map[string]string

	dispatcher core.Dispatcher
}
```

**改 New：**

```go
// New 构造一个 SOCKS5 入站。dispatcher 不可为 nil。
func New(tag, addr string, authMethod AuthMethod, users map[string]string, disp core.Dispatcher) *Server {
	if disp == nil {
		panic("socks: dispatcher must not be nil")
	}
	return &Server{
		tag:        tag,
		listenAddr: addr,
		authMethod: authMethod,
		users:      users,
		dispatcher: disp,
	}
}
```

**新增：**

```go
func (s *Server) Tag() string  { return s.tag }
func (s *Server) Type() string { return "socks" }

// ListenAddr 返回实际监听地址（Start 后有效，测试用）。
func (s *Server) ListenAddr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}
```

**改 Start 签名**（移除 handler 参数）：

```go
func (s *Server) Start(ctx context.Context) error {
	// ... 与现有代码相同，只是不再拿 handler
	// accept loop 里 go s.handleConn(ctx, conn) 也去掉 handler 参数
}
```

**重写 handleConn 尾段**（协议解析部分不动；从"建立 Session"往下重写）：

```go
	// -----------------------------------------------------------------
	// 5. 构造 Metadata
	// -----------------------------------------------------------------
	destAddr, err := core.ParseAddr(net.JoinHostPort(host, strconv.Itoa(int(port))))
	if err != nil {
		slog.Warn("SOCKS5 target parse failed", "remote", remoteAddr, "err", err)
		_ = writeSocks5Reply(conn, repGeneralFailure)
		return
	}
	md := &core.Metadata{
		Network:     core.NetworkTCP,
		Source:      addrFromNet(remoteAddr),
		Destination: destAddr,
		InboundTag:  s.tag,
		User:        username,
	}

	// -----------------------------------------------------------------
	// 6. 调度：dispatcher 负责路由 + 建连 + relay
	// -----------------------------------------------------------------
	//
	// onDialed 在出站 dial 成功、relay 开始之前调用；此时我们发 SOCKS5
	// success reply。之后 dispatcher 才开始 Relay。
	//
	// dial 失败 → dispatcher 返回错误，此时 onDialed 未被调用，我们把错
	// 误翻译成 REP 发给客户端。
	// relay 阶段错误 → onDialed 已被调用，success reply 已发出，只记日志。
	sentSuccess := false
	err = s.dispatcher.DispatchStream(ctx, md, conn, func() error {
		if err := writeSocks5Reply(conn, repSucceeded); err != nil {
			return err
		}
		sentSuccess = true
		connectionSuccess = true // 防止 defer 里再次 close inConn，dispatcher 会关
		return nil
	})
	if err != nil {
		if !sentSuccess {
			slog.Debug("SOCKS5 dial failed", "target", md.Destination, "err", err)
			_ = writeSocks5Reply(conn, mapErrToRep(err))
		} else {
			slog.Debug("SOCKS5 relay ended", "target", md.Destination, "err", err)
		}
		return
	}
}

// addrFromNet 把 net.Addr 转成 core.Addr（best-effort，取 IP:port 形式）。
func addrFromNet(a net.Addr) core.Addr {
	tcp, ok := a.(*net.TCPAddr)
	if !ok {
		return core.Addr{}
	}
	ip, ok := netip.AddrFromSlice(tcp.IP)
	if !ok {
		return core.Addr{}
	}
	return core.AddrFromIPPort(ip.Unmap(), uint16(tcp.Port))
}
```

（`writeSocks5Reply` 和 `authenticate` 保持不动；`socksRepSucceeded`/`socksRepGeneralFailure` 常量删除——用 errmap.go 里的 `repSucceeded`/`repGeneralFailure`。）

**新增 import：**

```go
import (
	...
	"net/netip"
	"github.com/CelestialLuminary36/Aether/core"
)
```

- [ ] **Step 4: 跑单元测试与集成测试**

Run: `go test ./inbound/socks/... -v`
Expected: `TestMapErrToRep` + `TestSOCKS_EndToEnd` 全部 PASS。

- [ ] **Step 5: 跑全项目测试**

Run: `go test ./...`
Expected: 全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add inbound/socks/
git commit -m "feat(socks): migrate to core.Inbound + Dispatcher"
```

---

## Task 11: `cmd/aether/main.go` 用新契约装配

**Rationale:** 主函数把新组件装起来。行为跟今天一样，看起来更清晰。

**Files:**
- Modify: `cmd/aether/main.go`

**Interfaces:**
- Consumes: `core.Dispatcher`, `direct.New`, `block.New`, `socks.New`, `dispatcher.New`

- [ ] **Step 1: 重写 main.go**

Replace `cmd/aether/main.go`:

```go
// Package main 是 Aether 代理的入口。
//
// Plan 1 版本：装配 SOCKS5 in + direct out，通过 StaticDispatcher 串起来。
// 无嗅探、无路由、无 DNS——那些将在后续 plan 加入。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/CelestialLuminary36/Aether/dispatcher"
	"github.com/CelestialLuminary36/Aether/inbound/socks"
	"github.com/CelestialLuminary36/Aether/outbound/block"
	"github.com/CelestialLuminary36/Aether/outbound/direct"
)

const (
	listenAddr = "127.0.0.1:1080"
	adminUser  = "admin"
	adminPass  = "123456"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 出站：direct + block
	out := direct.New("out-direct")
	blk := block.New("out-block")

	// 调度器：唯一出站是 direct
	disp := dispatcher.New(out, blk)

	// 入站：SOCKS5 带密码鉴权
	in := socks.New(
		"in-socks",
		listenAddr,
		socks.AuthPassword,
		map[string]string{adminUser: adminPass},
		disp,
	)

	slog.Info("Aether starting", "version", "1.27.1")

	if err := in.Start(ctx); err != nil && !errors.Is(err, net.ErrClosed) {
		slog.Error("Aether exited unexpectedly", "err", err)
		os.Exit(1)
	}

	slog.Info("Aether listening", "addr", listenAddr, "auth", "password")

	<-ctx.Done()
	slog.Info("Aether shutting down")
	_ = in.Close()
	slog.Info("Aether shut down gracefully")
}
```

- [ ] **Step 2: 验证编译**

Run: `go build ./...`
Expected: 成功。

- [ ] **Step 3: 手工冒烟测试**

Run:
```bash
go build -o /tmp/aether ./cmd/aether
/tmp/aether &
AETHER_PID=$!
sleep 0.5
curl -sSf --socks5 admin:123456@127.0.0.1:1080 https://example.com/ | head -c 200
kill $AETHER_PID
```
Expected: 打印 example.com 的 HTML 开头。

- [ ] **Step 4: 提交**

```bash
git add cmd/aether/main.go
git commit -m "feat(cmd): wire new pipeline in main"
```

---

## Task 12: 清理与 README 更新

**Rationale:** 收尾：把 `common/bufpool` 的说明注释加一句"当前仅在加密出站介入后生效"（避免未来读者误解），README 更新一下项目结构块（旧的 `inbound/interface.go` 已删）。

**Files:**
- Modify: `common/bufpool/bufpool.go`（注释一句）
- Modify: `README.md`

- [ ] **Step 1: 更新 bufpool 注释**

Edit `common/bufpool/bufpool.go`, add to package comment:

```go
// Package bufpool provides a generic, typed wrapper around sync.Pool for
// reusing fixed-size byte buffers, which helps reduce GC pressure during
// high-throughput relay operations.
//
// Note: for TCP↔TCP relays (Plan 1's only case), io.CopyBuffer will
// bypass this buffer entirely because *net.TCPConn implements
// io.ReaderFrom and takes the splice(2) fast path on Linux. This pool
// becomes effective once encrypted outbounds (Plan 6+) wrap the conn
// in something that does NOT implement io.ReaderFrom.
package bufpool
```

- [ ] **Step 2: 更新 README 项目结构**

Edit `README.md`. Replace the "Project Structure" block with:

```
.
├── cmd/aether           # Application entry point
├── common
│   ├── bufpool          # Reusable byte-buffer pool (for relay)
│   └── packet           # Head-reserved byte buffer (for future UDP protocols)
├── core                 # Contracts: Addr, Metadata, interfaces, semantic errors
├── dispatcher           # StaticDispatcher: current Plan 1 minimal impl
├── inbound
│   └── socks            # SOCKS5 inbound (implements core.Inbound)
└── outbound
    ├── block            # Reject all dials with core.ErrBlockedByRule
    └── direct           # Direct TCP dialer (implements core.Outbound)
```

- [ ] **Step 3: 提交**

```bash
git add common/bufpool/bufpool.go README.md
git commit -m "docs: update README and bufpool doc for new structure"
```

---

## 收尾

- [ ] **最终检查：跑全项目测试与构建**

```bash
go build ./...
go test ./...
```
Expected: 全绿。

- [ ] **对着 Plan 1 目标复核一遍**：
  - [x] `core/` 是零第三方依赖的叶子包
  - [x] `core.Metadata` 替代旧 `Session`，不再嵌 `context.Context`
  - [x] `core.PacketConn` 与 `net.Conn` 是对等抽象（PacketConn 接口已定义，实现留 Plan 4）
  - [x] `core.Dispatcher` 是唯一调度中心，inbound 不再持有 Handler
  - [x] `outbound/direct` 与 `outbound/block` 都实现新接口
  - [x] `core.Relay` 响应 ctx 取消，返回字节数与错误
  - [x] SOCKS5 dial 失败会翻译成正确的 REP 码
  - [x] 端到端集成测试通过；SOCKS5 → direct → 后端能读到数据

Plan 1 完成的标志：`curl --socks5 ... https://example.com` 能正常返回内容，且现在这次是走的新契约。行为等价于 Plan 1 前，但地基已换。
