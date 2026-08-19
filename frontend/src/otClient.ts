// The classic ot.js client state machine: Synchronized / AwaitingConfirm /
// AwaitingWithBuffer, expressed with two nullable operations.
import { Operation } from "./ot";
import type { UserOpWire } from "./connection";

export class OTClient {
  /** Number of server operations integrated so far. */
  revision = 0;
  /** Our connection id, used to recognize acknowledgements. */
  me = -1;
  /** Operation sent to the server, not yet acknowledged. */
  outstanding: Operation | null = null;
  /** Local edits made while waiting for the acknowledgement. */
  buffer: Operation | null = null;

  constructor(
    private sendEdit: (revision: number, op: Operation) => void,
    private applyRemote: (op: Operation) => void,
  ) {}

  get synchronized(): boolean {
    return this.outstanding === null && this.buffer === null;
  }

  /** The editor produced a local operation (already applied locally). */
  localEdit(op: Operation): void {
    if (op.isNoop()) return;
    if (!this.outstanding) {
      this.outstanding = op;
      this.sendEdit(this.revision, op);
    } else if (!this.buffer) {
      this.buffer = op;
    } else {
      this.buffer = Operation.compose(this.buffer, op);
    }
  }

  /** Integrate a History broadcast from the server. */
  handleHistory(start: number, ops: UserOpWire[]): void {
    for (let k = 0; k < ops.length; k++) {
      if (start + k < this.revision) continue; // already integrated
      const uop = ops[k];
      if (uop.id === this.me && this.outstanding) {
        // Acknowledgement of our outstanding operation.
        this.revision++;
        this.outstanding = this.buffer;
        this.buffer = null;
        if (this.outstanding) this.sendEdit(this.revision, this.outstanding);
        continue;
      }
      let op = Operation.fromJSON(uop.operation);
      if (this.outstanding) [this.outstanding, op] = Operation.transform(this.outstanding, op);
      if (this.buffer) [this.buffer, op] = Operation.transform(this.buffer, op);
      this.applyRemote(op);
      this.revision++;
    }
  }

  /**
   * After a reconnect: merge any buffered edits into the outstanding
   * operation and resend it at our current revision. The server transforms
   * it over everything we have not seen. (Like rustpad, an edit that was
   * committed but never acknowledged before the disconnect can be applied
   * twice in this rare race.)
   */
  resend(): void {
    if (this.outstanding && this.buffer) {
      this.outstanding = Operation.compose(this.outstanding, this.buffer);
      this.buffer = null;
    }
    if (this.outstanding) this.sendEdit(this.revision, this.outstanding);
  }
}
