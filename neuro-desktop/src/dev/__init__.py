import asyncio
import time
import importlib

from pathlib import Path

# Import configuration loader
from nakuritycore.utils.config import get_config_loader

# Load configuration
PROJECT_ROOT = Path(__file__).resolve().parents[2]
config = get_config_loader( # Load configuration
    PROJECT_ROOT / "src" / "dev" / "config.yaml"
).config.get("debug", {})

# Set log path from config or default
log_config = config.get("logging", {})
LOG_PATH = Path(log_config.get("file", "logs/neuro_os_trace.log"))
LOG_PATH.parent.mkdir(parents=True, exist_ok=True)

start_time = time.perf_counter()

from nakuritycore.utils.logging import Logger
from nakuritycore.data.config import LoggingConfig

# Set up logging
logging_config = LoggingConfig(
    level=log_config.get("level", "INFO"),
    log_file=log_config.get("file", "")
)
logger = Logger("neuro-desktop-controller", logging_config)

from nakuritycore.utils import Tracer
from nakuritycore.data.config import TracerConfig

tracer_config = config.get("trace", {})
tracer_config = TracerConfig(
    include_paths=tracer_config.get("include_paths", ["neuro-desktop", "windows-api"]),
    exclude_functions=set(tracer_config.get("exclude_functions", ["write_log", "trace"])),
    events=set(tracer_config.get("events", ["call", "return", "exception"])),
)
tracer = Tracer(tracer_config)

async def start():
    taskslist = [] # Tasks list

    WindowsAPIServer = getattr(importlib.import_module("windows-api"), "WindowsAPIServer")
    taskslist.append(asyncio.create_task( # Launch Windows API server
        WindowsAPIServer( # Windows API
            Path( # Load configuration
                PROJECT_ROOT.parent / "windows-api" / "src" / "resources" / "authentication.yaml"
            ).resolve()
        ).start()
    ))

    from .integration import NeuroClient
    windows_api_config = get_config_loader(
        PROJECT_ROOT.parent / "windows-api" / "src" / "resources" / "authentication.yaml"
    ).config
    neuro_desktop_config = get_config_loader(
        PROJECT_ROOT / "src" / "resources" / "authentication.yaml"
    ).config.get("neuro_backend", {})
    taskslist.append(asyncio.create_task( # Launch client
        NeuroClient.start( # give neuro client windows-api's uri
            f"ws://{windows_api_config.get('host', '127.0.0.1')}:{windows_api_config.get('port', 8765)}",
            f"ws://{neuro_desktop_config.get('host', '127.0.0.1')}:{neuro_desktop_config.get('port', 8766)}"
        )
    ))

    try:
        asyncio.gather(*taskslist)
    except KeyboardInterrupt:
        print("Server stopped.")
    finally:
        print("[SHUTDOWN] Cleanup complete")

__all__ = ["start"]