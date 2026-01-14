"""
Comprehensive unit tests for Action schema modules
Tests all action schema definitions and validation
"""

import pytest
from unittest.mock import Mock, patch
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).parents[2]))


class TestClickActionSchema:
    """Test click action schema"""
    
    @patch('src.dev.integration.Actions.click.pyautogui')
    def test_click_schema_structure(self, mock_pyautogui):
        """Test click schema returns proper Action object"""
        mock_pyautogui.size.return_value = (1920, 1080)
        
        from src.dev.integration.Actions.click import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "click"
        assert "click" in action.description.lower()
    
    @patch('src.dev.integration.Actions.click.pyautogui')
    def test_click_schema_properties(self, mock_pyautogui):
        """Test click schema has correct properties"""
        mock_pyautogui.size.return_value = (1920, 1080)
        
        from src.dev.integration.Actions.click import schema
        
        action = schema()
        properties = action.schema["properties"]
        
        assert "x" in properties
        assert "y" in properties
        assert "button" in properties
        
        # Check x coordinate validation
        assert properties["x"]["type"] == "integer"
        assert properties["x"]["minimum"] == 0
        assert properties["x"]["maximum"] == 1919
        
        # Check y coordinate validation
        assert properties["y"]["type"] == "integer"
        assert properties["y"]["minimum"] == 0
        assert properties["y"]["maximum"] == 1079
        
        # Check button options
        assert properties["button"]["type"] == "string"
        assert set(properties["button"]["enum"]) == {"left", "right", "middle"}
    
    @patch('src.dev.integration.Actions.click.pyautogui')
    def test_click_schema_required_fields(self, mock_pyautogui):
        """Test click schema required fields"""
        mock_pyautogui.size.return_value = (1920, 1080)
        
        from src.dev.integration.Actions.click import schema
        
        action = schema()
        required = action.schema.get("required", [])
        
        assert "button" in required
    
    @patch('src.dev.integration.Actions.click.pyautogui')
    def test_click_schema_adapts_to_screen_size(self, mock_pyautogui):
        """Test click schema adapts to different screen sizes"""
        mock_pyautogui.size.return_value = (2560, 1440)
        
        from src.dev.integration.Actions.click import schema
        
        action = schema()
        properties = action.schema["properties"]
        
        assert properties["x"]["maximum"] == 2559
        assert properties["y"]["maximum"] == 1439


class TestMoveActionSchema:
    """Test move action schema"""
    
    @patch('src.dev.integration.Actions.move.pyautogui')
    def test_move_schema_structure(self, mock_pyautogui):
        """Test move schema returns proper Action object"""
        mock_pyautogui.size.return_value = (1920, 1080)
        
        from src.dev.integration.Actions.move import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "move"
        assert "move" in action.description.lower()
    
    @patch('src.dev.integration.Actions.move.pyautogui')
    def test_move_schema_properties(self, mock_pyautogui):
        """Test move schema has correct properties"""
        mock_pyautogui.size.return_value = (1920, 1080)
        
        from src.dev.integration.Actions.move import schema
        
        action = schema()
        properties = action.schema["properties"]
        
        assert "x" in properties
        assert "y" in properties
        
        assert properties["x"]["type"] == "integer"
        assert properties["y"]["type"] == "integer"
    
    @patch('src.dev.integration.Actions.move.pyautogui')
    def test_move_schema_required_fields(self, mock_pyautogui):
        """Test move schema required fields"""
        mock_pyautogui.size.return_value = (1920, 1080)
        
        from src.dev.integration.Actions.move import schema
        
        action = schema()
        required = action.schema.get("required", [])
        
        assert "x" in required
        assert "y" in required


class TestHotkeyActionSchema:
    """Test hotkey action schema"""
    
    def test_hotkey_schema_structure(self):
        """Test hotkey schema returns proper Action object"""
        from src.dev.integration.Actions.hotkey import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "hotkey"
        assert "hotkey" in action.description.lower() or "combination" in action.description.lower()
    
    def test_hotkey_schema_properties(self):
        """Test hotkey schema has correct properties"""
        from src.dev.integration.Actions.hotkey import schema
        
        action = schema()
        properties = action.schema["properties"]
        
        assert "keys" in properties
        assert properties["keys"]["type"] == "array"
        assert properties["keys"]["items"]["type"] == "string"
        assert properties["keys"]["minItems"] == 1
    
    def test_hotkey_schema_required_fields(self):
        """Test hotkey schema required fields"""
        from src.dev.integration.Actions.hotkey import schema
        
        action = schema()
        required = action.schema.get("required", [])
        
        assert "keys" in required


class TestScreenshotActionSchema:
    """Test screenshot action schema"""
    
    def test_screenshot_schema_structure(self):
        """Test screenshot schema returns proper Action object"""
        from src.dev.integration.Actions.screenshot import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "screenshot"
        assert "screenshot" in action.description.lower()
    
    def test_screenshot_schema_properties(self):
        """Test screenshot schema has correct properties"""
        from src.dev.integration.Actions.screenshot import schema
        
        action = schema()
        properties = action.schema["properties"]
        
        assert "name" in properties
        assert "region" in properties
        
        assert properties["name"]["type"] == "string"
        assert properties["region"]["type"] == "array"
        assert properties["region"]["items"]["type"] == "integer"
        assert properties["region"]["minItems"] == 4
        assert properties["region"]["maxItems"] == 4


class TestKeyPressActionSchemas:
    """Test key press action schemas (press, keydown, keyup)"""
    
    def test_press_schema_structure(self):
        """Test press schema"""
        from src.dev.integration.Actions.press import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "press"
    
    def test_keydown_schema_structure(self):
        """Test keydown schema"""
        from src.dev.integration.Actions.keydown import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "keydown"
    
    def test_keyup_schema_structure(self):
        """Test keyup schema"""
        from src.dev.integration.Actions.keyup import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "keyup"


class TestDragActionSchemas:
    """Test drag action schemas (dragto, dragrel)"""
    
    @patch('src.dev.integration.Actions.dragto.pyautogui')
    def test_dragto_schema_structure(self, mock_pyautogui):
        """Test dragto schema"""
        mock_pyautogui.size.return_value = (1920, 1080)
        
        from src.dev.integration.Actions.dragto import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "dragto"
    
    @patch('src.dev.integration.Actions.dragto.pyautogui')
    def test_dragto_schema_properties(self, mock_pyautogui):
        """Test dragto schema has destination coordinates"""
        mock_pyautogui.size.return_value = (1920, 1080)
        
        from src.dev.integration.Actions.dragto import schema
        
        action = schema()
        properties = action.schema["properties"]
        
        assert "x" in properties
        assert "y" in properties
    
    def test_dragrel_schema_structure(self):
        """Test dragrel schema"""
        from src.dev.integration.Actions.dragrel import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "dragrel"
    
    def test_dragrel_schema_properties(self):
        """Test dragrel schema has relative coordinates"""
        from src.dev.integration.Actions.dragrel import schema
        
        action = schema()
        properties = action.schema["properties"]
        
        assert "x" in properties or "xOffset" in properties or "dx" in properties
        assert "y" in properties or "yOffset" in properties or "dy" in properties


class TestContextActionSchemas:
    """Test context-related action schemas"""
    
    def test_refresh_context_schema(self):
        """Test refresh_context schema"""
        from src.dev.integration.Actions.refresh_context import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "refresh_context"
    
    def test_get_more_text_schema(self):
        """Test get_more_text schema"""
        from src.dev.integration.Actions.get_more_text import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "get_more_text"
    
    def test_get_more_windows_schema(self):
        """Test get_more_windows schema"""
        from src.dev.integration.Actions.get_more_windows import schema
        from neuro_api.command import Action
        
        action = schema()
        
        assert isinstance(action, Action)
        assert action.name == "get_more_windows"


class TestActionLoader:
    """Test action loading functionality"""
    
    def test_load_actions_function_exists(self):
        """Test that load_actions function is importable"""
        from src.dev.integration import load_actions
        
        assert callable(load_actions)
    
    def test_load_actions_returns_list(self):
        """Test that load_actions returns a list"""
        from src.dev.integration import load_actions
        
        actions = load_actions()
        
        assert isinstance(actions, list)
    
    def test_load_actions_returns_action_objects(self):
        """Test that all loaded items are Action objects"""
        from src.dev.integration import load_actions
        from neuro_api.command import Action
        
        actions = load_actions()
        
        for action in actions:
            assert isinstance(action, Action)
    
    def test_load_actions_includes_expected_actions(self):
        """Test that common actions are loaded"""
        from src.dev.integration import load_actions
        
        actions = load_actions()
        action_names = [a.name for a in actions]
        
        expected_actions = ['click', 'move', 'hotkey', 'screenshot']
        for expected in expected_actions:
            assert expected in action_names, f"Expected action '{expected}' not found"
    
    def test_load_actions_no_duplicates(self):
        """Test that no duplicate action names are loaded"""
        from src.dev.integration import load_actions
        
        actions = load_actions()
        action_names = [a.name for a in actions]
        
        assert len(action_names) == len(set(action_names)), "Duplicate action names found"
    
    @patch('src.dev.integration.os.listdir')
    def test_load_actions_handles_missing_schema(self, mock_listdir):
        """Test that load_actions handles files without schema function"""
        mock_listdir.return_value = ['valid_action.py', 'invalid_action.py', '__init__.py']
        
        from src.dev.integration import load_actions
        
        # Should not raise an exception
        actions = load_actions()
        assert isinstance(actions, list)
    
    def test_all_action_schemas_have_names(self):
        """Test that all loaded actions have valid names"""
        from src.dev.integration import load_actions
        
        actions = load_actions()
        
        for action in actions:
            assert action.name is not None
            assert len(action.name) > 0
            assert isinstance(action.name, str)
    
    def test_all_action_schemas_have_descriptions(self):
        """Test that all loaded actions have descriptions"""
        from src.dev.integration import load_actions
        
        actions = load_actions()
        
        for action in actions:
            assert action.description is not None
            assert len(action.description) > 0
            assert isinstance(action.description, str)
    
    def test_all_action_schemas_have_valid_json_schemas(self):
        """Test that all loaded actions have valid JSON schemas"""
        from src.dev.integration import load_actions
        
        actions = load_actions()
        
        for action in actions:
            assert action.schema is not None
            assert isinstance(action.schema, dict)
            assert "type" in action.schema
            assert action.schema["type"] == "object"


class TestActionSchemaValidation:
    """Test validation logic in action schemas"""
    
    @patch('src.dev.integration.Actions.click.pyautogui')
    def test_click_coordinates_within_screen_bounds(self, mock_pyautogui):
        """Test that click schema enforces screen boundaries"""
        mock_pyautogui.size.return_value = (1920, 1080)
        
        from src.dev.integration.Actions.click import schema
        
        action = schema()
        x_max = action.schema["properties"]["x"]["maximum"]
        y_max = action.schema["properties"]["y"]["maximum"]
        
        # Max should be screen dimension - 1
        assert x_max == 1919
        assert y_max == 1079
    
    def test_hotkey_requires_at_least_one_key(self):
        """Test that hotkey schema requires at least one key"""
        from src.dev.integration.Actions.hotkey import schema
        
        action = schema()
        
        min_items = action.schema["properties"]["keys"]["minItems"]
        assert min_items >= 1
    
    def test_screenshot_region_has_exactly_four_values(self):
        """Test that screenshot region must have exactly 4 values"""
        from src.dev.integration.Actions.screenshot import schema
        
        action = schema()
        region_schema = action.schema["properties"]["region"]
        
        assert region_schema["minItems"] == 4
        assert region_schema["maxItems"] == 4


if __name__ == "__main__":
    pytest.main([__file__, "-v"])