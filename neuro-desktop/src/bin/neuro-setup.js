#!/usr/bin/env node

// neuro-os bootstrapper: installs OCR deps, sets up nakurity-backend, runs launch
// Usage: node src/bin/neuro-setup.js [--run] [--setup-backend]

const { spawnSync } = require('child_process');
const path = require('path');
const fs = require('fs');

function run(cmd, args, opts = {}) {
  const res = spawnSync(cmd, args, { stdio: 'inherit', shell: process.platform === 'win32', ...opts });
  if (res.status !== 0 && !opts.ignoreError) {
    process.exit(res.status || 1);
  }
  return res;
}

function npmInstall(pkg) {
  console.log(`Installing ${pkg}...`);
  run(process.execPath, ['-m', 'pip', 'install', pkg], { ignoreError: true });
}

function setupNakurityBackend() {
  console.log('\n=== Setting up Nakurity Backend ===');
  const backendPath = path.join(__dirname, '..', '..', '..', 'nakurity-backend');
  
  if (!fs.existsSync(backendPath)) {
    console.log('Nakurity backend not found at:', backendPath);
    console.log('Skipping backend setup.');
    return;
  }
  
  console.log('Found nakurity-backend at:', backendPath);
  
  // Run nakurity setup
  const setupScript = path.join(backendPath, 'bin', 'setup.js');
  if (fs.existsSync(setupScript)) {
    console.log('Running nakurity-backend setup...');
    run('node', [setupScript, '--init'], { cwd: backendPath, ignoreError: true });
  }
  
  // Create vision API config in neuro-os
  const configPath = path.join(__dirname, '..', 'config', 'vision_api.yaml');
  const config = `# Vision API Configuration
vision_api:
  enabled: true
  endpoint: "https://backend.nakurity.com/neuro-os/vision"
  api_key: "${process.env.NEURO_OS_API_KEY || 'your-api-key-here'}"
  model: "llama-3.2-90b-vision-preview"
  rate_limit: 10  # requests per minute
`;
  
  fs.mkdirSync(path.dirname(configPath), { recursive: true });
  fs.writeFileSync(configPath, config);
  console.log('✓ Created vision API config');
}

function main() {
  const args = process.argv.slice(2);
  
  console.log('=== neuro-os setup ===');
  
  // Install Python OCR deps (EasyOCR, Pillow, opencv-python)
  console.log('\nInstalling Python dependencies...');
  const pyPkgs = ['easyocr', 'pillow', 'opencv-python', 'numpy', 'requests'];
  for (const p of pyPkgs) {
    try { npmInstall(p); } catch (_) {}
  }

  // Setup nakurity backend if requested
  if (args.includes('--setup-backend')) {
    setupNakurityBackend();
  }

  console.log('\n✓ Setup complete.');
  
  if (args.includes('--run')) {
    // Run the launcher
    console.log('\nStarting neuro-os...');
    run('python', ['-m', 'src.dev.launch'], { cwd: path.join(__dirname, '..', '..') });
  } else {
    console.log('\nNext steps:');
    console.log('  1. Setup backend: node src/bin/neuro-setup.js --setup-backend');
    console.log('  2. Run: node src/bin/neuro-setup.js --run');
    console.log('\nOr compile binary: npm install -g pkg && pkg src/bin/neuro-setup.js');
  }
}

main();
