// window.lightron: the page's side of a Lightron app. Injected into every page of the app before its own scripts
// (macOS: Page.addScriptToEvaluateOnNewDocument; Windows: AddScriptToExecuteOnDocumentCreated), and carried over a
// binding the shell installs, window.__lightronPost (one JSON string per message).
(() => {
  if (window.lightron) return;
  // Looked up per call: in a fresh document the binding can be installed after this script runs.
  const post = (m) => window.__lightronPost(JSON.stringify(m));
  const platform = navigator.platform.startsWith("Mac") ? "darwin" : navigator.platform.startsWith("Win") ? "windows" : "linux";

  let seq = 0;
  const calls = new Map(); // invoke id → {resolve, reject}
  const listeners = new Map(); // event → Set(fn)

  // ---- files' paths (macOS) ----
  // A File's path on disk is something only the browser knows (DOM.getFileInfo); the shell can only answer
  // asynchronously, while code reading it (an upload handler) wants it at once. So the paths are looked up *before*
  // the page sees the files: a dropped / picked / pasted file event is held in the capture phase, each File's path
  // fetched, and the event dispatched again with the very same File objects. A few ms, once per drop.
  const paths = new WeakMap();
  const parked = new Map();
  window.__lightronFiles = parked;
  const ask = (file) =>
    new Promise((resolve) => {
      const id = ++seq;
      calls.set(id, { resolve, reject: resolve });
      parked.set(id, file);
      post({ t: "path", id });
    });
  const resolveAll = (files) => Promise.all(files.map((f) => (paths.has(f) ? null : ask(f).then((p) => paths.set(f, p || "")))));
  const replaying = new WeakSet();
  const hold = (e, files, again) => {
    if (platform !== "darwin" || !e.isTrusted || replaying.has(e) || !files.length || files.every((f) => paths.has(f))) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    const target = e.target;
    resolveAll(files).then(() => {
      const ev = again();
      replaying.add(ev);
      target.dispatchEvent(ev);
    });
  };
  const transfer = (src, files) => {
    const dt = new DataTransfer();
    for (const t of src.types) if (t !== "Files") dt.setData(t, src.getData(t));
    for (const f of files) dt.items.add(f);
    return dt;
  };
  addEventListener(
    "drop",
    (e) => {
      const files = Array.from(e.dataTransfer?.files ?? []);
      const dt = files.length ? transfer(e.dataTransfer, files) : null;
      const init = { bubbles: true, cancelable: true, composed: true, clientX: e.clientX, clientY: e.clientY, screenX: e.screenX, screenY: e.screenY, altKey: e.altKey, ctrlKey: e.ctrlKey, metaKey: e.metaKey, shiftKey: e.shiftKey };
      hold(e, files, () => new DragEvent("drop", { ...init, dataTransfer: dt }));
    },
    true,
  );
  addEventListener(
    "paste",
    (e) => {
      const files = Array.from(e.clipboardData?.files ?? []);
      const dt = files.length ? transfer(e.clipboardData, files) : null;
      hold(e, files, () => new ClipboardEvent("paste", { bubbles: true, cancelable: true, composed: true, clipboardData: dt }));
    },
    true,
  );
  addEventListener(
    "change",
    (e) => {
      const el = e.target;
      if (!(el instanceof HTMLInputElement) || el.type !== "file") return;
      hold(e, Array.from(el.files ?? []), () => new Event("change", { bubbles: true }));
    },
    true,
  );

  // ---- links leaving the app go to the default browser ----
  // Without this they'd open in the app's browser, in a window the app doesn't own. Not in iframes on Windows: WebView2
  // delivers messages from the top page only, so the link would be swallowed (the page's own handling, if any, runs).
  const ownLinks = window.top === window || platform !== "windows";
  const external = (href) => {
    try {
      const u = new URL(href, location.href);
      return /^(https?|mailto):$/.test(u.protocol) && u.origin !== location.origin ? u.href : "";
    } catch {
      return "";
    }
  };
  const open = window.open;
  window.open = function (u, ...rest) {
    const x = ownLinks && u && external(String(u));
    if (x) {
      post({ t: "open", url: x });
      return null;
    }
    return open.call(this, u, ...rest);
  };
  // Bubble phase, on window: after the page's own handlers (one that opened the link itself has called preventDefault).
  addEventListener("click", (e) => {
    if (!ownLinks || e.defaultPrevented || e.button > 1) return;
    const a = e.composedPath().find((n) => n instanceof HTMLAnchorElement && n.href);
    const x = a && external(a.href);
    if (!x) return;
    e.preventDefault();
    post({ t: "open", url: x });
  });

  // ---- right-click ----
  // No browser menu (Back, Reload, Save As, Print) where the page didn't answer with its own: an app has none. The
  // browser's stays where it is useful: an editable field or selected text (cut / copy / paste).
  addEventListener("contextmenu", (e) => {
    if (e.defaultPrevented) return;
    const t = e.target instanceof Element ? e.target : null;
    const editable = t && t.closest('input, textarea, select, [contenteditable=""], [contenteditable="true"], [contenteditable="plaintext-only"]');
    if (!editable && !String(getSelection() || "")) e.preventDefault();
  });

  // The window's title follows the page's (macOS does it by itself; WebView2's host window doesn't).
  if (platform === "windows") {
    const title = () => post({ t: "title", title: document.title });
    addEventListener("DOMContentLoaded", () => {
      title();
      const t = document.querySelector("title");
      if (t) new MutationObserver(title).observe(t, { childList: true, characterData: true, subtree: true });
    });
  }

  window.lightron = Object.freeze({
    platform,
    /** Calls the Go function bound under name (Options.Bind); resolves with what it returns, rejects with its error. */
    invoke: (name, ...args) =>
      new Promise((resolve, reject) => {
        const id = ++seq;
        calls.set(id, { resolve, reject });
        post({ t: "invoke", id, name, args });
      }),
    /** Sends a message to Options.OnMessage. */
    send: (msg) => post({ t: "send", msg }),
    /** Listens to events the Go side emits (Window.Emit / App.Emit); returns the function that stops listening. */
    on: (event, fn) => {
      if (!listeners.has(event)) listeners.set(event, new Set());
      listeners.get(event).add(fn);
      return () => listeners.get(event)?.delete(fn);
    },
    /** Brings this window and the app to the front (e.g. from a notification's click). */
    focus: () => post({ t: "focus" }),
    /** A picked / dropped / pasted File's path on disk, synchronously ("" if it has none). macOS. */
    getPathForFile: (file) => paths.get(file) || "",
    // ---- for the Go side ----
    __settle: (id, value, error) => {
      const c = calls.get(id);
      if (!c) return;
      calls.delete(id);
      parked.delete(id);
      error == null ? c.resolve(value) : c.reject(new Error(error));
    },
    __emit: (event, data) => {
      for (const fn of listeners.get(event) ?? []) {
        try {
          fn(data);
        } catch (err) {
          console.error(err);
        }
      }
    },
  });
})();
