# QY-DASH 官方插件市场与拓展仓库 (QY-DASH Plugin Marketplace & SDK)

<p align="center">
  <a href="https://github.com/wenfer/qy-plugins/actions/workflows/ci.yml"><img src="https://github.com/wenfer/qy-plugins/actions/workflows/ci.yml/badge.svg" alt="CI Status"></a>
  <img src="https://img.shields.io/badge/go-1.22+-00ADD8?style=flat&logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/schema-v1-blue" alt="Catalog Schema v1">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="License MIT">
</p>

本仓库是 [QY-DASH](https://github.com/wenfer/qy-xray) 官方微内核插件生态的核心枢纽，包含：
1. **官方插件市场索引 (`index.json`)**：驱动 QY-DASH 控制台「插件市场」一键安装与版本更新。
2. **官方独立 Go Plugin SDK (`pkg/pluginsdk`)**：零三方依赖的微内核 UDS 进程间通信与扩展开发工具包。
3. **官方参考插件 (`plugins/metrics-analyzer`)**：EWMA 实时遥测分析、工作池、内存复用与突发流量告警。
4. **插件开发脚手架 (`plugins/template`)**：开箱即用的插件模板，助您 5 分钟上手开发。
5. **规范与文档 (`docs/`)**：涵盖清单规范、IPC 协议二进制帧与前端原生 ESM 动态加载。

---

## 目录结构 (Repository Layout)

```
qy-plugins/
├── index.json                  # 官方插件市场索引 (Catalog Schema v1)
├── pkg/
│   └── pluginsdk/              # 官方独立 Go 插件开发 SDK
│       ├── ipc/                # 16-byte UDS 二进制帧编解码与缓冲池
│       ├── client.go           # 异步 RPC 多路复用与遥测事件流消费
│       ├── serve.go            # 插件子进程生命周期托管与优雅关机
│       ├── storage.go          # 隔离持久化 SQLite KV 存储客户端
│       └── types.go            # 接口定义 (Plugin, HostContext, Logger)
├── plugins/
│   ├── metrics-analyzer/       # 官方参考插件: EWMA 遥测分析与突发告警
│   │   ├── analyzer/           # 零内存逃逸分析引擎与工作池
│   │   ├── ui/index.js         # 前端 React 19 ESM 动态扩展组件
│   │   ├── main.go             # 插件后台子进程入口
│   │   └── plugin.json         # 插件清单规范
│   └── template/               # 最小化插件开发脚手架模板
│       ├── ui/index.js
│       ├── main.go
│       └── plugin.json
├── docs/
│   ├── MANIFEST_SPEC.md        # plugin.json 结构规范与多架构打包标准
│   ├── SDK_GUIDE.md            # Go SDK 与前端原生 ESM 扩展深度开发指南
│   └── PUBLISHING.md           # 插件市场提交流程与规范
├── scripts/
│   ├── package-plugin.sh       # 跨平台自动编译 (Linux amd64/arm64, Darwin) 与打包校验
│   └── validate-index.sh       # 市场 index.json 语法与校验和自动化验证
└── .github/workflows/          # 自动化 CI 测试与 Release 发版流水线
```

---

## 插件市场目录 (Marketplace Catalog)

当前市场索引已收录官方插件：

| 插件 ID | 名称 | 版本 | 架构支持 | 描述 |
| :--- | :--- | :--- | :--- | :--- |
| `metrics-analyzer` | **Metrics Analyzer** | `v1.0.0` | Linux amd64<br>Linux arm64<br>macOS arm64 | 高吞吐实时主机遥测分析引擎，具备 `sync.Pool` 零逃逸内存复用、EWMA 流量速率平滑与突发告警 |
| `starter-template` | **Starter Template** | `v1.0.0` | 源码模板 | 最小化样板模板，包含完整的 UDS 后台交互与前端 ESM 扩展挂载点 |

---

## QY-DASH 用户安装指南 (User Guide)

### 方式 1: 官方插件市场一键安装 (推荐)
1. 登录 QY-DASH 控制台，进入「系统设置」 $\rightarrow$「插件管理」 $\rightarrow$「插件市场」。
2. 浏览已收录的官方与社区插件，点击目标插件卡片右侧的 **「安装」** 按钮。
3. 主控将自动根据主机 CPU 架构（如 `linux/amd64` 或 `linux/arm64`）下载对应 release 包、校验 SHA-256 完整性并安全解压至 `plugins/` 目录。
4. 安装完成后，点击「启用」开关，插件进程即刻热插拔启动，前端界面无感动态加载。

### 方式 2: GitHub 仓库直连安装
在插件管理界面的「GitHub 仓库安装」输入框中输入：
```text
https://github.com/wenfer/qy-plugins
```
主控将自动检索 release 资源或按规范解构安装。

### 方式 3: 离线 Zip 包上传
下载对应系统架构的 `.zip` 压缩包（例如 `metrics-analyzer-linux-amd64.zip`），在插件管理页面点击 **「上传离线插件包」** 即可。

---

## 开发者快速上手 (Developer Quickstart)

QY-DASH 采用 **微内核独立进程架构**，后端插件运行在隔离的操作系统子进程中，与主控通过 Unix Domain Socket (UDS) 建立高速二进制通信；前端通过原生 ECMAScript Modules (`import()`) 动态注入，无需重新编译主控前端代码。

### 1. 引用官方 Go SDK
在您的插件项目中引用：
```bash
go get github.com/wenfer/qy-plugins/pkg/pluginsdk
```

### 2. 实现插件生命周期
编写 `main.go`：
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
	p.logger.Infof("MyPlugin started! Discovered host environment.")

	// 获取主机所有在线节点
	servers, err := ctx.GetServerList(context.Background())
	if err == nil {
		p.logger.Infof("Discovered %d active servers", len(servers))
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

### 3. 定义插件清单 `plugin.json`
```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "id": "my-plugin",
  "name": "My Plugin",
  "version": "1.0.0",
  "author": "YourName",
  "description": "My high performance QY-DASH extension",
  "main": "bin/my-plugin",
  "frontend": {
    "esm": "ui/index.js",
    "extension_points": {
      "navigation_tabs": [
        {
          "id": "my-tab",
          "title": "My Extension",
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
    React.createElement('h2', { className: 'text-base font-semibold' }, 'My Plugin Panel')
  );
}
```

### 5. 跨架构一键打包
运行内置打包脚本：
```bash
./scripts/package-plugin.sh plugins/template my-plugin 1.0.0
```
脚本将输出 `dist/my-plugin-linux-amd64.zip` 等多架构部署包，并自动打印用于 `index.json` 的 SHA-256 校验和与 release 片段。

---

## 提交插件到官方市场 (Publishing)

欢迎社区贡献插件！发布流程简述如下：
1. 遵循 [docs/MANIFEST_SPEC.md](docs/MANIFEST_SPEC.md) 清单与安全规范。
2. 在您的 GitHub 仓库创建 Release 并附带编译好的 `.zip` 压缩包。
3. Fork 本仓库，在 `index.json` 中追加您的插件条目与下载地址及 SHA-256。
4. 运行 `./scripts/validate-index.sh` 确保索引格式与校验和准确无误。
5. 向本仓库发起 Pull Request，CI 自动化流水线验证通过后由官方审核合并。
详细说明请参阅 [docs/PUBLISHING.md](docs/PUBLISHING.md)。

---

## 安全机制 (Security & Isolation)

QY-DASH 插件系统实施严格纵深防御：
- **进程级别沙箱**：插件在子进程独立执行，环境变量严苛脱敏，隔离主控密钥与数据库凭证。
- **崩溃毫秒感知**：异步 `cmd.Wait()` 监控，崩溃感知 SLA < 10ms，具备断路器与指数退避保护。
- **解压防御机制**：主控解包时具备 Zip-Slip 路径穿越拦截与 256MB 压缩炸弹累积防护。
- **存储隔离机制**：SQLite 底层依据插件 ID 动态隔离物理表，插件无法越权访问其他插件或主控数据。

---

## 许可证 (License)

本项目采用 [MIT License](LICENSE) 开源。
