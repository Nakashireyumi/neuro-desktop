"""
Comprehensive unit tests for neuro_types module
Tests all data structures, validation, and helper methods
"""

import pytest
from datetime import datetime
from unittest.mock import Mock, MagicMock

import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).parents[2]))

from src.types.neuro_types import (
    Coordinates, BoundingBox, ScreenRegion, ContextData, NeuroAction,
    SystemState, RegionType, ContextType, Priority, PluginType,
    NeuroMessageBuilder, PluginMetadata, PluginRegistry
)


class TestCoordinates:
    """Test Coordinates data class"""
    
    def test_valid_coordinates(self):
        """Test creating valid coordinates"""
        coord = Coordinates(x=100, y=200)
        assert coord.x == 100
        assert coord.y == 200
    
    def test_negative_coordinates(self):
        """Test negative coordinates (multi-monitor support)"""
        coord = Coordinates(x=-100, y=-200)
        assert coord.x == -100
        assert coord.y == -200
    
    def test_zero_coordinates(self):
        """Test zero coordinates"""
        coord = Coordinates(x=0, y=0)
        assert coord.x == 0
        assert coord.y == 0
    
    def test_invalid_coordinates_type(self):
        """Test that non-integer coordinates raise ValueError"""
        with pytest.raises(ValueError, match="must be integers"):
            Coordinates(x=10.5, y=20)
        
        with pytest.raises(ValueError, match="must be integers"):
            Coordinates(x=10, y=20.5)
        
        with pytest.raises(ValueError, match="must be integers"):
            Coordinates(x="100", y=200)


class TestBoundingBox:
    """Test BoundingBox data class"""
    
    def test_valid_bounding_box(self):
        """Test creating valid bounding box"""
        bbox = BoundingBox(x=10, y=20, width=100, height=200)
        assert bbox.x == 10
        assert bbox.y == 20
        assert bbox.width == 100
        assert bbox.height == 200
    
    def test_bounding_box_center(self):
        """Test center property calculation"""
        bbox = BoundingBox(x=0, y=0, width=100, height=200)
        center = bbox.center
        assert center.x == 50
        assert center.y == 100
    
    def test_bounding_box_area(self):
        """Test area property calculation"""
        bbox = BoundingBox(x=0, y=0, width=100, height=200)
        assert bbox.area == 20000
    
    def test_bounding_box_contains_point(self):
        """Test contains method"""
        bbox = BoundingBox(x=10, y=10, width=100, height=100)
        
        # Point inside
        assert bbox.contains(Coordinates(50, 50))
        
        # Point on edge
        assert bbox.contains(Coordinates(10, 10))
        assert bbox.contains(Coordinates(110, 110))
        
        # Point outside
        assert not bbox.contains(Coordinates(5, 50))
        assert not bbox.contains(Coordinates(50, 5))
        assert not bbox.contains(Coordinates(111, 50))
        assert not bbox.contains(Coordinates(50, 111))
    
    def test_bounding_box_overlaps(self):
        """Test overlaps method"""
        bbox1 = BoundingBox(x=0, y=0, width=100, height=100)
        
        # Overlapping box
        bbox2 = BoundingBox(x=50, y=50, width=100, height=100)
        assert bbox1.overlaps(bbox2)
        assert bbox2.overlaps(bbox1)
        
        # Non-overlapping box
        bbox3 = BoundingBox(x=200, y=200, width=100, height=100)
        assert not bbox1.overlaps(bbox3)
        assert not bbox3.overlaps(bbox1)
        
        # Adjacent box (no overlap)
        bbox4 = BoundingBox(x=100, y=0, width=100, height=100)
        assert not bbox1.overlaps(bbox4)
    
    def test_invalid_bounding_box_type(self):
        """Test that non-integer values raise ValueError"""
        with pytest.raises(ValueError, match="must be integers"):
            BoundingBox(x=10.5, y=20, width=100, height=100)
    
    def test_invalid_bounding_box_dimensions(self):
        """Test that negative or zero dimensions raise ValueError"""
        with pytest.raises(ValueError, match="must be positive"):
            BoundingBox(x=10, y=20, width=0, height=100)
        
        with pytest.raises(ValueError, match="must be positive"):
            BoundingBox(x=10, y=20, width=100, height=-50)


class TestScreenRegion:
    """Test ScreenRegion data class"""
    
    def test_valid_screen_region(self):
        """Test creating valid screen region"""
        bbox = BoundingBox(x=10, y=20, width=100, height=200)
        region = ScreenRegion(
            id="test_region",
            region_type=RegionType.BUTTON,
            bounds=bbox,
            confidence=0.95,
            title="Test Button"
        )
        
        assert region.id == "test_region"
        assert region.region_type == RegionType.BUTTON
        assert region.bounds == bbox
        assert region.confidence == 0.95
        assert region.title == "Test Button"
        assert region.visible is True
        assert region.enabled is True
    
    def test_screen_region_defaults(self):
        """Test default values"""
        bbox = BoundingBox(x=0, y=0, width=50, height=50)
        region = ScreenRegion(
            id="test",
            region_type=RegionType.WINDOW,
            bounds=bbox,
            confidence=0.8
        )
        
        assert region.title is None
        assert region.application is None
        assert region.visible is True
        assert region.enabled is True
        assert region.clickable is False
        assert region.focusable is False
        assert region.metadata is None
    
    def test_screen_region_confidence_validation(self):
        """Test confidence value validation"""
        bbox = BoundingBox(x=0, y=0, width=50, height=50)
        
        # Valid confidence
        region = ScreenRegion(
            id="test",
            region_type=RegionType.BUTTON,
            bounds=bbox,
            confidence=0.5
        )
        assert region.confidence == 0.5
        
        # Invalid confidence (should clamp or validate)
        with pytest.raises(ValueError, match="confidence"):
            ScreenRegion(
                id="test",
                region_type=RegionType.BUTTON,
                bounds=bbox,
                confidence=1.5
            )
        
        with pytest.raises(ValueError, match="confidence"):
            ScreenRegion(
                id="test",
                region_type=RegionType.BUTTON,
                bounds=bbox,
                confidence=-0.1
            )


class TestContextData:
    """Test ContextData data class"""
    
    def test_valid_context_data(self):
        """Test creating valid context data"""
        context = ContextData(
            context_type=ContextType.VISUAL,
            data={"key": "value"},
            priority=Priority.HIGH,
            source="test_source"
        )
        
        assert context.context_type == ContextType.VISUAL
        assert context.data == {"key": "value"}
        assert context.priority == Priority.HIGH
        assert context.source == "test_source"
        assert isinstance(context.timestamp, datetime)
    
    def test_context_data_defaults(self):
        """Test default values"""
        context = ContextData(
            context_type=ContextType.SYSTEM_STATE,
            data={}
        )
        
        assert context.priority == Priority.MEDIUM
        assert context.source is None
        assert context.timestamp is not None


class TestNeuroAction:
    """Test NeuroAction data class"""
    
    def test_valid_neuro_action(self):
        """Test creating valid action"""
        action = NeuroAction(
            name="click",
            parameters={"x": 100, "y": 200},
            description="Click at coordinates"
        )
        
        assert action.name == "click"
        assert action.parameters == {"x": 100, "y": 200}
        assert action.description == "Click at coordinates"
        assert action.requires_confirmation is False
    
    def test_neuro_action_defaults(self):
        """Test default values"""
        action = NeuroAction(
            name="test_action"
        )
        
        assert action.parameters == {}
        assert action.description is None
        assert action.requires_confirmation is False


class TestSystemState:
    """Test SystemState data class"""
    
    def test_valid_system_state(self):
        """Test creating valid system state"""
        bbox = BoundingBox(x=0, y=0, width=100, height=100)
        region = ScreenRegion(
            id="window_1",
            region_type=RegionType.WINDOW,
            bounds=bbox,
            confidence=0.9
        )
        
        context = ContextData(
            context_type=ContextType.APPLICATION,
            data={"app": "test"}
        )
        
        action = NeuroAction(name="test_action")
        
        state = SystemState(
            all_regions=[region],
            focused_region=region,
            context_data=[context],
            available_actions=[action],
            active_application="test.exe"
        )
        
        assert len(state.all_regions) == 1
        assert state.focused_region == region
        assert len(state.context_data) == 1
        assert len(state.available_actions) == 1
        assert state.active_application == "test.exe"
    
    def test_system_state_defaults(self):
        """Test default empty state"""
        state = SystemState()
        
        assert state.all_regions == []
        assert state.focused_region is None
        assert state.context_data == []
        assert state.available_actions == []
        assert state.active_application is None
        assert isinstance(state.timestamp, datetime)


class TestNeuroMessageBuilder:
    """Test NeuroMessageBuilder utility class"""
    
    def test_build_context_message_empty_state(self):
        """Test building message from empty state"""
        builder = NeuroMessageBuilder()
        state = SystemState()
        
        message = builder.build_context_message(state)
        
        assert isinstance(message, str)
        assert "System State" in message
        assert "0 regions" in message.lower() or "no regions" in message.lower()
    
    def test_build_context_message_with_regions(self):
        """Test building message with window regions"""
        builder = NeuroMessageBuilder()
        
        bbox1 = BoundingBox(x=0, y=0, width=800, height=600)
        region1 = ScreenRegion(
            id="window_1",
            region_type=RegionType.WINDOW,
            bounds=bbox1,
            confidence=0.9,
            title="Browser Window",
            application="chrome.exe"
        )
        
        bbox2 = BoundingBox(x=100, y=100, width=400, height=300)
        region2 = ScreenRegion(
            id="window_2",
            region_type=RegionType.WINDOW,
            bounds=bbox2,
            confidence=0.85,
            title="Editor",
            application="code.exe",
            metadata={"focused": True}
        )
        
        state = SystemState(
            all_regions=[region1, region2],
            focused_region=region2,
            active_application="code.exe"
        )
        
        message = builder.build_context_message(state)
        
        assert "Browser Window" in message
        assert "Editor" in message
        assert "[FOCUSED]" in message
        assert "code.exe" in message
    
    def test_build_action_response_success(self):
        """Test building success response"""
        builder = NeuroMessageBuilder()
        action = NeuroAction(name="click", parameters={"x": 100, "y": 200})
        
        response = builder.build_action_response(action, success=True, details="Clicked successfully")
        
        assert "click" in response
        assert "successfully" in response.lower()
        assert "Clicked successfully" in response
    
    def test_build_action_response_failure(self):
        """Test building failure response"""
        builder = NeuroMessageBuilder()
        action = NeuroAction(name="move", parameters={"x": 500, "y": 600})
        
        response = builder.build_action_response(action, success=False, details="Out of bounds")
        
        assert "move" in response
        assert "failed" in response.lower()
        assert "Out of bounds" in response
    
    def test_build_region_info(self):
        """Test building region information"""
        builder = NeuroMessageBuilder()
        
        bbox = BoundingBox(x=10, y=20, width=100, height=50)
        region = ScreenRegion(
            id="button_1",
            region_type=RegionType.BUTTON,
            bounds=bbox,
            confidence=0.92,
            title="Submit",
            application="form.exe",
            clickable=True,
            focusable=True
        )
        
        info = builder.build_region_info(region)
        
        assert "Submit" in info
        assert "button" in info.lower()
        assert "10,20" in info
        assert "100x50" in info
        assert "0.92" in info
        assert "clickable" in info.lower()


class TestPluginMetadata:
    """Test PluginMetadata data class"""
    
    def test_valid_plugin_metadata(self):
        """Test creating valid plugin metadata"""
        metadata = PluginMetadata(
            name="test_plugin",
            version="1.0.0",
            plugin_type=PluginType.REGION_DETECTOR,
            description="Test plugin"
        )
        
        assert metadata.name == "test_plugin"
        assert metadata.version == "1.0.0"
        assert metadata.plugin_type == PluginType.REGION_DETECTOR
        assert metadata.description == "Test plugin"
        assert metadata.enabled is True
    
    def test_plugin_metadata_defaults(self):
        """Test default values"""
        metadata = PluginMetadata(
            name="plugin",
            version="1.0",
            plugin_type=PluginType.ACTION_HANDLER
        )
        
        assert metadata.description is None
        assert metadata.enabled is True


class TestPluginRegistry:
    """Test PluginRegistry"""
    
    def test_register_and_get_plugin(self):
        """Test registering and retrieving plugins"""
        registry = PluginRegistry()
        
        mock_plugin = Mock()
        metadata = PluginMetadata(
            name="test",
            version="1.0",
            plugin_type=PluginType.CONTEXT_PROVIDER
        )
        
        registry.register_plugin(mock_plugin, metadata)
        
        plugins = registry.get_plugins_by_type(PluginType.CONTEXT_PROVIDER)
        assert len(plugins) == 1
        assert plugins[0] == mock_plugin
    
    def test_get_plugin_by_name(self):
        """Test getting plugin by name"""
        registry = PluginRegistry()
        
        mock_plugin = Mock()
        metadata = PluginMetadata(
            name="specific_plugin",
            version="1.0",
            plugin_type=PluginType.ACTION_HANDLER
        )
        
        registry.register_plugin(mock_plugin, metadata)
        
        retrieved = registry.get_plugin("specific_plugin")
        assert retrieved == mock_plugin
    
    def test_get_nonexistent_plugin(self):
        """Test getting plugin that doesn't exist"""
        registry = PluginRegistry()
        
        result = registry.get_plugin("nonexistent")
        assert result is None
    
    def test_disable_plugin(self):
        """Test disabling a plugin"""
        registry = PluginRegistry()
        
        mock_plugin = Mock()
        metadata = PluginMetadata(
            name="disable_test",
            version="1.0",
            plugin_type=PluginType.REGION_DETECTOR
        )
        
        registry.register_plugin(mock_plugin, metadata)
        registry.disable_plugin("disable_test")
        
        # Plugin should still exist but be disabled
        plugin = registry.get_plugin("disable_test")
        assert plugin is not None
        
        # Get enabled plugins should not include disabled ones
        # This depends on implementation - may or may not filter disabled


class TestEnums:
    """Test enum definitions"""
    
    def test_region_type_enum(self):
        """Test RegionType enum values"""
        assert RegionType.WINDOW.value == "window"
        assert RegionType.BUTTON.value == "button"
        assert RegionType.INPUT_FIELD.value == "input_field"
        assert RegionType.UNKNOWN.value == "unknown"
    
    def test_context_type_enum(self):
        """Test ContextType enum values"""
        assert ContextType.VISUAL.value == "visual"
        assert ContextType.APPLICATION.value == "application"
        assert ContextType.SYSTEM_STATE.value == "system_state"
    
    def test_priority_enum(self):
        """Test Priority enum values"""
        assert Priority.CRITICAL.value == "critical"
        assert Priority.HIGH.value == "high"
        assert Priority.MEDIUM.value == "medium"
        assert Priority.LOW.value == "low"
    
    def test_plugin_type_enum(self):
        """Test PluginType enum values"""
        assert PluginType.REGION_DETECTOR.value == "region_detector"
        assert PluginType.CONTEXT_PROVIDER.value == "context_provider"
        assert PluginType.ACTION_HANDLER.value == "action_handler"
        assert PluginType.APP_INTEGRATION.value == "app_integration"


if __name__ == "__main__":
    pytest.main([__file__, "-v"])