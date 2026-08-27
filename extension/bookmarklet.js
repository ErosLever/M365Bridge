// M365Bridge provisioning bookmarklet.
//
// Extension-free fallback for /provision/v1/session. Reads no cookies
// itself (ESTSAUTH/ESTSAUTHPERSISTENT are HttpOnly, unreachable from page
// script). Instead, it draws a small on-page overlay and polls the
// clipboard: copy each value in DevTools > Application > Cookies and the
// matching field auto-fills and gets a checkmark. ESTSAUTHPERSISTENT's
// value always starts with the ESTSAUTH value as a prefix, so that's used
// to tell them apart automatically; anything else that shows up next is
// treated as the provisioning secret. The clipboard is wiped after each
// auto-captured value so cookies/secret don't linger there. Every field is
// also a masked, editable input, so you can paste manually if clipboard
// access is denied. The endpoint and provisioning secret (not the cookies)
// are persisted in localStorage so they don't need to be re-entered every
// run.
//
// The endpoint is normally plain http://, so a direct fetch from this
// https:// page would be blocked as mixed content. Provisioning instead
// opens a same-origin relay tab on the endpoint (GET /provision/v1/relay)
// that does the POST itself; only the already-encrypted envelope travels
// in that tab's URL fragment.
//
// Install: copy the minified line below into a bookmark's URL field.

(() => {
  if (window.__m365bpOverlay) { window.__m365bpOverlay.style.outline = "3px solid #d33"; return; }

  const DECOY = "— cleared by M365Bridge —";
  const STORE = { secret: "m365bridge:provisionSecret", endpoint: "m365bridge:endpoint" };
  const S = { estsauth: "", estsauthpersistent: "", secret: "", endpoint: "http://127.0.0.1:8230", persistentSkipped: false, lastSeenClip: null };
  for (const key in STORE) {
    try { const saved = localStorage.getItem(STORE[key]); if (saved) S[key] = saved; } catch { /* storage unavailable */ }
  }

  // ESTSAUTHPERSISTENT's value is "<ESTSAUTH value><extra>", and ESTSAUTH
  // itself keeps both its "1." prefix and its trailing "." — if
  // ESTSAUTHPERSISTENT is captured/pasted first (or on its own), both
  // fields can be filled from it.
  const derivePersistentPrefix = (value) => value.match(/^(1\..+\.)[^.]+$/)?.[1] || null;

  const FIELDS = [
    { key: "estsauthpersistent", label: "ESTSAUTHPERSISTENT (copy this first — fills ESTSAUTH too)", placeholder: "copy from DevTools, or paste here", skip: true },
    { key: "estsauth", label: "ESTSAUTH", placeholder: "auto-fills from ESTSAUTHPERSISTENT, or paste here" },
    { key: "secret", label: "Provisioning secret", placeholder: "copy from data/provision-secret, or paste here" },
  ];
  const field = ({ key, label, placeholder, skip }) => `
    <label class=f data-key=${key}>
      <span class=c>&#9744;</span> ${label}${skip ? " <button class=skip type=button>skip</button>" : ""}
      <input type=password autocomplete=off placeholder="${placeholder}">
    </label>`;

  const root = document.createElement("div");
  window.__m365bpOverlay = root;
  root.style.cssText = "position:fixed;top:16px;right:16px;z-index:2147483647;width:340px;" +
    "font:13px/1.4 system-ui,sans-serif;background:#1e1e1e;color:#eee;border:1px solid #444;" +
    "border-radius:8px;padding:12px;box-shadow:0 4px 20px rgba(0,0,0,.5)";
  root.innerHTML = `
    <style>.t{display:block;font-weight:600;margin-bottom:6px}.f{display:block;margin:6px 0}input,button{width:100%;box-sizing:border-box}.skip{width:auto;float:right}</style>
    <span class=t>M365Bridge provisioning</span>
    <p id=status style="opacity:.85;margin:0 0 8px"></p>
    <label class=f data-key=endpoint>Endpoint<input></label>
    ${FIELDS.map(field).join("")}
    <button id=submit>Provision</button>
    <button id=close style="margin-top:6px">Close</button>`;
  document.documentElement.appendChild(root);

  const $ = (sel) => root.querySelector(sel);
  const rows = {};
  root.querySelectorAll(".f").forEach((row) => { rows[row.dataset.key] = row; });
  const input = (key) => rows[key].querySelector("input");
  const status = (msg) => { $("#status").textContent = msg; };
  const check = (key) => {
    const mark = rows[key].querySelector(".c");
    if (mark) mark.textContent = S[key] ? "☑" : (key === "estsauthpersistent" && S.persistentSkipped) ? "⊖" : "☐";
  };

  input("endpoint").value = S.endpoint;
  if (S.secret) { input("secret").value = S.secret; check("secret"); }

  const persist = (key) => {
    if (!STORE[key]) return;
    try { localStorage.setItem(STORE[key], S[key]); } catch { /* storage unavailable */ }
  };

  const bind = (key, onInput) => {
    input(key).addEventListener("input", (e) => {
      S[key] = e.target.value;
      S.lastSeenClip = e.target.value; // don't let the poller re-grab what was just typed/pasted
      persist(key);
      onInput?.();
      check(key);
    });
  };
  bind("endpoint");
  bind("estsauth");
  bind("secret");
  bind("estsauthpersistent", () => {
    S.persistentSkipped = false;
    if (!S.estsauth) {
      const derived = derivePersistentPrefix(S.estsauthpersistent);
      if (derived) { S.estsauth = derived; input("estsauth").value = derived; check("estsauth"); }
    }
  });

  rows.estsauthpersistent.querySelector(".skip").addEventListener("click", () => {
    S.persistentSkipped = true;
    check("estsauthpersistent");
    status("Skipped ESTSAUTHPERSISTENT.");
  });

  const wipeClip = async () => {
    try { await navigator.clipboard.writeText(DECOY); }
    catch { status("Captured, but could not auto-clear the clipboard — clear it manually."); }
  };

  const onClip = async (clip) => {
    if (!clip || clip === DECOY || clip === S.lastSeenClip) return;
    S.lastSeenClip = clip;

    if (!S.estsauth) {
      const derived = derivePersistentPrefix(clip);
      if (derived) {
        S.estsauth = derived; input("estsauth").value = derived; check("estsauth");
        S.estsauthpersistent = clip; input("estsauthpersistent").value = clip; check("estsauthpersistent");
        await wipeClip();
        status("ESTSAUTH and ESTSAUTHPERSISTENT both captured from that value. Copy the provisioning secret next.");
        return;
      }
      S.estsauth = clip; input("estsauth").value = clip; check("estsauth");
      await wipeClip();
      status("ESTSAUTH captured. Copy ESTSAUTHPERSISTENT next, or skip it.");
      return;
    }
    if (!S.estsauthpersistent && !S.persistentSkipped) {
      if (clip.startsWith(S.estsauth)) {
        S.estsauthpersistent = clip; input("estsauthpersistent").value = clip; check("estsauthpersistent");
        await wipeClip();
        status("ESTSAUTHPERSISTENT captured. Copy the provisioning secret next.");
        return;
      }
      S.persistentSkipped = true; check("estsauthpersistent");
    }
    if (!S.secret) {
      S.secret = clip; input("secret").value = clip; check("secret"); persist("secret");
      await wipeClip();
      status("Secret captured. Click Provision when ready.");
    }
  };

  const poll = setInterval(async () => {
    try { await onClip((await navigator.clipboard.readText()) || ""); }
    catch { /* clipboard read denied — user can still paste manually */ }
  }, 700);

  status("Copy the ESTSAUTHPERSISTENT and optionally ESTSAUTH cookie values from DevTools > Application > Cookies.");

  $("#close").addEventListener("click", () => { clearInterval(poll); root.remove(); delete window.__m365bpOverlay; });

  $("#submit").addEventListener("click", async () => {
    if (!S.estsauth || !S.secret) { status("Need at least ESTSAUTH and the provisioning secret."); return; }
    clearInterval(poll);
    status("Provisioning…");

    // Cookie values can't contain a quote or backslash (RFC 6265
    // cookie-octet), so this only needs to escape what could otherwise
    // break out of the string literal below — not a full JSON encoder.
    const esc = (s) => s.replace(/[\\"]/g, (c) => "\\" + c);
    const enc = new TextEncoder();
    const keyBytes = await crypto.subtle.digest("SHA-256", enc.encode(S.secret));
    const key = await crypto.subtle.importKey("raw", keyBytes, { name: "AES-GCM" }, false, ["encrypt"]);
    const nonce = crypto.getRandomValues(new Uint8Array(12));
    const requestId = btoa(String.fromCharCode(...crypto.getRandomValues(new Uint8Array(16))));
    const persistentJSON = S.estsauthpersistent
      ? `,{"name":"ESTSAUTHPERSISTENT","value":"${esc(S.estsauthpersistent)}"}`
      : "";
    const plaintext = enc.encode(
      `{"cookies":[{"name":"ESTSAUTH","value":"${esc(S.estsauth)}"}${persistentJSON}],` +
      `"issued_at":${Date.now()},"request_id":"${requestId}"}`
    );
    const ciphertext = await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: nonce, additionalData: enc.encode("m365bridge-provision-v1") },
      key,
      plaintext
    );
    const b64 = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf)));

    // This page is https:// and the endpoint is normally a local http://
    // service, so a direct fetch would be blocked as mixed content before
    // it ever reaches CORS. Instead, open a same-origin relay page on the
    // endpoint itself — only the already-encrypted envelope travels in the
    // URL fragment, never sent to any server on navigation — and let it do
    // the POST and report back.
    let endpointOrigin;
    try { endpointOrigin = new URL(S.endpoint).origin; }
    catch { status("Invalid endpoint URL."); return; }

    const onMessage = (event) => {
      if (event.origin !== endpointOrigin || event.data?.source !== "m365bridge-relay") return;
      window.removeEventListener("message", onMessage);
      clearTimeout(noResponseTimer);
      status(event.data.ok ? "M365Bridge session provisioned." : `Provisioning failed: HTTP ${event.data.status}`);
    };
    window.addEventListener("message", onMessage);
    const noResponseTimer = setTimeout(() => {
      window.removeEventListener("message", onMessage);
      status("No response from the relay tab — it may have been blocked as a popup, or the endpoint is unreachable.");
    }, 10000);

    const relayURL = new URL("/provision/v1/relay", endpointOrigin);
    relayURL.hash = `n=${encodeURIComponent(b64(nonce))}&c=${encodeURIComponent(b64(ciphertext))}`;
    // window.open() drops the referrer on this https -> http downgrade by
    // default, and the relay's server-side referer check requires it — a
    // clicked <a referrerpolicy="unsafe-url"> asks the browser to send it
    // anyway.
    const link = Object.assign(document.createElement("a"), {
      href: relayURL.href, target: "_blank", rel: "opener", referrerPolicy: "unsafe-url",
    });
    document.body.appendChild(link);
    link.click();
    link.remove();
  });
})();
