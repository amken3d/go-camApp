package main

import (
	"context"
	"fmt"
	"image"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/f32"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/vladimirvivien/go4vl/device"
	"github.com/vladimirvivien/go4vl/v4l2"
)

// Camera structures
type CameraInfo struct {
	Path  string
	Name  string
	Index int
}

type CameraInstance struct {
	Info               CameraInfo
	Device             *device.Device
	Active             bool
	Width              int
	Height             int
	FrameChan          chan []byte
	FrameMutex         sync.RWMutex // Use RWMutex for better performance
	DroppedFrames      uint64
	CurrentFrame       *image.RGBA
	ProcessedFrameChan chan *image.RGBA
	TextureOp          paint.ImageOp
	TextureUpdated     int32 // Use atomic for thread-safe flag
	LastFrameTime      time.Time
	// FPS tracking
	FPS           int32
	FrameCount    uint64
	LastFPSUpdate time.Time
	FPSMutex      sync.Mutex
}

type CameraApp struct {
	Cameras     []CameraInstance
	StatusText  string
	SelectedCam int
	ShowCamera  bool
	Theme       *material.Theme

	// UI widgets
	IncrementBtn    widget.Clickable
	ToggleCameraBtn widget.Clickable
	CameraButtons   []widget.Clickable
	Count           int

	// Performance optimization
	LastRenderTime time.Time
	FrameCounter   uint64
	Window         *app.Window

	// App rendering FPS
	AppFPS           int32
	AppFrameCount    uint64
	AppLastFPSUpdate time.Time
	AppFPSMutex      sync.Mutex
}

var cameraApp CameraApp

func main() {
	log.Println("Starting optimized pure Gio camera app...")

	// Initialize cameras
	initAllCameras()

	log.Printf("Camera initialization complete. Found %d cameras", len(cameraApp.Cameras))
	// Fix mutex copy issue
	for i := 0; i < len(cameraApp.Cameras); i++ {
		log.Printf("Camera %d: %s (Active: %v)", i, cameraApp.Cameras[i].Info.Name, cameraApp.Cameras[i].Active)
	}

	// Start Gio window
	runGioWindow()
}

func runGioWindow() {
	gioWindow := new(app.Window)
	cameraApp.Window = gioWindow
	cameraApp.Theme = material.NewTheme()
	cameraApp.CameraButtons = make([]widget.Clickable, len(cameraApp.Cameras))

	var ops op.Ops

	// Start a goroutine to trigger periodic redraws for smooth camera updates
	go func() {
		ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
		defer ticker.Stop()

		for range ticker.C {
			updateCameraFramesFromProcessed()

			if cameraApp.ShowCamera && cameraApp.SelectedCam < len(cameraApp.Cameras) {
				camera := &cameraApp.Cameras[cameraApp.SelectedCam]
				if atomic.LoadInt32(&camera.TextureUpdated) == 1 {
					gioWindow.Invalidate()
				}
			}
		}
	}()

	for {
		switch e := gioWindow.Event().(type) {
		case app.DestroyEvent:
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			// Track app rendering FPS
			atomic.AddUint64(&cameraApp.AppFrameCount, 1)
			updateAppFPS()

			// Handle UI interactions
			handleUIEvents(gtx)

			// Render the main layout
			renderMainLayout(gtx)

			e.Frame(gtx.Ops)

			// Performance tracking
			atomic.AddUint64(&cameraApp.FrameCounter, 1)
			if atomic.LoadUint64(&cameraApp.FrameCounter)%60 == 0 {
				now := time.Now()
				if !cameraApp.LastRenderTime.IsZero() {
					//fps := 60.0 / now.Sub(cameraApp.LastRenderTime).Seconds()

				}
				cameraApp.LastRenderTime = now
			}
		}
	}
}

func handleUIEvents(gtx layout.Context) {

	// Handle camera display toggle
	if cameraApp.ToggleCameraBtn.Clicked(gtx) {
		cameraApp.ShowCamera = !cameraApp.ShowCamera
		log.Printf("Camera display toggled: %v", cameraApp.ShowCamera)
	}

	// Handle camera selection buttons
	for i := range cameraApp.CameraButtons {
		if cameraApp.CameraButtons[i].Clicked(gtx) {
			if i != cameraApp.SelectedCam {
				cameraApp.SelectedCam = i
				log.Printf("Selected camera: %d", i)
			}
		}
	}
}

func renderMainLayout(gtx layout.Context) layout.Dimensions {
	return layout.Flex{
		Axis: layout.Horizontal,
	}.Layout(gtx,
		// Left panel for controls (smaller)
		layout.Flexed(0.25, func(gtx layout.Context) layout.Dimensions {
			return renderControlPanel(gtx)
		}),
		// Right panel for camera feed (larger)
		layout.Flexed(0.75, func(gtx layout.Context) layout.Dimensions {
			return renderCameraPanel(gtx)
		}),
	)
}

func renderControlPanel(gtx layout.Context) layout.Dimensions {
	return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{
			Axis: layout.Vertical,
		}.Layout(gtx,
			// Status
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Body2(cameraApp.Theme, cameraApp.StatusText).Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return renderAppInfo(gtx)
			}),

			layout.Rigid(layout.Spacer{Height: unit.Dp(15)}.Layout),

			// Camera controls header
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.H6(cameraApp.Theme, "Camera Controls").Layout(gtx)
			}),

			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),

			// Camera display toggle
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				text := "Camera: OFF"
				if cameraApp.ShowCamera {
					text = "Camera: ON"
				}
				return material.Button(cameraApp.Theme, &cameraApp.ToggleCameraBtn, text).Layout(gtx)
			}),

			layout.Rigid(layout.Spacer{Height: unit.Dp(15)}.Layout),

			// Camera selection
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if len(cameraApp.Cameras) == 0 {
					return material.Body2(cameraApp.Theme, "No cameras found").Layout(gtx)
				}

				return layout.Flex{
					Axis: layout.Vertical,
				}.Layout(gtx,
					// Camera selection header
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return material.Body2(cameraApp.Theme, "Select Camera:").Layout(gtx)
					}),

					layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),

					// Camera buttons
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return renderCameraButtons(gtx)
					}),

					layout.Rigid(layout.Spacer{Height: unit.Dp(15)}.Layout),

					// Selected camera info
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return renderCameraInfo(gtx, &cameraApp.Cameras[cameraApp.SelectedCam])
					}),
				)
			}),
		)
	})
}

func renderCameraButtons(gtx layout.Context) layout.Dimensions {
	children := make([]layout.FlexChild, 0)

	for i := range cameraApp.Cameras {
		i := i // capture loop variable
		child := layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if i >= len(cameraApp.CameraButtons) {
				return layout.Dimensions{}
			}

			text := fmt.Sprintf("Cam %d", i)
			if i == cameraApp.SelectedCam {
				text = fmt.Sprintf("✓ Cam %d", i)
			}

			return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				btn := material.Button(cameraApp.Theme, &cameraApp.CameraButtons[i], text)
				if i == cameraApp.SelectedCam {
					btn.Background = cameraApp.Theme.Palette.ContrastBg
				}
				return btn.Layout(gtx)
			})
		})
		children = append(children, child)
	}

	return layout.Flex{
		Axis: layout.Vertical,
	}.Layout(gtx, children...)
}

func renderCameraInfo(gtx layout.Context, camera *CameraInstance) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Caption(cameraApp.Theme, fmt.Sprintf("Camera: %s", camera.Info.Name)).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			fps := atomic.LoadInt32(&camera.FPS)
			return material.Caption(cameraApp.Theme, fmt.Sprintf("FPS: %d", fps)).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			droppedFrames := atomic.LoadUint64(&camera.DroppedFrames)
			return material.Caption(cameraApp.Theme, fmt.Sprintf("Dropped: %d", droppedFrames)).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !camera.LastFrameTime.IsZero() {
				timeSince := time.Since(camera.LastFrameTime)
				return material.Caption(cameraApp.Theme, fmt.Sprintf("Last frame: %v ago", timeSince.Truncate(time.Millisecond))).Layout(gtx)
			}
			return material.Caption(cameraApp.Theme, "No frames yet").Layout(gtx)
		}),
		// layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		// 	return material.Caption(cameraApp.Theme, fmt.Sprintf("Dropped: %d", droppedFrames)).Layout(gtx)
		// }),
	)
}

func renderCameraPanel(gtx layout.Context) layout.Dimensions {
	return layout.UniformInset(unit.Dp(5)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if !cameraApp.ShowCamera {
			return renderPlaceholder(gtx, "Camera Display is OFF")
		}

		if cameraApp.SelectedCam >= len(cameraApp.Cameras) {
			return renderPlaceholder(gtx, "Invalid Camera Selection")
		}

		return renderCameraWithGio(gtx)
	})
}

func renderCameraWithGio(gtx layout.Context) layout.Dimensions {
	if cameraApp.SelectedCam >= len(cameraApp.Cameras) {
		return renderPlaceholder(gtx, "Camera Index Out of Range")
	}

	camera := &cameraApp.Cameras[cameraApp.SelectedCam]

	if !camera.Active {
		return renderPlaceholder(gtx, "Camera Not Active")
	}

	// Use read lock for better performance
	camera.FrameMutex.RLock()
	currentFrame := camera.CurrentFrame
	camera.FrameMutex.RUnlock()

	if currentFrame == nil {
		return renderPlaceholder(gtx, "No Frame Data")
	}

	// Check if we need to update texture (atomic operation)
	if atomic.LoadInt32(&camera.TextureUpdated) == 1 {
		camera.FrameMutex.Lock()
		if atomic.CompareAndSwapInt32(&camera.TextureUpdated, 1, 0) {
			camera.TextureOp = paint.NewImageOp(camera.CurrentFrame)
		}
		camera.FrameMutex.Unlock()
	}

	if camera.TextureOp.Size().X == 0 {
		return renderPlaceholder(gtx, "Invalid Texture")
	}

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Get image and available space dimensions
		imgSize := camera.TextureOp.Size()
		availableSize := gtx.Constraints.Max

		// Calculate scaling to fit available space while maintaining aspect ratio
		scaleX := float32(availableSize.X) / float32(imgSize.X)
		scaleY := float32(availableSize.Y) / float32(imgSize.Y)
		scale := scaleX
		if scaleY < scaleX {
			scale = scaleY
		}

		// Limit scale to prevent oversizing
		if scale > 1.0 {
			scale = 1.0
		}

		scaledWidth := int(float32(imgSize.X) * scale)
		scaledHeight := int(float32(imgSize.Y) * scale)

		// Apply scaling transformation
		defer op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scale, scale))).Push(gtx.Ops).Pop()

		// Render the image
		camera.TextureOp.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)

		return layout.Dimensions{
			Size: image.Pt(scaledWidth, scaledHeight),
		}
	})
}

func renderPlaceholder(gtx layout.Context, message string) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return material.H4(cameraApp.Theme, message).Layout(gtx)
	})
}

// Enhanced findCameraDevices function with Raspberry Pi support
func findCameraDevices() ([]CameraInfo, error) {
	log.Println("Searching for camera devices...")
	var cameras []CameraInfo

	// First, look for standard V4L2 video devices
	matches, err := filepath.Glob("/dev/video*")
	if err != nil {
		return nil, fmt.Errorf("failed to find video devices: %w", err)
	}

	log.Printf("Found %d video device files", len(matches))

	re := regexp.MustCompile(`/dev/video(\d+)`)

	for _, devicePath := range matches {
		dev, err := device.Open(devicePath)
		if err != nil {
			continue
		}

		match := re.FindStringSubmatch(devicePath)
		index := 0
		if len(match) == 2 {
			fmt.Sscanf(match[1], "%d", &index)
		}

		caps := dev.Capability()
		name := caps.Card[:]
		name = strings.TrimRight(name, "\x00")
		if name == "" {
			name = fmt.Sprintf("Camera %d", index)
		}

		cameras = append(cameras, CameraInfo{
			Path:  devicePath,
			Name:  name,
			Index: index,
		})

		dev.Close()
	}

	// Check for Raspberry Pi cameras using rpicam-vid
	rpiCameras, err := findRaspberryPiCameras()
	if err != nil {
		log.Printf("Warning: Failed to detect Raspberry Pi cameras: %v", err)
	} else {
		// Add Raspberry Pi cameras to the list with higher indices
		nextIndex := len(cameras)
		for i, rpiCamera := range rpiCameras {
			cameras = append(cameras, CameraInfo{
				Path:  fmt.Sprintf("rpicam:%d", i), // Special path format for RPi cameras
				Name:  rpiCamera,
				Index: nextIndex + i,
			})
		}
	}

	sort.Slice(cameras, func(i, j int) bool {
		return cameras[i].Index < cameras[j].Index
	})

	log.Printf("Camera discovery complete. Found %d cameras", len(cameras))
	return cameras, nil
}

// findRaspberryPiCameras detects available Raspberry Pi cameras using rpicam-vid
func findRaspberryPiCameras() ([]string, error) {
	var cameras []string

	// Try to run rpicam-vid with --list-cameras to detect available cameras
	cmd := exec.Command("rpicam-vid", "--list-cameras")
	output, err := cmd.Output()
	if err != nil {
		// rpicam-vid not available or no cameras found
		return cameras, err
	}

	// Parse the output to find camera information
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for lines that contain camera information
		if strings.Contains(line, ":") && (strings.Contains(strings.ToLower(line), "imx") ||
			strings.Contains(strings.ToLower(line), "ov") ||
			strings.Contains(strings.ToLower(line), "camera")) {
			// Extract camera name from the line
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				cameraName := strings.TrimSpace(parts[1])
				if cameraName != "" {
					cameras = append(cameras, fmt.Sprintf("RPi Camera: %s", cameraName))
				}
			}
		}
	}

	// If no cameras found through listing, try a simple test
	if len(cameras) == 0 {
		// Try to run rpicam-vid briefly to see if any camera is available
		testCmd := exec.Command("rpicam-vid", "-t", "1", "--nopreview", "-o", "/dev/null")
		err := testCmd.Run()
		if err == nil {
			// Camera available but couldn't get detailed info
			cameras = append(cameras, "Raspberry Pi Camera")
		}
	}

	return cameras, nil
}

func initAllCameras() {
	devices, err := findCameraDevices()
	if err != nil {
		cameraApp.StatusText = "Error listing devices: " + err.Error()
		log.Printf("Error finding cameras: %v", err)
		return
	}

	if len(devices) == 0 {
		cameraApp.StatusText = "No camera devices found"
		log.Println("No camera devices found")
		return
	}

	cameraApp.StatusText = fmt.Sprintf("Found %d camera devices", len(devices))
	cameraApp.Cameras = make([]CameraInstance, len(devices))

	activeCameras := 0
	for i, deviceInfo := range devices {
		camera := &cameraApp.Cameras[i]
		camera.Info = deviceInfo

		log.Printf("Initializing camera %d: %s", i, deviceInfo.Name)
		err = initSingleCamera(camera)
		if err != nil {
			log.Printf("Failed to initialize camera %s: %v", deviceInfo.Name, err)
			camera.Active = false
		} else {
			activeCameras++
			go captureFramesForCamera(camera)
			log.Printf("Successfully initialized camera %d", i)
		}
	}

	cameraApp.StatusText = fmt.Sprintf("Initialized %d/%d cameras", activeCameras, len(devices))
	cameraApp.ShowCamera = activeCameras > 0

	// Initialize camera buttons after we know how many cameras we have
	cameraApp.CameraButtons = make([]widget.Clickable, len(cameraApp.Cameras))

	log.Printf("Camera initialization complete: %d active cameras", activeCameras)
}

// Enhanced initSingleCamera function with Raspberry Pi support
func initSingleCamera(camera *CameraInstance) error {
	// Check if this is a Raspberry Pi camera
	if strings.HasPrefix(camera.Info.Path, "rpicam:") {
		return initRaspberryPiCamera(camera)
	}

	// Handle regular V4L2 cameras (existing code)
	dev, err := device.Open(
		camera.Info.Path,
		device.WithIOType(v4l2.IOTypeMMAP),
		device.WithPixFormat(v4l2.PixFormat{
			Width:       640,
			Height:      480,
			PixelFormat: v4l2.PixelFmtMJPEG,
			Field:       v4l2.FieldNone,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to open camera: %w", err)
	}

	camera.Device = dev

	format, err := dev.GetPixFormat()
	if err != nil {
		dev.Close()
		return fmt.Errorf("failed to get pixel format: %w", err)
	}

	camera.Width = int(format.Width)
	camera.Height = int(format.Height)

	if err = dev.Start(context.Background()); err != nil {
		dev.Close()
		return fmt.Errorf("failed to start camera: %w", err)
	}

	camera.Active = true
	camera.FrameChan = make(chan []byte, 5) // Smaller buffer to reduce latency
	camera.ProcessedFrameChan = make(chan *image.RGBA, 2)

	// Start frame processing goroutine
	go processFramesForCamera(camera)

	return nil
}

// initRaspberryPiCamera initializes a Raspberry Pi camera using rpicam-vid
func initRaspberryPiCamera(camera *CameraInstance) error {
	// Set default dimensions for RPi camera
	camera.Width = 640
	camera.Height = 480

	camera.Active = true
	camera.FrameChan = make(chan []byte, 5)
	camera.ProcessedFrameChan = make(chan *image.RGBA, 2)

	// Start frame processing goroutine
	go processFramesForCamera(camera)

	log.Printf("Initialized Raspberry Pi camera: %s (%dx%d)", camera.Info.Name, camera.Width, camera.Height)

	return nil
}

// Enhanced captureFramesForCamera function (for V4L2 cameras only)
func captureFramesForCamera(camera *CameraInstance) {
	defer close(camera.FrameChan)

	// Skip if this is a Raspberry Pi camera (handled in processFramesForCamera)
	if strings.HasPrefix(camera.Info.Path, "rpicam:") {
		return
	}

	// Handle regular V4L2 cameras
	for camera.Active {
		// Read the next frame from the device
		frame := <-camera.Device.GetOutput()
		if frame == nil {
			atomic.AddUint64(&camera.DroppedFrames, 1)
			time.Sleep(16 * time.Millisecond)
			continue
		}

		// Send the frame to our channel
		select {
		case camera.FrameChan <- frame:
		default:
			// Channel buffer full, drop the frame
			atomic.AddUint64(&camera.DroppedFrames, 1)
		}
	}
}

func cleanupCameras() {
	log.Println("Cleaning up cameras...")
	// Fix mutex copy issue
	for i := 0; i < len(cameraApp.Cameras); i++ {
		camera := &cameraApp.Cameras[i]
		camera.Active = false
		time.Sleep(50 * time.Millisecond) // Reduced cleanup time

		if camera.Device != nil {
			camera.Device.Close()
			log.Printf("Closed camera %d: %s", i, camera.Info.Name)
		}

		camera.FrameMutex.Lock()
		camera.CurrentFrame = nil
		camera.FrameMutex.Unlock()
	}
	log.Println("Camera cleanup complete")
}

// Add this function to calculate and update FPS
func updateCameraFPS(camera *CameraInstance) {
	camera.FPSMutex.Lock()
	defer camera.FPSMutex.Unlock()

	now := time.Now()
	if camera.LastFPSUpdate.IsZero() {
		camera.LastFPSUpdate = now
		return
	}

	duration := now.Sub(camera.LastFPSUpdate)
	if duration >= time.Second {
		fps := float64(atomic.LoadUint64(&camera.FrameCount)) / duration.Seconds()
		atomic.StoreInt32(&camera.FPS, int32(fps))
		atomic.StoreUint64(&camera.FrameCount, 0)
		camera.LastFPSUpdate = now
	}
}

// Add this function to calculate app rendering FPS
func updateAppFPS() {
	cameraApp.AppFPSMutex.Lock()
	defer cameraApp.AppFPSMutex.Unlock()

	now := time.Now()
	if cameraApp.AppLastFPSUpdate.IsZero() {
		cameraApp.AppLastFPSUpdate = now
		return
	}

	duration := now.Sub(cameraApp.AppLastFPSUpdate)
	if duration >= time.Second {
		fps := float64(atomic.LoadUint64(&cameraApp.AppFrameCount)) / duration.Seconds()
		atomic.StoreInt32(&cameraApp.AppFPS, int32(fps))
		atomic.StoreUint64(&cameraApp.AppFrameCount, 0)
		cameraApp.AppLastFPSUpdate = now
	}
}

// Add this to your status bar or info panel
func renderAppInfo(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			appFPS := atomic.LoadInt32(&cameraApp.AppFPS)
			return material.Caption(cameraApp.Theme, fmt.Sprintf("App FPS: %d", appFPS)).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Spacer{Width: unit.Dp(20)}.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Caption(cameraApp.Theme, cameraApp.StatusText).Layout(gtx)
		}),
	)
}
