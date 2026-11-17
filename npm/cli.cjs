#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const os = require('os');
const { spawnSync } = require('child_process');
const { install } = require('./install.cjs');

async function main() {
  const isWindows = os.platform() === 'win32';
  const binaryName = isWindows ? 'milanuncios.exe' : 'milanuncios';
  const binDir = path.join(__dirname, 'bin');
  const binaryPath = path.join(binDir, binaryName);

  if (!fs.existsSync(binaryPath)) {
    try {
      await install();
    } catch {}
  }

  if (!fs.existsSync(binaryPath)) {
    console.error(`[milanuncios-cli] Error: Native binary could not be found or downloaded.`);
    console.error(`Please install with Go: go install github.com/gek0v/milanuncios-cli/cmd/milanuncios@latest`);
    process.exit(1);
  }

  const result = spawnSync(binaryPath, process.argv.slice(2), {
    stdio: 'inherit',
    windowsHide: false
  });

  if (result.error) {
    console.error(`[milanuncios-cli] Execution error: ${result.error.message}`);
    process.exit(1);
  }

  process.exit(result.status !== null ? result.status : 0);
}

main().catch((err) => {
  console.error(`[milanuncios-cli] Unexpected error: ${err.message}`);
  process.exit(1);
});
