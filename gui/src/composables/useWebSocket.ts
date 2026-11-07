import { onScopeDispose } from "vue";
import { apiRoot } from "@/api/client";
import type { WsMessage } from "@/api/types";

// The backend's message socket, with the reconnect the old App.vue had:
// 1 s after the first drop, doubling to 30 s, back to 1 s once a connection
// succeeds. Messages are not replayed, so every open — the first one too —
// asks the caller to re-sync what it may have missed.
export interface MessageSocket {
  start(): void;
  stop(): void;
}

// The token travels in Sec-WebSocket-Protocol: a browser cannot set
// Authorization on a handshake, and a query string is written to every
// proxy log. The prefix and the JWT alphabet are both valid header tokens.
const WS_TOKEN_PREFIX = "v2raya.v1.";

export function createMessageSocket(handlers: {
  onMessage(msg: WsMessage): void;
  onOpen(): void;
}): MessageSocket {
  let ws: WebSocket | null = null;
  let retries = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let stopped = false;

  function url(): string {
    let root = apiRoot();
    if (!root.trim() || root.startsWith("/"))
      root = location.protocol + "//" + location.host + root;
    const u = new URL(root);
    const proto = u.protocol === "https:" ? "wss" : "ws";
    let base = u.pathname;
    if (base.endsWith("/api")) base = base.slice(0, -4);
    return `${proto}://${u.host}${base}/api/message`;
  }

  function connect() {
    if (stopped) return;
    const token = localStorage.getItem("token") ?? "";
    const socket = token
      ? new WebSocket(url(), WS_TOKEN_PREFIX + token)
      : new WebSocket(url());
    ws = socket;
    socket.onopen = () => {
      retries = 0;
      handlers.onOpen();
    };
    socket.onmessage = (ev) => {
      if (!ev.data) return;
      let msg: unknown;
      try {
        msg = JSON.parse(ev.data);
      } catch {
        // a proxy's error page or a truncated frame is not a message
        return;
      }
      handlers.onMessage(msg as Parameters<typeof handlers.onMessage>[0]);
    };
    socket.onclose = () => {
      socket.onmessage = null;
      if (ws === socket) ws = null;
      if (stopped) return;
      const delay = Math.min(1000 * 2 ** retries, 30_000);
      retries++;
      timer = setTimeout(() => {
        timer = null;
        if (ws === null) connect();
      }, delay);
    };
  }

  return {
    start() {
      stopped = false;
      if (ws) ws.close();
      connect();
    },
    stop() {
      stopped = true;
      if (timer) {
        clearTimeout(timer);
        timer = null;
      }
      if (ws) {
        // detach first: onclose would otherwise schedule a reconnect
        ws.onclose = null;
        ws.close();
        ws = null;
      }
    },
  };
}

/** useMessageSocket binds a socket to the current effect scope: it stops when the scope does. */
export function useMessageSocket(handlers: {
  onMessage(msg: WsMessage): void;
  onOpen(): void;
}): MessageSocket {
  const socket = createMessageSocket(handlers);
  onScopeDispose(() => socket.stop());
  return socket;
}
