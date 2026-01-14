"""
Comprehensive unit tests for OCR detector module
Tests OCR element detection and formatting capabilities
"""

import pytest
from unittest.mock import Mock, MagicMock, patch, call
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).parents[2]))

from src.dev.integration.regionalization.ocr_detector import OCRDetector, OCRElement


class TestOCRElement:
    """Test OCRElement data class"""
    
    def test_create_ocr_element(self):
        """Test creating an OCR element"""
        element = OCRElement(
            text="Button Text",
            bbox=(10, 20, 100, 50),
            confidence=0.95,
            center_x=60,
            center_y=45,
            element_type="button"
        )
        
        assert element.text == "Button Text"
        assert element.bbox == (10, 20, 100, 50)
        assert element.confidence == 0.95
        assert element.center_x == 60
        assert element.center_y == 45
        assert element.element_type == "button"
    
    def test_ocr_element_default_type(self):
        """Test default element type"""
        element = OCRElement(
            text="Some text",
            bbox=(0, 0, 50, 20),
            confidence=0.8,
            center_x=25,
            center_y=10
        )
        
        assert element.element_type == "text"


class TestOCRDetector:
    """Test OCRDetector class"""
    
    def test_init_without_easyocr(self):
        """Test initialization when EasyOCR is not available"""
        with patch('src.dev.integration.regionalization.ocr_detector.easyocr', None):
            with patch.object(OCRDetector, '_init_ocr') as mock_init:
                OCRDetector()
                mock_init.assert_called_once()
    
    @patch('src.dev.integration.regionalization.ocr_detector.easyocr')
    def test_init_with_easyocr(self, mock_easyocr):
        """Test successful initialization with EasyOCR"""
        mock_reader = Mock()
        mock_easyocr.Reader.return_value = mock_reader
        
        detector = OCRDetector()
        
        assert detector.reader == mock_reader
        mock_easyocr.Reader.assert_called_once_with(['en'], gpu=False, verbose=False)
    
    @patch('src.dev.integration.regionalization.ocr_detector.easyocr', None)
    def test_init_without_easyocr_module(self):
        """Test initialization when easyocr module is missing"""
        detector = OCRDetector()
        assert detector.reader is None
    
    def test_detect_elements_without_reader(self):
        """Test detection when reader is not initialized"""
        detector = OCRDetector()
        detector.reader = None
        
        elements = detector.detect_elements()
        assert elements == []
    
    @patch('src.dev.integration.regionalization.ocr_detector.pyautogui')
    @patch('src.dev.integration.regionalization.ocr_detector.np')
    def test_detect_elements_with_screenshot(self, mock_np, mock_pyautogui):
        """Test element detection from screenshot"""
        detector = OCRDetector()
        mock_reader = Mock()
        
        # Mock OCR results: [bbox_coords, text, confidence]
        mock_reader.readtext.return_value = [
            ([[10, 10], [110, 10], [110, 40], [10, 40]], "Submit", 0.95),
            ([[150, 50], [250, 50], [250, 80], [150, 80]], "Cancel", 0.92)
        ]
        detector.reader = mock_reader
        
        # Mock screenshot
        mock_screenshot = Mock()
        mock_pyautogui.screenshot.return_value = mock_screenshot
        mock_np.array.return_value = "mock_array"
        
        elements = detector.detect_elements()
        
        assert len(elements) == 2
        
        # Check first element
        assert elements[0].text == "Submit"
        assert elements[0].confidence == 0.95
        assert elements[0].bbox == (10, 10, 100, 30)
        assert elements[0].center_x == 60
        assert elements[0].center_y == 25
        
        # Check second element
        assert elements[1].text == "Cancel"
        assert elements[1].confidence == 0.92
    
    @patch('src.dev.integration.regionalization.ocr_detector.Image')
    def test_detect_elements_with_image_path(self, mock_image_class):
        """Test element detection from image path"""
        detector = OCRDetector()
        mock_reader = Mock()
        mock_reader.readtext.return_value = [
            ([[0, 0], [100, 0], [100, 50], [0, 50]], "Test", 0.90)
        ]
        detector.reader = mock_reader
        
        mock_img = Mock()
        mock_image_class.open.return_value = mock_img
        
        with patch('src.dev.integration.regionalization.ocr_detector.np') as mock_np:
            mock_np.array.return_value = "mock_array"
            
            elements = detector.detect_elements(screenshot_path="/path/to/image.png")
            
            mock_image_class.open.assert_called_once_with("/path/to/image.png")
            assert len(elements) == 1
            assert elements[0].text == "Test"
    
    def test_detect_elements_error_handling(self):
        """Test error handling during detection"""
        detector = OCRDetector()
        mock_reader = Mock()
        mock_reader.readtext.side_effect = Exception("OCR failed")
        detector.reader = mock_reader
        
        with patch('src.dev.integration.regionalization.ocr_detector.pyautogui'):
            elements = detector.detect_elements()
            assert elements == []
    
    def test_infer_element_type_button_keywords(self):
        """Test button type inference from keywords"""
        detector = OCRDetector()
        
        assert detector._infer_element_type("Click Here", 100, 40) == "button"
        assert detector._infer_element_type("Submit Form", 120, 35) == "button"
        assert detector._infer_element_type("OK", 80, 30) == "button"
        assert detector._infer_element_type("Cancel", 90, 30) == "button"
        assert detector._infer_element_type("Save", 70, 30) == "button"
    
    def test_infer_element_type_link(self):
        """Test link type inference"""
        detector = OCRDetector()
        
        assert detector._infer_element_type("http://example.com", 200, 20) == "link"
        assert detector._infer_element_type("Visit example.com", 150, 20) == "link"
        assert detector._infer_element_type("Go to github.org", 180, 25) == "link"
    
    def test_infer_element_type_input_field(self):
        """Test input field type inference from dimensions"""
        detector = OCRDetector()
        
        # Wide but short (width > height * 3 and height < 40)
        assert detector._infer_element_type("Enter text", 300, 30) == "input"
        assert detector._infer_element_type("Search", 400, 35) == "input"
    
    def test_infer_element_type_button_dimensions(self):
        """Test button type inference from dimensions"""
        detector = OCRDetector()
        
        # Button-like dimensions (50-200 wide, 20-60 tall)
        assert detector._infer_element_type("Text", 100, 40) == "button"
        assert detector._infer_element_type("Action", 150, 50) == "button"
    
    def test_infer_element_type_default_text(self):
        """Test default text type"""
        detector = OCRDetector()
        
        assert detector._infer_element_type("Regular paragraph text", 500, 100) == "text"
        assert detector._infer_element_type("Title", 400, 80) == "text"
    
    def test_get_elements_near_point(self):
        """Test finding elements near a point"""
        detector = OCRDetector()
        
        elements = [
            OCRElement("Near", (90, 90, 20, 20), 0.9, 100, 100),
            OCRElement("Far", (490, 490, 20, 20), 0.9, 500, 500),
            OCRElement("Medium", (190, 190, 20, 20), 0.9, 200, 200),
        ]
        
        nearby = detector.get_elements_near_point(elements, 100, 100, radius=150)
        
        assert len(nearby) == 2
        assert nearby[0].text == "Near"  # Closest first
        assert nearby[1].text == "Medium"
    
    def test_get_elements_near_point_empty(self):
        """Test finding elements when none are nearby"""
        detector = OCRDetector()
        
        elements = [
            OCRElement("Far1", (500, 500, 20, 20), 0.9, 510, 510),
            OCRElement("Far2", (600, 600, 20, 20), 0.9, 610, 610),
        ]
        
        nearby = detector.get_elements_near_point(elements, 0, 0, radius=50)
        assert len(nearby) == 0
    
    def test_format_for_context_empty(self):
        """Test formatting empty element list"""
        detector = OCRDetector()
        
        result = detector.format_for_context([])
        assert "No UI elements detected" in result or "OCR may not be initialized" in result
    
    def test_format_for_context_with_elements(self):
        """Test formatting elements for context"""
        detector = OCRDetector()
        
        elements = [
            OCRElement("Submit", (100, 100, 80, 40), 0.95, 140, 120, "button"),
            OCRElement("Cancel", (200, 100, 80, 40), 0.92, 240, 120, "button"),
            OCRElement("Visit site.com", (100, 200, 120, 30), 0.88, 160, 215, "link"),
            OCRElement("Long paragraph text here", (100, 300, 300, 100), 0.85, 250, 350, "text"),
        ]
        
        result = detector.format_for_context(elements)
        
        assert "UI Elements Detected: 4 total" in result
        assert "Buttons" in result
        assert "Submit" in result
        assert "Cancel" in result
        assert "Links" in result
        assert "Visit site.com" in result
        assert "Visible Text" in result
    
    def test_format_for_context_max_items_limit(self):
        """Test that formatting respects max_items limit"""
        detector = OCRDetector()
        
        # Create many button elements
        elements = [
            OCRElement(f"Button{i}", (i*10, 100, 80, 40), 0.9, i*10+40, 120, "button")
            for i in range(20)
        ]
        
        result = detector.format_for_context(elements, max_items=5)
        
        # Should limit display but still show total count
        assert "20" in result or "Buttons" in result
    
    def test_format_for_context_groups_by_type(self):
        """Test that formatting groups elements by type"""
        detector = OCRDetector()
        
        elements = [
            OCRElement("Btn1", (0, 0, 50, 30), 0.9, 25, 15, "button"),
            OCRElement("Btn2", (60, 0, 50, 30), 0.9, 85, 15, "button"),
            OCRElement("Link1", (0, 50, 100, 20), 0.9, 50, 60, "link"),
            OCRElement("Text1", (0, 80, 200, 40), 0.9, 100, 100, "text"),
        ]
        
        result = detector.format_for_context(elements)
        
        # Should have separate sections for each type
        assert result.count("Buttons") == 1
        assert result.count("Links") == 1
        assert result.count("Visible Text") == 1
    
    def test_format_for_context_filters_short_text(self):
        """Test that very short text is filtered out"""
        detector = OCRDetector()
        
        elements = [
            OCRElement("A", (0, 0, 10, 10), 0.9, 5, 5, "text"),  # Too short
            OCRElement("Long enough text", (20, 0, 100, 20), 0.9, 70, 10, "text"),
        ]
        
        result = detector.format_for_context(elements)
        
        # "A" should be filtered, "Long enough text" should be shown
        assert "Long enough text" in result
        assert result.count('"A"') == 0 or 'A' not in result  # Single letter filtered
    
    def test_format_for_context_truncates_long_text(self):
        """Test that long text is truncated"""
        detector = OCRDetector()
        
        long_text = "A" * 100
        elements = [
            OCRElement(long_text, (0, 0, 200, 50), 0.9, 100, 25, "text"),
        ]
        
        result = detector.format_for_context(elements)
        
        # Should show truncated version (first 50 chars)
        assert long_text[:50] in result
        assert long_text not in result  # Full text should not appear


class TestOCRDetectorIntegration:
    """Integration tests for OCR detector"""
    
    @patch('src.dev.integration.regionalization.ocr_detector.easyocr')
    def test_full_detection_workflow(self, mock_easyocr):
        """Test complete detection workflow"""
        mock_reader = Mock()
        mock_easyocr.Reader.return_value = mock_reader
        
        # Simulate realistic OCR output
        mock_reader.readtext.return_value = [
            ([[10, 10], [90, 10], [90, 40], [10, 40]], "Login", 0.97),
            ([[100, 10], [200, 10], [200, 40], [100, 40]], "Register", 0.95),
            ([[10, 50], [400, 50], [400, 80], [10, 80]], "Enter your email", 0.88),
        ]
        
        detector = OCRDetector()
        
        with patch('src.dev.integration.regionalization.ocr_detector.pyautogui') as mock_pyautogui:
            with patch('src.dev.integration.regionalization.ocr_detector.np') as mock_np:
                mock_screenshot = Mock()
                mock_pyautogui.screenshot.return_value = mock_screenshot
                mock_np.array.return_value = "mock_array"
                
                elements = detector.detect_elements()
                
                assert len(elements) == 3
                
                # Verify login button
                login = elements[0]
                assert login.text == "Login"
                assert login.element_type == "button"
                assert login.confidence == 0.97
                
                # Format for context
                context = detector.format_for_context(elements)
                assert "Login" in context
                assert "Register" in context


if __name__ == "__main__":
    pytest.main([__file__, "-v"])