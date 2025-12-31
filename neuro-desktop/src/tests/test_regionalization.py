"""
Test script for the regionalization system
Tests all major components with proper error handling
"""

import asyncio
import logging
import sys
from pathlib import Path

# Add src to path for imports
sys.path.insert(0, str(Path(__file__).parent / "src"))

from src.regionalization.core import RegionalizationCore, WindowDetector
from src.types.neuro_types import PluginRegistry

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)

async def test_regionalization():
    """Test the regionalization system components"""
    
    try:
        logger.info("Starting regionalization system test...")
        
        # Test window detection
        logger.info("Testing window detection...")
        window_detector = WindowDetector()
        
        # Test getting active application
        active_app = await window_detector.get_active_application()
        logger.info(f"Active application: {active_app}")
        
        # Test detecting windows
        windows = await window_detector.detect_windows()
        logger.info(f"Detected {len(windows)} windows")
        for window in windows[:5]:  # Show first 5
            logger.info(f"  - {window.title} ({window.application}) - {window.bounds}")
        
        # Test getting focused window
        focused = await window_detector.get_focused_window()
        if focused:
            logger.info(f"Focused window: {focused.title} ({focused.application})")
        
        # Test full regionalization core
        logger.info("Testing regionalization core...")
        core = RegionalizationCore()
        
        # Force update system state
        await core.force_update()
        
        # Get current state
        state = core.get_current_state()
        if state:
            logger.info(f"System state:")
            logger.info(f"  Active app: {state.active_application}")
            logger.info(f"  Total regions: {len(state.all_regions)}")
            logger.info(f"  Context items: {len(state.context_data)}")
            logger.info(f"  Available actions: {len(state.available_actions)}")
            
            # Show context message
            context_msg = core.get_context_message()
            logger.info(f"Context message length: {len(context_msg)}")
        
        logger.info("Regionalization system test completed successfully!")
        
    except ImportError as e:
        logger.warning(f"Import error (expected on systems without win32gui): {e}")
        logger.info("Regionalization system would run with fallbacks in production")
        
    except Exception as e:
        logger.error(f"Test failed with error: {e}")
        import traceback
        traceback.print_exc()

if __name__ == "__main__":
    asyncio.run(test_regionalization())