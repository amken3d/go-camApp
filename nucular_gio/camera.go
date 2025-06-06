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
	"gioui.org/widget/material"
	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/style"
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
	FrameMutex         sync.Mutex
	DroppedFrames      uint64
	CurrentFrame       *image.RGBA
	ProcessedFrameChan chan *image.RGBA
	TextureOp          paint.ImageOp
	TextureUpdated     bool
}

type CameraApp struct {
	Cameras     []CameraInstance
	StatusText  string
	SelectedCam int
	ShowCamera  bool
	GioWindow   *app.Window
	Theme       *material.Theme
}

var cameraApp CameraApp

func main() {
	// Initialize cameras
	initAllCameras()

	// Start both Gio window for smooth camera rendering and Nucular for controls
	go runGioWindow()

	// Start nucular control window
	wnd := nucular.NewMasterWindow(nucular.WindowClosable, "Camera Controls", updatefn)
	wnd.SetStyle(style.FromTheme(style.RedTheme, 2.0))
	wnd.Main()

	// Cleanup when exiting
	cleanupCameras()
}

func runGioWindow() {
	gioWindow := new(app.Window)
	cameraApp.GioWindow = gioWindow
	cameraApp.Theme = material.NewTheme()

	var ops op.Ops

	for {

		switch e := gioWindow.Event().(type) {
		case app.DestroyEvent:
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)

			if cameraApp.ShowCamera && cameraApp.SelectedCam < len(cameraApp.Cameras) {
				renderCameraWithGio(gtx)
			} else {
				renderPlaceholder(gtx)
			}

			e.Frame(gtx.Ops)
		}

	}
}

func renderCameraWithGio(gtx layout.Context) layout.Dimensions {
	if cameraApp.SelectedCam >= len(cameraApp.Cameras) {
		return renderPlaceholder(gtx)
	}

	camera := &cameraApp.Cameras[cameraApp.SelectedCam]

	camera.FrameMutex.Lock()
	currentFrame := camera.CurrentFrame
	textureUpdated := camera.TextureUpdated
	camera.FrameMutex.Unlock()

	if currentFrame == nil || !camera.Active {
		return renderPlaceholder(gtx)
	}

	if textureUpdated {
		camera.FrameMutex.Lock()
		camera.TextureOp = paint.NewImageOp(currentFrame)
		camera.TextureUpdated = false
		camera.FrameMutex.Unlock()
		log.Printf("Updated texture: %dx%d", currentFrame.Bounds().Dx(), currentFrame.Bounds().Dy())
	}

	if camera.TextureOp.Size().X == 0 {
		return renderPlaceholder(gtx)
	}

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Get image and window dimensions
		imgSize := camera.TextureOp.Size()
		windowSize := gtx.Constraints.Max

		log.Printf("Image size: %dx%d, Window size: %dx%d", imgSize.X, imgSize.Y, windowSize.X, windowSize.Y)

		// Calculate scaling to fit window while maintaining aspect ratio
		scaleX := float32(windowSize.X) / float32(imgSize.X)
		scaleY := float32(windowSize.Y) / float32(imgSize.Y)
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

		log.Printf("Scale: %f, Scaled size: %dx%d", scale, scaledWidth, scaledHeight)

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

func renderPlaceholder(gtx layout.Context) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return material.H3(cameraApp.Theme, "No Camera Feed").Layout(gtx)
	})
}

var count int

func updatefn(w *nucular.Window) {
	// Status row
	w.Row(30).Dynamic(1)
	w.Label(cameraApp.StatusText, "LC")

	// Counter (original functionality)
	w.Row(50).Dynamic(1)
	if w.ButtonText(fmt.Sprintf("increment: %d", count)) {
		count++
	}

	// Camera control section
	w.Row(30).Dynamic(1)
	w.Label("Camera Controls", "LC")

	// Show/Hide camera toggle
	w.Row(30).Dynamic(1)
	if w.ButtonText(fmt.Sprintf("Camera Display: %s", map[bool]string{true: "ON", false: "OFF"}[cameraApp.ShowCamera])) {
		cameraApp.ShowCamera = !cameraApp.ShowCamera
		if cameraApp.GioWindow != nil {
			cameraApp.GioWindow.Invalidate()
		}
	}

	// Camera selection
	if len(cameraApp.Cameras) > 0 {
		w.Row(30).Dynamic(1)
		w.Label("Select Camera:", "LC")

		w.Row(30).Dynamic(1)
		for i := range cameraApp.Cameras {
			if w.ButtonText(fmt.Sprintf("Cam %d", i)) {
				cameraApp.SelectedCam = i
				if cameraApp.GioWindow != nil {
					cameraApp.GioWindow.Invalidate()
				}
			}
		}

		// Selected camera info
		if cameraApp.SelectedCam < len(cameraApp.Cameras) {
			camera := &cameraApp.Cameras[cameraApp.SelectedCam]

			w.Row(20).Dynamic(1)
			w.Label(fmt.Sprintf("Current: %s", camera.Info.Name), "LC")

			w.Row(20).Dynamic(1)
			w.Label(fmt.Sprintf("Resolution: %dx%d", camera.Width, camera.Height), "LC")

			w.Row(20).Dynamic(1)
			w.Label(fmt.Sprintf("Status: %s", map[bool]string{true: "Active", false: "Inactive"}[camera.Active]), "LC")

			w.Row(20).Dynamic(1)
			w.Label(fmt.Sprintf("Dropped frames: %d", atomic.LoadUint64(&camera.DroppedFrames)), "LC")

			// Debug info
			camera.FrameMutex.Lock()
			hasFrame := camera.CurrentFrame != nil
			textureSize := camera.TextureOp.Size()
			camera.FrameMutex.Unlock()

			w.Row(20).Dynamic(1)
			w.Label(fmt.Sprintf("Has Frame: %v", hasFrame), "LC")

			w.Row(20).Dynamic(1)
			w.Label(fmt.Sprintf("Texture Size: %dx%d", textureSize.X, textureSize.Y), "LC")
		}
	} else {
		w.Row(50).Dynamic(1)
		w.Label("No cameras found", "CC")
	}

	// Camera list
	w.Row(30).Dynamic(1)
	w.Label("Available Cameras:", "LC")

	for i, camera := range cameraApp.Cameras {
		w.Row(25).Dynamic(1)
		status := "Inactive"
		if camera.Active {
			status = "Active"
		}
		w.Label(fmt.Sprintf("%d: %s [%s]", i, camera.Info.Name, status), "LC")
	}
}

func findCameraDevices() ([]CameraInfo, error) {
	var cameras []CameraInfo

	matches, err := filepath.Glob("/dev/video*")
	if err != nil {
		return nil, fmt.Errorf("failed to find video devices: %w", err)
	}

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

	return cameras, nil
}

func initAllCameras() {
	devices, err := findCameraDevices()
	if err != nil {
		cameraApp.StatusText = "Error listing devices: " + err.Error()
		return
	}

	if len(devices) == 0 {
		cameraApp.StatusText = "No camera devices found"
		return
	}

	cameraApp.StatusText = fmt.Sprintf("Found %d camera devices", len(devices))
	cameraApp.Cameras = make([]CameraInstance, len(devices))

	activeCameras := 0
	for i, deviceInfo := range devices {
		camera := &cameraApp.Cameras[i]
		camera.Info = deviceInfo

		err = initSingleCamera(camera)
		if err != nil {
			log.Printf("Failed to initialize camera %s: %v", deviceInfo.Name, err)
			camera.Active = false
		} else {
			activeCameras++
			go captureFramesForCamera(camera)
		}
	}

	cameraApp.StatusText = fmt.Sprintf("Initialized %d/%d cameras", activeCameras, len(devices))
	cameraApp.ShowCamera = activeCameras > 0
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
	camera.FrameChan = make(chan []byte, 60)
	camera.ProcessedFrameChan = make(chan *image.RGBA, 10)

	// Start frame processing goroutine
	go processFramesForCamera(camera)

	return nil
}

func processFramesForCamera(camera *CameraInstance) {
	defer close(camera.ProcessedFrameChan)

	for camera.Active {
		select {
		case frameData, ok := <-camera.FrameChan:
			if !ok {
				return
			}

			// Decode JPEG frame
			img, err := jpeg.Decode(bytes.NewReader(frameData))
			if err != nil {
				log.Printf("Failed to decode frame: %v", err)
				continue
			}

			bounds := img.Bounds()
			rgbaImg := image.NewRGBA(bounds)
			draw.Draw(rgbaImg, bounds, img, bounds.Min, draw.Src)

			// Update the current frame for display
			camera.FrameMutex.Lock()
			camera.CurrentFrame = rgbaImg
			camera.TextureUpdated = true
			camera.FrameMutex.Unlock()

			// Trigger window redraw
			if cameraApp.GioWindow != nil {
				cameraApp.GioWindow.Invalidate()
			}

			select {
			case camera.ProcessedFrameChan <- rgbaImg:
			default:
				// Drop if processing channel is full
			}
		}
	}
}

func captureFramesForCamera(camera *CameraInstance) {
	defer close(camera.FrameChan)

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
}

func cleanupCameras() {
	for i := range cameraApp.Cameras {
		camera := &cameraApp.Cameras[i]
		camera.Active = false
		time.Sleep(100 * time.Millisecond)

		if camera.Device != nil {
			camera.Device.Close()
		}

		camera.FrameMutex.Lock()
		camera.CurrentFrame = nil
		camera.FrameMutex.Unlock()
	}
}
