const extensionAPI = globalThis.browser ?? globalThis.chrome;
const form = document.querySelector("#provision-form");
const endpointInput = document.querySelector("#endpoint");
const secretInput = document.querySelector("#secret");
const destination = document.querySelector("#destination");
const button = document.querySelector("#provision");
const status = document.querySelector("#status");

function provisioningURL(value) {
  const url = new URL(value);
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("Bridge endpoint must use HTTP or HTTPS.");
  }
  url.pathname = "/provision/v1/session";
  url.search = "";
  url.hash = "";
  return url;
}

function updateDestination() {
  try {
    destination.textContent = provisioningURL(endpointInput.value).href;
  } catch {
    destination.textContent = "Enter a valid bridge endpoint";
  }
}

function base64(bytes) {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function randomID() {
  return base64(crypto.getRandomValues(new Uint8Array(16)));
}

async function encryptProvisioningPayload(secret, cookies) {
  const encoder = new TextEncoder();
  const keyBytes = await crypto.subtle.digest("SHA-256", encoder.encode(secret));
  const key = await crypto.subtle.importKey(
    "raw",
    keyBytes,
    { name: "AES-GCM" },
    false,
    ["encrypt"]
  );
  const nonce = crypto.getRandomValues(new Uint8Array(12));
  const plaintext = encoder.encode(JSON.stringify({
    cookies,
    issued_at: Date.now(),
    request_id: randomID()
  }));
  const ciphertext = await crypto.subtle.encrypt(
    {
      name: "AES-GCM",
      iv: nonce,
      additionalData: encoder.encode("m365bridge-provision-v1")
    },
    key,
    plaintext
  );

  return {
    version: 1,
    nonce: base64(nonce),
    ciphertext: base64(new Uint8Array(ciphertext))
  };
}

async function restoreSettings() {
  const saved = await extensionAPI.storage.local.get(["endpoint", "provisionSecret"]);
  if (saved.endpoint) endpointInput.value = saved.endpoint;
  if (saved.provisionSecret) {
    secretInput.value = saved.provisionSecret;
  } else if (globalThis.M365BRIDGE_BUILD_CONFIG?.provisionSecret) {
    secretInput.value = globalThis.M365BRIDGE_BUILD_CONFIG.provisionSecret;
  }
  updateDestination();
}

endpointInput.addEventListener("input", updateDestination);

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  button.disabled = true;
  status.dataset.kind = "";
  status.textContent = "Reading the required Microsoft login cookies...";

  try {
    const url = provisioningURL(endpointInput.value);
    const secret = secretInput.value;
    const [activeTab] = await extensionAPI.tabs.query({ active: true, currentWindow: true });
    if (!activeTab?.id) {
      throw new Error("Could not identify the active browser tab.");
    }
    const cookieStores = await extensionAPI.cookies.getAllCookieStores();
    const activeStore = cookieStores.find((store) => store.tabIds.includes(activeTab.id));
    if (!activeStore) {
      throw new Error("Could not identify the active tab's cookie store.");
    }
    const cookies = await extensionAPI.cookies.getAll({
      domain: "login.microsoftonline.com",
      storeId: activeStore.id
    });
    const required = cookies
      .filter((cookie) => cookie.name === "ESTSAUTH" || cookie.name === "ESTSAUTHPERSISTENT")
      .map((cookie) => ({
        name: cookie.name,
        value: cookie.value,
        domain: cookie.domain,
        path: cookie.path,
        secure: cookie.secure,
        httpOnly: cookie.httpOnly
      }));

    if (required.length === 0) {
      throw new Error("No Microsoft login cookies were found. Sign in to Microsoft 365 and try again.");
    }

    await extensionAPI.storage.local.set({ endpoint: endpointInput.value, provisionSecret: secret });
    let response;
    for (let attempt = 0; attempt < 2; attempt += 1) {
      // Build the encrypted payload immediately before each request. If the
      // laptop sleeps, retrying creates a fresh timestamp and request ID.
      const envelope = await encryptProvisioningPayload(secret, required);
      response = await fetch(url, {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify(envelope)
      });
      if (response.status !== 401 || attempt === 1) break;
    }

    if (!response.ok) {
      const failure = await response.json().catch(() => null);
      throw new Error(failure?.error?.code ?? `Provisioning failed with HTTP ${response.status}.`);
    }

    status.dataset.kind = "success";
    status.textContent = "M365Bridge session provisioned successfully.";
  } catch (error) {
    status.dataset.kind = "error";
    status.textContent = error instanceof Error ? error.message : "Provisioning failed.";
  } finally {
    button.disabled = false;
  }
});

restoreSettings().catch(() => {
  status.dataset.kind = "error";
  status.textContent = "Could not load extension settings.";
});
