import os
import pathlib
import shutil
import subprocess
import tempfile
import unittest


@unittest.skipUnless(os.name == "posix" and shutil.which("bash") and os.geteuid() != 0, "needs non-root Bash")
class BundleRebuildTests(unittest.TestCase):
    def test_launcher_uses_selected_python_and_stops_the_client(self):
        desktop = pathlib.Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            launcher = root / "start.sh"
            launcher.write_text((desktop / "scripts/bundle/templates/start.sh").read_text().replace("@SERVER@", "server"))
            server = root / "server"
            server.write_text('#!/usr/bin/env bash\n[[ "${1:-}" == "setup" ]] && exit 0\nfor i in {1..100}; do [[ -f agent-ready ]] && exit 0; sleep 0.02; done\nexit 1\n')
            server.chmod(0o755)
            python = root / "selected-python"
            python.write_text('#!/usr/bin/env bash\nprintf "%s\\n" "$*" "$PYTHONPATH" > agent-ready\nexec sleep 30\n')
            python.chmod(0o755)
            env = dict(os.environ, NEURO_AGENT_PYTHON=str(python), NEURO_NO_AGENT="0")
            result = subprocess.run(["bash", str(launcher)], env=env, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            invocation = (root / "agent-ready").read_text()
            self.assertIn("-m controller.agent --bridge 127.0.0.1:9876", invocation)
            self.assertIn(str(root / "agent/Lib"), invocation)

    def test_rebuilding_preserves_operator_state_and_stages_a_launcher(self):
        desktop = pathlib.Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            for relative in ["scripts/bundle/templates", "apps/neuro-integration/integration-docs", "apps/neuro-dashboard", "frontend/dist", "backend/python/controller", "config", "catalog/games", "tools"]:
                (root / relative).mkdir(parents=True)
            shutil.copyfile(desktop / "scripts/bundle/dev.sh", root / "scripts/bundle/dev.sh")
            shutil.copyfile(desktop / "scripts/bundle/prod.sh", root / "scripts/bundle/prod.sh")
            shutil.copyfile(desktop / "scripts/build-manager.py", root / "scripts/build-manager.py")
            shutil.copytree(desktop / "apps/neuro-manager", root / "apps/neuro-manager", ignore=shutil.ignore_patterns("__pycache__"))
            for name in ["start.sh", "start.bat", "README.txt"]:
                shutil.copyfile(desktop / "scripts/bundle/templates" / name, root / "scripts/bundle/templates" / name)
            for relative, contents in {
                "apps/neuro-integration/permissions.example.json": "example policy",
                "apps/neuro-integration/integration-docs/Action Script Documentation.md": "action reference",
                "backend/python/requirements.txt": "",
                "backend/python/controller/agent.py": "# staged agent",
                "frontend/dist/index.html": "dashboard",
                "catalog/index.json": "catalog",
                "marketplace-index.json": "marketplace snapshot",
                "catalog/extensions-state.json": "must not replace operator state",
                "catalog/extension-permissions.json": "must not copy source approvals",
                "catalog/integrations.json": "must not copy source credential hashes",
                "catalog/games/stock.json": "stock profile",
                "config/permissions.example.json": "example",
            }.items():
                (root / relative).write_text(contents)
            go = root / "tools/go"
            go.write_text('#!/usr/bin/env bash\nwhile [[ $# -gt 0 ]]; do\n if [[ "$1" == "-o" ]]; then shift; printf "#!/bin/sh\\nexit 0\\n" > "$1"; chmod +x "$1"; break; fi\n shift\ndone\n')
            go.chmod(0o755)
            npm = root / "tools/npm"
            npm.write_text("#!/bin/sh\nexit 0\n")
            npm.chmod(0o755)
            state = {
                "dashboard-token": "existing hash",
                "relay-token": "existing secret",
                "permissions.json": "operator policy",
                "catalog/extensions-state.json": "operator extensions",
                "catalog/extension-permissions.json": "per-extension approvals",
                "catalog/integrations.json": "private integration configuration",
                "catalog/games/custom.json": "custom profile",
                "catalog/games/stock.json": "edited stock profile",
                "config/permissions.example.json": "edited config",
                "plugins/custom/notes.txt": "extension data",
            }
            for bundle in ["dev", "neuro-desktop"]:
                for relative, contents in state.items():
                    path = root / "dist" / bundle / relative
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text(contents)
            env = dict(os.environ, PATH=str(root / "tools") + os.pathsep + os.environ["PATH"], NEURO_BUNDLE_NO_LAUNCH="1", NEURO_BUNDLE_SKIP_VENV="1", NEURO_BUNDLE_SKIP_CLIENT="1", NEURO_MANAGER_SOURCE_ONLY="1")
            env["NEURO_MARKETPLACE_SOURCE"] = str(root / "marketplace-index.json")
            for script, bundle in [("dev.sh", "dev"), ("prod.sh", "neuro-desktop")] * 2:
                result = subprocess.run(["bash", str(root / "scripts/bundle" / script)], cwd=root.parent, env=env, capture_output=True, text=True, timeout=20)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                for preserved in ["dev", "neuro-desktop"]:
                    for relative, contents in state.items():
                        self.assertEqual((root / "dist" / preserved / relative).read_text(), contents)
                self.assertFalse((root / "dist" / bundle / "catalog/catalog").exists())
                self.assertFalse((root / "dist" / bundle / "config/config").exists())
                launcher = root / "dist" / bundle / "start.sh"
                self.assertTrue(os.access(launcher, os.X_OK))
                self.assertNotIn("@SERVER@", launcher.read_text())
                self.assertTrue(os.access(root / "dist" / bundle / "manager/neuro-manager", os.X_OK))
                self.assertEqual((root / "dist" / bundle / "catalog/marketplace.json").read_text(), "marketplace snapshot")


if __name__ == "__main__":
    unittest.main()
