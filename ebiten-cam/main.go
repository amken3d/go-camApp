package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/amken3d/cimgui-go/backend"
	ebitenbackend "github.com/amken3d/cimgui-go/backend/ebiten-backend"
	"github.com/amken3d/cimgui-go/examples/common"
	"github.com/amken3d/cimgui-go/imgui"

	"github.com/vladimirvivien/go4vl/device"
	"github.com/vladimirvivien/go4vl/v4l2"
)

const (
	screenWidth  = 1200
	screenHeight = 900
	frameWidth   = 640
	frameHeight  = 480
	devicePath   = "/dev/video0" // Update this to match your camera device
)

var (
	currentBackend *ebitenbackend.EbitenBackend
	texture        *backend.Texture
	camera         *device.Device
	frameCount     uint64
	droppedFrames  uint64
	lastFrame      *image.RGBA
	running        bool
	cameraMutex    sync.Mutex
)

// showVideoStream displays the camera video in an ImGui window
func showVideoStream() {
	// Position the window
	basePos := imgui.MainViewport().Pos()
	imgui.SetNextWindowPosV(imgui.NewVec2(basePos.X+60, basePos.Y+60), imgui.CondOnce, imgui.NewVec2(0, 0))
	imgui.SetNextWindowSizeV(imgui.NewVec2(float32(frameWidth)+30, float32(frameHeight)+100), imgui.CondOnce)

	// Create a window for the video
	imgui.Begin("V4L2 Camera Feed")

	// Display stats
	imgui.Text(fmt.Sprintf("Frames: %d (Dropped: %d)", frameCount, droppedFrames))

	// Display the video texture
	if texture != nil {
		imgui.ImageV(
			texture.ID,
			imgui.NewVec2(float32(frameWidth), float32(frameHeight)),
			imgui.NewVec2(0, 0),
			imgui.NewVec2(1, 1),
		)
	} else {
		imgui.Text("No video texture available")
	}

	imgui.End()
}

// updateCameraFrame attempts to get a new frame from the camera and update the texture
func updateCameraFrame() {
	cameraMutex.Lock()
	defer cameraMutex.Unlock()

	if camera == nil || !running {
		return
	}

	// Try to get a frame with a short timeout
	select {
	case frame := <-camera.GetOutput():
		if frame == nil {
			droppedFrames++
			return
		}

		// Convert frame bytes to image (depends on pixel format)
		var img image.Image
		var err error

		// Assuming MJPEG format
		img, err = jpeg.Decode(io.NewSectionReader(bytes.NewReader(frame), 0, int64(len(frame))))
		if err != nil {
			droppedFrames++
			return
		}

		// Convert to RGBA
		bounds := img.Bounds()
		rgba := image.NewRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}

		// Update the texture with the new frame
		lastFrame = rgba
		frameCount++
		if currentBackend != nil && texture != nil {
			currentBackend.UpdateTexture(texture.ID, rgba)

		}

	case <-time.After(16 * time.Millisecond): // ~60fps timeout
		// No frame available in time
		return
	}
}

// initCamera initializes the camera device
func initCamera() error {
	cameraMutex.Lock()
	defer cameraMutex.Unlock()

	// Close any existing camera first
	closeCamera()

	// Open camera device
	dev, err := device.Open(
		devicePath,
		device.WithIOType(v4l2.IOTypeMMAP),
		device.WithPixFormat(v4l2.PixFormat{
			Width:       frameWidth,
			Height:      frameHeight,
			PixelFormat: v4l2.PixelFmtMJPEG, // MJPEG for better performance
			Field:       v4l2.FieldNone,
		}),
	)

	if err != nil {
		return fmt.Errorf("failed to open device: %w", err)
	}

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Start streaming
	if err := dev.Start(ctx); err != nil {
		cancel()
		dev.Close()
		return fmt.Errorf("failed to start device: %w", err)
	}

	camera = dev
	running = true

	// Force GC to clean up any previous resources
	runtime.GC()

	return nil
}

// closeCamera safely closes the camera
func closeCamera() {
	if camera != nil {
		running = false
		camera.Close()
		camera = nil

		// Force GC to clean up resources
		runtime.GC()
	}
}

func afterCreateContext() {
	// Create an empty texture for the video
	texture = &backend.Texture{
		ID:     currentBackend.CreateEmptyTexture(frameWidth, frameHeight),
		Width:  frameWidth,
		Height: frameHeight,
	}

	// Initialize the camera
	if err := initCamera(); err != nil {
		log.Printf("Failed to initialize camera: %v", err)
	}
}

func beforeDestroyContext() {
	// Clean up resources
	cameraMutex.Lock()
	defer cameraMutex.Unlock()

	closeCamera()

	// Delete texture if it exists
	if currentBackend != nil && texture != nil {
		currentBackend.DeleteTexture(texture.ID)
		texture = nil
	}
}

func loop() {
	// Update the texture with new frame data
	updateCameraFrame()

	// Clear callback pool
	imgui.ClearSizeCallbackPool()

	// Show the video stream
	showVideoStream()

	// Show demo windows
	common.ShowWidgetsDemo()
}

func main() {
	common.Initialize()

	currentBackend = ebitenbackend.NewEbitenBackend()
	_, _ = backend.CreateBackend(currentBackend)

	// Setup hooks
	currentBackend.SetAfterCreateContextHook(afterCreateContext)
	currentBackend.SetBeforeDestroyContextHook(beforeDestroyContext)

	// Add hooks for before/after rendering to handle potential issues
	currentBackend.SetBeforeRenderHook(func() {
		// Nothing special needed here, but hook is set
	})

	currentBackend.SetAfterRenderHook(func() {
		// Nothing special needed here, but hook is set
	})

	// Set application background color
	currentBackend.SetBgColor(imgui.NewVec4(0.2, 0.2, 0.2, 1.0))

	// Create window
	currentBackend.CreateWindow("V4L2 Video in cimgui-go", screenWidth, screenHeight)

	// Set close callback
	currentBackend.SetCloseCallback(func() {
		fmt.Println("Window is closing, cleaning up resources")
		beforeDestroyContext()
	})

	// Run the application with our loop function
	currentBackend.Run(loop)
}
