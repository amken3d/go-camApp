// Copyright 2014 The go-gl Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Renders video from V4L2 cameras using GLFW 3 and OpenGL 4.1 core forward-compatible profile.
package main

import (
	"bytes"
	"context"
	"fmt"
	gl "github.com/go-gl/gl/v3.1/gles2"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/vladimirvivien/go4vl/device"
	"github.com/vladimirvivien/go4vl/v4l2"
	"image"
	"image/jpeg"
	"io"
	"log"

	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// CameraInfo stores information about a connected camera device
type CameraInfo struct {
	Path  string // Device path like "/dev/video0"
	Name  string // Human-readable name
	Index int    // Numeric index
}

// PerformanceMetrics tracks rendering performance data
type PerformanceMetrics struct {
	framesProcessed int
	droppedFrames   int
	lastFrameTime   time.Time
	lastMetricsTime time.Time
	currentFPS      float64
	avgProcessingMS float64
	sumProcessingMS float64
}

const (
	windowWidth       = 800
	windowHeight      = 600
	frameWidth        = 640
	frameHeight       = 480
	smallFrameWidth   = 160
	smallFrameHeight  = 120
	maxPreviewCameras = 4 // Maximum number of small preview cameras to show
	thumbPadding      = 10
)

// Global variables to manage cameras and state
var (
	cameras        []CameraInfo
	activeCameras  []*device.Device
	selectedCamera int
	showMultiView  bool = true
	mainTexture    uint32
	smallTextures  []uint32
	frameCounter   uint64
	droppedFrames  uint64
	lastUpdate     time.Time
	currentFPS     float64
)

func init() {
	// GLFW event handling must run on the main OS thread
	runtime.LockOSThread()
}

func main() {
	// Initialize GLFW and OpenGL
	if err := glfw.Init(); err != nil {
		log.Fatalln("failed to initialize glfw:", err)
	}
	defer glfw.Terminate()

	// Find all available camera devices
	var err error
	cameras, err = findCameraDevices()
	if err != nil {
		log.Fatalf("Failed to find camera devices: %v", err)
	}

	if len(cameras) == 0 {
		log.Fatalf("No camera devices found")
	}

	fmt.Printf("Found %d camera devices:\n", len(cameras))
	for i, cam := range cameras {
		fmt.Printf("%d: %s (%s)\n", i, cam.Name, cam.Path)
	}

	// Initialize activeCameras slice
	activeCameras = make([]*device.Device, len(cameras))

	// Set up window and OpenGL context
	glfw.WindowHint(glfw.Resizable, glfw.False)
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)

	window, err := glfw.CreateWindow(windowWidth, windowHeight, "V4L2 Multi-Camera", nil, nil)
	if err != nil {
		panic(err)
	}
	window.MakeContextCurrent()

	// Add keyboard callback for camera switching
	window.SetKeyCallback(keyCallback)

	// Initialize Glow
	if err := gl.Init(); err != nil {
		panic(err)
	}

	version := gl.GoStr(gl.GetString(gl.VERSION))
	fmt.Println("OpenGL version", version)

	// Configure the vertex and fragment shaders
	program, err := newProgram(vertexShader, fragmentShader)
	if err != nil {
		panic(err)
	}

	gl.UseProgram(program)

	// In v4l2.go, modify the font initialization line:
	uiManager, err := NewUIManager("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 24.0, windowWidth, windowHeight)
	if err != nil {
		log.Printf("Failed to initialize UI with default font: %v", err)
		// Try a fallback font
		uiManager, err = NewUIManager("/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf", 24.0, windowWidth, windowHeight)
		if err != nil {
			log.Fatalf("Failed to initialize UI with fallback font: %v", err)
		}
	}

	defer uiManager.Cleanup()

	// Set up mouse callbacks to handle UI interaction
	window.SetCursorPosCallback(func(w *glfw.Window, xpos, ypos float64) {
		// This will be handled in uiManager.Update()
	})

	window.SetMouseButtonCallback(func(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
		// This will be handled in uiManager.Update()
	})

	// Add buttons for camera control
	camButtonWidth := float32(150)
	camButtonHeight := float32(40)
	padding := float32(10)

	// Toggle multi-view button
	uiManager.AddButton(
		padding,
		padding,
		camButtonWidth,
		camButtonHeight,
		"Toggle Multi-View",
		func() {
			showMultiView = !showMultiView
		},
	)

	// Camera selection buttons (up to 4 buttons vertically)
	for i := 0; i < len(cameras) && i < 4; i++ {
		index := i // Capture for closure
		uiManager.AddButton(
			padding,
			padding*2+camButtonHeight+float32(i)*(camButtonHeight+padding),
			camButtonWidth,
			camButtonHeight,
			fmt.Sprintf("Camera %d", i),
			func() {
				if index != selectedCamera {
					selectedCamera = index
					initSelectedCamera()
				}
			},
		)
	}

	// Set up camera matrix and view
	projection := mgl32.Perspective(mgl32.DegToRad(45.0), float32(windowWidth)/windowHeight, 0.1, 10.0)
	projectionUniform := gl.GetUniformLocation(program, gl.Str("projection\x00"))
	gl.UniformMatrix4fv(projectionUniform, 1, false, &projection[0])

	camera := mgl32.LookAtV(mgl32.Vec3{0, 0, 3}, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 1, 0})
	cameraUniform := gl.GetUniformLocation(program, gl.Str("camera\x00"))
	gl.UniformMatrix4fv(cameraUniform, 1, false, &camera[0])

	model := mgl32.Ident4()
	modelUniform := gl.GetUniformLocation(program, gl.Str("model\x00"))
	gl.UniformMatrix4fv(modelUniform, 1, false, &model[0])

	textureUniform := gl.GetUniformLocation(program, gl.Str("tex\x00"))
	gl.Uniform1i(textureUniform, 0)

	//gl.BindFragDataLocation(program, 0, gl.Str("outputColor\x00"))

	// Initialize the main camera (camera at index 0)
	selectedCamera = 0
	if err := initSelectedCamera(); err != nil {
		log.Fatalf("Failed to initialize main camera: %v", err)
	}

	// Create main texture for the selected camera
	mainTexture, err = createEmptyTexture(frameWidth, frameHeight)
	if err != nil {
		log.Fatalf("Failed to create main texture: %v", err)
	}

	// Initialize textures for all other cameras
	smallTextures = make([]uint32, len(cameras))
	for i := range smallTextures {
		if i != selectedCamera {
			smallTextures[i], err = createEmptyTexture(smallFrameWidth, smallFrameHeight)
			if err != nil {
				log.Printf("Failed to create texture for camera %d: %v", i, err)
				continue
			}

			// Try to initialize this camera
			if err := initCamera(i); err != nil {
				log.Printf("Failed to initialize camera %d: %v", i, err)
			}
		}
	}

	// Configure the vertex data for a simple quad
	var vao uint32
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)

	var vbo uint32
	gl.GenBuffers(1, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(quadVertices)*4, gl.Ptr(quadVertices), gl.STATIC_DRAW)

	//vertAttrib := uint32(gl.GetAttribLocation(program, gl.Str("vert\x00")))
	// Use:
	const vertAttribLocation = 0
	gl.BindAttribLocation(program, vertAttribLocation, gl.Str("vert\x00"))
	// ... after program linking:
	gl.EnableVertexAttribArray(vertAttribLocation)
	gl.VertexAttribPointerWithOffset(vertAttribLocation, 3, gl.FLOAT, false, 5*4, 0)

	//gl.EnableVertexAttribArray(vertAttrib)
	//gl.VertexAttribPointerWithOffset(vertAttrib, 3, gl.FLOAT, false, 5*4, 0)

	texCoordAttrib := uint32(gl.GetAttribLocation(program, gl.Str("vertTexCoord\x00")))
	gl.EnableVertexAttribArray(texCoordAttrib)
	gl.VertexAttribPointerWithOffset(texCoordAttrib, 2, gl.FLOAT, false, 5*4, 3*4)

	// Configure global settings
	gl.Enable(gl.DEPTH_TEST)
	gl.DepthFunc(gl.LESS)
	gl.ClearColor(0.0, 0.0, 0.0, 1.0)

	lastUpdate = time.Now()

	// Main render loop
	for !window.ShouldClose() {
		gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

		// Update FPS counter every second
		now := time.Now()
		atomic.AddUint64(&frameCounter, 1)

		elapsed := now.Sub(lastUpdate)
		if elapsed >= time.Second {
			// Calculate frames per second
			currentFPS = float64(atomic.LoadUint64(&frameCounter)) / elapsed.Seconds()
			atomic.StoreUint64(&frameCounter, 0)

			// Update title with stats and camera info
			title := fmt.Sprintf("V4L2 Multi-Camera | Camera: %s | FPS: %.1f | Dropped: %d",
				cameras[selectedCamera].Name, currentFPS, atomic.LoadUint64(&droppedFrames))
			window.SetTitle(title)

			lastUpdate = now
		}

		// Update main camera texture
		if activeCameras[selectedCamera] != nil {
			updateTextureWithCameraFrame(activeCameras[selectedCamera], mainTexture, &droppedFrames)
		}

		// Render main camera view
		renderMainCameraView(vao, program, modelUniform)

		// Update and render preview cameras if multi-view is enabled
		if showMultiView {
			for i, cam := range activeCameras {
				if i == selectedCamera || cam == nil {
					continue
				}

				// Update texture for this camera
				updateTextureWithCameraFrame(cam, smallTextures[i], &droppedFrames)
			}

			// Render the small preview cameras
			renderPreviewCameras(vao, program, modelUniform)
		}
		// Update and draw UI
		uiManager.Update(window)
		uiManager.Draw()

		// Draw performance metrics and status text using formatted text
		uiManager.DrawTextFormatted(
			float32(windowWidth-150),
			float32(20),
			1.0,
			mgl32.Vec3{1, 1, 1},
			"FPS: %.1f",
			currentFPS,
		)

		uiManager.DrawTextFormatted(
			float32(windowWidth-150),
			float32(50),
			1.0,
			mgl32.Vec3{1, 1, 1},
			"Camera: %s",
			cameras[selectedCamera].Name,
		)

		// Show dropped frames count
		uiManager.DrawTextFormatted(
			float32(windowWidth-150),
			float32(80),
			1.0,
			mgl32.Vec3{1, 1, 0},
			"Dropped: %d",
			atomic.LoadUint64(&droppedFrames),
		)

		// Maintenance
		window.SwapBuffers()
		glfw.PollEvents()
	}

	// Clean up resources before exit
	for i := range activeCameras {
		closeCamera(i)
	}
}

// Find all available camera devices
func findCameraDevices() ([]CameraInfo, error) {
	var cameras []CameraInfo

	// Look for video devices in /dev/
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
		name := string(caps.Card[:])

		// Clean up the name string by removing null bytes
		name = strings.TrimRight(name, "\x00")

		// Add to our list
		cameras = append(cameras, CameraInfo{
			Path:  devicePath,
			Name:  name,
			Index: index,
		})

		// Close the device as we're just checking
		dev.Close()
	}

	// Sort cameras by their index
	sort.Slice(cameras, func(i, j int) bool {
		return cameras[i].Index < cameras[j].Index
	})

	return cameras, nil
}

// Initialize the currently selected camera
func initSelectedCamera() error {
	return initCamera(selectedCamera)
}

// Initialize camera at the given index
func initCamera(index int) error {
	// Skip if this camera is already running
	if activeCameras[index] != nil {
		return nil
	}

	// Get camera info
	if index >= len(cameras) {
		return fmt.Errorf("camera index out of range")
	}

	camInfo := cameras[index]

	// Open the device with appropriate settings
	width := frameWidth
	height := frameHeight

	// For non-selected cameras, use smaller resolution
	if index != selectedCamera {
		width = smallFrameWidth
		height = smallFrameHeight
	}

	dev, err := device.Open(camInfo.Path,
		device.WithIOType(v4l2.IOTypeMMAP),
		device.WithPixFormat(v4l2.PixFormat{
			Width:       uint32(width),
			Height:      uint32(height),
			PixelFormat: v4l2.PixelFmtMJPEG, // Use MJPEG for better performance
			Field:       v4l2.FieldNone,
		}),
	)

	if err != nil {
		return fmt.Errorf("failed to open camera device %s: %w", camInfo.Path, err)
	}

	// Start the camera
	if err := dev.Start(context.Background()); err != nil {
		dev.Close()
		return fmt.Errorf("failed to start camera %s: %w", camInfo.Path, err)
	}

	// Store in our active cameras slice
	activeCameras[index] = dev

	return nil
}

// Close a specific camera
func closeCamera(index int) {
	if index >= len(activeCameras) || activeCameras[index] == nil {
		return
	}

	activeCameras[index].Stop()
	activeCameras[index].Close()
	activeCameras[index] = nil
}

// createEmptyTexture creates an initial texture of the specified dimensions
func createEmptyTexture(width, height int32) (uint32, error) {
	var texture uint32
	gl.GenTextures(1, &texture)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, texture)

	// Set texture parameters
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)

	// Initialize with empty data
	emptyData := make([]byte, width*height*3) // RGB format
	gl.TexImage2D(
		gl.TEXTURE_2D,
		0,
		gl.RGB,
		width,
		height,
		0,
		gl.RGB,
		gl.UNSIGNED_BYTE,
		gl.Ptr(emptyData),
	)

	return texture, nil
}

// updateTextureWithCameraFrame captures a frame from the camera and updates the OpenGL texture
func updateTextureWithCameraFrame(cam *device.Device, texture uint32, droppedFrames *uint64) bool {
	if cam == nil {
		return false
	}

	// Get frame from the output channel with a timeout
	select {
	case frame := <-cam.GetOutput():
		if frame == nil {
			atomic.AddUint64(droppedFrames, 1)
			return false
		}

		// Convert frame bytes to image (depends on pixel format)
		var img image.Image
		var err error

		// Assuming MJPEG format
		img, err = jpeg.Decode(io.NewSectionReader(bytes.NewReader(frame), 0, int64(len(frame))))
		if err != nil {
			atomic.AddUint64(droppedFrames, 1)
			return false
		}

		// Convert to RGBA format for OpenGL
		bounds := img.Bounds()
		width, height := bounds.Dx(), bounds.Dy()
		rgba := image.NewRGBA(bounds)

		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}

		// Update the texture with new frame data
		gl.BindTexture(gl.TEXTURE_2D, texture)
		gl.TexImage2D(
			gl.TEXTURE_2D,
			0,
			gl.RGBA,
			int32(width),
			int32(height),
			0,
			gl.RGBA,
			gl.UNSIGNED_BYTE,
			gl.Ptr(rgba.Pix),
		)
		return true

	case <-time.After(50 * time.Millisecond): // Short timeout for responsive UI
		// Timeout waiting for frame
		atomic.AddUint64(droppedFrames, 1)
		return false
	}
}

// Render the main camera view (full screen)
func renderMainCameraView(vao uint32, program uint32, modelUniform int32) {
	gl.UseProgram(program)

	// Set up model matrix for main view
	model := mgl32.Ident4()
	gl.UniformMatrix4fv(modelUniform, 1, false, &model[0])

	gl.BindVertexArray(vao)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, mainTexture)

	gl.DrawArrays(gl.TRIANGLES, 0, 6)
}

// Render small preview cameras
func renderPreviewCameras(vao uint32, program uint32, modelUniform int32) {
	gl.UseProgram(program)
	gl.BindVertexArray(vao)

	// Calculate size and position for small preview windows
	previewSize := float32(0.2) // Size relative to the full window
	padding := float32(0.01)    // Padding between previews

	// Count active cameras (excluding the selected one)
	activeCount := 0
	for i, cam := range activeCameras {
		if i != selectedCamera && cam != nil {
			activeCount++
		}
	}

	if activeCount == 0 {
		return
	}

	// Limit to max number of preview cameras
	if activeCount > maxPreviewCameras {
		activeCount = maxPreviewCameras
	}

	// Place the preview windows in the bottom-right corner
	startX := 1.0 - previewSize - padding
	startY := -1.0 + padding

	// Keep track of how many we've rendered
	rendered := 0

	for i, cam := range activeCameras {
		// Skip the main camera and inactive cameras
		if i == selectedCamera || cam == nil {
			continue
		}

		// Skip if we've reached the max
		if rendered >= maxPreviewCameras {
			break
		}

		// Calculate position for this preview
		x := startX
		y := startY + (float32(rendered) * (previewSize + padding))

		// Create model matrix for this preview camera
		model := mgl32.Ident4()
		model = model.Mul4(mgl32.Translate3D(x, y, 0))
		model = model.Mul4(mgl32.Scale3D(previewSize, previewSize, 1))

		gl.UniformMatrix4fv(modelUniform, 1, false, &model[0])

		// Draw this preview
		gl.ActiveTexture(gl.TEXTURE0)
		gl.BindTexture(gl.TEXTURE_2D, smallTextures[i])
		gl.DrawArrays(gl.TRIANGLES, 0, 6)

		rendered++
	}
}

// Keyboard callback to handle camera switching and controls
func keyCallback(window *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
	if action != glfw.Press {
		return
	}

	switch key {
	case glfw.KeyEscape:
		window.SetShouldClose(true)

	case glfw.KeyM:
		// Toggle multi-view mode
		showMultiView = !showMultiView

	case glfw.Key1, glfw.Key2, glfw.Key3, glfw.Key4, glfw.Key5, glfw.Key6, glfw.Key7, glfw.Key8, glfw.Key9:
		// Switch to camera 0-8 when pressing 1-9 keys
		newIndex := int(key) - int(glfw.Key1)
		if newIndex < len(cameras) {
			// Handle camera switching
			if newIndex != selectedCamera {
				// Save the old selected camera index to update its texture later
				oldSelected := selectedCamera

				// Update selected camera
				selectedCamera = newIndex

				// Make sure the camera is initialized
				if activeCameras[selectedCamera] == nil {
					if err := initSelectedCamera(); err != nil {
						log.Printf("Failed to initialize camera %d: %v", selectedCamera, err)
						// Revert selection if failed
						selectedCamera = oldSelected
						return
					}
				}

				// Update textures for the cameras that changed roles
				if smallTextures[oldSelected] == 0 {
					var err error
					smallTextures[oldSelected], err = createEmptyTexture(smallFrameWidth, smallFrameHeight)
					if err != nil {
						log.Printf("Failed to create small texture for camera %d: %v", oldSelected, err)
					}
				}
			}
		}
	}
}

func newProgram(vertexShaderSource, fragmentShaderSource string) (uint32, error) {
	vertexShader, err := compileShader(vertexShaderSource, gl.VERTEX_SHADER)
	if err != nil {
		return 0, err
	}

	fragmentShader, err := compileShader(fragmentShaderSource, gl.FRAGMENT_SHADER)
	if err != nil {
		return 0, err
	}

	program := gl.CreateProgram()

	gl.AttachShader(program, vertexShader)
	gl.AttachShader(program, fragmentShader)
	gl.LinkProgram(program)

	var status int32
	gl.GetProgramiv(program, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetProgramiv(program, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetProgramInfoLog(program, logLength, nil, gl.Str(log))

		return 0, fmt.Errorf("failed to link program: %v", log)
	}

	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)

	return program, nil
}

func compileShader(source string, shaderType uint32) (uint32, error) {
	shader := gl.CreateShader(shaderType)

	csources, free := gl.Strs(source)
	gl.ShaderSource(shader, 1, csources, nil)
	free()
	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetShaderInfoLog(shader, logLength, nil, gl.Str(log))

		return 0, fmt.Errorf("failed to compile %v: %v", source, log)
	}

	return shader, nil
}

//var vertexShader = `
//#version 330
//
//uniform mat4 projection;
//uniform mat4 camera;
//uniform mat4 model;
//
//in vec3 vert;
//in vec2 vertTexCoord;
//
//out vec2 fragTexCoord;
//
//void main() {
//    fragTexCoord = vertTexCoord;
//    gl_Position = projection * camera * model * vec4(vert, 1);
//}
//` + "\x00"
//
//var fragmentShader = `
//#version 330
//
//uniform sampler2D tex;
//
//in vec2 fragTexCoord;
//
//out vec4 outputColor;
//
//void main() {
//    outputColor = texture(tex, fragTexCoord);
//}
//` + "\x00"

var vertexShader = `
#version 100
uniform mat4 projection;
uniform mat4 camera;
uniform mat4 model;

attribute vec3 vert;
attribute vec2 vertTexCoord;

varying vec2 fragTexCoord;

void main() {
    fragTexCoord = vertTexCoord;
    gl_Position = projection * camera * model * vec4(vert, 1);
}
` + "\x00"

var fragmentShader = `
#version 100
precision mediump float;

uniform sampler2D tex;

varying vec2 fragTexCoord;

void main() {
    gl_FragColor = texture2D(tex, fragTexCoord);
}
` + "\x00"

/// Added for gles 3.1 support

// Simple quad vertices to display the camera texture
var quadVertices = []float32{
	//  X, Y, Z, U, V
	// Front face (clockwise)
	-1.0, -1.0, 0.0, 0.0, 0.0, // Bottom-left
	1.0, -1.0, 0.0, 1.0, 0.0, // Bottom-right
	1.0, 1.0, 0.0, 1.0, 1.0, // Top-right

	1.0, 1.0, 0.0, 1.0, 1.0, // Top-right
	-1.0, 1.0, 0.0, 0.0, 1.0, // Top-left
	-1.0, -1.0, 0.0, 0.0, 0.0, // Bottom-left
}
