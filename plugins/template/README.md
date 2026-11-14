# 开发者示例插件 (Starter Template)

包含后台服务与前端界面的最小插件示例，供开发者参考与二次开发。

## 目录结构

```
template/
├── main.go          # Go 后台入口
├── plugin.json      # 插件清单文件
├── ui/
│   └── index.js     # 前端组件
└── README.md
```

## 使用方法

1. 将本目录复制为 `plugins/your-plugin-name`
2. 修改 `plugin.json` 中的 `id`、`name`、`description` 等信息
3. 在 `main.go` 中编写业务逻辑，在 `ui/index.js` 中编写前端界面
4. 执行 `scripts/package-plugin.sh` 编译打包
