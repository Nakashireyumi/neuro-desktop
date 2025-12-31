const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

// --------------- CONFIG ----------------
const projectRoot = process.cwd();
const venvDir = path.join(projectRoot, ".venv");

const winApiRepoDir = path.join(projectRoot, "windows-api");
const winApiRepoUrl = "https://github.com/Nakashireyumi/windows-api.git";
const winApiRequirements = path.join(
  winApiRepoDir,
  "src",
  "contributions",
  "cassitly",
  "python",
  "requirements.txt"
);

const neuroOSRepoDir = path.join(projectRoot, "neuro-os");
const neuroOSRepoUrl = "https://github.com/Nakashireyumi/neuro-os.git";
const neuroOSRequirements = path.join(neuroOSRepoDir, "requirements.txt");

const mainScript = path.join(projectRoot, "neuro-os", "src", "dev", "launch.py");

// --------------- HELPERS ----------------
function run(command, args = [], options = {}) {
  return new Promise((resolve, reject) => {
    const proc = spawn(command, args, {
      stdio: "inherit",
      shell: true,
      ...options,
    });
    proc.on("close", (code) => {
      if (code !== 0) return reject(new Error(`${command} exited with ${code}`));
      resolve();
    });
  });
}

function existsSync(p) {
  try { fs.accessSync(p); return true; } catch { return false; }
}

// --------------- CORE LOGIC ----------------
async function ensurePython() {
  console.log("[CHECK] Checking for Python...");
  try {
    await run("python", ["--version"]);
    return "python";
  } catch {
    const embedded = path.join(projectRoot, "bin", "winpython", "python.exe");
    if (existsSync(embedded)) {
      console.log("[SETUP] Using bundled WinPython...");
      return embedded;
    }
    throw new Error("No system or embedded Python found.");
  }
}

async function ensureGit() {
  console.log("[CHECK] Checking for Git...");
  try {
    await run("git", ["--version"]);
    return "git";
  } catch {
    const embedded = path.join(projectRoot, "bin", "git-portable", "bin", "git.exe");
    if (existsSync(embedded)) {
      console.log("[SETUP] Using bundled Git Portable...");
      return embedded;
    }
    throw new Error("No system or embedded Git found.");
  }
}

async function ensureWindowsApi(gitCmd) {
  if (!fs.existsSync(winApiRepoDir)) {
    console.log("[SETUP] Cloning windows-api...");
    await run(gitCmd, ["clone", winApiRepoUrl]);
  } else {
    console.log("[UPDATE] windows-api already exists. Pulling latest...");
    await run(gitCmd, ["-C", winApiRepoDir, "pull"]);
  }
}

async function ensureNeuroOS(gitCmd) {
  if (!fs.existsSync(neuroOSRepoDir)) {
    console.log("[SETUP] Cloning neuro-os...");
    await run(gitCmd, ["clone", neuroOSRepoUrl]);
  } else {
    console.log("[UPDATE] neuro-os already exists. Pulling latest...");
    await run(gitCmd, ["-C", neuroOSRepoUrl, "pull"]);
  }
}

async function ensureVenv(pythonCmd) {
  if (!fs.existsSync(venvDir)) {
    console.log("[SETUP] Creating virtual environment...");
    await run(pythonCmd, ["-m", "venv", ".venv"]);
  } else {
    console.log("[SETUP] Virtual environment already exists.");
  }
}

function getVenvPython() {
  return process.platform === "win32"
    ? path.join(venvDir, "Scripts", "python.exe")
    : path.join(venvDir, "bin", "python");
}

function getVenvPip() {
  return process.platform === "win32"
    ? path.join(venvDir, "Scripts", "pip.exe")
    : path.join(venvDir, "bin", "pip");
}

async function installDependencies(pythonCmd) {
  console.log("[INSTALL] Installing Python dependencies...");

  // Prefer pip from the venv if it exists, else use provided pythonCmd
  const pipPath = getVenvPip();

  if (existsSync(pipPath)) {
    await run(pipPath, ["install", "-r", winApiRequirements]);
    await run(pipPath, ["install", "-r", neuroOSRequirements]);
  } else {
    console.log("[WARN] No venv pip found yet, using system/embedded Python to install...");
    await run(pythonCmd, ["-m", "pip", "install", "-r", winApiRequirements]);
    await run(pythonCmd, ["-m", "pip", "install", "-r", neuroOSRequirements]);
  }
}

async function launchMain(pythonCmd) {
  console.log("[LAUNCH] Running main Python file...");
  const pyPath = getVenvPython();

  const execPython = existsSync(pyPath) ? pyPath : pythonCmd;
  await run(execPython, ["-m", mainScript]);
}

// --------------- MAIN FLOW ----------------
(async () => {
  try {
    const pythonCmd = await ensurePython();
    const gitCmd = await ensureGit();
    await ensureNeuroOS(gitCmd);
    await ensureWindowsApi(gitCmd);
    await ensureVenv(pythonCmd);
    await installDependencies(pythonCmd);
    await launchMain(pythonCmd);
    console.log("[DONE] Everything launched successfully!");
  } catch (err) {
    console.error("[ERROR]", err.message);
    process.exit(1);
  }
})();
