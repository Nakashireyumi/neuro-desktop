"""
Comprehensive unit tests for Vision API client
Tests authentication, session management, and screenshot analysis
"""

import pytest
from unittest.mock import Mock, MagicMock, patch, call
from pathlib import Path
import sys
import base64

sys.path.insert(0, str(Path(__file__).parents[2]))

from src.dev.integration.regionalization.vision_api_client import VisionAPIClient


class TestVisionAPIClientInit:
    """Test VisionAPIClient initialization"""
    
    def test_init_with_defaults(self):
        """Test initialization with default values"""
        client = VisionAPIClient()
        
        assert client.base_url == "https://backend.nakurity.com/api"
        assert client.vision_endpoint == "https://backend.nakurity.com/api/neuro-os/vision"
        assert client.session_endpoint == "https://backend.nakurity.com/api/session"
        assert client.session_key is None
        assert client._heartbeat_interval == 60
    
    def test_init_with_custom_values(self):
        """Test initialization with custom base URL and session key"""
        client = VisionAPIClient(
            base_url="https://custom.api.com",
            session_key="test-key-123"
        )
        
        assert client.base_url == "https://custom.api.com"
        assert client.vision_endpoint == "https://custom.api.com/neuro-os/vision"
        assert client.session_key == "test-key-123"
    
    def test_init_creates_session(self):
        """Test that initialization creates a requests session"""
        client = VisionAPIClient()
        
        assert client.session is not None
        assert 'Content-Type' in client.session.headers
        assert client.session.headers['Content-Type'] == 'application/json'


class TestVisionAPIClientSessionManagement:
    """Test session management functionality"""
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_claim_session_success(self, mock_session_class):
        """Test successful session claim"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.json.return_value = {
            'success': True,
            'sessionKey': 'new-session-key',
            'heartbeatInterval': 30000
        }
        mock_session.get.return_value = mock_response
        
        client = VisionAPIClient()
        result = client.claim_session()
        
        assert result is True
        assert client.session_key == 'new-session-key'
        assert client._heartbeat_interval == 30.0
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_claim_session_failure(self, mock_session_class):
        """Test failed session claim"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.json.return_value = {
            'success': False,
            'error': 'No sessions available'
        }
        mock_session.get.return_value = mock_response
        
        client = VisionAPIClient()
        result = client.claim_session()
        
        assert result is False
        assert client.session_key is None
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_claim_session_exception(self, mock_session_class):
        """Test session claim with network exception"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        mock_session.get.side_effect = Exception("Network error")
        
        client = VisionAPIClient()
        result = client.claim_session()
        
        assert result is False
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    @patch('src.dev.integration.regionalization.vision_api_client.time')
    def test_send_heartbeat_success(self, mock_time, mock_session_class):
        """Test successful heartbeat"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.raise_for_status = Mock()
        mock_session.post.return_value = mock_response
        
        # Mock time to simulate passage of time
        mock_time.time.side_effect = [0, 61]  # Start at 0, then 61 seconds later
        
        client = VisionAPIClient(session_key="test-key")
        client._last_heartbeat = 0
        
        result = client.send_heartbeat()
        
        assert result is True
        assert client._last_heartbeat == 61
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    @patch('src.dev.integration.regionalization.vision_api_client.time')
    def test_send_heartbeat_too_soon(self, mock_time, mock_session_class):
        """Test heartbeat when called too soon"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_time.time.return_value = 30
        
        client = VisionAPIClient(session_key="test-key")
        client._last_heartbeat = 0
        client._heartbeat_interval = 60
        
        result = client.send_heartbeat()
        
        assert result is True  # Returns True but doesn't send
        mock_session.post.assert_not_called()
    
    def test_send_heartbeat_without_session_key(self):
        """Test heartbeat without session key"""
        client = VisionAPIClient()
        
        result = client.send_heartbeat()
        assert result is False
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_release_session_success(self, mock_session_class):
        """Test successful session release"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.raise_for_status = Mock()
        mock_session.post.return_value = mock_response
        
        client = VisionAPIClient(session_key="test-key")
        result = client.release_session()
        
        assert result is True
        assert client.session_key is None
    
    def test_release_session_without_key(self):
        """Test release when no session key exists"""
        client = VisionAPIClient()
        
        result = client.release_session()
        assert result is True


class TestVisionAPIClientAnalysis:
    """Test screenshot analysis functionality"""
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_analyze_screenshot_success(self, mock_session_class):
        """Test successful screenshot analysis"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        # Mock successful API response
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            'success': True,
            'analysis': 'The screenshot shows a login form with username and password fields.'
        }
        mock_response.raise_for_status = Mock()
        mock_session.post.return_value = mock_response
        
        client = VisionAPIClient(session_key="test-key")
        
        # Mock image bytes
        test_image_bytes = b"fake_image_data"
        
        result = client.analyze_screenshot(screenshot_bytes=test_image_bytes)
        
        assert result == 'The screenshot shows a login form with username and password fields.'
        mock_session.post.assert_called_once()
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    @patch('src.dev.integration.regionalization.vision_api_client.pyautogui')
    def test_analyze_screenshot_without_image(self, mock_pyautogui, mock_session_class):
        """Test analysis when no image is provided (takes screenshot)"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {'success': True, 'analysis': 'Desktop view'}
        mock_response.raise_for_status = Mock()
        mock_session.post.return_value = mock_response
        
        # Mock screenshot
        mock_screenshot = Mock()
        mock_screenshot.save = Mock()
        mock_pyautogui.screenshot.return_value = mock_screenshot
        
        client = VisionAPIClient(session_key="test-key")
        result = client.analyze_screenshot()
        
        assert result == 'Desktop view'
        mock_pyautogui.screenshot.assert_called_once()
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_analyze_screenshot_with_pil_image(self, mock_session_class):
        """Test analysis with PIL Image object"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {'success': True, 'analysis': 'Image analyzed'}
        mock_response.raise_for_status = Mock()
        mock_session.post.return_value = mock_response
        
        # Mock PIL Image
        mock_image = Mock()
        mock_image.save = Mock()
        
        client = VisionAPIClient(session_key="test-key")
        
        with patch('src.dev.integration.regionalization.vision_api_client.BytesIO'):
            result = client.analyze_screenshot(screenshot_image=mock_image)
            
            assert result == 'Image analyzed'
            mock_image.save.assert_called_once()
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_analyze_screenshot_with_custom_prompt(self, mock_session_class):
        """Test analysis with custom prompt"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {'success': True, 'analysis': 'Custom analysis'}
        mock_response.raise_for_status = Mock()
        mock_session.post.return_value = mock_response
        
        client = VisionAPIClient(session_key="test-key")
        
        client.analyze_screenshot(
            screenshot_bytes=b"data",
            prompt="Describe the UI elements in detail"
        )
        
        # Verify prompt was included in request
        call_args = mock_session.post.call_args
        payload = call_args[1]['json']
        assert payload['prompt'] == "Describe the UI elements in detail"
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_analyze_screenshot_auth_failure_retry(self, mock_session_class):
        """Test retry logic on authentication failure"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        # First call: 401 error
        mock_response_401 = Mock()
        mock_response_401.status_code = 401
        
        # After reclaim: success
        mock_response_ok = Mock()
        mock_response_ok.status_code = 200
        mock_response_ok.json.return_value = {'success': True, 'analysis': 'Success'}
        mock_response_ok.raise_for_status = Mock()
        
        # Mock session claim
        mock_claim_response = Mock()
        mock_claim_response.json.return_value = {
            'success': True,
            'sessionKey': 'new-key',
            'heartbeatInterval': 60000
        }
        
        mock_session.post.side_effect = [mock_response_401, mock_response_ok]
        mock_session.get.return_value = mock_claim_response
        
        client = VisionAPIClient(session_key="old-key")
        result = client.analyze_screenshot(screenshot_bytes=b"data")
        
        assert result == 'Success'
        assert client.session_key == 'new-key'
        assert mock_session.post.call_count == 2  # First attempt + retry
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_analyze_screenshot_rate_limit(self, mock_session_class):
        """Test handling of rate limit (429) response"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.status_code = 429
        mock_session.post.return_value = mock_response
        
        client = VisionAPIClient(session_key="test-key")
        result = client.analyze_screenshot(screenshot_bytes=b"data")
        
        assert result is None
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_analyze_screenshot_api_error(self, mock_session_class):
        """Test handling of API error response"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            'success': False,
            'error': 'Invalid image format'
        }
        mock_response.raise_for_status = Mock()
        mock_session.post.return_value = mock_response
        
        client = VisionAPIClient(session_key="test-key")
        result = client.analyze_screenshot(screenshot_bytes=b"data")
        
        assert result is None
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_analyze_screenshot_without_session_key(self, mock_session_class):
        """Test analysis attempts to claim session if no key exists"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        # Mock successful session claim
        mock_claim_response = Mock()
        mock_claim_response.json.return_value = {
            'success': True,
            'sessionKey': 'claimed-key',
            'heartbeatInterval': 60000
        }
        
        # Mock successful analysis
        mock_analysis_response = Mock()
        mock_analysis_response.status_code = 200
        mock_analysis_response.json.return_value = {
            'success': True,
            'analysis': 'Analysis result'
        }
        mock_analysis_response.raise_for_status = Mock()
        
        mock_session.get.return_value = mock_claim_response
        mock_session.post.return_value = mock_analysis_response
        
        client = VisionAPIClient()
        result = client.analyze_screenshot(screenshot_bytes=b"data")
        
        assert result == 'Analysis result'
        assert client.session_key == 'claimed-key'
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_analyze_screenshot_claim_fails(self, mock_session_class):
        """Test analysis when session claim fails"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_claim_response = Mock()
        mock_claim_response.json.return_value = {'success': False}
        mock_session.get.return_value = mock_claim_response
        
        client = VisionAPIClient()
        result = client.analyze_screenshot(screenshot_bytes=b"data")
        
        assert result is None
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_analyze_screenshot_network_error(self, mock_session_class):
        """Test handling of network errors"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        import requests
        mock_session.post.side_effect = requests.RequestException("Network error")
        
        client = VisionAPIClient(session_key="test-key")
        result = client.analyze_screenshot(screenshot_bytes=b"data")
        
        assert result is None


class TestVisionAPIClientUtility:
    """Test utility methods"""
    
    def test_is_available_with_session_key(self):
        """Test availability check when session key exists"""
        client = VisionAPIClient(session_key="test-key")
        
        assert client.is_available() is True
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_is_available_without_session_can_claim(self, mock_session_class):
        """Test availability when no key but can claim"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.json.return_value = {
            'success': True,
            'sessionKey': 'new-key',
            'heartbeatInterval': 60000
        }
        mock_session.get.return_value = mock_response
        
        client = VisionAPIClient()
        
        result = client.is_available()
        assert result is True
        assert client.session_key == 'new-key'
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_is_available_cannot_claim(self, mock_session_class):
        """Test availability when cannot claim session"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.json.return_value = {'success': False}
        mock_session.get.return_value = mock_response
        
        client = VisionAPIClient()
        
        result = client.is_available()
        assert result is False
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    def test_destructor_releases_session(self, mock_session_class):
        """Test that destructor releases session"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        mock_response = Mock()
        mock_response.raise_for_status = Mock()
        mock_session.post.return_value = mock_response
        
        client = VisionAPIClient(session_key="test-key")
        
        # Call destructor
        client.__del__()
        
        # Session should be released
        assert mock_session.post.called


class TestVisionAPIClientIntegration:
    """Integration tests for Vision API client"""
    
    @patch('src.dev.integration.regionalization.vision_api_client.requests.Session')
    @patch('src.dev.integration.regionalization.vision_api_client.time')
    def test_full_workflow(self, mock_time, mock_session_class):
        """Test complete workflow: claim, heartbeat, analyze, release"""
        mock_session = Mock()
        mock_session_class.return_value = mock_session
        
        # Mock responses
        claim_response = Mock()
        claim_response.json.return_value = {
            'success': True,
            'sessionKey': 'session-123',
            'heartbeatInterval': 30000
        }
        
        heartbeat_response = Mock()
        heartbeat_response.raise_for_status = Mock()
        
        analysis_response = Mock()
        analysis_response.status_code = 200
        analysis_response.json.return_value = {
            'success': True,
            'analysis': 'Analysis result'
        }
        analysis_response.raise_for_status = Mock()
        
        release_response = Mock()
        release_response.raise_for_status = Mock()
        
        mock_session.get.return_value = claim_response
        mock_session.post.side_effect = [
            heartbeat_response,
            analysis_response,
            release_response
        ]
        
        mock_time.time.side_effect = [0, 31, 31]
        
        # Workflow
        client = VisionAPIClient()
        
        # 1. Claim session
        assert client.claim_session() is True
        assert client.session_key == 'session-123'
        
        # 2. Send heartbeat
        client._last_heartbeat = 0
        assert client.send_heartbeat() is True
        
        # 3. Analyze screenshot
        result = client.analyze_screenshot(screenshot_bytes=b"data")
        assert result == 'Analysis result'
        
        # 4. Release session
        assert client.release_session() is True
        assert client.session_key is None


if __name__ == "__main__":
    pytest.main([__file__, "-v"])