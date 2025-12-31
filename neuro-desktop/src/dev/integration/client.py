import json
import asyncio
import websockets

from neuro_api.api import AbstractNeuroAPI, NeuroAction
from neuro_api.command import *
from . import load_actions

# Regionalization context provider
try:
    from src.dev.integration.regionalization.core import RegionalizationCore
except Exception:
    RegionalizationCore = None

# -------- Neurosama / Neuro-API integration --------

class NeuroClient(AbstractNeuroAPI):
    """
    Subclassing the SDK’s WebSocket client, handling incoming action requests from Neuro,
    then forwarding them to Windows via your WebSocket interface.
    """
    def __init__(self, websocket):
        self.websocket = websocket
        self.name = "Neuro's Desktop"
        self._context_task = None
        self._reg = RegionalizationCore() if RegionalizationCore else None
        self.windows_api_uri = "ws://127.0.0.1:8765"
        self.auth_token = "super-secret-token"
        self._action_in_progress = False
        super().__init__(self.name)

    async def write_to_websocket(self, data: str) -> None:
        await self.websocket.send(data)

    async def read_from_websocket(self) -> str:
        return await self.websocket.recv()

    async def initialize(self):
        # Send startup command
        await self.send_startup_command()

        # Register actions
        actions = load_actions()

        await self.register_actions(actions)

        # Send a one-time capability/context primer so Neuro understands this game
        try:
            action_names = [a.name for a in load_actions()]
            primer = (
                f"Neuro-OS Windows integration is active. I can control the Windows UI via actions: "
                f"{', '.join(action_names)}. "
                f"I will periodically send summaries of the current screen, focused window, and available UI targets."
            )
            await self.send_context(primer, silent=True)
        except Exception:
            pass

        # Start periodic context publishing to the Neuro backend (rich summaries)
        if self._reg and self._context_task is None:
            self._context_task = asyncio.create_task(self._publish_context_loop())

    async def handle_action(self, action: NeuroAction):
        """
        This is called by the SDK when Neuro-sama requests an action.
        e.g. action.name might be "move", "click", etc.
        action.data is a dict of parameters.
        """
        name = action.name
        # Safely parse params
        try:
            params = json.loads(action.data) if action.data else {}
        except Exception:
            params = {}
        print(f"[NEURO] Received action: {name}, params: {params}")

        # Handle context_update specially
        if name == "context_update":
            await self.send_action_result(action.id_, True, "context_update received by neuro-os")
            return

        # Execute the Windows action and wait for actual result
        self._action_in_progress = True
        try:
            success, message = await self._execute_windows_action(name, params)
            await self.send_action_result(action.id_, success, message)
        except Exception as e:
            await self.send_action_result(action.id_, False, f"Error: {str(e)}")
        finally:
            # We are no longer blocking Neuro; allow context publishing again
            self._action_in_progress = False

    async def on_connect(self):
        print("[NEURO] Connected to Neuro API")

    async def on_disconnect(self):
        print("[NEURO] Disconnected from Neuro API")

    async def _execute_windows_action(self, name: str, params: dict) -> tuple[bool, str]:
        """Execute a Windows action and return (success, message)."""
        try:
            # Fill defaults for click if coordinates are missing
            if name == "click" and ("x" not in params or "y" not in params):
                try:
                    import pyautogui
                    x, y = pyautogui.position()
                    params = {**params, "x": int(x), "y": int(y)}
                except Exception:
                    pass

            async with websockets.connect(self.windows_api_uri) as ws:
                msg = {"token": self.auth_token, "action": name, **params}
                await ws.send(json.dumps(msg))
                resp_text = await ws.recv()
                
                # Parse response
                try:
                    resp = json.loads(resp_text)
                    if resp.get("status") == "ok":
                        result = resp.get("result", {})
                        return True, f"Action '{name}' completed: {json.dumps(result)}"
                    elif resp.get("status") == "error":
                        error = resp.get("error", {})
                        error_msg = error.get("message", "Unknown error")
                        return False, f"Action '{name}' failed: {error_msg}"
                    else:
                        return True, f"Action '{name}' completed with unknown status"
                except json.JSONDecodeError:
                    return True, f"Action '{name}' completed (response: {resp_text[:100]})"
                    
        except Exception as e:
            print(f"[WINERR] {e}")
            return False, f"Failed to execute action: {str(e)}"

    async def _publish_context_once(self):
        if not self._reg:
            print("[CONTEXT] Regionalization not available")
            return
        # Do not send context while Neuro is waiting for an action result
        if getattr(self, "_action_in_progress", False):
            print("[CONTEXT] Skipping - action in progress")
            return
        try:
            print("[CONTEXT] Updating regionalization state...")
            await self._reg.force_update()
            state = self._reg.get_current_state()
            if not state:
                print("[CONTEXT] No state available")
                return
            context_msg = self._reg.get_context_message()
            print(f"[CONTEXT] Sending context: {context_msg[:200]}...")
            # Use SDK "context" command (proper protocol) so backend accepts it
            await self.send_context(context_msg, silent=True)
            print("[CONTEXT] Context sent successfully")
        except Exception as e:
            print(f"[CONTEXT_ERR] Failed to publish context: {e}")
            import traceback
            traceback.print_exc()

    async def _publish_context_loop(self):
        print("[CONTEXT] Starting context publishing loop (2s check, send on change)")
        last_context_msg = None
        
        while True:
            # Check state more frequently but only send if changed
            if not self._reg:
                await asyncio.sleep(5)
                continue
                
            if getattr(self, "_action_in_progress", False):
                await asyncio.sleep(2)
                continue
                
            try:
                await self._reg.force_update()
                state = self._reg.get_current_state()
                if not state:
                    await asyncio.sleep(2)
                    continue
                    
                context_msg = self._reg.get_context_message()
                
                # Only send if context actually changed
                if context_msg != last_context_msg:
                    print(f"[CONTEXT] State changed, sending update")
                    await self.send_context(context_msg, silent=True)
                    last_context_msg = context_msg
                    print("[CONTEXT] Context sent successfully")
                else:
                    print("[CONTEXT] No change, skipping update")
                    
            except Exception as e:
                print(f"[CONTEXT_ERR] Failed to publish context: {e}")
                import traceback
                traceback.print_exc()
                
            await asyncio.sleep(2)  # Check every 2 seconds
    
    @staticmethod
    async def start(
        windows_api_uri = "ws://127.0.0.1:8765",
        neuro_backend_uri = "ws://127.0.0.1:8000",
        auth_token = "super-secret-token"
    ):
        async with websockets.connect(neuro_backend_uri) as websocket:
            client = NeuroClient(websocket)

            # Set up client
            client.windows_api_uri = windows_api_uri
            client.auth_token = auth_token
            await client.initialize()

            # Read messages in a loop
            while True:
                try:
                    await client.read_message()
                except websockets.exceptions.ConnectionClosed:
                    break
