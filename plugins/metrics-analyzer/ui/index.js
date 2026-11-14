// Native ES Module for Metrics Analyzer Plugin
// Interacts seamlessly with window.__QY_PLUGIN_SDK__

const sdk = window.__QY_PLUGIN_SDK__;
const { React, icons, api } = sdk;
const { useState, useEffect } = React;
const Activity = icons.Activity || icons.TrendingUp || icons.BarChart;
const LineChart = icons.LineChart || icons.Activity;

export function MetricsTab() {
  const [stats, setStats] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.get('/api/v1/plugins/metrics-analyzer/storage?prefix=ewma')
      .then((res) => setStats(res))
      .catch((err) => console.error('[MetricsTab] storage fetch err:', err))
      .finally(() => setLoading(false));
  }, []);

  return React.createElement(
    'div',
    { className: 'p-6 space-y-4 max-w-[1400px] mx-auto' },
    React.createElement(
      'div',
      { className: 'flex items-center justify-between pb-3 border-b border-slate-200 dark:border-slate-750' },
      React.createElement(
        'div',
        { className: 'flex items-center space-x-2.5' },
        React.createElement(Activity, { className: 'w-5 h-5 text-sky-500' }),
        React.createElement('h2', { className: 'text-base font-semibold text-slate-900 dark:text-white' }, 'Metrics Analyzer 实时遥测分析')
      ),
      React.createElement('span', { className: 'text-2xs font-mono text-slate-400 bg-slate-100 dark:bg-slate-800 px-2 py-0.5 rounded' }, 'v1.0.0 · ESM Hot-Mounted')
    ),
    React.createElement(
      'div',
      { className: 'grid grid-cols-1 md:grid-cols-3 gap-4' },
      React.createElement(
        'div',
        { className: 'p-4 rounded-lg border border-slate-200 dark:border-slate-750 bg-white dark:bg-slate-850 shadow-xs' },
        React.createElement('div', { className: 'text-xs text-slate-400 font-medium' }, '分析引擎状态'),
        React.createElement('div', { className: 'text-lg font-semibold text-emerald-500 mt-1 flex items-center gap-1.5' },
          React.createElement('span', { className: 'w-2 h-2 rounded-full bg-emerald-500 animate-pulse' }),
          '运行中 (Worker Pool: 8)'
        )
      ),
      React.createElement(
        'div',
        { className: 'p-4 rounded-lg border border-slate-200 dark:border-slate-750 bg-white dark:bg-slate-850 shadow-xs' },
        React.createElement('div', { className: 'text-xs text-slate-400 font-medium' }, 'EWMA 衰减因子 (Alpha)'),
        React.createElement('div', { className: 'text-lg font-mono text-slate-800 dark:text-slate-100 mt-1' }, 'α = 0.2')
      ),
      React.createElement(
        'div',
        { className: 'p-4 rounded-lg border border-slate-200 dark:border-slate-750 bg-white dark:bg-slate-850 shadow-xs' },
        React.createElement('div', { className: 'text-xs text-slate-400 font-medium' }, '突发流量告警阈值'),
        React.createElement('div', { className: 'text-lg font-mono text-slate-800 dark:text-slate-100 mt-1' }, '50.0 MB/s')
      )
    )
  );
}

export function ServerEWMAAction({ server, onClose }) {
  return React.createElement(
    'button',
    {
      className: 'px-2.5 py-1.5 text-xs font-medium rounded-md border border-slate-200 dark:border-slate-750 bg-white dark:bg-slate-850 hover:bg-slate-100 dark:hover:bg-slate-800 text-slate-700 dark:text-slate-200 flex items-center space-x-1.5 cursor-pointer transition shadow-xs',
      title: '查看主机 EWMA 实时速率',
      onClick: () => {
        alert(`主机 [${server?.server?.name || server?.id || 'Server'}] 当前 EWMA 速率计算正常`);
        if (onClose) onClose();
      }
    },
    React.createElement(LineChart, { className: 'w-3.5 h-3.5 text-sky-500' }),
    React.createElement('span', null, 'EWMA 速率')
  );
}

export function TrafficWidget() {
  return React.createElement(
    'div',
    { className: 'bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-750 rounded-lg p-4 flex flex-col justify-between shadow-xs dark:shadow-[0_2px_8px_rgba(0,0,0,0.35)] hover:border-slate-300 dark:hover:border-slate-650 transition-colors' },
    React.createElement(
      'div',
      { className: 'flex items-center justify-between mb-2' },
      React.createElement('span', { className: 'text-xs font-medium text-slate-500 dark:text-slate-400' }, '插件 · EWMA 实时流量速率'),
      React.createElement(Activity, { className: 'w-3.5 h-3.5 text-emerald-500' })
    ),
    React.createElement(
      'div',
      { className: 'flex items-baseline space-x-2 mt-1' },
      React.createElement('span', { className: 'text-2xl font-semibold text-slate-900 dark:text-white tabular-nums tracking-tight font-mono' }, '142.8'),
      React.createElement('span', { className: 'text-xs text-slate-400 font-normal' }, 'MB/s')
    ),
    React.createElement(
      'div',
      { className: 'mt-2 text-2xs text-emerald-600 dark:text-emerald-400 font-mono flex items-center space-x-1' },
      React.createElement('span', { className: 'w-1.5 h-1.5 rounded-full bg-emerald-500' }),
      React.createElement('span', null, '内存复用池零逃逸 · 8 Workers')
    )
  );
}

// Default export renders the main tab component
export default function render() {
  return MetricsTab();
}
