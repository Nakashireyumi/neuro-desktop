"""
Nakurity Vision API Client
Sends screenshots to Groq vision API for detailed analysis
"""
import base64
import logging
import requests
from typing import Optional, Dict
from io import BytesIO

logger = logging.getLogger(__name__)

class VisionAPIClient:
    """Client for Nakurity Vision API with session-based authentication"""
    
    def __init__(self, base_url: str = None, session_key: str = None):
        self.base_url = base_url or "https://backend.nakurity.com/api"
        self.vision_endpoint = f"{self.base_url}/neuro-os/vision"
        self.session_endpoint = f"{self.base_url}/session"
        self.session_key = session_key
        self.session = requests.Session()
        self.session.headers.update({
            'Content-Type': 'application/json'
        })
        self._heartbeat_interval = 60  # seconds
        self._last_heartbeat = 0
    
    def claim_session(self) -> bool:
        """Claim a new session key from the backend"""
        try:
            response = self.session.get(f"{self.session_endpoint}/claim", timeout=10)
            response.raise_for_status()
            
            result = response.json()
            if result.get('success'):
                self.session_key = result.get('sessionKey')
                self._heartbeat_interval = result.get('heartbeatInterval', 60000) / 1000
                logger.info(f"Session claimed successfully: {self.session_key[:8]}...")
                return True
            else:
                logger.error(f"Failed to claim session: {result}")
                return False
                
        except Exception as e:
            logger.error(f"Session claim failed: {e}")
            return False
    
    def send_heartbeat(self) -> bool:
        """Send heartbeat to keep session alive"""
        if not self.session_key:
            return False
        
        import time
        now = time.time()
        if now - self._last_heartbeat < self._heartbeat_interval:
            return True  # Not time yet
        
        try:
            headers = {'X-Session-Key': self.session_key}
            response = self.session.post(f"{self.session_endpoint}/heartbeat", 
                                        headers=headers, timeout=10)
            response.raise_for_status()
            
            self._last_heartbeat = now
            return True
            
        except Exception as e:
            logger.warning(f"Heartbeat failed: {e}")
            return False
    
    def release_session(self) -> bool:
        """Release the current session"""
        if not self.session_key:
            return True
        
        try:
            headers = {'X-Session-Key': self.session_key}
            response = self.session.post(f"{self.session_endpoint}/release",
                                        headers=headers, timeout=10)
            response.raise_for_status()
            
            logger.info("Session released")
            self.session_key = None
            return True
            
        except Exception as e:
            logger.warning(f"Session release failed: {e}")
            return False
    
    def analyze_screenshot(self, screenshot_image=None, screenshot_bytes: bytes = None, 
                          prompt: Optional[str] = None) -> Optional[str]:
        """
        Analyze a screenshot using Groq vision model
        
        Args:
            screenshot_image: PIL Image object
            screenshot_bytes: Raw image bytes
            prompt: Optional custom prompt
            
        Returns:
            Analysis text or None if failed
        """
        # Ensure we have a session key
        if not self.session_key:
            if not self.claim_session():
                logger.warning("Vision API: No session key available")
                return None
        
        # Send heartbeat if needed
        self.send_heartbeat()
        
        try:
            # Convert image to base64
            if screenshot_image:
                buffer = BytesIO()
                screenshot_image.save(buffer, format='PNG')
                image_bytes = buffer.getvalue()
            elif screenshot_bytes:
                image_bytes = screenshot_bytes
            else:
                # Take screenshot
                try:
                    import pyautogui
                    screenshot = pyautogui.screenshot()
                    buffer = BytesIO()
                    screenshot.save(buffer, format='PNG')
                    image_bytes = buffer.getvalue()
                except Exception as e:
                    logger.error(f"Failed to take screenshot: {e}")
                    return None
            
            # Encode to base64
            image_b64 = base64.b64encode(image_bytes).decode('utf-8')
            
            # Prepare request
            payload = {
                'image': image_b64,
                'prompt': prompt
            }
            
            # Call API with session authentication
            headers = {'X-Session-Key': self.session_key}
            response = self.session.post(self.vision_endpoint, json=payload, 
                                        headers=headers, timeout=30)
            
            if response.status_code == 401:
                logger.error("Vision API authentication failed - session expired, reclaiming...")
                self.session_key = None
                if self.claim_session():
                    # Retry with new session
                    return self.analyze_screenshot(screenshot_bytes=image_bytes, prompt=prompt)
                return None
            
            if response.status_code == 429:
                logger.warning("Vision API rate limit exceeded")
                return None
            
            response.raise_for_status()
            
            result = response.json()
            if result.get('success'):
                analysis = result.get('analysis', '')
                logger.info(f"Vision analysis received: {len(analysis)} chars")
                return analysis
            else:
                logger.error(f"Vision API error: {result.get('error')}")
                return None
                
        except requests.RequestException as e:
            logger.error(f"Vision API request failed: {e}")
            return None
        except Exception as e:
            logger.error(f"Vision analysis failed: {e}")
            return None
    
    def is_available(self) -> bool:
        """Check if vision API is configured and available"""
        return bool(self.base_url and (self.session_key or self.claim_session()))
    
    def __del__(self):
        """Release session on cleanup"""
        try:
            self.release_session()
        except Exception:
            pass
