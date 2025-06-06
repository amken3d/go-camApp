package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/TotallyGamerJet/clay"
	"github.com/Zyko0/go-sdl3/sdl"
	"github.com/vladimirvivien/go4vl/device"
	"github.com/vladimirvivien/go4vl/v4l2"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Find all available camera devices
func findCameraDevices() ([]CameraInfo, error) {
	var cameras []CameraInfo

	// First, look for standard V4L2 video devices in /dev/
	matches, err := filepath.Glob("/dev/video*")
	if err != nil {
		return nil, fmt.Errorf("failed to find video devices: %w", err)
	}

	// Regular expression to extract the numeric index
	re := regexp.MustCompile(`/dev/video(\d+)`)

	for _, devicePath := range matches {
		// Try to get device information
		dev, err := device.Open(devicePath)
		if err != nil {
			// Skip devices we can't open
			continue
		}

		// Get the device index
		match := re.FindStringSubmatch(devicePath)
		index := 0
		if len(match) == 2 {
			fmt.Sscanf(match[1], "%d", &index)
		}

		// Get the camera name
		caps := dev.Capability()
		name := caps.Card[:]

		// Clean up the name string by removing null bytes
		name = strings.TrimRight(name, "\x00")
		if name == "" {
			name = fmt.Sprintf("Camera %d", index)
		}

		// Add to our list
		cameras = append(cameras, CameraInfo{
			Path:  devicePath,
			Name:  name,
			Index: index,
		})

		// Close the device as we're just checking
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

	// Sort cameras by their index
	sort.Slice(cameras, func(i, j int) bool {
		return cameras[i].Index < cameras[j].Index
	})

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

func initAllCameras(appData *CameraAppData) {
	// List available camera devices
	devices, err := findCameraDevices()
	if err != nil {
		appData.StatusText = "Error listing devices: " + err.Error()
		appData.StatusColor = clay.Color{R: 255, G: 100, B: 100, A: 255}
		return
	}

	if len(devices) == 0 {
		appData.StatusText = "No camera devices found"
		appData.StatusColor = clay.Color{R: 255, G: 100, B: 100, A: 255}
		return
	}

	appData.StatusText = fmt.Sprintf("Found %d camera devices", len(devices))
	appData.StatusColor = clay.Color{R: 100, G: 255, B: 100, A: 255}
	log.Printf("Found %d camera devices: %v", len(devices), devices)

	// Initialize cameras array
	appData.Cameras = make([]CameraInstance, len(devices))

	// Initialize each camera
	for i, deviceInfo := range devices {
		camera := &appData.Cameras[i]
		camera.Info = deviceInfo

		// Initialize the camera device
		err = initSingleCamera(camera, appData.Renderer)
		if err != nil {
			log.Printf("Failed to initialize camera %s: %v", deviceInfo.Name, err)
			camera.Active = false
		} else {
			// Start frame capture for this camera
			go captureFramesForCamera(camera)
		}
	}

	// Update status
	activeCameras := 0
	for _, camera := range appData.Cameras {
		if camera.Active {
			activeCameras++
		}
	}

	appData.StatusText = fmt.Sprintf("Initialized %d/%d cameras", activeCameras, len(devices))
	if activeCameras > 0 {
		appData.StatusColor = clay.Color{R: 100, G: 255, B: 100, A: 255}
	} else {
		appData.StatusColor = clay.Color{R: 255, G: 100, B: 100, A: 255}
	}
}
func initSingleCamera(camera *CameraInstance, renderer *sdl.Renderer) error {
	// Check if this is a Raspberry Pi camera
	if strings.HasPrefix(camera.Info.Path, "rpicam:") {
		return initRaspberryPiCamera(camera, renderer)
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

	// Get actual camera format
	format, err := dev.GetPixFormat()
	if err != nil {
		dev.Close()
		return fmt.Errorf("failed to get pixel format: %w", err)
	}

	log.Printf("Camera %s format: %+v", camera.Info.Name, format)
	camera.Width = int(format.Width)
	camera.Height = int(format.Height)

	// Create main texture
	camera.Texture, err = renderer.CreateTexture(
		sdl.PIXELFORMAT_RGBA32,
		sdl.TEXTUREACCESS_STATIC,
		camera.Width,
		camera.Height,
	)
	if err != nil {
		dev.Close()
		return fmt.Errorf("failed to create main texture: %w", err)
	}

	// Create thumbnail texture (scaled down)
	thumbnailWidth := camera.Width / 4
	thumbnailHeight := camera.Height / 4
	if thumbnailWidth < 80 {
		thumbnailWidth = 80
		thumbnailHeight = 60
	}

	camera.ThumbnailTexture, err = renderer.CreateTexture(
		sdl.PIXELFORMAT_RGBA32,
		sdl.TEXTUREACCESS_STATIC,
		thumbnailWidth,
		thumbnailHeight,
	)
	if err != nil {
		camera.Texture.Destroy()
		dev.Close()
		return fmt.Errorf("failed to create thumbnail texture: %w", err)
	}

	// Start the camera stream
	if err = dev.Start(context.Background()); err != nil {
		camera.ThumbnailTexture.Destroy()
		camera.Texture.Destroy()
		dev.Close()
		return fmt.Errorf("failed to start camera: %w", err)
	}

	camera.Active = true
	camera.FrameChan = make(chan []byte, 10)

	return nil
}

// initRaspberryPiCamera initializes a Raspberry Pi camera using rpicam-vid
func initRaspberryPiCamera(camera *CameraInstance, renderer *sdl.Renderer) error {
	// Set default dimensions for RPi camera
	camera.Width = 640
	camera.Height = 480

	// Create main texture
	var err error
	camera.Texture, err = renderer.CreateTexture(
		sdl.PIXELFORMAT_RGBA32,
		sdl.TEXTUREACCESS_STATIC,
		camera.Width,
		camera.Height,
	)
	if err != nil {
		return fmt.Errorf("failed to create main texture: %w", err)
	}

	// Create thumbnail texture (scaled down)
	thumbnailWidth := camera.Width / 4
	thumbnailHeight := camera.Height / 4
	if thumbnailWidth < 80 {
		thumbnailWidth = 80
		thumbnailHeight = 60
	}

	camera.ThumbnailTexture, err = renderer.CreateTexture(
		sdl.PIXELFORMAT_RGBA32,
		sdl.TEXTUREACCESS_STATIC,
		thumbnailWidth,
		thumbnailHeight,
	)
	if err != nil {
		camera.Texture.Destroy()
		return fmt.Errorf("failed to create thumbnail texture: %w", err)
	}

	camera.Active = true
	camera.FrameChan = make(chan []byte, 10)

	log.Printf("Initialized Raspberry Pi camera: %s (%dx%d)", camera.Info.Name, camera.Width, camera.Height)

	return nil
}
func captureFramesForCamera(camera *CameraInstance) {
	defer close(camera.FrameChan)

	// Check if this is a Raspberry Pi camera
	if strings.HasPrefix(camera.Info.Path, "rpicam:") {
		captureRaspberryPiFrames(camera)
		return
	}

	// Handle regular V4L2 cameras (existing code)
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

// captureRaspberryPiFrames captures frames from Raspberry Pi camera using rpicam-vid
func captureRaspberryPiFrames(camera *CameraInstance) {
	for camera.Active {
		// Start rpicam-vid process
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

		// Read MJPEG stream from rpicam-vid
		go readRPiMJPEGStream(stdout, camera.FrameChan, &camera.Active)

		// Wait for the command to finish or camera to be deactivated
		for camera.Active {
			if cmd.Process != nil {
				// Check if process is still running
				err = cmd.Process.Signal(syscall.Signal(0))
				if err != nil {
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}

		// Clean up
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait()
		stdout.Close()

		if !camera.Active {
			break
		}

		// Brief pause before restarting
		time.Sleep(time.Second)
	}
}

// readRPiMJPEGStream reads MJPEG frames from rpicam-vid stdout
func readRPiMJPEGStream(reader io.Reader, frames chan<- []byte, active *bool) {
	buffer := make([]byte, 1024*1024) // 1MB buffer
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

		// Look for JPEG frame boundaries
		data := frameBuffer.Bytes()
		for {
			// Find JPEG start marker (0xFF 0xD8)
			startIdx := bytes.Index(data, []byte{0xFF, 0xD8})
			if startIdx == -1 {
				break
			}

			// Find JPEG end marker (0xFF 0xD9)
			endIdx := bytes.Index(data[startIdx+2:], []byte{0xFF, 0xD9})
			if endIdx == -1 {
				// Incomplete frame, wait for more data
				break
			}

			endIdx += startIdx + 2 + 2 // Include the end marker

			// Extract the complete JPEG frame
			frame := make([]byte, endIdx-startIdx)
			copy(frame, data[startIdx:endIdx])

			// Send frame to channel
			select {
			case frames <- frame:
			default:
				// Channel full, drop frame
			}

			// Remove processed frame from buffer
			remaining := data[endIdx:]
			frameBuffer.Reset()
			frameBuffer.Write(remaining)
			data = frameBuffer.Bytes()
		}
	}
}

func updateCameraFrames(appData *CameraAppData) {
	for i := range appData.Cameras {
		camera := &appData.Cameras[i]
		if !camera.Active {
			continue
		}

		// Try to get a new frame
		select {
		case frame, ok := <-camera.FrameChan:
			if !ok {
				continue
			}
			// Update textures with new frame
			err := updateCameraTextures(camera, frame)
			if err != nil {
				log.Printf("Error updating textures for camera %s: %v", camera.Info.Name, err)
			}
		default:
			// No new frame available, continue
		}
	}
}

func updateCameraTextures(camera *CameraInstance, frameData []byte) error {
	camera.FrameMutex.Lock()
	defer camera.FrameMutex.Unlock()

	// Decode the JPEG image
	img, err := jpeg.Decode(io.NewSectionReader(bytes.NewReader(frameData), 0, int64(len(frameData))))
	if err != nil {
		return fmt.Errorf("failed to decode frame: %w", err)
	}

	// Convert to RGBA
	bounds := img.Bounds()
	rgbaImg := image.NewRGBA(bounds)
	draw.Draw(rgbaImg, bounds, img, bounds.Min, draw.Src)

	// Update main texture
	if camera.Texture != nil {
		err = camera.Texture.Update(nil, rgbaImg.Pix, int32(rgbaImg.Stride))
		if err != nil {
			return fmt.Errorf("failed to update main texture: %w", err)
		}
	}

	// Create and update thumbnail texture
	if camera.ThumbnailTexture != nil {
		// Scale down the image for thumbnail
		thumbnailImg := scaleImage(rgbaImg, 4) // Scale down by factor of 4

		err = camera.ThumbnailTexture.Update(nil, thumbnailImg.Pix, int32(thumbnailImg.Stride))
		if err != nil {
			return fmt.Errorf("failed to update thumbnail texture: %w", err)
		}
	}

	return nil
}

// Simple image scaling function
func scaleImage(src *image.RGBA, scaleFactor int) *image.RGBA {
	srcBounds := src.Bounds()
	newWidth := srcBounds.Dx() / scaleFactor
	newHeight := srcBounds.Dy() / scaleFactor

	if newWidth < 1 {
		newWidth = 1
	}
	if newHeight < 1 {
		newHeight = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Simple nearest neighbor scaling
	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			srcX := x * scaleFactor
			srcY := y * scaleFactor
			if srcX < srcBounds.Dx() && srcY < srcBounds.Dy() {
				dst.Set(x, y, src.At(srcX, srcY))
			}
		}
	}

	return dst
}

func loadPlaceholderImage(appData *CameraAppData) error {
	// Load the placeholder image file
	imgFile, err := os.Open("640x480.jpg")
	if err != nil {
		return fmt.Errorf("failed to open placeholder image: %w", err)
	}
	defer imgFile.Close()

	// Decode the JPEG image
	img, err := jpeg.Decode(imgFile)
	if err != nil {
		return fmt.Errorf("failed to decode placeholder image: %w", err)
	}

	// Convert to RGBA
	bounds := img.Bounds()
	rgbaImg := image.NewRGBA(bounds)
	draw.Draw(rgbaImg, bounds, img, bounds.Min, draw.Src)

	// Create texture from the image
	width, height := int32(bounds.Dx()), int32(bounds.Dy())
	texture, err := appData.Renderer.CreateTexture(
		sdl.PIXELFORMAT_RGBA32,
		sdl.TEXTUREACCESS_STATIC,
		int(width),
		int(height),
	)
	if err != nil {
		return fmt.Errorf("failed to create placeholder texture: %w", err)
	}

	// Update texture with image data
	err = texture.Update(nil, rgbaImg.Pix, int32(rgbaImg.Stride))
	if err != nil {
		texture.Destroy()
		return fmt.Errorf("failed to update placeholder texture: %w", err)
	}

	// Store the placeholder texture
	appData.PlaceholderTexture = texture

	return nil
}

func cleanupCameras(appData *CameraAppData) {
	for i := range appData.Cameras {
		camera := &appData.Cameras[i]

		// Stop camera activity
		camera.Active = false

		// Give time for goroutines to finish
		time.Sleep(100 * time.Millisecond)

		// Close device
		if camera.Device != nil {
			camera.Device.Close()
		}

		// Destroy textures
		camera.FrameMutex.Lock()
		if camera.Texture != nil {
			camera.Texture.Destroy()
			camera.Texture = nil
		}
		if camera.ThumbnailTexture != nil {
			camera.ThumbnailTexture.Destroy()
			camera.ThumbnailTexture = nil
		}
		camera.FrameMutex.Unlock()
	}

	// Destroy placeholder texture
	if appData.PlaceholderTexture != nil {
		appData.PlaceholderTexture.Destroy()
		appData.PlaceholderTexture = nil
	}
}
