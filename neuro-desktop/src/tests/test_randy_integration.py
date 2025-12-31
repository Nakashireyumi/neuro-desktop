"""
Test script to send regionalization context to Randy development server
"""

import asyncio
import websockets
import json
import logging
import sys
from pathlib import Path

# Add src to path for imports
sys.path.insert(0, str(Path(__file__).parent / "src"))

from src.regionalization.core import RegionalizationCore

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

async def test_randy_integration():
    """Test sending regionalization context to Randy"""
    
    try:
        logger.info("Starting regionalization system...")
        
        # Initialize regionalization core
        core = RegionalizationCore()
        
        # Update system state
        await core.force_update()
        state = core.get_current_state()
        
        if not state:
            logger.warning("No system state available")
            return
            
        # Build context message
        context_message = core.get_context_message()
        logger.info(f"Generated context message: {len(context_message)} characters")
        
        # Create message for Randy
        randy_message = {
            "type": "context_update",
            "timestamp": state.timestamp.isoformat(),
            "data": {
                "active_application": state.active_application,
                "total_regions": len(state.all_regions),
                "focused_region": {
                    "title": state.focused_region.title if state.focused_region else None,
                    "type": state.focused_region.region_type.value if state.focused_region else None,
                    "bounds": {
                        "x": state.focused_region.bounds.x if state.focused_region else 0,
                        "y": state.focused_region.bounds.y if state.focused_region else 0,
                        "width": state.focused_region.bounds.width if state.focused_region else 0,
                        "height": state.focused_region.bounds.height if state.focused_region else 0
                    } if state.focused_region else None
                },
                "available_actions_count": len(state.available_actions),
                "context_summary": context_message
            }
        }
        
        logger.info("Attempting to connect to Randy on ws://127.0.0.1:8000...")
        
        # Connect to Randy
        try:
            async with websockets.connect("ws://127.0.0.1:8000") as websocket:
                logger.info("Connected to Randy successfully!")
                
                # Send context message
                await websocket.send(json.dumps(randy_message))
                logger.info("Context message sent to Randy")
                
                # Wait for response
                try:
                    response = await asyncio.wait_for(websocket.recv(), timeout=5.0)
                    logger.info(f"Received response from Randy: {response}")
                except asyncio.TimeoutError:
                    logger.info("No response from Randy (this is normal for a development server)")
                
        except ConnectionRefusedError:
            logger.error("Could not connect to Randy on port 8000. Is Randy running?")
        except Exception as e:
            logger.error(f"Error connecting to Randy: {e}")
            
    except Exception as e:
        logger.error(f"Test failed: {e}")
        import traceback
        traceback.print_exc()

if __name__ == "__main__":
    asyncio.run(test_randy_integration())