# QY-DASH 使用与运维指南 (User & Admin Guide)

> **当前版本**：v2.2.24  
> **适用平台**：Linux (x86_64 / ARM64) · Debian / Ubuntu / CentOS / Alpine Linux  
> **相关链接**：[项目仓库 README](../README.md) · [版本发布规范](VERSIONING.md)

---

## 目录 (Table of Contents)

- [1. 系统架构与核心概念](#1-系统架构与核心概念)
- [2. 服务端部署指南](#2-服务端部署指南)
  - [2.1 Docker Compose 部署 (推荐)](#21-docker-compose-部署-推荐)
  - [2.2 Docker 单命令运行](#22-docker-单命令运行)
  - [2.3 裸机二进制部署](#23-裸机二进制部署)
  - [2.4 关键环境变量与安全主密钥](#24-关键环境变量与安全主密钥)
- [3. 被控节点与探针接入 (Agent)](#3-被控节点与探针接入-agent)
  - [3.1 一键极速接入](#31-一键极速接入)
  - [3.2 Alpine Linux (OpenRC) 接入](#32-alpine-linux-openrc-接入)
  - [3.3 探针采集指标与性能保障](#33-探针采集指标与性能保障)
  - [3.4 Agent 常用维护命令与卸载](#34-agent-常用维护命令与卸载)
- [4. 节点部署与双代理内核编排](#4-节点部署与双代理内核编排)
  - [4.1 Xray-core (VLESS-Reality)](#41-xray-core-vless-reality)
  - [4.2 Sing-box (Hysteria2 / TUIC / 双协议并存)](#42-sing-box-hysteria2--tuic--双协议并存)
  - [4.3 多网络栈适配 (IPv4 / IPv6 / NAT / NAT64)](#43-多网络栈适配-ipv4--ipv6--nat--nat64)
  - [4.4 Cloudflare 域名与 DNS 联动](#44-cloudflare-域名与-dns-联动)
- [5. 诊断工具矩阵](#5-诊断工具矩阵)
  - [5.1 IP 纯净度与流媒体/AI 解锁画像](#51-ip-纯净度与流媒体ai-解锁画像)
  - [5.2 回国网络路由与 GFW 阻断检测](#52-回国网络路由与-gfw-阻断检测)
  - [5.3 端口连通性检查 (TCPing) 与本地交叉排障](#53-端口连通性检查-tcping-与本地交叉排障)
  - [5.4 一键免客户端 Web SSH 终端](#54-一键免客户端-web-ssh-终端)
  - [5.5 智能磁盘垃圾清理 (Disk Cleaner)](#55-智能磁盘垃圾清理-disk-cleaner)
- [6. 订阅转换与节点管理](#6-订阅转换与节点管理)
  - [6.1 聚合流程与架构设计](#61-聚合流程与架构设计)
  - [6.2 订阅源接入 (Sources) 与容灾降级](#62-订阅源接入-sources-与容灾降级)
  - [6.3 统一节点列表与批量测速诊断](#63-统一节点列表与批量测速诊断)
  - [6.4 节点清洗规则 (Conversion Rules)](#64-节点清洗规则-conversion-rules)
  - [6.5 策略组与分流方案 (Profiles)](#65-策略组与分流方案-profiles)
  - [6.6 链式代理与节点中继 (Dialer-Proxy) 说明](#66-链式代理与节点中继-dialer-proxy-说明)
  - [6.7 客户端多格式分发与 User-Agent 自适应](#67-客户端多格式分发与-user-agent-自适应)
- [7. 运维安全与故障排查 FAQ](#7-运维安全与故障排查-faq)

---

## 1. 系统架构与核心概念

QY-DASH 采用现代化 **C/S (Master / Agent) 拓扑**，彻底解耦中枢调度与底层服务器采集：

```
                           ┌────────────────────────┐
                           │      Web 浏览器 SPA     │
                           │   React 19 + Tailwind  │
                           └───────────┬────────────┘
                                       │ HTTP / WebSocket
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                       服务端控制中枢 (qy-core)                              │
│                                                                             │
│  ┌───────────────────────┐  ┌───────────────────────┐  ┌─────────────────┐ │
│  │     REST API 路由     │  │   指标广播 / WebSocket │  │ 订阅转换聚合管线 │ │
│  └───────────┬───────────┘  └───────────┬───────────┘  └────────┬────────┘ │
│              │                          │                       │          │
│  ┌───────────▼───────────┐  ┌───────────▼───────────┐           │          │
│  │ SQLite 驱动 (无 CGO)  │  │  AES-256 主密钥保险箱 │           │          │
│  └───────────────────────┘  └───────────────────────┘           │          │
└──────────────────────────────────────▲──────────────────────────┼───────────┘
                                       │ 长连接安全上报           │
                 ┌─────────────────────┴─────────────────────┐    │ 客户端拉取
                 │                                           │    │ (/sub/:token)
                 ▼                                           ▼    ▼
     ┌───────────────────────┐                   ┌───────────────────────┐
     │  被控 VPS 节点 A      │                   │   客户端软件订阅接入  │
     │  qy-agent 探针        │                   │   Clash / Mihomo      │
     │  Xray / Sing-box 核心 │                   │   Sing-box / V2Ray    │
     └───────────────────────┘                   └───────────────────────┘
```

- **`qy-core`（服务端）**：单二进制程序，内嵌编译好的 React 19 SPA 前端，内置纯 Go SQLite 存储与 AES-256-GCM 凭据加密引擎。
- **`qy-agent`（被控端）**：单二进制极轻量探针（常驻内存 < 10MB），主动出站向服务端建立安全双向隧道，无需服务端持有 VPS root 密码，适应复杂 NAT 与内网环境。
- **`qy-tui`（终端客户端）**：面向键盘流运维设计的 Bubble Tea TUI 终端界面，直接连接 `qy-core` 即可管理全网节点资产。

---

## 2. 服务端部署指南

### 2.1 Docker Compose 部署 (推荐)

在准备作为控制中枢的 VPS 或本地服务器上创建 `docker-compose.yml`：

```yaml
services:
  qy-dash:
    image: ghcr.io/<你的GitHub用户名>/qy-dash:latest
    container_name: qy-dash
    restart: unless-stopped
    ports:
      - "2026:2026"
    environment:
      # 管理员账号与密码 (首次部署必须修改强密码)
      - WEB_USER=admin
      - WEB_PASS=MyStrongPassword_2026
      # 数据库文件持久化位置 (自动兼容老版本 /data/qy-xray.db)
      - DB_PATH=/data/qy-dash.db
      # 强烈建议显式设定固定密钥 (至少 16 位字符串)
      - SECRET=Your_Super_Secret_Key_32_Characters!
    volumes:
      - ./data:/data
```

启动并验证服务：

```bash
docker compose up -d
docker compose logs -f
```

打开浏览器访问 `http://<服务器IP>:2026`，输入设定的管理员账密即可进入控制台。

### 2.2 Docker 单命令运行

```bash
mkdir -p /opt/qy-dash/data

docker run -d \
  --name qy-dash \
  --restart unless-stopped \
  -p 2026:2026 \
  -e WEB_USER=admin \
  -e WEB_PASS=MyStrongPassword_2026 \
  -e SECRET=Your_Super_Secret_Key_32_Characters! \
  -v /opt/qy-dash/data:/data \
  ghcr.io/<你的GitHub用户名>/qy-dash:latest
```

### 2.3 裸机二进制部署

下载对应架构的 `qy-dash-core` 二进制文件后，配置 Systemd 守护：

创建 `/etc/systemd/system/qy-dash-core.service`：

```ini
[Unit]
Description=QY-DASH Server Core
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/var/lib/qy-dash
Environment="WEB_USER=admin"
Environment="WEB_PASS=MyStrongPassword_2026"
Environment="SECRET=Your_Super_Secret_Key_32_Characters!"
Environment="DB_PATH=/var/lib/qy-dash/qy-dash.db"
Environment="LISTEN=:2026"
ExecStart=/usr/local/bin/qy-dash-core
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
mkdir -p /var/lib/qy-dash
systemctl daemon-reload
systemctl enable --now qy-dash-core
systemctl status qy-dash-core
```

### 2.4 关键环境变量与安全主密钥

| 环境变量 | 默认值 | 作用说明 |
|:---|:---|:---|
| `WEB_USER` | `admin` | 管理后台登录用户名 |
| `WEB_PASS` | ⚠️ `admin123` | 管理后台密码。**若使用默认弱口令，系统会在终端输出醒目警告** |
| `LISTEN` | `:2026` | HTTP 与 WebSocket 监听地址端口 |
| `DB_PATH` | `~/.qy-core/qy-core.db` | SQLite 数据库持久化存储绝对路径 |
| `SECRET` | 自动生成 | AES-256 加密主密钥。用于加密存储节点证书、上游订阅 URL 与 Cloudflare 令牌。若未显式指定，系统会自动生成并存入数据库同级目录下的 `master.key`。**迁移数据库时必须同时迁移 `master.key`，否则已有加密数据将永远无法解密！** |
| `TRUSTED_PROXIES` | 空 | 可信反向代理 IP 网段（如 `172.17.0.0/16`）。仅在有反向代理且需还原客户端真实 IP 时配置 |

---

## 3. 被控节点与探针接入 (Agent)

### 3.1 一键极速接入

在 Web 控制台的 **服务器管理** 页面点击右上角 **「添加服务器」**，系统会生成携带专属凭据的自动化部署命令。在被控 VPS 执行：

```bash
curl -fsSL http://<服务端地址>:2026/install.sh | sh -s -- \
  --server http://<服务端地址>:2026 \
  --secret <你的注册密钥> \
  --name "US-硅谷-01"
```

脚本将自动执行以下流程：
1. 识别当前 VPS 架构（`x86_64` 或 `aarch64`）并自适应拉取对应的 `qy-agent`。
2. 安装到 `/usr/local/bin/qy-agent`。
3. 注册 Systemd 开机自启守护服务并立即启动。
4. 探针建立双向长连接，秒级在 Web 控制台卡片中点亮上线。

### 3.2 Alpine Linux (OpenRC) 接入

对于轻量或容器化的 Alpine Linux 系统，直接使用 `wget` 即可运行：

```bash
wget -qO- http://<服务端地址>:2026/install.sh | sh -s -- \
  --server http://<服务端地址>:2026 \
  --secret <你的注册密钥> \
  --name "Alpine-NAT-01"
```

脚本会自动检测到 OpenRC，写入 `/etc/init.d/qy-agent` 并启用 `supervise-daemon` 实现进程防崩溃自动重启。

### 3.3 探针采集指标与性能保障

Agent 采取毫秒级直读 Linux 内核 `/proc` 接口，无多余子进程开销：
- **动态资源**：CPU 实时使用率、内存占用（已用/总量）、Swap 占用、磁盘使用率、1/5/15 分钟系统负载。
- **网络流速**：实时上行与下行带宽速率（B/s, KB/s, MB/s 自动换算）、出站与入站总累计流量。
- **连接追踪**：实时活跃 TCP 连接数统计。
- **硬件元数据**：CPU 型号与核心数、内核版本、Linux 发行版、系统持续开机时长（Uptime）。

### 3.4 Agent 常用维护命令与卸载

```bash
# 查看探针运行状态
systemctl status qy-agent          # Systemd (Debian / Ubuntu / CentOS)
rc-service qy-agent status         # OpenRC (Alpine Linux)

# 查看实时采集上报日志
journalctl -u qy-agent -f          # Systemd
tail -f /var/log/qy-agent.log       # OpenRC

# 一键卸载 Agent（自动清理服务及二进制）
curl -fsSL http://<服务端地址>:2026/install.sh | sh -s -- --uninstall
```

---

## 4. 节点部署与双代理内核编排

QY-DASH 支持通过 Agent 隧道直接在被控机编排并托管代理核心，支持 **Xray-core** 与 **Sing-box** 双内核并行！

### 4.1 Xray-core (VLESS-Reality)
- **协议组合**：`VLESS + XTLS-Reality + Vision`。
- **零证书门槛**：自动随机生成符合标准的 UUID、公私钥对（X25519）与 ShortID，无需提前申请配置 TLS 证书。
- **免流与伪装 SNI**：内置常用大厂 SNI 白名单与随机回落探测，提供高抗封锁性能。

### 4.2 Sing-box (Hysteria2 / TUIC / 双协议并存)
- **协议覆盖**：全生命周期支持 **Hysteria2** (基于 UDP 的极速拥塞控制协议) 与 **TUIC**。
- **双协议共存**：同一台 VPS 上可同时运行 Xray (TCP VLESS-Reality) 与 Sing-box (UDP Hysteria2)，端口自动规划互不冲突。
- **自适应节点后缀**：系统会自动在节点名称末尾追加 `[VLESS-REALITY]`、`[HYSTERIA2]` 标识，便于客户端区分。

### 4.3 多网络栈适配 (IPv4 / IPv6 / NAT / NAT64)
在部署节点时可根据 VPS 的网络条件自由选择网络栈：
1. **仅 IPv4**：最广泛兼容的传统 VPS 配置。
2. **IPv4 & IPv6 双栈**：同时生成 IPv4 与 IPv6 入口，自动测量双栈连通性。
3. **IPv4 NAT (端口段)**：针对各类共享 IP 的 NAT VPS，可自定义可用的端口范围（如 `10000-10100`），核心仅在该范围内侦听。
4. **仅 IPv6 (NAT64)**：针对纯 IPv6 的极便宜 VPS，部署时自动注入公共 NAT64 路由与 DNS64 地址，解除对 GitHub、Docker Hub 及 IPv4 外部源的依赖。

### 4.4 Cloudflare 域名与 DNS 联动
进入控制台右上角 **「Cloudflare 配置」**，填入你的 Cloudflare API Token 与主托管域名（如 `example.com`）：
- **自动添加 A / AAAA 记录**：在部署节点时勾选「自动配置 DNS」，系统将根据节点公网 IP 自动在 Cloudflare 添加对应子域名解析。
- **生命周期同步删除**：删除节点或彻底卸载核心时，可勾选同步删除 Cloudflare 解析，避免历史废弃记录残留。

---

## 5. 诊断工具矩阵

### 5.1 IP 纯净度与流媒体/AI 解锁画像
在服务器列表点击节点的 **「IP 纯净度」** 按钮，系统将执行综合诊断：
- **Google "送中" 检测**：深度探测请求是否被 Google 重定向至香港（HK）或中国大陆（CN）IP 数据库，排查 YouTube Premium 无法购买及地区受限。
- **IP 属性分析**：公网出口 IP、归属国家/地区、运营商 ISP、机房 ASN、数据中心（Datacenter）或原生家宽（Residential）属性判定。
- **流媒体解锁测试**：实时探测 Netflix（全部片库/仅自制剧/封锁）、Disney+、YouTube Premium 解锁。
- **AI 智能服务权限**：OpenAI / ChatGPT Web 端与官方 API 入口连通性检测。

### 5.2 回国网络路由与 GFW 阻断检测
- **三网回国链路追踪**：从被控 VPS 主动向中国电信（163/CN2 GIA）、中国联通（4837/9929）、中国移动（CMI）骨干网测试节点发起链路追踪，诊断是否存在绕路与高丢包。
- **GFW 阻断侦测**：分别从大陆境外对节点的 IPv4 与 IPv6 代理端口发起双向握手，直接在控制台标识当前节点在大陆境内的可达性（正常 / IPv4 被封 / IPv6 被封 / 全阻断）。

### 5.3 端口连通性检查 (TCPing) 与本地交叉排障
- **公网 TCP 握手时延**：支持单端口、多端口（`22, 443, 8443`）或区间段（`8080-8085`）并发 TCPing，测量往返时延（RTT）及端口开放状态（Open / Closed / Filtered）。
- **本地 LISTEN 交叉比对**：若端口在公网表现为 Filtered，系统自动联动被控机检查本地是否正在侦听该端口。若是本地正在侦听但外部超时，系统会精确提示“服务已启动，阻断源于云服务商安全组或系统防火墙未放行”。

### 5.4 一键免客户端 Web SSH 终端
在任意在线服务器卡片点击 **「Web 终端」**，即可在浏览器内唤起基于 xterm.js 与 WebSocket PTY 的全功能交互式 Linux 控制台，支持 `Ctrl+C`、Top、Vim 等完整终端快捷键与富文本渲染。

### 5.5 智能磁盘垃圾清理 (Disk Cleaner)
点击服务器操作栏的 **「清理」**，一键调用安全清理脚本：
- 清理 Docker 悬空镜像、无用网络与容器日志 (`/var/lib/docker/containers/*/*.log`)。
- 清理 Systemd Journal 超过 3 天或体积过大的旧日志 (`journalctl --vacuum-time=3d`)。
- 清理 APT / YUM / Pacman / APK 的软件包下载缓存，释放关键磁盘空间。

---

## 6. 订阅转换与节点管理

QY-DASH 内置原生声明式订阅转换与分流编排引擎。

### 6.1 聚合流程与架构设计
转换引擎的工作管线如下：
```
[外部机场源 1] ──┐
[外部机场源 2] ──┼─► [解码与反混淆] ──► [合并自建节点] ──► [过滤与去重] ──► [正则重命名] ──► [排序截断] ──► [策略组与分流] ──► [格式自适应分发]
[自建 VPS 节点] ─┘
```

### 6.2 订阅源接入 (Sources) 与容灾降级
在 **「订阅转换与节点管理」➔「订阅源管理」** 添加上游订阅：
- **格式兼容**：自动兼容 v2ray (Base64)、Clash (YAML)、Sing-box (JSON)、Surge 等主流协议源。
- **安全与 SSRF 防护**：对上游链接执行严格的私有网段拦截与重定向白名单校验；敏感链接入库采用 AES-256 加密保存。
- **崩溃降级保护**：若某个外部机场源临时宕机或出现网络抖动，系统会自动降级复用最后一次成功的本地节点缓存，绝不导致客户端拉取失败。

### 6.3 统一节点列表与批量测速诊断
在 **「节点管理」** 视图中：
- 全局集中查看自建节点与机场订阅池中的所有节点。
- 支持一键发起**全节点批量存活与延迟测速**，丢包率与 RTT 毫秒级展示。
- 节点卡片清晰展示 GFW 阻断状态徽章与送中识别标记。

### 6.4 节点清洗规则 (Conversion Rules)
进入 **「节点转换规则」**，通过全界面化声明规则，完成海量节点的自动化清洗：
- **过滤关键字**：正则匹配保留包含指定关键字的节点（如 `HK|香港|TW|台湾`）。
- **排除关键字与引流清理**：内置过期、官网、群组等垃圾引流关键词过滤器，自动剔除占位节点。
- **Emoji 过滤**：转换管线默认自动剥离节点名称中的国旗与表情符号，无需手动配置，确保客户端节点列表整洁统一。
- **协议白名单**：指定仅保留 `VLESS`、`Hysteria2`、`Trojan` 等特定协议。
- **智能去重**：支持按 `Host + Port` 或按完整连接参数进行去重。
- **模板化重命名**：提供直观的占位符插入（`{region}`、`{index:02d}`、`{name}`、`{protocol}`），例如：
  `{region} {index:02d} | 专线` ➔ `香港 01 | 专线`。
- **包含自建节点**：勾选后，系统管理的所有在线自建 VPS 节点将自动并入转换管线。

### 6.5 策略组与分流方案 (Profiles)
进入 **「分流与策略组配置」**，系统已采用全新的**弹窗编辑模式**：
- **策略组卡片化列表**：高信息密度展示策略名称、模式标签、命中自建节点数、地区标签与延迟探针。
- **弹窗编辑 (`SubProfileGroupModal`)**：
  - **模板快捷载入**：内置「节点选择 (主策略)」、「自建节点专属组」、「常用地区自动测速」、「OpenAI 专属分流」、「流媒体解锁专线」等模板。
  - **模式支持**：
    - `select`：手动选择节点或策略。
    - `url-test`：自动测速探针选择最低延迟。
    - `fallback`：故障转移模式，按顺序优先选择可用节点。
    - `load-balance`：负载均衡模式。
    > [!NOTE]
    > 新版 Clash / Mihomo 内核已彻底移除 `relay` 策略组类型（在配置中包含 `type: relay` 会导致内核启动报 `relay type was removed, please using dialer-proxy instead`）。系统已全面移除 `relay` 策略组选项，历史遗留的旧版 relay 策略组将在编辑弹窗中展示废弃警告并提供一键转换为 `select`。
  - **快捷地区选择**：一键点选常用地区（港、日、美、新、台、韩、欧），自动过滤对应地域节点。
  - **探针库选择**：内置 URL-TEST 探针库（ChatGPT CDN trace、YouTube 204、Google 204、Cloudflare CDN），可一键选取。
- **分流规则表 (Routing Rules)**：直观配置分流条目（如 `DOMAIN-SUFFIX,openai.com,OpenAI`、`GEOIP,CN,DIRECT`、`MATCH,节点选择`）。
- **远端规则集 (Rule Providers)**：支持引用外部行为规则集（Clash Rule Providers / Sing-box Rule Sets）。

### 6.6 链式代理与节点中继 (Dialer-Proxy)
在网络受限环境或需要跨国中转时，常常需要前置中转与海外落地串联建立链路：
- **关于 Relay 策略组的废弃**：
  在早期的旧版 Clash 中，多跳代理曾使用 `type: relay` 策略组实现。但**现代 Clash / Mihomo 内核已彻底移除 relay 策略组**（若配置文件中出现 `type: relay`，新版内核将直接抛错拒绝启动：`relay type was removed, please using dialer-proxy instead`）。
- **现代内核标准方案：`dialer-proxy` (Clash) 与 `detour` (Sing-box)**：
  现代 Clash / Mihomo 采用更底层、更健壮的代理链字段 `dialer-proxy: "前置代理名"`，Sing-box 对应 `"detour": "前置代理名"`。
  - **策略组前置中继**：在策略组弹窗编辑中，可直接在「前置代理 (dialer-proxy)」中指定前置节点或前置组（如国内 BGP 中转 VPS 或前置中转组），该策略组筛选出来的所有落地机将自动生成 `前置 ➔ 落地` 拓扑并注入 `dialer-proxy`。
  - **清洗规则全局附加**：在「节点清洗规则 (Conversion Rules)」高级设置中，支持配置「附加前置代理 (dialer-proxy)」，批量为匹配规则的海外落地节点挂载指定中转跳板。
  - **跨格式与订阅双向解析**：系统原生兼容并双向解析上游 Clash YAML 中的 `dialer-proxy` 字段、Sing-box JSON 中的 `detour` 字段以及 URI 分享链接中的 `dialer_proxy` 参数；在渲染输出时自适应输出 Clash YAML 的 `dialer-proxy` 与 Sing-box 的 `detour`。

### 6.7 客户端多格式分发与 User-Agent 自适应
在 **「输出订阅」** 模块生成客户端订阅地址：
- **公网分发格式**：
  - `http://<域名>:2026/sub/<Token>`：**智能自适应入口**，根据客户端的 HTTP `User-Agent` 自动协商返回对应格式（Clash、Clash.Meta、Sing-box 或 Base64）。
  - `http://<域名>:2026/sub/<Token>/clash`：强制输出 Clash / Mihomo YAML。
  - `http://<域名>:2026/sub/<Token>/singbox`：强制输出 Sing-box JSON。
- **安全与防刷**：每个输出订阅均拥有独立的 256 位熵随机 Token，支持一键轮换（轮换后旧链接立即失效），并在服务端配置有访问频次限制。

---

## 7. 告警通知配置 (Telegram & Webhook)

进入控制台顶部导航栏 **「告警通知」**，可集中管理通知通道与触发策略：

### 7.1 Telegram 机器人
- **配置参数**：
  - **Bot Token**：向官方 [@BotFather](https://t.me/BotFather) 发送 `/newbot` 获取。
  - **Chat ID**：个人接收可通过 [@userinfobot](https://t.me/userinfobot) 获取；群组接收需先将机器人加入群聊，群组 ID 通常以 `-100` 开头。
  - **自定义 API 地址 (可选)**：针对国内或受限网络，可指定反代端点（如 `https://tg-proxy.example.com`）。
- **即时测试**：在编辑界面点击「发送 Telegram 测试通知」，即时验证联通性。

### 7.2 通用 Webhook
- **端点 URL**：接收 POST JSON 的目标地址。
- **签名 Secret (可选)**：随 `X-Webhook-Secret` 请求头及请求体 `secret` 字段携带。
- **格式兼容**：发送标准 JSON Payload，内置 `content`（兼容 Discord）与 `text`（兼容 Slack / 飞书 / 企业微信群机器人），免中间件直连。

### 7.3 事件订阅与防抖机制
- **服务器离线失联报警**：当主机连续 15 秒未上报心跳时触发告警，内置 60 秒冷却防抖，避免网络抖动轰炸。
- **服务器恢复上线提醒**：当离线主机重新接入并上报指标时发出恢复通知。
- **VPS 账单到期续费提醒**：后台每日自动巡检，在主机到期前 7 天、3 天、1 天以及当天推送预警。

---

## 8. 运维安全与故障排查 FAQ

### Q1: 忘记了 Web 控制台管理员密码怎么办？
可以通过修改服务端的环境变量或启动参数直接重置密码：
- 若使用 Docker 部署：修改 `docker-compose.yml` 中的 `WEB_PASS` 字段，随后执行 `docker compose up -d` 重新创建容器即可立即生效。
- 若使用裸机部署：在 `/etc/systemd/system/qy-dash-core.service` 中修改 `WEB_PASS`，执行 `systemctl daemon-reload && systemctl restart qy-dash-core`。

### Q2: 提示「严重安全警告：控制台仍在使用默认密码 admin123」？
这是系统的防呆安全机制。若未显式更改 `WEB_PASS`，系统会持续在日志中发出警示。请务必修改为一个包含字母、数字和符号的强密码，防止被全网扫描猜解。

### Q3: 迁移服务器时如何保证历史数据和加密节点不丢失？
1. 复制 SQLite 数据库文件（如 `./data/qy-dash.db` 或兼容的老版本 `qy-xray.db`）。
2. **务必同时复制同目录下的 `master.key` 文件**！`master.key` 存储了对称加密的根私钥。如果丢失该文件且启动时未配置相同的 `SECRET` 环境变量，数据库内加密存储的 VPS 凭据和节点参数将永久无法解密。

### Q4: 客户端拉取订阅提示 404 或内容为空？
1. 确认「输出订阅」中绑定的「订阅源」已至少成功抓取到 1 个可用节点。
2. 确认转换规则中的过滤关键词未把所有节点全部误过滤。
3. 在 Web 控制台的输出订阅卡片点击 **「预览」** 按钮，系统会试算并打印真实的转换结果与匹配告警，可快速定位被哪个规则阻断。

### Q5: 节点的 GFW 检测提示「阻断」？
1. 检查云服务商（如腾讯云、阿里云、AWS、甲骨文云等）后台的 **安全组 (Security Group)** 入方向规则，确保对应的代理端口已放行。
2. 使用控制台内置的 **「端口检查」** 工具检测本地端口是否处于 `LISTEN` 状态。
3. 若端口被中国大陆防火墙完全阻断，可进入节点配置界面，一键修改监听端口，或切换为 Sing-box 的 Hysteria2 协议。
