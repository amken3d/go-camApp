# # V4L2 Camera Streaming with Dear ImGui and Ebiten


This example demonstrates how to integrate a V4L2 (Video4Linux2) camera feed within a Dear ImGui user interface using the cimgui-go library with the Ebiten game engine backend. The application creates an interactive window that displays a live video stream from a connected camera device while also providing ImGui interface widgets


## Key Features


1. **Live Camera Feed Display**: Captures and displays a real-time video stream from a connected camera using the go4vl library
2. **ImGui Integration**: Provides a graphical user interface built with Dear ImGui, showing camera feed statistics
3. **Efficient Frame Processing**: Processes MJPEG frames from the camera and converts them to RGBA textures
4. **Proper Resource Management**: Includes robust handling of resources with appropriate cleanup mechanisms

## Technical Implementation
The application follows these main steps:
1. **Initialization**: Sets up the Ebiten backend for ImGui and initializes the camera device
2. **Texture Management**: Creates an empty texture initially, which is updated with camera frames
3. **Frame Capture**: Continuously captures frames from the camera in MJPEG format
4. **Image Processing**: Converts captured frames to RGBA images suitable for display
5. **UI Rendering**: Renders the video feed in an ImGui window along with frame statistics

The implementation uses several Go packages:
- : Go bindings for Dear ImGui `github.com/amken3d/cimgui-go`
- : 2D game engine for rendering `github.com/hajimehoshi/ebiten/v2`
- : Library for accessing V4L2 devices `github.com/vladimirvivien/go4vl`

## Error Prevention
The code includes several mechanisms to prevent crashes and memory leaks:
- Mutex locking for thread-safe camera operations
- Proper cleanup of resources during shutdown
- Timeout handling for frame capture
- Null checks before accessing resources
- Forced garbage collection to prevent memory buildup

## Usage
To run this example, ensure you have:
1. A compatible webcam connected to your system
2. The correct device path set in the code (default is ) `/dev/video0`
3. Required Go dependencies installed

Build and run the example using the included Makefile:
``` 
make run
```
This example serves as a practical demonstration of how to integrate hardware video feeds with immediate mode GUI frameworks in Go, providing a foundation for building more complex video processing applications.
