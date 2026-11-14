// Native ES Module for Starter Template Plugin
// Integrates with window.__QY_PLUGIN_SDK__

const sdk = window.__QY_PLUGIN_SDK__;
const { React, icons } = sdk;
const Box = icons.Box || icons.Activity;

export function StarterTab() {
  return React.createElement(
    'div',
    { className: 'p-6 space-y-4 max-w-[1200px] mx-auto' },
    React.createElement(
      'div',
      { className: 'flex items-center space-x-3 pb-3 border-b border-slate-200 dark:border-slate-750' },
      React.createElement(Box, { className: 'w-5 h-5 text-sky-500' }),
      React.createElement('h2', { className: 'text-base font-semibold text-slate-900 dark:text-white' }, 'QY-DASH 插件示例 (Starter Template)')
    ),
    React.createElement(
      'div',
      { className: 'bg-white dark:bg-slate-850 p-6 rounded-lg border border-slate-200 dark:border-slate-750 shadow-xs space-y-3' },
      React.createElement('p', { className: 'text-sm text-slate-700 dark:text-slate-300 leading-relaxed' },
        '恭喜！Starter Template 插件的前端 ESM 模块已成功动态挂载到控制台导航栏。'
      ),
      React.createElement('p', { className: 'text-xs text-slate-500 dark:text-slate-400 leading-relaxed' },
        '您可以参考此模板的代码，通过修改 plugin.json、main.go 和 ui/index.js 快速定制开发属于您自己的微内核扩展。'
      )
    )
  );
}
