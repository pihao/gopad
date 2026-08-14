// WebSocket wrapper: JSON protocol dispatch plus reconnect with
// exponential backoff.

export interface UserInfo {
  name: string;
  hue: number;
}

export interface CursorData {
  cursors: number[];
  selections: [number, number][];
}

export interface UserOpWire {
  id: number;
  operation: unknown;
}

export interface Handlers {
  onConnected(): void;
  /** code is the WebSocket close code (1008 = server rejected our state). */
  onDisconnected(code: number): void;
  onIdentity(id: number): void;
  onHistory(start: number, ops: UserOpWire[]): void;
  onLanguage(lang: string): void;
  onUserInfo(id: number, info: UserInfo | null): void;
  onUserCursor(id: number, data: CursorData): void;
  onExpiry(ttlSeconds: number, expiresAt: number): void;
  onKilled(reason: string): void;
}

export class Connection {
  private ws: WebSocket | null = null;
  private attempts = 0;
  private disposed = false;

  constructor(
    private url: string,
    private h: Handlers,
  ) {
    this.open();
  }

  private open(): void {
    const ws = new WebSocket(this.url);
    this.ws = ws;
    ws.onopen = () => {
      this.attempts = 0;
      this.h.onConnected();
    };
    ws.onmessage = (ev) => {
      let msg: Record<string, unknown>;
      try {
        msg = JSON.parse(ev.data as string);
      } catch {
        return;
      }
      this.dispatch(msg);
    };
    ws.onclose = (ev) => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.h.onDisconnected(ev.code);
      if (!this.disposed) {
        const delay = Math.min(500 * 2 ** this.attempts++, 10_000);
        setTimeout(() => {
          if (!this.disposed) this.open();
        }, delay);
      }
    };
  }

  send(msg: unknown): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  dispose(): void {
    this.disposed = true;
    this.ws?.close();
  }

  private dispatch(msg: Record<string, unknown>): void {
    if (msg.Identity !== undefined) {
      this.h.onIdentity(msg.Identity as number);
    } else if (msg.History !== undefined) {
      const h = msg.History as { start: number; operations: UserOpWire[] | null };
      this.h.onHistory(h.start, h.operations ?? []);
    } else if (msg.Language !== undefined) {
      this.h.onLanguage(msg.Language as string);
    } else if (msg.UserInfo !== undefined) {
      const u = msg.UserInfo as { id: number; info: UserInfo | null };
      this.h.onUserInfo(u.id, u.info ?? null);
    } else if (msg.UserCursor !== undefined) {
      const u = msg.UserCursor as { id: number; data: { cursors: number[] | null; selections: [number, number][] | null } };
      this.h.onUserCursor(u.id, {
        cursors: u.data.cursors ?? [],
        selections: u.data.selections ?? [],
      });
    } else if (msg.Expiry !== undefined) {
      const e = msg.Expiry as { ttlSeconds: number; expiresAt: number };
      this.h.onExpiry(e.ttlSeconds, e.expiresAt);
    } else if (msg.Killed !== undefined) {
      this.h.onKilled((msg.Killed as { reason: string }).reason);
    }
  }
}
