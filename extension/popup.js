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

async function restoreSettings() {
  const saved = await extensionAPI.storage.local.get(["endpoint", "provisionSecret"]);
  if (saved.endpoint) endpointInput.value = saved.endpoint;
  if (saved.provisionSecret) secretInput.value = saved.provisionSecret;
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
    const cookies = await extensionAPI.cookies.getAll({ domain: "login.microsoftonline.com" });
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
    const response = await fetch(url, {
      method: "POST",
      headers: {
        "Authorization": `Bearer ${secret}`,
        "Content-Type": "application/json"
      },
      body: JSON.stringify({ cookies: required })
    });

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
