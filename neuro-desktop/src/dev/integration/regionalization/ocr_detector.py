"""
Advanced OCR-based UI element detection
Uses EasyOCR for better accuracy without external dependencies
"""
import logging
from typing import List, Optional, Tuple
from dataclasses import dataclass

logger = logging.getLogger(__name__)

@dataclass
class OCRElement:
    """Detected UI element from OCR"""
    text: str
    bbox: Tuple[int, int, int, int]  # x, y, width, height
    confidence: float
    center_x: int
    center_y: int
    element_type: str = "text"  # text, button, link, input

class OCRDetector:
    """Detects UI elements using OCR"""
    
    def __init__(self):
        self.reader = None
        self._init_ocr()
    
    def _init_ocr(self):
        """Initialize EasyOCR reader"""
        try:
            import easyocr
            # Initialize with English, use GPU if available
            self.reader = easyocr.Reader(['en'], gpu=False, verbose=False)
            logger.info("EasyOCR initialized successfully")
        except ImportError:
            logger.warning("EasyOCR not installed - OCR detection disabled")
            logger.warning("Install with: pip install easyocr opencv-python")
        except Exception as e:
            logger.error(f"Failed to initialize EasyOCR: {e}")
    
    def detect_elements(self, screenshot_path: str = None, screenshot_image=None) -> List[OCRElement]:
        """
        Detect UI elements from screenshot
        
        Args:
            screenshot_path: Path to screenshot image file
            screenshot_image: PIL Image or numpy array
        
        Returns:
            List of detected OCR elements with coordinates
        """
        if not self.reader:
            return []
        
        try:
            import numpy as np
            from PIL import Image
            
            # Convert screenshot to numpy array if needed
            if screenshot_image is not None:
                if isinstance(screenshot_image, Image.Image):
                    img_array = np.array(screenshot_image)
                else:
                    img_array = screenshot_image
            elif screenshot_path:
                img = Image.open(screenshot_path)
                img_array = np.array(img)
            else:
                # Take screenshot
                import pyautogui
                screenshot = pyautogui.screenshot()
                img_array = np.array(screenshot)
            
            # Run OCR detection
            results = self.reader.readtext(img_array)
            
            elements = []
            for detection in results:
                bbox_coords, text, conf = detection
                
                # Calculate bounding box
                x_coords = [point[0] for point in bbox_coords]
                y_coords = [point[1] for point in bbox_coords]
                
                x = int(min(x_coords))
                y = int(min(y_coords))
                width = int(max(x_coords) - x)
                height = int(max(y_coords) - y)
                
                center_x = x + width // 2
                center_y = y + height // 2
                
                # Infer element type from context
                element_type = self._infer_element_type(text, width, height)
                
                element = OCRElement(
                    text=text.strip(),
                    bbox=(x, y, width, height),
                    confidence=conf,
                    center_x=center_x,
                    center_y=center_y,
                    element_type=element_type
                )
                
                elements.append(element)
            
            logger.info(f"Detected {len(elements)} UI elements via OCR")
            return elements
            
        except Exception as e:
            logger.error(f"OCR detection failed: {e}")
            return []
    
    def _infer_element_type(self, text: str, width: int, height: int) -> str:
        """Infer UI element type from text and dimensions"""
        text_lower = text.lower()
        
        # Button indicators
        button_keywords = ['click', 'button', 'submit', 'ok', 'cancel', 'save', 'delete',
                          'subscribe', 'like', 'share', 'comment', 'play', 'pause']
        if any(kw in text_lower for kw in button_keywords):
            return "button"
        
        # Link indicators
        if text.startswith('http') or '.com' in text_lower or '.org' in text_lower:
            return "link"
        
        # Input field indicators (wide but short)
        if width > height * 3 and height < 40:
            return "input"
        
        # Button dimensions (roughly square or wide button)
        if 50 < width < 200 and 20 < height < 60:
            return "button"
        
        return "text"
    
    def get_elements_near_point(self, elements: List[OCRElement], x: int, y: int, 
                               radius: int = 100) -> List[OCRElement]:
        """Get UI elements near a specific point"""
        nearby = []
        for elem in elements:
            dist = ((elem.center_x - x) ** 2 + (elem.center_y - y) ** 2) ** 0.5
            if dist <= radius:
                nearby.append(elem)
        return sorted(nearby, key=lambda e: ((e.center_x - x) ** 2 + (e.center_y - y) ** 2))
    
    def format_for_context(self, elements: List[OCRElement], max_items: int = 20) -> str:
        """Format detected elements for context message"""
        if not elements:
            return "[No UI elements detected - OCR may not be initialized]"
        
        # Group by element type
        grouped = {}
        for elem in elements:
            if elem.element_type not in grouped:
                grouped[elem.element_type] = []
            grouped[elem.element_type].append(elem)
        
        lines = []
        lines.append(f"UI Elements Detected: {len(elements)} total")
        
        # Show buttons first (most interactive)
        if 'button' in grouped:
            lines.append(f"\nButtons ({len(grouped['button'])}):") 
            for elem in grouped['button'][:10]:
                lines.append(f'  - "{elem.text}" at ({elem.center_x}, {elem.center_y})')
        
        # Then links
        if 'link' in grouped:
            lines.append(f"\nLinks ({len(grouped['link'])}):")
            for elem in grouped['link'][:5]:
                lines.append(f'  - "{elem.text}" at ({elem.center_x}, {elem.center_y})')
        
        # Show some text elements for context
        if 'text' in grouped:
            lines.append(f"\nVisible Text ({len(grouped['text'])} items):")
            # Show most confident text
            top_text = sorted(grouped['text'], key=lambda e: e.confidence, reverse=True)[:10]
            for elem in top_text:
                if len(elem.text) > 3:  # Skip very short text
                    lines.append(f'  - "{elem.text[:50]}" at ({elem.center_x}, {elem.center_y})')
        
        return "\n".join(lines)
