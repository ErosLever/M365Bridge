const fs = require("node:fs");
const path = require("node:path");

const extensionDir = __dirname;
const outputRoot = path.join(extensionDir, "dist");
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
  if (permissions.size !== 2 || !permissions.has("cookies") || !permissions.has("storage")) {
    throw new Error(`${fileName}: permissions must contain only cookies and storage`);
  }

  return manifest;
}

for (const fileName of sharedFiles) {
  const filePath = path.join(extensionDir, fileName);
  if (!fs.statSync(filePath).isFile()) {
    throw new Error(`missing extension asset: ${fileName}`);
  }
}

fs.rmSync(outputRoot, { recursive: true, force: true });

for (const [target, manifestFile] of Object.entries(targets)) {
  const outputDir = path.join(outputRoot, target);
  const manifest = readManifest(manifestFile);
  fs.mkdirSync(outputDir, { recursive: true });
  fs.writeFileSync(
    path.join(outputDir, "manifest.json"),
    `${JSON.stringify(manifest, null, 2)}\n`,
    { mode: 0o644 }
  );

  for (const fileName of sharedFiles) {
    fs.copyFileSync(path.join(extensionDir, fileName), path.join(outputDir, fileName));
  }

  console.log(`Packaged ${target}: ${path.relative(process.cwd(), outputDir)}`);
}
