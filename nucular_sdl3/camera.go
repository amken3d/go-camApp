package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/Zyko0/go-sdl3/bin/binsdl"

	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Zyko0/go-sdl3/sdl"
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
	Texture            *sdl.Texture
	ThumbnailTexture   *sdl.Texture
	ProcessedFrameChan chan *image.RGBA
}

type CameraApp struct {
	Cameras            []CameraInstance
	StatusText         string
	SelectedCam        int
	ShowCamera         bool
	Renderer           *sdl.Renderer
	Window             *sdl.Window
	PlaceholderTexture *sdl.Texture
}

var app CameraApp

func main() {
	defer binsdl.Load().Unload()

	// Initialize SDL for camera display
	if err := sdl.Init(sdl.INIT_VIDEO); err != nil {
		log.Fatal("Failed to initialize SDL:", err)
	}
	defer sdl.Quit()

	var (
		window   *sdl.Window
		renderer *sdl.Renderer
		err      error
	)

	window, renderer, err = sdl.CreateWindowAndRenderer("Multi-Camera App", 640, 480, sdl.WINDOW_RESIZABLE|sdl.WINDOW_HIGH_PIXEL_DENSITY)
	if err != nil {
		panic(err)
	}
	// Enable linear filtering for better rendering quality
	if err = renderer.SetDrawBlendMode(sdl.BLENDMODE_BLEND); err != nil {
		panic(err)
	}

	defer window.Destroy()
	app.Window = window
	app.Renderer = renderer

	// Initialize cameras
	initAllCameras()

	// Start nucular control window in separate goroutine
	go func() {
		wnd := nucular.NewMasterWindow(0, "Camera Controls", updatefn)
		wnd.SetStyle(style.FromTheme(style.RedTheme, 2.0))
		wnd.Main()
	}()

	// Main SDL loop for camera display
	_ = sdl.RunLoop(func() error {
		var event sdl.Event
		for sdl.PollEvent(&event) {
			switch event.Type {
			case sdl.EVENT_QUIT:
				return sdl.EndLoop
			case sdl.EVENT_KEY_DOWN:
				println("Key pressed:")
			}
		}

		// Update camera frames
		updateCameraFrames()

		// Render camera feed
		renderCamera()

		time.Sleep(16 * time.Millisecond) // ~60 FPS
		return nil
	})
}

// Cleanup
var count int

func updatefn(w *nucular.Window) {
	// Status row
	w.Row(30).Dynamic(1)
	w.Label(app.StatusText, "LC")

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
	if w.ButtonText(fmt.Sprintf("Camera Display: %s", map[bool]string{true: "ON", false: "OFF"}[app.ShowCamera])) {
		app.ShowCamera = !app.ShowCamera
		if app.ShowCamera {
			app.Window.Show()
		} else {
			app.Window.Hide()
		}
	}

	// Camera selection
	if len(app.Cameras) > 0 {
		w.Row(30).Dynamic(1)
		w.Label("Select Camera:", "LC")

		w.Row(30).Dynamic(1)
		for i, _ := range app.Cameras {
			if w.ButtonText(fmt.Sprintf("Cam %d", i)) {
				app.SelectedCam = i
			}
		}

		// Selected camera info
		if app.SelectedCam < len(app.Cameras) {
			camera := &app.Cameras[app.SelectedCam]

			w.Row(20).Dynamic(1)
			w.Label(fmt.Sprintf("Current: %s", camera.Info.Name), "LC")

			w.Row(20).Dynamic(1)
			w.Label(fmt.Sprintf("Resolution: %dx%d", camera.Width, camera.Height), "LC")

			w.Row(20).Dynamic(1)
			w.Label(fmt.Sprintf("Status: %s", map[bool]string{true: "Active", false: "Inactive"}[camera.Active]), "LC")

			w.Row(20).Dynamic(1)
			w.Label(fmt.Sprintf("Dropped frames: %d", atomic.LoadUint64(&camera.DroppedFrames)), "LC")
		}
	} else {
		w.Row(50).Dynamic(1)
		w.Label("No cameras found", "CC")
	}

	// Camera list
	w.Row(30).Dynamic(1)
	w.Label("Available Cameras:", "LC")

	for i, camera := range app.Cameras {
		w.Row(25).Dynamic(1)
		status := "Inactive"
		if camera.Active {
			status = "Active"
		}
		w.Label(fmt.Sprintf("%d: %s [%s]", i, camera.Info.Name, status), "LC")
	}
}

func renderCamera() {
	app.Renderer.SetDrawColor(0, 0, 0, 255)
	app.Renderer.Clear()

	if app.SelectedCam < len(app.Cameras) && app.ShowCamera {
		camera := &app.Cameras[app.SelectedCam]

		if camera.Active && camera.Texture != nil {
			camera.FrameMutex.Lock()

			// Get window size
			w, h, _ := app.Window.Size()

			// Calculate aspect ratio preserving scaling
			cameraAspect := float32(camera.Width) / float32(camera.Height)
			windowAspect := float32(w) / float32(h)

			var dstRect sdl.FRect
			if cameraAspect > windowAspect {
				// Camera is wider, fit to width
				dstRect.W = float32(w)
				dstRect.H = float32(w) / cameraAspect
				dstRect.X = 0
				dstRect.Y = (float32(h) - dstRect.H) / 2
			} else {
				// Camera is taller, fit to height
				dstRect.H = float32(h)
				dstRect.W = float32(h) * cameraAspect
				dstRect.Y = 0
				dstRect.X = (float32(w) - dstRect.W) / 2
			}

			app.Renderer.RenderTexture(camera.Texture, nil, &dstRect)
			camera.FrameMutex.Unlock()
		} else {
			// Draw "No Signal" text
			app.Renderer.SetDrawColor(64, 64, 64, 255)
			app.Renderer.RenderFillRect(&sdl.FRect{X: 0, Y: 0, W: 640, H: 480})
		}
	}

	app.Renderer.Present()
}

// [Include all the camera detection and management functions from your original example]
// findCameraDevices, findRaspberryPiCameras, initAllCameras, etc.

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

	rpiCameras, err := findRaspberryPiCameras()
	if err != nil {
		log.Printf("Warning: Failed to detect Raspberry Pi cameras: %v", err)
	} else {
		nextIndex := len(cameras)
		for i, rpiCamera := range rpiCameras {
			cameras = append(cameras, CameraInfo{
				Path:  fmt.Sprintf("rpicam:%d", i),
				Name:  rpiCamera,
				Index: nextIndex + i,
			})
		}
	}

	sort.Slice(cameras, func(i, j int) bool {
		return cameras[i].Index < cameras[j].Index
	})

	return cameras, nil
}

func findRaspberryPiCameras() ([]string, error) {
	var cameras []string

	cmd := exec.Command("rpicam-vid", "--list-cameras")
	output, err := cmd.Output()
	if err != nil {
		return cameras, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, ":") && (strings.Contains(strings.ToLower(line), "imx") ||
			strings.Contains(strings.ToLower(line), "ov") ||
			strings.Contains(strings.ToLower(line), "camera")) {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				cameraName := strings.TrimSpace(parts[1])
				if cameraName != "" {
					cameras = append(cameras, fmt.Sprintf("RPi Camera: %s", cameraName))
				}
			}
		}
	}

	if len(cameras) == 0 {
		testCmd := exec.Command("rpicam-vid", "-t", "1", "--nopreview", "-o", "/dev/null")
		err := testCmd.Run()
		if err == nil {
			cameras = append(cameras, "Raspberry Pi Camera")
		}
	}

	return cameras, nil
}

func initAllCameras() {
	devices, err := findCameraDevices()
	if err != nil {
		app.StatusText = "Error listing devices: " + err.Error()
		return
	}

	if len(devices) == 0 {
		app.StatusText = "No camera devices found"
		return
	}

	app.StatusText = fmt.Sprintf("Found %d camera devices", len(devices))
	app.Cameras = make([]CameraInstance, len(devices))

	activeCameras := 0
	for i, deviceInfo := range devices {
		camera := &app.Cameras[i]
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

	app.StatusText = fmt.Sprintf("Initialized %d/%d cameras", activeCameras, len(devices))
	app.ShowCamera = activeCameras > 0
}

func initSingleCamera(camera *CameraInstance) error {
	if strings.HasPrefix(camera.Info.Path, "rpicam:") {
		return initRaspberryPiCamera(camera)
	}

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

	camera.Texture, err = app.Renderer.CreateTexture(
		sdl.PIXELFORMAT_RGBA32,
		sdl.TEXTUREACCESS_STATIC,
		camera.Width,
		camera.Height,
	)
	if err != nil {
		dev.Close()
		return fmt.Errorf("failed to create texture: %w", err)
	}

	if err = dev.Start(context.Background()); err != nil {
		camera.Texture.Destroy()
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

			// Decode in separate goroutine
			img, err := jpeg.Decode(bytes.NewReader(frameData))
			if err != nil {
				log.Printf("Failed to decode frame: %v", err)
				continue
			}

			bounds := img.Bounds()
			rgbaImg := image.NewRGBA(bounds)
			draw.Draw(rgbaImg, bounds, img, bounds.Min, draw.Src)

			select {
			case camera.ProcessedFrameChan <- rgbaImg:
			default:
				// Drop if processing channel is full
			}
		}
	}
}

func initRaspberryPiCamera(camera *CameraInstance) error {
	camera.Width = 640
	camera.Height = 480

	var err error
	camera.Texture, err = app.Renderer.CreateTexture(
		sdl.PIXELFORMAT_RGBA32,
		sdl.TEXTUREACCESS_STATIC,
		camera.Width,
		camera.Height,
	)
	if err != nil {
		return fmt.Errorf("failed to create texture: %w", err)
	}

	camera.Active = true
	camera.FrameChan = make(chan []byte, 10)

	return nil
}

func captureFramesForCamera(camera *CameraInstance) {
	defer close(camera.FrameChan)

	if strings.HasPrefix(camera.Info.Path, "rpicam:") {
		captureRaspberryPiFrames(camera)
		return
	}

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

func captureRaspberryPiFrames(camera *CameraInstance) {
	for camera.Active {
		cmd := exec.Command("rpicam-vid",
			"-t", "0",
			"--codec", "mjpeg",
			"--width", fmt.Sprintf("%d", camera.Width),
			"--height", fmt.Sprintf("%d", camera.Height),
			"--framerate", "30",
			"-n",
			"-o", "-")

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("Failed to get stdout pipe for RPi camera: %v", err)
			time.Sleep(time.Second)
			continue
		}

		if err := cmd.Start(); err != nil {
			log.Printf("Failed to start rpicam-vid: %v", err)
			time.Sleep(time.Second)
			continue
		}

		go readRPiMJPEGStream(stdout, camera.FrameChan, &camera.Active)

		for camera.Active {
			if cmd.Process != nil {
				err = cmd.Process.Signal(syscall.Signal(0))
				if err != nil {
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}

		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait()
		stdout.Close()

		if !camera.Active {
			break
		}

		time.Sleep(time.Second)
	}
}

func readRPiMJPEGStream(reader io.Reader, frames chan<- []byte, active *bool) {
	buffer := make([]byte, 1024*1024)
	frameBuffer := bytes.NewBuffer(nil)

	for *active {
		n, err := reader.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from rpicam-vid: %v", err)
			}
			break
		}

		frameBuffer.Write(buffer[:n])

		data := frameBuffer.Bytes()
		for {
			startIdx := bytes.Index(data, []byte{0xFF, 0xD8})
			if startIdx == -1 {
				break
			}

			endIdx := bytes.Index(data[startIdx+2:], []byte{0xFF, 0xD9})
			if endIdx == -1 {
				break
			}

			endIdx += startIdx + 2 + 2

			frame := make([]byte, endIdx-startIdx)
			copy(frame, data[startIdx:endIdx])

			select {
			case frames <- frame:
			default:
			}

			remaining := data[endIdx:]
			frameBuffer.Reset()
			frameBuffer.Write(remaining)
			data = frameBuffer.Bytes()
		}
	}
}

func updateCameraFrames() {
	for i := range app.Cameras {
		camera := &app.Cameras[i]
		if !camera.Active {
			continue
		}

		select {
		case rgbaImg, ok := <-camera.ProcessedFrameChan:
			if !ok {
				continue
			}

			camera.FrameMutex.Lock()
			if camera.Texture != nil {
				camera.Texture.Update(nil, rgbaImg.Pix, int32(rgbaImg.Stride))
			}
			camera.FrameMutex.Unlock()
		default:
		}
	}
}

func updateCameraTextures(camera *CameraInstance, frameData []byte) error {
	camera.FrameMutex.Lock()
	defer camera.FrameMutex.Unlock()

	img, err := jpeg.Decode(io.NewSectionReader(bytes.NewReader(frameData), 0, int64(len(frameData))))
	if err != nil {
		return fmt.Errorf("failed to decode frame: %w", err)
	}

	bounds := img.Bounds()
	rgbaImg := image.NewRGBA(bounds)
	draw.Draw(rgbaImg, bounds, img, bounds.Min, draw.Src)

	if camera.Texture != nil {
		err = camera.Texture.Update(nil, rgbaImg.Pix, int32(rgbaImg.Stride))
		if err != nil {
			return fmt.Errorf("failed to update texture: %w", err)
		}
	}

	return nil
}

func cleanupCameras() {
	for i := range app.Cameras {
		camera := &app.Cameras[i]
		camera.Active = false
		time.Sleep(100 * time.Millisecond)

		if camera.Device != nil {
			camera.Device.Close()
		}

		camera.FrameMutex.Lock()
		if camera.Texture != nil {
			camera.Texture.Destroy()
			camera.Texture = nil
		}
		camera.FrameMutex.Unlock()
	}
}
