# Neuro-OS Changelog

All notable changes and improvements to Neuro-OS are documented here.

---

## [0.0.3a-dev] - 2025-11-02 - Developmental Cache
No info

---

## [0.0.2-alpha] - 2025-01-22 - Major UI Context & Security Update

### 🎉 Major Features Added

#### Enhanced UI Detection System
- **OCR-based UI element detection** using EasyOCR
  - Automatically detects text, buttons, links, and input fields
  - Provides exact coordinates for every UI element
  - Groups elements by type for better organization
  - Runs locally (no API calls required)
  
- **Vision API integration** (optional)
  - AI-powered screenshot analysis using Groq Llama Vision
  - Semantic understanding of complex UIs
  - Contextual descriptions of UI layout
  - Session-based authentication

#### Security Improvements
- **Session-based authentication** for Nakurity backend
  - Automatic session claim/heartbeat/release
  - Machine fingerprinting (IP + User-Agent hash)
  - 5-minute timeout with heartbeat keep-alive
  - Prevents API key sharing/stealing

### ✅ Bugs Fixed

#### Context Message Formatting
- **Fixed**: Malformed action descriptions showing `clickNeuro-OS Windows integration is active...`
- **Solution**: Corrected string concatenation in action registration
- **Impact**: Clean, readable context messages for Neuro

#### Coordinate Validation
- **Fixed**: PyAutoGUI fail-safe errors when coordinates out of bounds
- **Solution**: Added coordinate clamping and validation
- **Impact**: No more crashes from invalid click positions

### 🔧 Technical Changes

#### Files Modified
```
neuro-os/
├── src/
│   ├── regionalization/
│   │   ├── core.py                    # Vision API integration
│   │   ├── ocr_detector.py            # EasyOCR element detection
│   │   └── vision_api_client.py       # Session-based API client
│   ├── dev/
│   │   └── neuro_integration/
│   │       └── client.py              # Fixed context formatting
│   └── types/
│       └── neuro_types.py             # Enhanced message builder
```

#### Nakurity Backend (Separate Repo)
```
nakurity-backend/
├── api/
│   ├── session-store.js               # Shared session management
│   ├── session.js                     # Session endpoints
│   └── vision.js                      # Vision API endpoint
└── vercel.json                        # Deployment config
```

### 📊 Performance Improvements
- **OCR detection**: Runs every 2 seconds, ~200-500ms latency
- **Vision API**: Runs every 20 seconds, ~2-5s latency
- **Context updates**: Only sent when state changes (saves API costs)

### 🔐 Security Updates
- Session-based authentication replaces static API keys
- Automatic session cleanup on exit
- Rate limiting (10 req/min) on Vision API
- Fingerprint validation prevents session hijacking

---

## [0.0.1-alpha] - Initial Release

### Context System Improvements
- Added screen resolution to context messages
- Included mouse position tracking
- Fixed window title truncation
- Added coordinate bounds checking

### Regionalization System
- Basic window detection using Windows API
- Application name extraction
- Focused window tracking
- Region-based action generation

---

## What Neuro Can See Now

### Before (v0.0.1-alpha)
```
Available Actions (15):
  - click_window_67420: Click on Visual Studio Code
  - click_window_198602: Click on Terminal
```

### After (v0.0.2-alpha)
```
Screen Resolution: 1920x1080 (coordinates: 0-1919, 0-1079)
Mouse Position: (640, 480)
Text near mouse: "Subscribe"

Active Application: chrome.exe

Detected Text on Screen (127 items):
  - "Subscribe" at (640, 480)
  - "Like" at (320, 450)
  - "Share" at (520, 450)
  ... and 124 more text items

UI Elements Detected: 45 total

Buttons (12):
  - "Subscribe" at (640, 480)
  - "Like" at (320, 450)
  - "Play" at (960, 540)

Links (8):
  - "Evil Neuro's Channel" at (200, 150)
  - "Watch Later" at (1700, 300)

Visible Windows (3):
  1. Evil Neuro - YouTube [FOCUSED]
     Position: (0, 0), Size: 1920x1080, Click center: (960, 540)

AI Vision Analysis (Optional):
The screenshot shows a YouTube video page with:
- Video player in center (large area)
- "Subscribe" button (red, top-right of video)
- Like/Dislike buttons below video
- Comment section at bottom
```

---

## Installation & Setup

### Quick Start
```bash
# Clone repository with submodules
git clone --recursive https://github.com/Nakashireyumi/neuro-os.git
cd neuro-os

# Automated setup (recommended)
node src/bin/neuro-setup.js --run

# Or manual setup:
pip install -r requirements.txt
pip install easyocr opencv-python pillow numpy requests

# Setup windows-api submodule
cd windows-api && vcpkg install
python -m venv .venv && .venv\Scripts\activate
pip install -r src/contributions/cassitly/python/requirements.txt
cd ..

# Run neuro-os
python -m src.dev.launch
```

### Optional: Enable Vision API
```python
# In src/dev/neuro_integration/client.py, line 34
self._reg = RegionalizationCore(enable_vision_api=True)
```

---

## Known Issues

### Windows-API Port Hangs (Pending Fix)
- **Issue**: Port 8766 remains occupied after stopping neuro-os
- **Workaround**: Manually kill process or restart system
- **Status**: Fix planned for next release

### OCR Limitations
- Requires Tesseract or EasyOCR installation
- May miss UI elements without visible text
- Accuracy varies with font size and contrast

### Vision API Limitations (Optional Feature)
- Rate limited to 10 requests per minute
- Requires Groq API key in backend
- Higher latency (~2-5 seconds) vs OCR

---

## Future Roadmap

### Planned Features
1. **Accessibility API Integration**: Use Windows UIA for semantic UI info
2. **Application-Specific Detectors**: Chrome, VSCode, Discord detectors
3. **Context Caching**: Cache OCR results for unchanged regions
4. **Enhanced Safety Monitor**: Priority system, user override protection
5. **Visual Feedback**: On-screen overlay showing detected elements

### Performance Optimizations
- Redis session store for production scale
- Context compression to reduce token usage
- Selective OCR scanning (only changed regions)

---

## Migration Guide

### Upgrading from v0.0.1-alpha to v0.0.2-alpha

1. **Install new dependencies:**
   ```bash
   pip install easyocr opencv-python pillow numpy requests
   # Or use automated setup:
   node src/bin/neuro-setup.js
   ```

2. **No config changes required** - OCR works out of the box

3. **Optional**: Enable Vision API for enhanced analysis
   ```python
   # In src/dev/neuro_integration/client.py
   self._reg = RegionalizationCore(enable_vision_api=True)
   ```

4. **Backend**: Deploy Nakurity backend to Vercel (optional, for Vision API)
   - Set `GROQ_API_KEY` environment variable
   - Deploy to `backend.nakurity.com`

---

## Credits

### Contributors
- **Core Development**: Nakashireyumi
- **UI Detection System**: OCR & Vision API integration
- **Backend Infrastructure**: Nakurity backend with Groq Vision

### Technologies Used
- **EasyOCR**: Text detection and recognition
- **Groq Llama Vision**: AI-powered UI analysis
- **Windows API**: Native window management
- **PyAutoGUI**: Screen automation
- **Neuro-sama API**: Integration with Neuro backend

---

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

## Support

### Issues & Bug Reports
- **GitHub Issues**: https://github.com/Nakashireyumi/neuro-os/issues
- **Documentation**: See README.md and this CHANGELOG.md

### Need Help?
- Check `QUICKSTART.md` for quick setup guide
- Review `IMPROVEMENTS.md` for technical details
- Test with `python -m src.regionalization.ocr_detector`

---

**Last Updated**: 2025-01-22
**Version**: 0.0.2-alpha
**Status**: 🚧 Alpha - Active Development
