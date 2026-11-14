# 插件开发入门模板 (Starter Template)

用于构建 QY-DASH 微内核插件的极简脚手架模板，集成 Go UDS IPC 后台子进程与原生 ESM React 前端扩展。

## 目录结构

```
template/
├── main.go          # 实现 pluginsdk.Plugin 接口的 Go 入口
├── plugin.json      # 定义权限声明与前端 UI 扩展点的插件清单
├── ui/
│   └── index.js     # 原生 ESM 前端组件 (兼容 React 19)
└── README.md
```

## 快速上手

1. 将本目录复制至 `plugins/your-plugin-name`
2. 修改 `plugin.json` 中的 `id`、`name`、`description` 等信息
3. 在 `main.go` 中编写业务逻辑，在 `ui/index.js` 中编写前端组件
4. 使用 `scripts/package-plugin.sh` 进行多架构交叉编译与打包验证
