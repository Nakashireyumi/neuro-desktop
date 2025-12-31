# todo: make this export and initialoze the action schema for the neuro-client
import os
import importlib
from pathlib import Path

from neuro_api.command import Action
import importlib.util

ACTIONS_DIR = Path(__file__).parent / "Actions"

def load_actions():
  # Load all Action definitions dynamically
  actions = []
  for file in os.listdir(ACTIONS_DIR):
      if not file.endswith(".py") or file.startswith("__"):
          continue

      file_path = os.path.join(ACTIONS_DIR, file)
      spec = importlib.util.spec_from_file_location(file[:-3], file_path)
      module = importlib.util.module_from_spec(spec)
      spec.loader.exec_module(module)

      if hasattr(module, "schema"):
          act = module.schema()
          # Validate action is correctly formatted
          if isinstance(act, Action):
              actions.append(act)
              print(f"[ACTION] Loaded: {act.name}")
          else:
              print(f"[ACTION] Error: {file} did not return a valid Action")

  return actions

from .client import NeuroClient

__all__ = ["NeuroClient", "load_actions"]