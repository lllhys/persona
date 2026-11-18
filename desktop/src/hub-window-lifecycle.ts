/** Share one asynchronous hub-window creation. A hub change starts a new generation, so a retired pending create
 * cannot replace the current window or its IPC. */
export class HubWindowLifecycle<T> {
  #pending: Promise<T> | undefined;
  #generation = 0;

  get generation(): number { return this.#generation; }
  get pending(): boolean { return this.#pending !== undefined; }

  show(create: () => Promise<T>): Promise<T> {
    if (!this.#pending) {
      this.#pending = (async () => create())();
      const pending = this.#pending;
      const clear = () => { if (this.#pending === pending) this.#pending = undefined; };
      pending.then(clear, clear);
    }
    return this.#pending;
  }

  replace(): number {
    this.#generation++;
    this.#pending = undefined;
    return this.#generation;
  }
}
