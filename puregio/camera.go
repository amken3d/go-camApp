package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"log"
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
					fps := 60.0 / now.Sub(cameraApp.LastRenderTime).Seconds()
					log.Printf("Render FPS: %.1f", fps)
				}
				cameraApp.LastRenderTime = now
			}
		}
	}
}

func handleUIEvents(gtx layout.Context) {
	// Handle increment button
	if cameraApp.IncrementBtn.Clicked(gtx) {
		cameraApp.Count++
	}

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

			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),

			// Counter button
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Button(cameraApp.Theme, &cameraApp.IncrementBtn,
					fmt.Sprintf("Count: %d", cameraApp.Count)).Layout(gtx)
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
						return renderCameraInfo(gtx)
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

func renderCameraInfo(gtx layout.Context) layout.Dimensions {
	if cameraApp.SelectedCam >= len(cameraApp.Cameras) {
		return layout.Dimensions{}
	}

	camera := &cameraApp.Cameras[cameraApp.SelectedCam]

	return layout.Flex{
		Axis: layout.Vertical,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Caption(cameraApp.Theme, fmt.Sprintf("Current: %s", camera.Info.Name)).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Caption(cameraApp.Theme, fmt.Sprintf("Resolution: %dx%d", camera.Width, camera.Height)).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			status := "Inactive"
			if camera.Active {
				status = "Active"
			}
			return material.Caption(cameraApp.Theme, fmt.Sprintf("Status: %s", status)).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Caption(cameraApp.Theme, fmt.Sprintf("Dropped: %d", atomic.LoadUint64(&camera.DroppedFrames))).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			camera.FrameMutex.RLock()
			hasFrame := camera.CurrentFrame != nil
			camera.FrameMutex.RUnlock()
			return material.Caption(cameraApp.Theme, fmt.Sprintf("Frame: %v", hasFrame)).Layout(gtx)
		}),
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

func findCameraDevices() ([]CameraInfo, error) {
	log.Println("Searching for camera devices...")
	var cameras []CameraInfo

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

	sort.Slice(cameras, func(i, j int) bool {
		return cameras[i].Index < cameras[j].Index
	})

	log.Printf("Camera discovery complete. Found %d cameras", len(cameras))
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

func initSingleCamera(camera *CameraInstance) error {
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

func processFramesForCamera(camera *CameraInstance) {
	defer close(camera.ProcessedFrameChan)
	log.Printf("Starting frame processing for camera: %s", camera.Info.Name)

	frameCount := 0
	lastLogTime := time.Now()

	for camera.Active {
		select {
		case frameData, ok := <-camera.FrameChan:
			if !ok {
				log.Printf("Frame channel closed for camera: %s", camera.Info.Name)
				return
			}

			frameCount++
			now := time.Now()

			// Log every 5 seconds instead of every 60 frames
			if now.Sub(lastLogTime) >= 5*time.Second {
				fps := float64(frameCount) / now.Sub(lastLogTime).Seconds()
				log.Printf("Camera %s: %d frames, %.1f FPS", camera.Info.Name, frameCount, fps)
				frameCount = 0
				lastLogTime = now
			}

			// Decode JPEG frame
			img, err := jpeg.Decode(bytes.NewReader(frameData))
			if err != nil {
				log.Printf("Failed to decode frame for camera %s: %v", camera.Info.Name, err)
				continue
			}

			bounds := img.Bounds()
			rgbaImg := image.NewRGBA(bounds)
			draw.Draw(rgbaImg, bounds, img, bounds.Min, draw.Src)

			// Update the current frame for display (use write lock briefly)
			camera.FrameMutex.Lock()
			camera.CurrentFrame = rgbaImg
			camera.LastFrameTime = now
			camera.FrameMutex.Unlock()

			// Set texture updated flag atomically
			atomic.StoreInt32(&camera.TextureUpdated, 1)

			select {
			case camera.ProcessedFrameChan <- rgbaImg:
			default:
				// Drop if processing channel is full
			}
		case <-time.After(100 * time.Millisecond):
			// Timeout to prevent blocking if no frames
			if !camera.Active {
				return
			}
		}
	}

	log.Printf("Frame processing stopped for camera: %s", camera.Info.Name)
}

func captureFramesForCamera(camera *CameraInstance) {
	defer close(camera.FrameChan)
	log.Printf("Starting frame capture for camera: %s", camera.Info.Name)

	for camera.Active {
		frame := <-camera.Device.GetOutput()
		if frame == nil {
			atomic.AddUint64(&camera.DroppedFrames, 1)
			time.Sleep(16 * time.Millisecond)
			continue
		}

		select {
		case camera.FrameChan <- frame:
		default:
			atomic.AddUint64(&camera.DroppedFrames, 1)
		}
	}

	log.Printf("Frame capture stopped for camera: %s", camera.Info.Name)
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
