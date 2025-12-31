import sys
from pathlib import Path
import json
import time

# Add project root to path
sys.path.insert(0, str(Path(__file__).parents[2]))

print("=== Smoke Tests: neuro-os ===")

# 1) Test NeuroMessageBuilder and types
print("\n[1] Testing NeuroMessageBuilder and types...")
try:
    from src.types.neuro_types import (
        NeuroMessageBuilder, DetailLevel,
        ScreenRegion, BoundingBox, SystemState, ContextData,
        ContextType, Priority
    )
    from datetime import datetime

    # Build a minimal fake state
    window = ScreenRegion(
        id="window_1",
        region_type=type('EnumVal', (), {'value': 'window'})(),  # placeholder; will be overwritten below
        bounds=BoundingBox(x=10, y=20, width=800, height=600),
        confidence=0.95,
        title="Test App",
        application="test.exe",
        clickable=True,
        focusable=True,
        metadata={"focused": True}
    )
    # Fix enum properly by importing RegionType
    from src.types.neuro_types import RegionType
    window.region_type = RegionType.WINDOW

    state = SystemState(
        active_application="test.exe",
        focused_region=window,
        all_regions=[window],
        context_data=[ContextData(
            context_type=ContextType.SYSTEM_STATE,
            timestamp=datetime.now(),
            data={"note": "smoke test"},
            confidence=0.9,
            source="smoke",
            priority=Priority.MEDIUM
        )],
        available_actions=[],
        timestamp=datetime.now()
    )

    builder = NeuroMessageBuilder(max_text_items=5, max_windows=3, detail_level=DetailLevel.MINIMAL)
    builder.update_state(state)
    msg = builder.build_context_message()
    print("- Context message length:", len(msg))
    assert isinstance(msg, str) and len(msg) > 0
    print("OK: NeuroMessageBuilder built a message.")
except Exception as e:
    print("FAIL: NeuroMessageBuilder test:", e)

# 2) Test RegionalizationCore pagination helpers import
print("\n[2] Testing RegionalizationCore pagination helpers...")
try:
    from src.regionalization.core import RegionalizationCore
    core = RegionalizationCore()
    # Should have new methods
    assert hasattr(core, 'get_ocr_elements_paginated')
    assert hasattr(core, 'get_windows_paginated')
    # Call with defaults (should not raise)
    ocr_page, ocr_total = core.get_ocr_elements_paginated()
    win_page, win_total = core.get_windows_paginated()
    print(f"- OCR page/items: {len(ocr_page)}/{ocr_total}")
    print(f"- Windows page/items: {len(win_page)}/{win_total}")
    print("OK: RegionalizationCore pagination helpers callable.")
except Exception as e:
    print("FAIL: RegionalizationCore pagination:", e)

# 3) Test Dashboard Flask routes with test client
print("\n[3] Testing Dashboard routes...")
try:
    from src.admin.dashboard import app
    client = app.test_client()

    # Stats before starting monitor
    resp = client.get('/api/relay/stats')
    print("- /api/relay/stats status:", resp.status_code)
    print("  body:", resp.json)

    # Start monitoring (will likely fail to connect if relay not running, but route should succeed)
    resp = client.post('/api/relay/start')
    print("- /api/relay/start status:", resp.status_code, resp.json)

    # Give the monitor thread a brief moment
    time.sleep(1.0)

    # Fetch connections and messages
    resp_conn = client.get('/api/relay/connections')
    resp_msgs = client.get('/api/relay/messages?limit=5')
    print("- /api/relay/connections:", resp_conn.status_code, resp_conn.json)
    print("- /api/relay/messages:", resp_msgs.status_code, resp_msgs.json)

    # Stop monitoring to cleanup
    resp = client.post('/api/relay/stop')
    print("- /api/relay/stop status:", resp.status_code, resp.json)

    # Render traffic page
    resp_page = client.get('/traffic')
    print("- /traffic page status:", resp_page.status_code, "html length:", len(resp_page.data))

    print("OK: Dashboard routes basic check done.")
except Exception as e:
    print("FAIL: Dashboard routes:", e)

# 4) Test pagination action schemas
print("\n[4] Testing pagination action schemas...")
try:
    from src.dev.integration.Actions.get_more_text import schema as get_more_text_schema
    from src.dev.integration.Actions.get_more_windows import schema as get_more_windows_schema
    from src.dev.integration.Actions.refresh_context import schema as refresh_context_schema
    from neuro_api.command import Action
    
    actions = [
        get_more_text_schema(),
        get_more_windows_schema(),
        refresh_context_schema()
    ]
    
    for action in actions:
        assert isinstance(action, Action)
        assert action.name
        assert action.description
        assert isinstance(action.schema, dict)
        assert action.schema.get('type') == 'object'
        assert 'properties' in action.schema
        print(f"- Action '{action.name}': OK ({len(action.schema['properties'])} properties)")
    
    print("OK: All pagination actions have valid schemas.")
except Exception as e:
    print("FAIL: Pagination action schemas:", e)
    import traceback
    traceback.print_exc()

print("\n=== Smoke Tests Complete ===")
