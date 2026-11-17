#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const os = require('os');
const https = require('https');
const { execSync } = require('child_process');

const pkg = require('./package.json');
const VERSION = pkg.version;
const REPO = 'gek0v/milanuncios-cli';

function getPlatform() {
  const p = os.platform();
  if (p === 'win32') return 'windows';
  if (p === 'darwin') return 'darwin';
  if (p === 'linux') return 'linux';
  throw new Error(`Unsupported platform: ${p}`);
}

function getArch() {
  const a = os.arch();
  if (a === 'x64') return 'amd64';
  if (a === 'arm64') return 'arm64';
  throw new Error(`Unsupported architecture: ${a}`);
}

function download(url, dest) {
  return new Promise((resolve, reject) => {
    https.get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return download(res.headers.location, dest).then(resolve).catch(reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`Failed to download ${url}: HTTP ${res.statusCode}`));
      }
      const file = fs.createWriteStream(dest);
      res.pipe(file);
      file.on('finish', () => file.close(resolve));
      file.on('error', (err) => {
        fs.unlink(dest, () => reject(err));
      });
    }).on('error', reject);
  });
}

async function install() {
  let platform, arch;
  try {
    platform = getPlatform();
    arch = getArch();
  } catch (err) {
    console.warn(`[milanuncios-cli] ${err.message}`);
    return;
  }

  const isWindows = platform === 'windows';
  const ext = isWindows ? 'zip' : 'tar.gz';
  const archiveName = `milanuncios-cli_${VERSION}_${platform}_${arch}.${ext}`;
  const url = `https://github.com/${REPO}/releases/download/v${VERSION}/${archiveName}`;

  const binDir = path.join(__dirname, 'bin');
  if (!fs.existsSync(binDir)) {
    fs.mkdirSync(binDir, { recursive: true });
  }

  const binaryName = isWindows ? 'milanuncios.exe' : 'milanuncios';
  const binaryPath = path.join(binDir, binaryName);

  if (fs.existsSync(binaryPath)) {
    return;
  }

  const tmpArchive = path.join(binDir, archiveName);
  console.log(`[milanuncios-cli] Downloading native binary from ${url}...`);

  try {
    await download(url, tmpArchive);
    console.log(`[milanuncios-cli] Extracting ${archiveName}...`);

    if (isWindows) {
      execSync(`tar -xf "${tmpArchive}" -C "${binDir}" milanuncios.exe`, { stdio: 'ignore' });
    } else {
      execSync(`tar -xzf "${tmpArchive}" -C "${binDir}" milanuncios`, { stdio: 'ignore' });
      fs.chmodSync(binaryPath, 0o755);
    }
    try {
      fs.unlinkSync(tmpArchive);
    } catch {}
    console.log(`[milanuncios-cli] Successfully installed ${binaryName}`);
  } catch (err) {
    console.warn(`[milanuncios-cli] Download notice: ${err.message}`);
    console.warn(`[milanuncios-cli] The binary will be downloaded upon first execution, or you can compile it from source via Go.`);
    try {
      if (fs.existsSync(tmpArchive)) fs.unlinkSync(tmpArchive);
    } catch {}
  }
}

if (require.main === module) {
  install().catch((err) => {
    console.warn(`[milanuncios-cli] Install warning: ${err.message}`);
  });
}

module.exports = { install };
