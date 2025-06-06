
# Go V4L2 Camera Application - Multi-Backend Comparison

A comprehensive real-time camera streaming application written in Go that demonstrates **6 different GUI/backend approaches** for rendering camera feeds. This project showcases various Go GUI frameworks and their performance characteristics when handling live video streams.

## 🎯 Project Overview

This project provides **6 distinct implementations** of the same camera application, each using a different GUI framework or rendering backend. This allows for direct performance comparison and demonstrates the versatility of Go for multimedia applications.

## 🚀 Available Backends/Implementations

### 1. **Pure Gio** (`cd puregio`)
- **Framework**: Gio UI (Pure Go)
- **Description**: Single-window application with both controls and camera feed rendered in Gio
- **Features**: Native Go implementation, hardware-accelerated rendering
- **Build**: `go run -o puregio`

### 2. **Nucular + Gio Hybrid** (`cd nucular_gio`)
- **Framework**: Nucular (controls) + Gio (camera rendering)
- **Description**: Separate windows - Nucular for controls, Gio for optimized camera display
- **Features**: Good performance for camera rendering, familiar controls with Nucular
- **Build**: `go build -o nucular_gio`

### 3. **Nucular + SDL3 Backend** (`cd camera.go`)
- **Framework**: SDL3 + Nucular
- **Description**: SDL3 for camera rendering with Nucular control panel
- **Features**: Hardware acceleration, cross-platform compatibility
- **Build**: `go build -o camera_sdl3`

### 4. **Dear ImGui (CIMGUI-Go) + Ebiten** (`cd ebiten-cam`)
- **Framework**: Dear ImGui (CIMGUI-Go) with Ebiten backend
- **Description**: ImGui interface with Ebiten 2D game engine rendering
- **Features**: Game engine optimizations, Dear ImGui widgets
- **Build**: `go build  -o imgui_ebiten`

### 5. **GLFW/OpenGL** (`cd pureglfw`)
- **Framework**: GLFW with direct OpenGL backend
- **Description**: Raw OpenGL rendering for maximum performance
- **Features**: Direct GPU access, minimal overhead
- **Build**: `go build  -o imgui_opengl`

### 6. **Clay + SDL3** (`imgui_sdl.go`)
- **Framework**: Clay for layout with SDL3 backend
- **Description**: SDL3 for window management and rendering. Clay for UI layout management
- **Features**: Direct GPU access, minimal overhead
- **Build**: `go build -tags sdl -o imgui_sdl`

## 📊 Performance Comparison (TODO )

| Backend | Camera FPS | UI Responsiveness | Memory Usage | CPU Usage | Complexity |
|---------|------------|-------------------|--------------|-----------|------------|
| Pure Gio | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐ |
| Nucular+Gio | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ |
| SDL3 | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |
| ImGui+Ebiten | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |
| ImGui+OpenGL | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| ImGui+SDL2 | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ |

## 🎯 Key Features (All Implementations)

- **Real-time camera video streaming** (30-60 FPS)
- **Multiple camera support** with dynamic detection
- **Frame statistics monitoring** (FPS, dropped frames)
- **MJPEG video format support**
- **Configurable resolution** (default: 640x480)
- **Live camera switching**
- **Frame drop detection and recovery**
- **Cross-platform compatibility** (Linux primary, some Windows/macOS)

## 🛠️ Prerequisites

### Common Requirements
- **Go 1.24.3** or later
- **Linux operating system** (primary support)
- **V4L2-compatible webcam**
- **X11 development libraries**

### Backend-Specific Requirements

#### For SDL Backends:

# Ubuntu/Debian
`sudo apt-get install libsdl2-dev libsdl2-image-dev`
# For SDL3
`sudo apt-get install libsdl3-dev`

#### For OpenGL Backend:
# Ubuntu/Debian
sudo apt-get install libgl1-mesa-dev libglu1-mesa-dev

#### For Gio (Pure Go - no external dependencies):
# No additional system dependencies required!

## 🚀 Quick Start

1. **Clone the repository:**
2. **Choose and build your preferred backend:**

# Pure Gio (Recommended for beginners)
`go build -tags puregio -o puregio`
# Nucular + Gio (Best performance)
`go build -tags giocam -o nucular_gio`
# SDL3 Backend
`go build -tags cam -o camera_sdl`
# Dear ImGui + Ebiten
`go build -tags imgui -o imgui_ebiten`
# Dear ImGui + OpenGL (Advanced)
`go build -tags opengl -o imgui_opengl`
# Dear ImGui + SDL2
`go build -tags sdl -o imgui_sdl`


## 🎮 Usage

### Controls (All Backends)
- **Camera Toggle**: Turn camera display on/off
- **Camera Selection**: Switch between detected cameras
- **Statistics**: View real-time FPS and frame drop information
- **Counter**: Test UI responsiveness with increment button

### Camera Detection
The application automatically detects all V4L2 cameras at `/dev/video*` and allows switching between them during runtime.

## 🏗️ Architecture

### Camera Pipeline (Common to All)
1. **V4L2 Device Detection**: Scans `/dev/video*` devices
2. **Camera Initialization**: Opens device with MJPEG format
3. **Frame Capture**: Continuous frame capture in background goroutine
4. **Image Processing**: MJPEG → RGBA conversion
5. **Texture Upload**: Backend-specific texture creation and updates
6. **Rendering**: Backend-specific display rendering

### Backend-Specific Optimizations
- **Gio**: Native Go rendering with GPU acceleration
- **SDL**: Hardware-accelerated texture streaming
- **ImGui**: Immediate mode GUI with efficient texture binding
- **OpenGL**: Direct GPU memory management

## 🔧 Advanced Configuration

### Resolution Settings
Modify the camera resolution in any implementation:
`go device.WithPixFormat(v4l2.PixFormat{ Width: 1280, // Change from default 640 Height: 720, // Change from default 480 PixelFormat: v4l2.PixelFmtMJPEG, Field: v4l2.FieldNone, })
`


### Performance Tuning
- **Frame Buffer Size**: Adjust channel buffer sizes for latency vs. smoothness
- **Render Rate**: Modify ticker intervals for different FPS targets
- **Memory Management**: Configure GC settings for consistent performance

## 🐛 Troubleshooting

### Common Issues

1. **No cameras detected:**
   ```bash
   # Check camera permissions
   ls -la /dev/video*
   sudo chmod 666 /dev/video0
   ```

2. **Poor performance:**
    - Try different backends (Nucular+Gio recommended for best camera performance)
    - Reduce resolution
    - Check system resources

3. **Build errors:**
    - Ensure all system dependencies are installed
    - Check Go version compatibility
    - Verify build tags are correct

## 🎯 Which Backend Should I Choose?

- **Beginners**: Start with **Pure Gio** (no external dependencies) or 
- **Best Performance**: Use **GLFW/OpenGL**  Also the most difficult to implement 
- **Game Development**: Try **ImGui + Ebiten**
- **Cross-Platform**: Consider **SDL3** backends
- **Production**: **SDL3** or **Pure Gio** for stability

## 📚 Dependencies

### Core Libraries (All Backends)
- `github.com/vladimirvivien/go4vl` - V4L2 camera access
- Standard Go libraries for image processing

### Backend-Specific Dependencies
- **Gio**: `gioui.org` - Pure Go UI framework
- **Nucular**: `github.com/aarzilli/nucular` - Immediate mode GUI
- **SDL3**: `github.com/Zyko0/go-sdl3` or SDL2 bindings
- **ImGui**: `github.com/amken3d/cimgui-go` - Dear ImGui Go bindings
- **Ebiten**: `github.com/hajimehoshi/ebiten/v2` - 2D game engine

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Add new backend implementations or improve existing ones
4. Test with real cameras
5. Submit a pull request

## 📄 License

This project is licensed under the MIT License - see the LICENSE file for details.
Thirdparty packages and libraries have their own licenses.

## 🙏 Acknowledgments

- V4L2 Linux kernel subsystem
- Go community for excellent multimedia libraries
- GUI framework maintainers for their outstanding work
- Allen Dang and Dear ImGui for the immediate mode GUI paradigm
- GIO for an amazing go based UI system

---

**Note**: This project serves as both a practical camera application and a comprehensive comparison of Go GUI frameworks for multimedia applications. Each backend implementation demonstrates different approaches to real-time video rendering in Go.