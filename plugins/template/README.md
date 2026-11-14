# Starter Template Plugin

Minimal boilerplate template for building QY-DASH microkernel plugins with Go UDS IPC backend and native ESM React frontend.

## Directory Structure

```
template/
├── main.go          # Go entrypoint implementing pluginsdk.Plugin
├── plugin.json      # Manifest defining permissions & UI extension points
├── ui/
│   └── index.js     # Native ESM frontend component (React 19 compatible)
└── README.md
```

## Quick Start

1. Copy this directory to `plugins/your-plugin-name`
2. Update `id`, `name`, `description` in `plugin.json`
3. Add your business logic in `main.go` and UI in `ui/index.js`
4. Compile and package using `scripts/package-plugin.sh`
