package main

import (
	"fmt"
	"github.com/Zyko0/go-sdl3/bin/binsdl"
	"github.com/Zyko0/go-sdl3/bin/binttf"
	"hash/fnv"
	"log"
	"strconv"
	"strings"
	"sync"

	"unsafe"

	"github.com/TotallyGamerJet/clay"
	"github.com/TotallyGamerJet/clay/examples/fonts"
	"github.com/TotallyGamerJet/clay/renderers/sdl3"

	"github.com/Zyko0/go-sdl3/sdl"
	"github.com/Zyko0/go-sdl3/ttf"

	"github.com/vladimirvivien/go4vl/device"
)

type CameraInfo struct {
	Path  string
	Name  string
	Index int
}

type CameraInstance struct {
	Info             CameraInfo
	Device           *device.Device
	Texture          *sdl.Texture
	ThumbnailTexture *sdl.Texture
	FrameChan        chan []byte
	Active           bool
	Width            int
	Height           int
	FrameMutex       sync.RWMutex
	DroppedFrames    uint64
}

type CameraAppData struct {
	Cameras            []CameraInstance
	SelectedCamera     int
	StatusText         string
	StatusColor        clay.Color
	Renderer           *sdl.Renderer
	PlaceholderTexture *sdl.Texture
	KeyStates          map[sdl.Scancode]bool
}

func handleClayError(errorData clay.ErrorData) {
	panic(errorData)
}

const (
	FontIdBody16 = iota
	FontIdBody20
	FontIdBody10
)

// Improved sanitizeText function
func sanitizeText(text string) string {
	if text == "" {
		return "No text"
	}

	// Remove null bytes and non-printable characters
	cleanText := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1 // Drop the character
		}
		return r
	}, text)

	// Ensure the string is never empty after cleaning
	if cleanText == "" {
		return "No text"
	}

	// Limit length to prevent issues
	if len(cleanText) > 128 {
		cleanText = cleanText[:128] + "..."
	}

	return cleanText
}

// Safe text rendering with debugging
func safeText(id string, text string, config clay.TextElementConfig) {
	// Use debugText to log the text and get sanitized version
	cleanText := debugText(id, text)

	// Add a try-catch equivalent for each text rendering
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Text rendering recovered from panic for [%s]: %v", id, r)
			}
		}()
		clay.Text(cleanText, clay.TextConfig(config))
	}()
}

func main() {
	const (
		winWidth, winHeight = 1200, 800
	)

	// Initialize SDL
	defer binsdl.Load().Unload()
	defer binttf.Load().Unload()
	//sdl.LoadLibrary("./SDL/build/libSDL3.so.0")
	//ttf.LoadLibrary("./SDL_ttf/build/libSDL3_ttf.so.0")

	if err := sdl.Init(sdl.INIT_VIDEO); err != nil {
		panic(err)
	}
	defer sdl.Quit()

	if err := ttf.Init(); err != nil {
		panic(err)
	}
	defer ttf.Quit()

	var (
		window   *sdl.Window
		renderer *sdl.Renderer
		err      error
	)

	window, renderer, err = sdl.CreateWindowAndRenderer("Multi-Camera App", winWidth, winHeight, sdl.WINDOW_RESIZABLE|sdl.WINDOW_HIGH_PIXEL_DENSITY)

	if err != nil {
		panic(err)
	}

	// Enable linear filtering for better rendering quality
	if err = renderer.SetDrawBlendMode(sdl.BLENDMODE_BLEND); err != nil {
		panic(err)
	}

	defer window.Destroy()
	if err = window.SetResizable(true); err != nil {
		panic(err)
	}

	// Initialize font and text engine
	textEngine, err := ttf.CreateRendererTextEngine(renderer)
	if err != nil {
		panic(err)
	}

	stream, err := sdl.IOFromConstMem(fonts.RobotoRegularTTF)
	if err != nil {
		panic(err)
	}

	font, err := ttf.OpenFontIO(stream, false, 14)
	if err != nil {
		panic(err)
	}
	defer font.Close()
	// Set font hinting for sharper text
	font.SetHinting(ttf.HINTING_LIGHT)
	if err := testFontEngine(textEngine, font); err != nil {
		log.Printf("WARNING: Font engine test failed: %v", err)
		// Consider using a fallback rendering approach
	}

	rendererData := &sdl3.RendererData{
		Renderer:   renderer,
		TextEngine: textEngine,
		Fonts: []*ttf.Font{
			font,
		},
	}

	// Initialize Clay
	totalMemorySize := clay.MinMemorySize() * 2
	memory := make([]byte, totalMemorySize)
	// Store memory in a global or package-level variable to prevent GC
	clayMemory := memory // Declare clayMemory as a package-level variable

	// Ensure memory is properly aligned
	alignedMemory := clayMemory
	if uintptr(unsafe.Pointer(&memory[0]))%16 != 0 {
		// Find the next aligned address
		offset := 16 - (uintptr(unsafe.Pointer(&memory[0])) % 16)
		if int(offset) < len(memory) {
			alignedMemory = memory[offset:]
		}
	}

	arena := clay.CreateArenaWithCapacityAndMemory(alignedMemory)
	clay.Initialize(arena, clay.Dimensions{Width: winWidth, Height: winHeight}, clay.ErrorHandler{ErrorHandlerFunction: handleClayError})
	clay.SetMeasureTextFunction(sdl3.MeasureText, unsafe.Pointer(&rendererData.Fonts))

	// Initialize camera app data
	appData := &CameraAppData{
		StatusText:     "Initializing cameras...",
		StatusColor:    clay.Color{R: 255, G: 255, B: 0, A: 255},
		Renderer:       renderer,
		SelectedCamera: 0,
		KeyStates:      make(map[sdl.Scancode]bool),
	}

	// Start cameras initialization
	initAllCameras(appData)
	loadPlaceholderImage(appData)

	// Main rendering loop
	_ = sdl.RunLoop(func() error {
		scrollDelta := clay.Vector2{}
		var event sdl.Event
		for sdl.PollEvent(&event) {
			switch event.Type {
			case sdl.EVENT_QUIT:
				// Clean up cameras before exiting
				cleanupCameras(appData)
				return sdl.EndLoop

			case sdl.EVENT_WINDOW_RESIZED:
				e := event.WindowEvent()
				clay.SetLayoutDimensions(clay.Dimensions{
					Width:  float32(e.Data1),
					Height: float32(e.Data2),
				})

			case sdl.EVENT_MOUSE_WHEEL:
				e := event.MouseWheelEvent()
				scrollDelta = clay.Vector2{
					X: e.X,
					Y: e.Y,
				}

			case sdl.EVENT_KEY_DOWN:
				e := event.KeyboardEvent()
				appData.KeyStates[e.Scancode] = true
				handleKeyPress(appData, e.Scancode)

			case sdl.EVENT_KEY_UP:
				e := event.KeyboardEvent()
				appData.KeyStates[e.Scancode] = false

			case sdl.EVENT_MOUSE_BUTTON_DOWN:
				e := event.MouseButtonEvent()
				if e.Type == sdl.EVENT_MOUSE_BUTTON_DOWN {
					handleMouseClick(appData, float32(e.X), float32(e.Y))
				}
			}
		}

		state, x, y := sdl.GetMouseState()
		clay.SetPointerState(clay.Vector2{
			X: x,
			Y: y,
		}, state&sdl.BUTTON_LEFT != 0)

		clay.UpdateScrollContainers(true, scrollDelta, 0.01)

		// Update frames for all active cameras
		updateCameraFrames(appData)

		// Create UI layout
		renderCommands := createMultiCameraLayout(appData, renderer)

		// Clear the screen
		_ = renderer.SetDrawColor(0, 0, 0, 255)
		_ = renderer.Clear()

		// Render UI with error handling
		func() {
			defer func() {
				if r := recover(); r != nil {
					//log.Printf("Recovered from rendering panic: %v", r)
				}
			}()

			// Ensure all text fields in appData are valid
			appData.StatusText = sanitizeText(appData.StatusText)
			if appData.StatusText == "" {
				appData.StatusText = "Ready"
			}

			// Ensure camera names are sanitized
			for i := range appData.Cameras {
				if appData.Cameras[i].Info.Name == "" {
					appData.Cameras[i].Info.Name = fmt.Sprintf("Camera %d", i+1)
				} else {
					appData.Cameras[i].Info.Name = sanitizeText(appData.Cameras[i].Info.Name)
				}
			}

			err = sdl3.ClayRender(rendererData, renderCommands)
			if err != nil {
				log.Printf("Rendering error: %v", err.Error())
			}
		}()

		// Render main camera view
		renderMainCameraView(appData)

		// Render thumbnail views
		renderThumbnailViews(appData)

		_ = renderer.Present()

		return nil
	})
}

func handleKeyPress(appData *CameraAppData, scancode sdl.Scancode) {
	switch scancode {
	case sdl.SCANCODE_LEFT:
		if appData.SelectedCamera > 0 {
			appData.SelectedCamera--
		}
	case sdl.SCANCODE_RIGHT:
		if appData.SelectedCamera < len(appData.Cameras)-1 {
			appData.SelectedCamera++
		}
	case sdl.SCANCODE_1, sdl.SCANCODE_2, sdl.SCANCODE_3, sdl.SCANCODE_4,
		sdl.SCANCODE_5, sdl.SCANCODE_6, sdl.SCANCODE_7, sdl.SCANCODE_8, sdl.SCANCODE_9:
		// Direct camera selection with number keys
		cameraIndex := int(scancode - sdl.SCANCODE_1)
		if cameraIndex < len(appData.Cameras) {
			appData.SelectedCamera = cameraIndex
		}
	}
}

func handleMouseClick(appData *CameraAppData, x, y float32) {
	// Check if click is on any thumbnail
	for i := range appData.Cameras {
		thumbnailID := fmt.Sprintf("Thumbnail%d", i)
		element := clay.GetElementData(SafeID(thumbnailID))
		if element.Found {
			bbox := element.BoundingBox
			if x >= bbox.X && x <= bbox.X+bbox.Width &&
				y >= bbox.Y && y <= bbox.Y+bbox.Height {
				appData.SelectedCamera = i
				break
			}
		}
	}
}

// Add this debugging function to your code
func debugText(id string, text string) string {
	// Log the text that's about to be rendered
	cleanText := sanitizeText(text)
	//log.Printf("Debug text [%s]: '%s' -> '%s'", id, text, cleanText)
	return cleanText
}

// Add this function to test if the font engine is working properly
func testFontEngine(textEngine *ttf.TextEngine, font *ttf.Font) error {
	// Try to create a simple text surface
	surface, err := font.RenderTextBlended("Test", sdl.Color{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return fmt.Errorf("font rendering test failed: %v", err)
	}
	surface.Destroy()
	return nil
}

// SafeID generates a safe element ID using a numeric hash value instead of the original string
func SafeID(label string) clay.ElementId {
	// Use FNV hash to convert the string to a numeric value
	h := fnv.New32a()
	h.Write([]byte(label))
	hash := h.Sum32()

	// Convert the hash to a string of digits, which is safe for C string conversion
	safeLabel := "id_" + strconv.FormatUint(uint64(hash), 10)

	// Use the clay.ID function with the safe label
	return clay.ID(safeLabel)
}

// SafeIDWithIndex generates a safe element ID with an index, mimicking clay.GetElementIdWithIndex
func SafeIDWithIndex(label string, index uint32) clay.ElementId {
	// Combine the label and index
	combinedLabel := label + "_" + strconv.FormatUint(uint64(index), 10)
	return SafeID(combinedLabel)
}
