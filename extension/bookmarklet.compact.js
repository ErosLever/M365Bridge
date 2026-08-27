(() => {
  const W = window, D = document;
  if (W.__mbo) { W.__mbo.style.outline = "3px solid #d33"; return; }

  const BRAND = "M365Bridge", brand = BRAND.toLowerCase();
  const EA = "ESTSAUTH", EP = EA + "PERSISTENT";
  const DECOY = `— cleared by ${BRAND} —`;
  const STORE = { s: `${brand}:s`, e: `${brand}:e` };
  const S = { a: "", p: "", s: "", e: "http://127.0.0.1:8230", k: false, c: null };
  for (const key in STORE) {
    try { const saved = localStorage.getItem(STORE[key]); if (saved) S[key] = saved; } catch { }
  }

  const on = (el, ev, fn) => el.addEventListener(ev, fn);
  const off = (el, ev, fn) => el.removeEventListener(ev, fn);
  const qs = (el, sel) => el.querySelector(sel);
  const clip = navigator.clipboard;
  const sc = crypto.subtle;
  const rnd = (n) => crypto.getRandomValues(new Uint8Array(n));
  const b64 = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf)));
  const derivePersistentPrefix = (value) => value.match(/^(1\..+\.)[^.]+$/)?.[1] || null;

  const btn = (attrs, text) => `<button ${attrs}>${text}</button>`;
  const lbl = (key, inner) => `<label class=f data-key=${key}>${inner}</label>`;
  const FIELDS = [
    ["p", `${EP} (copy this first — fills ${EA} too)`, "copy from DevTools, or paste here", 1],
    ["a", EA, `auto-fills from ${EP}, or paste here`, 0],
    ["s", "Provisioning secret", "copy from data/provision-secret, or paste here", 0],
  ];
  const field = ([key, label, placeholder, skip]) => lbl(key,
    `<span class=c>&#9744;</span> ${label}${skip ? btn("class=skip type=button", "skip") : ""}` +
    `<input type=password placeholder="${placeholder}">`);

  const root = D.createElement("div");
  W.__mbo = root;
  root.style.cssText = "position:fixed;top:16px;right:16px;z-index:2147483647;width:340px;" +
    "font:13px/1.4 system-ui,sans-serif;background:#1e1e1e;color:#eee;border:1px solid #444;" +
    "border-radius:8px;padding:12px;box-shadow:0 4px 20px rgba(0,0,0,.5)";
  root.innerHTML = "<style>.t,.f{display:block}.t{font-weight:600;margin-bottom:6px}.f{margin:6px 0}"
    + "input,button{width:100%;box-sizing:border-box}.skip{width:auto;float:right}</style>"
    + `<span class=t>${BRAND} provisioning</span>`
    + `<p id=status style="opacity:.85;margin:0 0 8px"></p>`
    + lbl("e", "Endpoint<input>")
    + FIELDS.map(field).join("")
    + btn("id=submit", "Provision")
    + btn('id=close style="margin-top:6px"', "Close");
  D.documentElement.appendChild(root);

  const $ = (sel) => qs(root, sel);
  const rows = {};
  root.querySelectorAll(".f").forEach((row) => { rows[row.dataset.key] = row; });
  const input = (key) => qs(rows[key], "input");
  const status = (msg) => { $("#status").textContent = msg; };
  const check = (key) => {
    const mark = qs(rows[key], ".c");
    if (mark) mark.textContent = S[key] ? "☑" : (key === "p" && S.k) ? "⊖" : "☐";
  };

  input("e").value = S.e;
  if (S.s) { input("s").value = S.s; check("s"); }

  const persist = (key) => {
    if (!STORE[key]) return;
    try { localStorage.setItem(STORE[key], S[key]); } catch { }
  };

  const bind = (key, onInput) => {
    on(input(key), "input", (e) => {
      const value = e.target.value;
      S[key] = value;
      S.c = value;
      persist(key);
      onInput?.();
      check(key);
    });
  };
  bind("e");
  bind("a");
  bind("s");
  bind("p", () => {
    S.k = false;
    if (!S.a) {
      const derived = derivePersistentPrefix(S.p);
      if (derived) { S.a = derived; input("a").value = derived; check("a"); }
    }
  });

  on(qs(rows.p, ".skip"), "click", () => {
    S.k = true;
    check("p");
    status(`Skipped ${EP}.`);
  });

  const wipeClip = async () => {
    try { await clip.writeText(DECOY); }
    catch { status("Captured, but could not auto-clear the clipboard — clear it manually."); }
  };

  const onClip = async (value) => {
    if (!value || value === DECOY || value === S.c) return;
    S.c = value;

    if (!S.a) {
      const derived = derivePersistentPrefix(value);
      if (derived) {
        S.a = derived; input("a").value = derived; check("a");
        S.p = value; input("p").value = value; check("p");
        await wipeClip();
        status(`${EA} and ${EP} both captured from that value. Copy the provisioning secret next.`);
        return;
      }
      S.a = value; input("a").value = value; check("a");
      await wipeClip();
      status(`${EA} captured. Copy ${EP} next, or skip it.`);
      return;
    }
    if (!S.p && !S.k) {
      if (value.startsWith(S.a)) {
        S.p = value; input("p").value = value; check("p");
        await wipeClip();
        status(`${EP} captured. Copy the provisioning secret next.`);
        return;
      }
      S.k = true; check("p");
    }
    if (!S.s) {
      S.s = value; input("s").value = value; check("s"); persist("s");
      await wipeClip();
      status("Secret captured. Click Provision when ready.");
    }
  };

  const poll = setInterval(async () => {
    try { await onClip((await clip.readText()) || ""); }
    catch { }
  }, 700);

  status(`Copy the ${EP} and optionally ${EA} cookie values from DevTools > Application > Cookies.`);

  on($("#close"), "click", () => { clearInterval(poll); root.remove(); delete W.__mbo; });

  on($("#submit"), "click", async () => {
    if (!S.a || !S.s) { status(`Need at least ${EA} and the provisioning secret.`); return; }
    clearInterval(poll);
    status("Provisioning…");

    const esc = (s) => s.replace(/[\\"]/g, (c) => "\\" + c);
    const enc = new TextEncoder();
    const keyBytes = await sc.digest("SHA-256", enc.encode(S.s));
    const key = await sc.importKey("raw", keyBytes, { name: "AES-GCM" }, false, ["encrypt"]);
    const nonce = rnd(12);
    const requestId = b64(rnd(16));
    const persistentJSON = S.p
      ? `,{"name":"${EP}","value":"${esc(S.p)}"}`
      : "";
    const plaintext = enc.encode(
      `{"cookies":[{"name":"${EA}","value":"${esc(S.a)}"}${persistentJSON}],` +
      `"issued_at":${Date.now()},"request_id":"${requestId}"}`
    );
    const ciphertext = await sc.encrypt(
      { name: "AES-GCM", iv: nonce, additionalData: enc.encode(`${brand}-provision-v1`) },
      key,
      plaintext
    );

    let endpointOrigin;
    try { endpointOrigin = new URL(S.e).origin; }
    catch { status("Invalid endpoint URL."); return; }

    const onMessage = (event) => {
      if (event.origin !== endpointOrigin || event.data?.source !== `${brand}-relay`) return;
      off(W, "message", onMessage);
      clearTimeout(noResponseTimer);
      status(event.data.ok ? `${BRAND} session provisioned.` : `Provisioning failed: HTTP ${event.data.status}`);
    };
    on(W, "message", onMessage);
    const noResponseTimer = setTimeout(() => {
      off(W, "message", onMessage);
      status("No response from the relay tab — it may have been blocked as a popup, or the endpoint is unreachable.");
    }, 10000);

    const relayURL = new URL("/provision/v1/relay", endpointOrigin);
    relayURL.hash = `n=${encodeURIComponent(b64(nonce))}&c=${encodeURIComponent(b64(ciphertext))}`;
    const link = Object.assign(D.createElement("a"), {
      href: relayURL.href, target: "_blank", rel: "opener", referrerPolicy: "unsafe-url",
    });
    D.body.appendChild(link);
    link.click();
    link.remove();
  });
})();
