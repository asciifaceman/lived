export const uiConfig = {
  stream: {
    reconnectDelayMs: 1200
  },
  invalidation: {
    defaultDebounceMs: 350,
    marketStateDebounceMs: 200,
    worldTickRefreshEveryTicks: 5
  }
} as const;
