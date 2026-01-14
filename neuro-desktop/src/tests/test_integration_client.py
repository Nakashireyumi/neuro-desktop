"""
Comprehensive unit tests for the Neuro integration client
Tests client initialization, action handling, and context publishing
"""

import pytest
import asyncio
import json
from unittest.mock import Mock, MagicMock, AsyncMock, patch, call
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).parents[2]))

# Test constants for sensitive values
TEST_AUTH_TOKEN = "super-secret-token"  # noqa
TEST_WINDOWS_API_URI = "ws://127.0.0.1:8765"


class TestNeuroClientInitialization:
    """Test NeuroClient initialization"""
    
    @patch('src.dev.integration.client.RegionalizationCore', None)
    def test_client_init_without_regionalization(self):
        """Test client initializes without regionalization core"""
        from src.dev.integration.client import NeuroClient
        
        mock_websocket = Mock()
        client = NeuroClient(mock_websocket)
        
        assert client.websocket == mock_websocket
        assert client.name == "Neuro's Desktop"
        assert client._reg is None
        assert client.windows_api_uri == TEST_WINDOWS_API_URI
        assert client.auth_token == TEST_AUTH_TOKEN
        assert client._action_in_progress is False
    
    @patch('src.dev.integration.client.RegionalizationCore')
    def test_client_init_with_regionalization(self, mock_reg_class):
        """Test client initializes with regionalization core"""
        from src.dev.integration.client import NeuroClient
        
        mock_websocket = Mock()
        mock_reg_instance = Mock()
        mock_reg_class.return_value = mock_reg_instance
        
        client = NeuroClient(mock_websocket)
        
        assert client._reg == mock_reg_instance
        mock_reg_class.assert_called_once()
    
    def test_client_inherits_from_abstract_neuro_api(self):
        """Test that NeuroClient inherits from AbstractNeuroAPI"""
        from src.dev.integration.client import NeuroClient
        from neuro_api.api import AbstractNeuroAPI
        
        mock_websocket = Mock()
        client = NeuroClient(mock_websocket)
        
        assert isinstance(client, AbstractNeuroAPI)


class TestNeuroClientWebSocketMethods:
    """Test websocket communication methods"""
    
    @pytest.mark.asyncio
    async def test_write_to_websocket(self):
        """Test writing data to websocket"""
        from src.dev.integration.client import NeuroClient
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        
        await client.write_to_websocket("test data")
        
        mock_websocket.send.assert_called_once_with("test data")
    
    @pytest.mark.asyncio
    async def test_read_from_websocket(self):
        """Test reading data from websocket"""
        from src.dev.integration.client import NeuroClient
        
        mock_websocket = AsyncMock()
        mock_websocket.recv.return_value = "received data"
        
        client = NeuroClient(mock_websocket)
        result = await client.read_from_websocket()
        
        assert result == "received data"
        mock_websocket.recv.assert_called_once()


class TestNeuroClientInitialize:
    """Test client initialization workflow"""
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.load_actions')
    async def test_initialize_sends_startup_command(self, mock_load_actions):
        """Test that initialize sends startup command"""
        from src.dev.integration.client import NeuroClient
        
        mock_load_actions.return_value = []
        mock_websocket = AsyncMock()
        
        client = NeuroClient(mock_websocket)
        client.send_startup_command = AsyncMock()
        client.register_actions = AsyncMock()
        client.send_context = AsyncMock()
        
        await client.initialize()
        
        client.send_startup_command.assert_called_once()
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.load_actions')
    async def test_initialize_registers_actions(self, mock_load_actions):
        """Test that initialize registers actions"""
        from src.dev.integration.client import NeuroClient
        from neuro_api.command import Action
        
        mock_action = Mock(spec=Action)
        mock_action.name = "test_action"
        mock_load_actions.return_value = [mock_action]
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        client.send_startup_command = AsyncMock()
        client.register_actions = AsyncMock()
        client.send_context = AsyncMock()
        
        await client.initialize()
        
        client.register_actions.assert_called_once_with([mock_action])
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.load_actions')
    async def test_initialize_sends_context_primer(self, mock_load_actions):
        """Test that initialize sends initial context primer"""
        from src.dev.integration.client import NeuroClient
        
        mock_load_actions.return_value = []
        mock_websocket = AsyncMock()
        
        client = NeuroClient(mock_websocket)
        client.send_startup_command = AsyncMock()
        client.register_actions = AsyncMock()
        client.send_context = AsyncMock()
        
        await client.initialize()
        
        # Should send context with action names
        assert client.send_context.called
        call_args = client.send_context.call_args
        context_message = call_args[0][0]
        assert "Neuro-OS Windows integration" in context_message or "integration is active" in context_message


class TestNeuroClientActionHandling:
    """Test action handling"""
    
    @pytest.mark.asyncio
    async def test_handle_action_context_update(self):
        """Test handling context_update action"""
        from src.dev.integration.client import NeuroClient
        from neuro_api.api import NeuroAction
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        client.send_action_result = AsyncMock()
        
        action = NeuroAction(id_="action-1", name="context_update", data="{}")
        
        await client.handle_action(action)
        
        client.send_action_result.assert_called_once_with(
            "action-1", success=True, message="context_update received by neuro-os"
        )
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.websockets.connect')
    async def test_handle_action_click(self, mock_ws_connect):
        """Test handling click action"""
        from src.dev.integration.client import NeuroClient
        from neuro_api.api import NeuroAction
        
        # Mock Windows API response
        mock_win_ws = AsyncMock()
        mock_win_ws.send = AsyncMock()
        mock_win_ws.recv = AsyncMock(return_value=json.dumps({
            "status": "ok",
            "result": {"clicked": True}
        }))
        mock_ws_connect.return_value.__aenter__.return_value = mock_win_ws
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        client.send_action_result = AsyncMock()
        
        action = NeuroAction(
            id_="action-2",
            name="click",
            data=json.dumps({"x": 100, "y": 200, "button": "left"})
        )
        
        await client.handle_action(action)
        
        # Should execute action and send result
        client.send_action_result.assert_called_once()
        result_call = client.send_action_result.call_args
        assert result_call[0][0] == "action-2"  # action id
        assert result_call[0][1] is True  # success
    
    @pytest.mark.asyncio
    async def test_handle_action_sets_in_progress_flag(self):
        """Test that action handling sets in-progress flag"""
        from src.dev.integration.client import NeuroClient
        from neuro_api.api import NeuroAction
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        client._execute_windows_action = AsyncMock(return_value=(True, "Success"))
        client.send_action_result = AsyncMock()
        
        action = NeuroAction(id_="action-3", name="test", data="{}")
        
        assert client._action_in_progress is False
        
        # Create task to check flag during execution
        async def check_flag() -> bool:
            await asyncio.sleep(0.01)
            # At this point, flag should be True
            return client._action_in_progress
        
        # Start both tasks
        check_task = asyncio.create_task(check_flag())
        await client.handle_action(action)
        
        # Verify flag was True during execution
        flag_was_true = await check_task
        assert flag_was_true is True
        
        # After completion, flag should be False
        assert client._action_in_progress is False
    
    @pytest.mark.asyncio
    async def test_handle_action_error(self):
        """Test handling action execution error"""
        from src.dev.integration.client import NeuroClient
        from neuro_api.api import NeuroAction
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        client._execute_windows_action = AsyncMock(side_effect=Exception("Test error"))
        client.send_action_result = AsyncMock()
        
        action = NeuroAction(id_="action-4", name="test", data="{}")
        
        await client.handle_action(action)
        
        # Should send error result
        result_call = client.send_action_result.call_args
        assert result_call[0][1] is False  # success = False
        assert "Error" in result_call[0][2] or "error" in result_call[0][2].lower()


class TestNeuroClientWindowsActionExecution:
    """Test Windows action execution"""
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.websockets.connect')
    async def test_execute_windows_action_success(self, mock_ws_connect):
        """Test successful Windows action execution"""
        from src.dev.integration.client import NeuroClient
        
        # Mock Windows API response
        mock_win_ws = AsyncMock()
        mock_win_ws.send = AsyncMock()
        mock_win_ws.recv = AsyncMock(return_value=json.dumps({
            "status": "ok",
            "result": {"success": True}
        }))
        mock_ws_connect.return_value.__aenter__.return_value = mock_win_ws
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        
        success, message = await client._execute_windows_action("move", {"x": 100, "y": 200})
        
        assert success is True
        assert "move" in message.lower()
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.websockets.connect')
    async def test_execute_windows_action_error_response(self, mock_ws_connect):
        """Test Windows action execution with error response"""
        from src.dev.integration.client import NeuroClient
        
        mock_win_ws = AsyncMock()
        mock_win_ws.send = AsyncMock()
        mock_win_ws.recv = AsyncMock(return_value=json.dumps({
            "status": "error",
            "error": {"message": "Invalid coordinates"}
        }))
        mock_ws_connect.return_value.__aenter__.return_value = mock_win_ws
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        
        success, message = await client._execute_windows_action("click", {"x": -1, "y": -1})
        
        assert success is False
        assert "Invalid coordinates" in message
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.pyautogui')
    @patch('src.dev.integration.client.websockets.connect')
    async def test_execute_click_fills_missing_coordinates(self, mock_ws_connect, mock_pyautogui):
        """Test that click action fills missing coordinates from current position"""
        from src.dev.integration.client import NeuroClient
        
        mock_pyautogui.position.return_value = (500, 600)
        
        mock_win_ws = AsyncMock()
        mock_win_ws.send = AsyncMock()
        mock_win_ws.recv = AsyncMock(return_value=json.dumps({"status": "ok", "result": {}}))
        mock_ws_connect.return_value.__aenter__.return_value = mock_win_ws
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        
        # Call with missing coordinates
        await client._execute_windows_action("click", {"button": "left"})
        
        # Should have filled in coordinates
        sent_message = json.loads(mock_win_ws.send.call_args[0][0])
        assert sent_message["x"] == 500
        assert sent_message["y"] == 600
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.websockets.connect')
    async def test_execute_windows_action_connection_error(self, mock_ws_connect):
        """Test Windows action execution with connection error"""
        from src.dev.integration.client import NeuroClient
        
        mock_ws_connect.side_effect = Exception("Connection failed")
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        
        success, message = await client._execute_windows_action("test", {})
        
        assert success is False
        assert "Failed to execute" in message or "Connection failed" in message


class TestNeuroClientContextPublishing:
    """Test context publishing functionality"""
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.RegionalizationCore')
    async def test_publish_context_once(self, mock_reg_class):
        """Test publishing context once"""
        from src.dev.integration.client import NeuroClient
        
        mock_reg = Mock()
        mock_reg.force_update = AsyncMock()
        mock_reg.get_current_state = Mock(return_value=Mock())
        mock_reg.get_context_message = Mock(return_value="Context message")
        mock_reg_class.return_value = mock_reg
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        client.send_context = AsyncMock()
        
        await client._publish_context_once()
        
        mock_reg.force_update.assert_called_once()
        client.send_context.assert_called_once_with("Context message", silent=True)
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.RegionalizationCore')
    async def test_publish_context_skips_during_action(self, mock_reg_class):
        """Test that context publishing is skipped during action execution"""
        from src.dev.integration.client import NeuroClient
        
        mock_reg = Mock()
        mock_reg_class.return_value = mock_reg
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        client._action_in_progress = True
        client.send_context = AsyncMock()
        
        await client._publish_context_once()
        
        # Should not send context
        client.send_context.assert_not_called()
    
    @pytest.mark.asyncio
    async def test_publish_context_without_regionalization(self):
        """Test context publishing when regionalization is unavailable"""
        from src.dev.integration.client import NeuroClient
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        client._reg = None
        client.send_context = AsyncMock()
        
        await client._publish_context_once()
        
        # Should not crash, just skip
        client.send_context.assert_not_called()


class TestNeuroClientCallbacks:
    """Test client callback methods"""
    
    @pytest.mark.asyncio
    async def test_on_connect(self):
        """Test on_connect callback"""
        from src.dev.integration.client import NeuroClient
        
        mock_websocket = Mock()
        client = NeuroClient(mock_websocket)
        
        # Should not raise exception
        await client.on_connect()
    
    @pytest.mark.asyncio
    async def test_on_disconnect(self):
        """Test on_disconnect callback"""
        from src.dev.integration.client import NeuroClient
        
        mock_websocket = Mock()
        client = NeuroClient(mock_websocket)
        
        # Should not raise exception
        await client.on_disconnect()


class TestNeuroClientStart:
    """Test client start method"""
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.websockets.connect')
    async def test_start_method_connects(self, mock_ws_connect):
        """Test that start method connects to backend"""
        from src.dev.integration.client import NeuroClient
        
        mock_websocket = AsyncMock()
        mock_websocket.recv = AsyncMock(side_effect=asyncio.CancelledError())
        mock_ws_connect.return_value.__aenter__.return_value = mock_websocket
        
        try:
            await NeuroClient.start(
                windows_api_uri="ws://localhost:8765",
                neuro_backend_uri="ws://localhost:8000",
                auth_token=TEST_AUTH_TOKEN
            )
        except asyncio.CancelledError:
            pass
        
        mock_ws_connect.assert_called_once_with("ws://localhost:8000")


class TestNeuroClientIntegration:
    """Integration tests for NeuroClient"""
    
    @pytest.mark.asyncio
    @patch('src.dev.integration.client.load_actions')
    @patch('src.dev.integration.client.RegionalizationCore')
    async def test_full_initialization_workflow(self, mock_reg_class, mock_load_actions):
        """Test complete initialization workflow"""
        from src.dev.integration.client import NeuroClient
        from neuro_api.command import Action
        
        # Setup mocks
        mock_action = Mock(spec=Action)
        mock_action.name = "test_action"
        mock_load_actions.return_value = [mock_action]
        
        mock_reg = Mock()
        mock_reg.force_update = AsyncMock()
        mock_reg.get_current_state = Mock(return_value=None)
        mock_reg_class.return_value = mock_reg
        
        mock_websocket = AsyncMock()
        client = NeuroClient(mock_websocket)
        
        # Mock parent class methods
        client.send_startup_command = AsyncMock()
        client.register_actions = AsyncMock()
        client.send_context = AsyncMock()
        
        # Initialize
        await client.initialize()
        
        # Verify workflow
        client.send_startup_command.assert_called_once()
        client.register_actions.assert_called_once()
        assert client.send_context.called


if __name__ == "__main__":
    pytest.main([__file__, "-v"])