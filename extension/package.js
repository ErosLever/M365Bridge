const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");

const extensionDir = __dirname;
const outputRoot = path.join(extensionDir, "dist");
const defaultSecretPath = path.resolve(extensionDir, "../data/provision-secret");
const minimumSecretLength = 32;
const sharedFiles = ["popup.html", "popup.css", "popup.js"];
const targets = {
  chromium: "manifest.chromium.json",
  firefox: "manifest.firefox.json"
};

function readManifest(fileName) {
  const filePath = path.join(extensionDir, fileName);
  const manifest = JSON.parse(fs.readFileSync(filePath, "utf8"));

  if (manifest.manifest_version !== 3) {
    throw new Error(`${fileName}: manifest_version must be 3`);
  }
  if (manifest.action?.default_popup !== "popup.html") {
    throw new Error(`${fileName}: action.default_popup must be popup.html`);
  }

  const permissions = new Set(manifest.permissions ?? []);
  if (permissions.size !== 3 || !permissions.has("cookies") || !permissions.has("storage") || !permissions.has("tabs")) {
    throw new Error(`${fileName}: permissions must contain only cookies, storage, and tabs`);
  }

  return manifest;
}

function readSecretFile(filePath) {
  let secret;
  try {
    secret = fs.readFileSync(filePath, "utf8").trim();
  } catch (error) {
    throw new Error(`could not read provisioning secret file ${filePath}: ${error.message}`);
  }
  if (Buffer.byteLength(secret, "utf8") < minimumSecretLength) {
    throw new Error(`provisioning secret file ${filePath} must contain at least ${minimumSecretLength} bytes`);
  }
  return secret;
}

function createProvisionSecret(filePath) {
  fs.mkdirSync(path.dirname(filePath), { recursive: true, mode: 0o700 });
  const secret = crypto.randomBytes(24).toString("base64");
  let descriptor;
  try {
    descriptor = fs.openSync(filePath, "wx", 0o600);
    fs.writeFileSync(descriptor, `${secret}\n`, { encoding: "utf8" });
    fs.fsyncSync(descriptor);
  } catch (error) {
    if (descriptor !== undefined) {
      fs.closeSync(descriptor);
      fs.rmSync(filePath, { force: true });
    }
    if (error.code === "EEXIST") {
      throw new Error(`another process created provisioning secret file ${filePath} first`);
    }
    throw new Error(`could not create provisioning secret file ${filePath}: ${error.message}`);
  }
  fs.closeSync(descriptor);
  return secret;
}

function resolveProvisionSecret(environment = process.env, fallbackPath = defaultSecretPath, createIfMissing = false) {
  const configuredFile = environment.M365_PROVISION_SECRET_FILE?.trim();
  if (configuredFile) return readSecretFile(configuredFile);

  const configuredSecret = environment.M365_PROVISION_SECRET?.trim();
  if (configuredSecret) {
    if (Buffer.byteLength(configuredSecret, "utf8") < minimumSecretLength) {
      throw new Error(`M365_PROVISION_SECRET must contain at least ${minimumSecretLength} bytes`);
    }
    return configuredSecret;
  }

  if (!fs.existsSync(fallbackPath)) {
    if (createIfMissing) return createProvisionSecret(fallbackPath);
    throw new Error(
      `provisioning secret not found at ${fallbackPath}; configure M365_PROVISION_SECRET_FILE or M365_PROVISION_SECRET, or add --create-secret-if-missing`
    );
  }
  return readSecretFile(fallbackPath);
}

function packagedPopupHTML(source, embedSecret) {
  if (!embedSecret) return source;
  const script = '    <script src="config.js"></script>\n';
  const marker = '    <script src="popup.js"></script>';
  if (!source.includes(marker)) {
    throw new Error("popup.html must load popup.js");
  }
  return source.replace(marker, `${script}${marker}`);
}

function packageExtension(options = {}) {
  const arguments = process.argv.slice(2);
  const embedSecret = options.embedSecret ?? arguments.includes("--embed-provision-secret");
  const createIfMissing = options.createIfMissing ?? arguments.includes("--create-secret-if-missing");
  if (createIfMissing && !embedSecret) {
    throw new Error("--create-secret-if-missing requires --embed-provision-secret");
  }
  const secret = embedSecret
    ? resolveProvisionSecret(options.environment, options.defaultSecretPath, createIfMissing)
    : "";
  const popupHTML = packagedPopupHTML(
    fs.readFileSync(path.join(extensionDir, "popup.html"), "utf8"),
    embedSecret
  );

  for (const fileName of sharedFiles) {
    const filePath = path.join(extensionDir, fileName);
    if (!fs.statSync(filePath).isFile()) {
      throw new Error(`missing extension asset: ${fileName}`);
    }
  }

  // outputRoot may be a Docker bind-mount target, which cannot itself be
  // removed. Recreate each generated target below instead.
  fs.mkdirSync(outputRoot, { recursive: true });

  for (const [target, manifestFile] of Object.entries(targets)) {
    const outputDir = path.join(outputRoot, target);
    const manifest = readManifest(manifestFile);
    fs.rmSync(outputDir, { recursive: true, force: true });
    fs.mkdirSync(outputDir, { recursive: true });
    fs.writeFileSync(
      path.join(outputDir, "manifest.json"),
      `${JSON.stringify(manifest, null, 2)}\n`,
      { mode: 0o644 }
    );
    fs.writeFileSync(path.join(outputDir, "popup.html"), popupHTML, { mode: 0o644 });

    for (const fileName of sharedFiles.filter((fileName) => fileName !== "popup.html")) {
      fs.copyFileSync(path.join(extensionDir, fileName), path.join(outputDir, fileName));
    }
    if (embedSecret) {
      const config = `globalThis.M365BRIDGE_BUILD_CONFIG = Object.freeze({ provisionSecret: ${JSON.stringify(secret)} });\n`;
      fs.writeFileSync(path.join(outputDir, "config.js"), config, { mode: 0o600 });
    }

    console.log(`Packaged ${target}: ${path.relative(process.cwd(), outputDir)}`);
  }
}

if (require.main === module) packageExtension();

module.exports = { createProvisionSecret, packagedPopupHTML, packageExtension, readSecretFile, resolveProvisionSecret };
