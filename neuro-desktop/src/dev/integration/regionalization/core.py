"""
Core regionalization system for Neuro-OS
Detects and manages screen regions, provides context to Neuro
"""

import asyncio
import logging
from typing import Dict, List, Optional, Set
from datetime import datetime

# Handle missing dependencies gracefully
try:
    import psutil
    import win32gui
    import win32process
    import win32api
    import pyautogui
except ImportError as e:
    logging.warning(f"Some dependencies not available: {e}")
    psutil = None
    win32gui = None
    win32process = None
    win32api = None
    pyautogui = None

from pathlib import Path

try:
    from .ocr_detector import OCRDetector
except ImportError:
    OCRDetector = None

try:
    from .vision_api_client import VisionAPIClient
except ImportError:
    VisionAPIClient = None

from ..types.neuro_types import (
    ScreenRegion, RegionType, BoundingBox, Coordinates,
    ContextData, ContextType, SystemState, NeuroAction,
    PluginRegistry, PluginType, Priority, NeuroMessageBuilder
)

logger = logging.getLogger(__name__)

class WindowDetector:
    """Detects windows and basic UI regions using Windows API"""
    
    def __init__(self):
        self.cached_windows = {}
        self.last_update = None
        self.available = win32gui is not None
    
    async def get_active_application(self) -> Optional[str]:
        """Get the currently active application"""
        if not self.available:
            return None
            
        try:
            hwnd = win32gui.GetForegroundWindow()
            if not hwnd:
                return None
                
            try:
                _, pid = win32process.GetWindowThreadProcessId(hwnd)
            except Exception:
                return None
                
            try:
                process = psutil.Process(pid)
                return process.name()
            except Exception:
                return None
                
        except Exception as e:
            logger.warning(f"Failed to get active application: {e}")
            return None
    
    async def detect_windows(self) -> List[ScreenRegion]:
        """Detect all visible windows"""
        if not self.available:
            return []
            
        regions = []
        
        def enum_windows_proc(hwnd, _):
            try:
                if not win32gui.IsWindowVisible(hwnd):
                    return True
            except Exception:
                return True
                
            try:
                # Get window rect with error handling
                try:
                    rect = win32gui.GetWindowRect(hwnd)
                except Exception:
                    return True
                    
                if rect[2] - rect[0] < 10 or rect[3] - rect[1] < 10:  # Skip tiny windows
                    return True
                
                # Get window title with error handling
                try:
                    title = win32gui.GetWindowText(hwnd)
                except Exception:
                    title = "Unknown Window"
                    
                if not title:
                    return True
                
                # Get process name with error handling
                try:
                    _, pid = win32process.GetWindowThreadProcessId(hwnd)
                    process = psutil.Process(pid)
                    app_name = process.name()
                except Exception:
                    app_name = "Unknown"
                
                # Create screen region with validation
                x, y = rect[0], rect[1]
                width, height = rect[2] - rect[0], rect[3] - rect[1]
                
                # Skip invalid windows
                if width <= 0 or height <= 0:
                    return True
                
                bounds = BoundingBox(x=x, y=y, width=width, height=height)
                
                region = ScreenRegion(
                    id=f"window_{hwnd}",
                    region_type=RegionType.WINDOW,
                    bounds=bounds,
                    confidence=0.9,
                    title=title,
                    application=app_name,
                    clickable=True,
                    focusable=True,
                    metadata={"hwnd": hwnd, "pid": pid}
                )
                
                regions.append(region)
                
            except Exception as e:
                logger.debug(f"Error processing window {hwnd}: {e}")
                
            return True
        
        try:
            win32gui.EnumWindows(enum_windows_proc, None)
        except Exception as e:
            logger.error(f"Error enumerating windows: {e}")
            
        return regions
    
    async def get_focused_window(self) -> Optional[ScreenRegion]:
        """Get the currently focused window region"""
        if not self.available:
            return None
            
        try:
            hwnd = win32gui.GetForegroundWindow()
            if not hwnd:
                return None
                
            # Get window rect and title with error handling
            try:
                rect = win32gui.GetWindowRect(hwnd)
            except Exception:
                return None
                
            try:
                title = win32gui.GetWindowText(hwnd)
            except Exception:
                title = "Unknown Window"
            
            # Get process info with error handling
            try:
                _, pid = win32process.GetWindowThreadProcessId(hwnd)
                process = psutil.Process(pid)
                app_name = process.name()
            except Exception:
                app_name = "Unknown"
            
            bounds = BoundingBox(
                x=rect[0],
                y=rect[1],
                width=rect[2] - rect[0], 
                height=rect[3] - rect[1]
            )
            
            return ScreenRegion(
                id=f"focused_window_{hwnd}",
                region_type=RegionType.WINDOW,
                bounds=bounds,
                confidence=1.0,
                title=title,
                application=app_name,
                clickable=True,
                focusable=True,
                metadata={"hwnd": hwnd, "pid": pid, "focused": True}
            )
            
        except Exception as e:
            logger.error(f"Error getting focused window: {e}")
            return None

class BasicRegionDetector:
    """Basic region detector using simple heuristics"""
    
    def __init__(self):
        self.common_button_sizes = [(75, 23), (100, 32), (120, 30)]
        self.common_input_heights = [20, 23, 25, 30]
    
    async def detect_ui_regions(self, screenshot: bytes, window_region: ScreenRegion) -> List[ScreenRegion]:
        """Detect basic UI regions within a window"""
        regions = []
        
        # For now, we'll create some basic regions based on common patterns
        window_bounds = window_region.bounds
        
        # Title bar region (top of window)
        title_bar = ScreenRegion(
            id=f"{window_region.id}_titlebar",
            region_type=RegionType.TOOLBAR,
            bounds=BoundingBox(
                x=window_bounds.x,
                y=window_bounds.y,
                width=window_bounds.width,
                height=30  # Standard title bar height
            ),
            confidence=0.8,
            title="Title Bar",
            application=window_region.application,
            clickable=True,
            parent_id=window_region.id
        )
        regions.append(title_bar)
        
        # Content area (below title bar)
        content_area = ScreenRegion(
            id=f"{window_region.id}_content",
            region_type=RegionType.CONTENT_AREA,
            bounds=BoundingBox(
                x=window_bounds.x,
                y=window_bounds.y + 30,
                width=window_bounds.width,
                height=window_bounds.height - 30
            ),
            confidence=0.7,
            title="Content Area",
            application=window_region.application,
            clickable=True,
            parent_id=window_region.id
        )
        regions.append(content_area)
        
        return regions

class ContextExtractor:
    """Extracts context information from the system state"""
    
    def __init__(self):
        self.application_contexts = {}
    
    async def extract_visual_context(self, screenshot: bytes, regions: List[ScreenRegion]) -> List[ContextData]:
        """Extract visual context from screenshot and regions"""
        context_list = []
        
        # Basic visual statistics
        visual_data = {
            "total_regions": len(regions),
            "region_types": {},
            "clickable_regions": 0,
            "largest_region": None,
            "screenshot_size": len(screenshot) if screenshot else 0
        }
        
        largest_area = 0
        for region in regions:
            region_type = region.region_type.value
            visual_data["region_types"][region_type] = visual_data["region_types"].get(region_type, 0) + 1
            
            if region.clickable:
                visual_data["clickable_regions"] += 1
                
            if region.bounds.area > largest_area:
                largest_area = region.bounds.area
                visual_data["largest_region"] = {
                    "type": region_type,
                    "title": region.title,
                    "area": region.bounds.area
                }
        
        context = ContextData(
            context_type=ContextType.VISUAL,
            timestamp=datetime.now(),
            data=visual_data,
            confidence=0.8,
            source="ContextExtractor",
            priority=Priority.MEDIUM
        )
        context_list.append(context)
        
        return context_list
    
    async def extract_interactive_context(self, regions: List[ScreenRegion]) -> List[ContextData]:
        """Extract interactive context from regions"""
        interactive_data = {
            "available_actions": [],
            "interaction_points": [],
            "focus_candidates": []
        }
        
        for region in regions:
            if region.clickable:
                interactive_data["available_actions"].append({
                    "action": "click",
                    "target": region.id,
                    "description": f"Click on {region.title or region.region_type.value}",
                    "confidence": region.confidence
                })
                
                interactive_data["interaction_points"].append({
                    "x": region.bounds.center.x,
                    "y": region.bounds.center.y,
                    "type": "click",
                    "region_id": region.id
                })
            
            if region.focusable:
                interactive_data["focus_candidates"].append({
                    "region_id": region.id,
                    "type": region.region_type.value,
                    "title": region.title
                })
        
        context = ContextData(
            context_type=ContextType.INTERACTIVE,
            timestamp=datetime.now(),
            data=interactive_data,
            confidence=0.9,
            source="ContextExtractor",
            priority=Priority.HIGH
        )
        
        return [context]
    
    async def extract_application_context(self, app_name: str, focused_region: Optional[ScreenRegion]) -> List[ContextData]:
        """Extract application-specific context"""
        app_data = {
            "application": app_name,
            "is_known_app": self._is_known_application(app_name),
            "focused_element": None,
            "suggested_actions": []
        }
        
        if focused_region:
            app_data["focused_element"] = {
                "type": focused_region.region_type.value,
                "title": focused_region.title,
                "bounds": {
                    "x": focused_region.bounds.x,
                    "y": focused_region.bounds.y,
                    "width": focused_region.bounds.width,
                    "height": focused_region.bounds.height
                }
            }
        
        # Add application-specific suggestions
        if app_name:
            app_data["suggested_actions"] = self._get_app_suggestions(app_name)
        
        context = ContextData(
            context_type=ContextType.APPLICATION,
            timestamp=datetime.now(),
            data=app_data,
            confidence=0.7,
            source="ContextExtractor",
            priority=Priority.MEDIUM
        )
        
        return [context]
    
    def _is_known_application(self, app_name: str) -> bool:
        """Check if we have specific knowledge about this application"""
        known_apps = {
            "notepad.exe", "chrome.exe", "firefox.exe", "code.exe",
            "explorer.exe", "calculator.exe", "cmd.exe", "powershell.exe"
        }
        return app_name.lower() in known_apps if app_name else False
    
    def _get_app_suggestions(self, app_name: str) -> List[str]:
        """Get application-specific action suggestions"""
        suggestions = {
            "chrome.exe": ["Navigate to URL", "Open new tab", "Search"],
            "notepad.exe": ["Save file", "Open file", "Find text"],
            "code.exe": ["Open file", "Run command", "Search files"],
            "explorer.exe": ["Navigate folders", "Create folder", "Copy files"]
        }
        return suggestions.get(app_name.lower(), []) if app_name else []

class RegionalizationCore:
    """Main regionalization system coordinator"""
    
    def __init__(self, plugin_registry: Optional[PluginRegistry] = None, enable_vision_api: bool = False):
        self.plugin_registry = plugin_registry or PluginRegistry()
        self.window_detector = WindowDetector()
        self.basic_detector = BasicRegionDetector()
        self.context_extractor = ContextExtractor()
        self.message_builder = NeuroMessageBuilder()
        self.ocr_detector = OCRDetector() if OCRDetector else None
        
        # Vision API for AI-powered UI analysis
        self.vision_client = None
        if enable_vision_api and VisionAPIClient:
            self.vision_client = VisionAPIClient()
            logger.info("Vision API client initialized")
        
        self.current_state: Optional[SystemState] = None
        self.update_interval = 2.0  # seconds
        self.running = False
        self._last_ocr_elements = []
        self._last_vision_analysis = None
        self._vision_update_counter = 0
        self._vision_update_interval = 10  # Update vision every 10 regular updates (20 seconds)
        
    async def start(self):
        """Start the regionalization system"""
        self.running = True
        logger.info("Starting regionalization system")
        
        # Start the main update loop
        asyncio.create_task(self._update_loop())
    
    async def stop(self):
        """Stop the regionalization system"""
        self.running = False
        
        # Release vision API session
        if self.vision_client:
            try:
                self.vision_client.release_session()
            except Exception:
                pass
        
        logger.info("Stopping regionalization system")
    
    async def _update_loop(self):
        """Main update loop that refreshes system state"""
        while self.running:
            try:
                await self._update_system_state()
                await asyncio.sleep(self.update_interval)
            except Exception as e:
                logger.error(f"Error in update loop: {e}")
                await asyncio.sleep(1.0)  # Brief pause on error
    
    async def _update_system_state(self):
        """Update the current system state"""
        try:
            # Get active application
            active_app = await self.window_detector.get_active_application()
            
            # Detect windows
            window_regions = await self.window_detector.detect_windows()
            
            # Get focused window
            focused_region = await self.window_detector.get_focused_window()
            
            # Detect UI regions within focused window
            ui_regions = []
            if focused_region:
                screenshot = await self._take_screenshot()
                ui_regions = await self.basic_detector.detect_ui_regions(screenshot, focused_region)
            
            # Combine all regions
            all_regions = window_regions + ui_regions
            
            # Extract context
            context_data = []
            if all_regions:
                screenshot = await self._take_screenshot()
                visual_context = await self.context_extractor.extract_visual_context(screenshot, all_regions)
                interactive_context = await self.context_extractor.extract_interactive_context(all_regions)
                app_context = await self.context_extractor.extract_application_context(active_app, focused_region)
                
                context_data.extend(visual_context)
                context_data.extend(interactive_context) 
                context_data.extend(app_context)
            
            # Generate available actions
            available_actions = await self._generate_actions(all_regions, active_app)
            
            # Update system state
            self.current_state = SystemState(
                active_application=active_app,
                focused_region=focused_region,
                all_regions=all_regions,
                context_data=context_data,
                available_actions=available_actions,
                timestamp=datetime.now()
            )
            
            # Run OCR detection on current screen
            if self.ocr_detector:
                try:
                    ocr_elements = self.ocr_detector.detect_elements()
                    self._last_ocr_elements = ocr_elements
                    logger.debug(f"OCR detected {len(ocr_elements)} UI elements")
                except Exception as e:
                    logger.warning(f"OCR detection failed: {e}")
            
            # Run vision API analysis periodically (less frequent than OCR)
            if self.vision_client:
                self._vision_update_counter += 1
                if self._vision_update_counter >= self._vision_update_interval:
                    self._vision_update_counter = 0
                    try:
                        screenshot = await self._take_screenshot()
                        if screenshot and screenshot != b"screenshot_placeholder":
                            analysis = self.vision_client.analyze_screenshot(
                                screenshot_bytes=screenshot,
                                prompt="Analyze this Windows UI screenshot. List all visible UI elements (buttons, links, text fields, menus) with their locations (top-left, center, bottom-right, etc). Be specific and concise."
                            )
                            if analysis:
                                self._last_vision_analysis = analysis
                                logger.info(f"Vision analysis updated: {len(analysis)} chars")
                    except Exception as e:
                        logger.warning(f"Vision API analysis failed: {e}")
            
            # Update message builder
            self.message_builder.update_state(self.current_state)
            
            logger.debug(f"Updated system state: {len(all_regions)} regions, {len(context_data)} context items")
            
        except Exception as e:
            logger.error(f"Error updating system state: {e}")
    
    async def _take_screenshot(self) -> bytes:
        """Take a screenshot of the current screen with enhanced error handling"""
        if not pyautogui:
            return b"screenshot_unavailable"
            
        try:
            # Use asyncio to run screenshot in thread to avoid blocking
            loop = asyncio.get_event_loop()
            
            def take_screenshot_sync():
                try:
                    # Disable failsafe to prevent hang
                    pyautogui.FAILSAFE = False
                    
                    # Get screen size first to ensure it's accessible
                    screen_width, screen_height = pyautogui.size()
                    if screen_width <= 0 or screen_height <= 0:
                        logger.error("Invalid screen dimensions")
                        return None
                    
                    # Take screenshot
                    screenshot = pyautogui.screenshot()
                    if screenshot is None:
                        logger.error("Screenshot returned None")
                        return None
                        
                    # Convert to bytes
                    from io import BytesIO
                    buffer = BytesIO()
                    screenshot.save(buffer, format='PNG')
                    return buffer.getvalue()
                    
                except Exception as e:
                    logger.error(f"Screenshot capture error: {e}")
                    return None
            
            # Run with timeout to prevent hanging
            screenshot_data = await asyncio.wait_for(
                loop.run_in_executor(None, take_screenshot_sync),
                timeout=5.0  # 5 second timeout
            )
            
            if screenshot_data is None:
                logger.warning("Screenshot failed, using placeholder")
                return b"screenshot_placeholder"
                
            return screenshot_data
            
        except asyncio.TimeoutError:
            logger.warning("Screenshot timed out, using placeholder")
            return b"screenshot_placeholder"
        except Exception as e:
            logger.error(f"Error taking screenshot: {e}")
            return b"screenshot_placeholder"
    
    async def _generate_actions(self, regions: List[ScreenRegion], app_name: Optional[str]) -> List[NeuroAction]:
        """Generate available actions based on current state"""
        actions = []
        
        # Basic click actions for clickable regions
        for region in regions:
            if region.clickable:
                action = NeuroAction(
                    name=f"click_{region.id}",
                    description=f"Click on {region.title or region.region_type.value}",
                    parameters={"x": region.bounds.center.x, "y": region.bounds.center.y},
                    target_region=region,
                    estimated_duration=0.5
                )
                actions.append(action)
        
        # Application-specific actions
        if app_name:
            app_actions = self._get_application_actions(app_name)
            actions.extend(app_actions)
        
        return actions
    
    def _get_application_actions(self, app_name: str) -> List[NeuroAction]:
        """Get application-specific actions"""
        actions = []
        
        app_specific = {
            "notepad.exe": [
                NeuroAction("save_file", "Save the current file", {"hotkey": ["ctrl", "s"]}),
                NeuroAction("open_file", "Open a file", {"hotkey": ["ctrl", "o"]}),
                NeuroAction("find_text", "Find text in document", {"hotkey": ["ctrl", "f"]})
            ],
            "chrome.exe": [
                NeuroAction("new_tab", "Open new tab", {"hotkey": ["ctrl", "t"]}),
                NeuroAction("close_tab", "Close current tab", {"hotkey": ["ctrl", "w"]}),
                NeuroAction("refresh", "Refresh page", {"key": "F5"})
            ]
        }
        
        return app_specific.get(app_name.lower(), []) if app_name else []
    
    def get_current_state(self) -> Optional[SystemState]:
        """Get the current system state"""
        return self.current_state
    
    def get_context_message(self) -> str:
        """Get formatted context message for Neuro"""
        base_message = self.message_builder.build_context_message(self._last_ocr_elements, self.ocr_detector)
        
        # Append vision analysis if available
        if self._last_vision_analysis:
            base_message += f"\n\nAI Vision Analysis:\n{self._last_vision_analysis}"
        
        return base_message
    
    def get_ocr_elements(self):
        """Get last detected OCR elements"""
        return self._last_ocr_elements
    
    async def force_update(self):
        """Force an immediate system state update"""
        await self._update_system_state()
    
    def get_region_by_id(self, region_id: str) -> Optional[ScreenRegion]:
        """Get a specific region by its ID"""
        if not self.current_state:
            return None
            
        for region in self.current_state.all_regions:
            if region.id == region_id:
                return region
        return None
    
    def get_regions_by_type(self, region_type: RegionType) -> List[ScreenRegion]:
        """Get all regions of a specific type"""
        if not self.current_state:
            return []
            
        return [region for region in self.current_state.all_regions 
                if region.region_type == region_type]