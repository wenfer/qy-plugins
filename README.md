# QY-DASH 插件市场与开发 SDK

<p align="center">
  <a href="https://github.com/wenfer/qy-plugins/actions/workflows/ci.yml"><img src="https://github.com/wenfer/qy-plugins/actions/workflows/ci.yml/badge.svg" alt="CI Status"></a>
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/Schema-v1-blue" alt="Catalog Schema v1">
  <img src="https://img.shields.io/badge/License-MIT-slate" alt="License MIT">
</p>

本仓库为 [QY-DASH](https://github.com/wenfer/qy-dash) 官方插件市场索引、Go 扩展开发 SDK 与官方参考插件仓库：

1. **官方插件市场索引 (`index.json`)**：为 QY-DASH 控制台「插件市场」提供插件元数据、版本包下载地址与 SHA-256 校验和。
2. **官方 Go Plugin SDK (`pkg/pluginsdk`)**：基于 Unix Domain Socket (UDS) 的进程间通信开发包，无外部第三方依赖。
3. **官方参考插件 (`plugins/metrics-analyzer`)**：演示基于 EWMA 算法的节点流量平滑统计与突发带宽告警。
4. **插件开发模板 (`plugins/template`)**：包含后台服务与前端界面挂载点的最小开发模板。
5. **规范与文档 (`docs/`)**：包含清单契约规范、SDK 开发手册与插件发布提交流程。

---

## 目录结构

```
qy-plugins/
├── index.json                  # 官方插件市场索引 (Catalog Schema v1)
├── pkg/
│   └── pluginsdk/              # Go 插件开发 SDK
│       ├── ipc/                # 16 字节定长二进制帧编解码
│       ├── client.go           # 异步 RPC 通信与节点监控指标流消费
│       ├── serve.go            # 插件子进程生命周期托管与优雅关闭
│       ├── storage.go          # 插件独立持久化存储客户端 (SQLite)
│       └── types.go            # 接口定义 (Plugin, HostContext, Logger)
├── plugins/
│   ├── metrics-analyzer/       # 官方参考插件: 流量分析与突发告警
│   │   ├── analyzer/           # 工作池并发处理与数据平滑计算
│   │   ├── ui/index.js         # 前端 React 19 ESM 动态挂载组件
│   │   ├── main.go             # 插件后台子进程入口
│   │   └── plugin.json         # 插件清单规范
│   └── template/               # 最小插件开发模板
│       ├── ui/index.js
│       ├── main.go
│       └── plugin.json
├── docs/
│   ├── MANIFEST_SPEC.md        # plugin.json 结构规范与多架构打包标准
│   ├── SDK_GUIDE.md            # Go SDK 与前端原生 ESM 扩展开发指南
│   └── PUBLISHING.md           # 插件市场提交流程与规范
├── scripts/
│   ├── package-plugin.sh       # 跨平台编译 (Linux amd64/arm64, Darwin) 与打包校验
│   └── validate-index.sh       # 市场 index.json 语法与校验和自动化验证
└── .github/workflows/          # 自动化 CI 测试与 Release 发版流水线
```

---

## 插件市场目录

当前官方市场索引收录的插件：

| 插件 ID | 名称 | 版本 | 架构支持 | 描述 |
| :--- | :--- | :--- | :--- | :--- |
| `metrics-analyzer` | **流量分析与突发告警** | `v1.0.0` | Linux amd64<br>Linux arm64<br>macOS arm64 | 实时统计各节点进出站流量，自动平滑带宽波动，并在流量异常突增时推送告警通知 |
| `starter-template` | **开发者示例插件** | `v1.0.0` | 源码模板 | 包含后台服务与前端界面的最小插件示例，供开发者参考与二次开发 |

---

## 用户安装方式

### 方式 1: 官方插件市场在线安装 (推荐)

1. 登录 QY-DASH 控制台，进入「系统设置」 $\rightarrow$「插件中心」 $\rightarrow$「插件市场」。
2. 浏览已收录的插件，点击目标插件卡片中的 **「安装」** 按钮。
3. 主服务将依据主机 CPU 架构（如 `linux/amd64` 或 `linux/arm64`）自动下载 release 包、校验 SHA-256 完整性并解压至 `plugins/` 目录。
4. 安装完成后点击「启用」，主服务将启动插件子进程并挂载前端扩展组件。

### 方式 2: GitHub 仓库安装

在插件中心「手动安装」中输入 GitHub 仓库地址：
```text
https://github.com/wenfer/qy-plugins
```
主服务将自动拉取对应 release 包并完成安装。

### 方式 3: 离线 Zip 包上传

在 Release 页面下载对应系统架构的 `.zip` 文件（例如 `metrics-analyzer-linux-amd64.zip`），在插件中心点击 **「手动安装」** 上传即可。

---

## 插件开发快速开始

QY-DASH 插件系统采用进程隔离设计：
- **后端插件**：运行在独立的操作系统子进程中，通过 Unix Domain Socket (UDS) 与主服务通信；
- **前端扩展**：基于原生 ECMAScript 模块 (`import()`) 动态加载，无需重新编译主服务前端。

### 1. 引入 Go SDK

在插件项目中引用：

```bash
go get github.com/wenfer/qy-plugins/pkg/pluginsdk
```

### 2. 实现插件生命周期接口

创建 `main.go`：

```go
package main

import (
	"context"
	"log"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk"
)

type MyPlugin struct {
	logger pluginsdk.Logger
}

func (p *MyPlugin) Start(ctx pluginsdk.HostContext) error {
	p.logger = ctx.Logger()
	p.logger.Infof("MyPlugin started")

	// 获取在线节点列表
	servers, err := ctx.GetServerList(context.Background())
	if err == nil {
		p.logger.Infof("已发现 %d 个在线节点", len(servers))
	}
	return nil
}

func (p *MyPlugin) Stop() error {
	p.logger.Infof("MyPlugin stopping...")
	return nil
}

func main() {
	if err := pluginsdk.Serve(&MyPlugin{}); err != nil {
		log.Fatalf("Fatal: %v", err)
	}
}
```

### 3. 配置清单 `plugin.json`

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "id": "my-plugin",
  "name": "My Plugin",
  "version": "1.0.0",
  "author": "YourName",
  "description": "插件功能简述",
  "main": "bin/my-plugin",
  "frontend": {
    "esm": "ui/index.js",
    "extension_points": {
      "navigation_tabs": [
        {
          "id": "my-tab",
          "title": "自定义面板",
          "icon": "Box",
          "component": "MyTab"
        }
      ]
    }
  },
  "permissions": [
    "servers:read",
    "storage:isolated"
  ]
}
```

### 4. 编写前端组件 `ui/index.js`

```javascript
const sdk = window.__QY_PLUGIN_SDK__;
const { React, icons } = sdk;
const Box = icons.Box || icons.Activity;

export function MyTab() {
  return React.createElement(
    'div',
    { className: 'p-6 space-y-4 max-w-[1200px] mx-auto' },
    React.createElement('h2', { className: 'text-base font-semibold' }, '自定义扩展面板')
  );
}
```

### 5. 编译打包

运行内置脚本编译并生成发布包：

```bash
./scripts/package-plugin.sh plugins/template my-plugin 1.0.0
```

脚本将输出 `dist/my-plugin-linux-amd64.zip` 等多架构部署包，并打印用于 `index.json` 的 SHA-256 校验和与 release 元数据片段。

---

## 提交插件到官方市场

1. 按照 [docs/MANIFEST_SPEC.md](docs/MANIFEST_SPEC.md) 完善 `plugin.json` 清单。
2. 在插件仓库创建 GitHub Release 并上传各架构编译包（`.zip`）。
3. Fork 本仓库，在 `index.json` 中追加插件条目、下载地址与 SHA-256 哈希。
4. 运行 `./scripts/validate-index.sh` 进行格式验证。
5. 提交 Pull Request，CI 校验通过后即可合并入官方索引。

详细指南请参阅 [docs/PUBLISHING.md](docs/PUBLISHING.md)。

---

## 安全隔离机制

- **独立子进程**：插件运行在独立操作系统子进程中，不共享主服务内存与数据库连接，主服务敏感环境变量自动脱敏。
- **退出监听与熔断保护**：主服务通过异步进程等待实时感知插件退出；插件若连续异常崩溃，将触发断路器与退避机制，避免频繁重启耗尽主机资源。
- **解压安全校验**：安装过程自动校验压缩包内路径，拦截 Zip-Slip 路径穿越攻击，并限制单插件解压后总容量（默认上限 256MB）。
- **数据存储隔离**：为每个插件分配按 ID 隔离的独立 SQLite 存储表，禁止跨插件越权访问。

---

## 开源协议

本项目基于 [MIT License](LICENSE) 开源。
