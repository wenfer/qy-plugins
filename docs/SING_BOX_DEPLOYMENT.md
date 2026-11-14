# sing-box 部署设计

面板支持在部署时选择 Xray-core 或 sing-box。旧请求未传 `core` 时仍按 Xray-core 的 VLESS Reality 模式处理；新部署界面默认选择 sing-box，重新安装保留现有核心与协议开关，并可独立启用以下协议实例：

| 实例 | 传输 | 默认用途 | 服务端端口 |
| --- | --- | --- | --- |
| VLESS + REALITY + Vision | TCP | 稳定的通用代理连接 | 独立 TCP 端口 |
| Hysteria2 | QUIC / UDP | 高丢包、高延迟链路 | 独立 UDP 端口 |

双协议由同一个 sing-box 进程管理，但各自使用独立端口和凭据。节点名称分别追加 `[VLESS-REALITY]` 与 `[HYSTERIA2]`，数据库保存两条协议节点，二维码界面可切换查看，内置订阅会同时聚合两个分享链接。

## Reality 目标域名实测

部署界面的“节点实测推荐”由目标 VPS 自己并发访问候选域名，只保留满足以下条件的目标：

- 强制 TLS 1.3 握手成功；
- ALPN 协商为 HTTP/2 或 HTTP/3；
- 公共证书链校验成功；
- 在节点线路上的 TCP 建连延迟可接受。

若候选解析地址与节点公网地址属于相同 IPv4 `/24`、`/16` 或 IPv6 `/48`、`/32` 前缀，会优先排序；否则按节点实测建连延迟排序。地址前缀相近不等同于同 ASN，因此界面不会将普通低延迟候选标成“同 ASN 邻居”。Apple 域名仅保留为手动备用，不参与随机选择，默认回退目标为 `www.yahoo.com`。

同一套实测控件也用于已部署节点的“重新安装”表单。Xray-core 与 sing-box 的 Reality 实例都可以重新实测、选择候选并应用 SNI。

## 从 Xray-core 切换到 sing-box

已部署的 Xray Reality 节点可从管理抽屉选择“重新安装”，再选择 sing-box 和所需协议。表单会回填现有 Reality 端口、SNI 和连接域名；切换时保留 UUID、x25519 密钥对和 ShortID，为新增的 Hysteria2 实例生成独立密码和 UDP 端口。保持原连接参数时，已有 VLESS 客户端链接继续适用。

重新安装先校验所选协议、端口、NAT 范围、连接域名和现有凭据，再停止并卸载旧 Xray 与 sing-box，清理服务单元、二进制、配置、旧证书、日志和旧端口的 TCP/UDP 防火墙规则，然后全新安装所选核心。期间代理中断，qy-agent 保留。清理失败会中止安装并报错；新安装失败则清理半成品和新端口规则，需重新安装，不恢复已卸载的旧核心。

sing-box 部署后仍可通过“重新安装”同时管理 Reality TCP、Hysteria2 UDP、SNI 和协议开关。修改会保留仍然启用的协议凭据；关闭某协议会从订阅中移除对应节点，之后重新启用时会生成新凭据。

## 节点连接域名与 DNS

“节点连接域名”可填写由任意 DNS 服务商管理的完整域名，无需 Cloudflare 账号或凭证；留空使用 VPS 地址。连接域名用于客户端分享链接，与 Reality 目标伪装域名（SNI）独立。TCP 与 UDP 两条分享链接使用同一个连接域名及各自的端口。

新部署默认不启用 Cloudflare 自动配置。使用自定义域名时，请自行将 A / AAAA 记录解析到 VPS 对应的公网 IP，使用仅 DNS 解析模式。面板不会同步或清理用户自行管理的解析记录；填写域名也不会自动申请公共 CA 证书，Hysteria2 仍使用下述自签名证书方案。

已配置 Cloudflare 时，可勾选“通过 Cloudflare 自动配置并维护 DNS”，由面板同步 A / AAAA 记录。后台 DDNS 只维护带 Cloudflare 记录 ID 的节点。重新安装时关闭该选项，会解除面板的 DNS 管理关系并保留已有远端 DNS 记录；清空连接域名则改回 VPS 地址。

## 远端布局

- 核心：`/usr/local/bin/sing-box`
- 配置：`/usr/local/etc/sing-box/config.json`
- Hysteria2 证书：`/usr/local/etc/sing-box/tls.crt` 与 `tls.key`
- 服务：`sing-box.service`，并兼容 OpenRC 和无服务管理器环境
- 日志：systemd journal，回退到 `/var/log/sing-box.log`

部署脚本会校验 CPU 架构、NAT 端口范围和配置语法，分别放行 TCP/UDP 防火墙规则。安装前统一清理两种核心，避免旧服务、证书、配置和未使用协议的端口放行规则残留。未完成远端卸载时保留本地节点记录，不显示卸载成功。

## TCP / UDP 检测与运行状态

端口检查支持 `tcp:443, udp:8443` 混合输入，裸端口按 TCP 处理。默认读取 VPS 的全部协议实例，按协议分别检查本地监听；相同数字的 TCP 和 UDP 端口分别保留结果。

TCP 使用握手测试；UDP 使用 QUIC 版本协商请求，只有收到匹配连接 ID 的版本协商响应才标记可达。无应答显示“UDP 未确认”，不能据此断言端口关闭。本地 UDP 监听与公网 QUIC 响应分别显示，该检查不替代 Hysteria2 认证及代理出网测试。

Agent 分别上报 Xray 与 sing-box 状态；WebSocket 携带完整节点列表。旧 Agent 未上报 sing-box 状态时不使用 Xray 状态覆盖它，需升级 Agent 才能实时感知 sing-box 的外部状态变化。sing-box 的启动/停止操作作用于同一核心内的全部协议节点；查看配置展示远端 JSON 和每条协议链接。

## Hysteria2 TLS 边界

当前版本自动生成有效期十年的自签名 EC 证书，客户端链接包含 `insecure=1`。链路仍然加密，但客户端不通过公共 CA 验证服务器身份。界面会明确提示该取舍。

数据模型已预留 `tls_mode`，后续可以增加 ACME 模式而无需迁移节点结构。ACME 需要域名已解析到 VPS，并处理 HTTP-01、TLS-ALPN-01 或 DNS-01 的验证条件。

## 客户端兼容性

- sing-box 客户端与 Mihomo/Clash Meta 可使用 Hysteria2。
- 支持 Reality 的客户端可使用 VLESS Reality 节点。
- 不支持 Hysteria2 的传统 Clash 客户端会跳过或拒绝对应节点，应使用 Reality 节点或升级内核。

## API 兼容

`POST /api/v1/servers/:id/deploy` 新增 `core`、`reality_port`、`hysteria2_port`、`enable_reality` 和 `enable_hysteria2`。响应同时返回 `nodes`/`share_links`，并保留旧版 `node`/`share_link` 字段，主节点优先选择 VLESS Reality。

`subdomain` 是可选的节点连接域名，`auto_bind_cf=false` 时不依赖 Cloudflare。省略 `subdomain` 保留已有连接域名；显式传空字符串恢复 VPS 地址。`auto_bind_cf=true` 要求有效的连接域名和 Cloudflare 凭证，部署前校验。
