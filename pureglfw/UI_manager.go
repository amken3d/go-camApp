package main

import (
	"fmt"
	gl "github.com/go-gl/gl/v3.1/gles2"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/nullboundary/glfont"
)

// UIButton represents a clickable button
type UIButton struct {
	X           float32
	Y           float32
	Width       float32
	Height      float32
	Label       string
	NormalColor mgl32.Vec4
	HoverColor  mgl32.Vec4
	PressColor  mgl32.Vec4
	TextColor   mgl32.Vec3
	TextScale   float32
	isHovered   bool
	isPressed   bool
	onClick     func()
}

// UIManager handles all UI elements and rendering
type UIManager struct {
	font         *glfont.Font
	buttons      []*UIButton
	uiProgram    uint32
	windowWidth  int
	windowHeight int
	cursorX      float64
	cursorY      float64
	mousePressed bool
}

// NewUIManager creates a new UI manager
func NewUIManager(fontPath string, fontSize float32, windowWidth, windowHeight int) (*UIManager, error) {
	// Create shader program for UI elements (rectangles) first
	uiProgram, err := newProgram(uiVertexShader, uiFragmentShader)
	if err != nil {
		return nil, fmt.Errorf("failed to create UI shader program: %v", err)
	}

	//// Initialize glfont for text rendering
	//font, err := glfont.LoadFont(fontPath, int32(fontSize), windowWidth, windowHeight)
	//if err != nil {
	//	return nil, fmt.Errorf("failed to load font: %v", err)
	//}

	return &UIManager{
		//font:         font,
		buttons:      make([]*UIButton, 0),
		uiProgram:    uiProgram,
		windowWidth:  windowWidth,
		windowHeight: windowHeight,
	}, nil
}

// AddButton adds a new button to the UI
func (ui *UIManager) AddButton(x, y, width, height float32, label string, onClick func()) *UIButton {
	button := &UIButton{
		X:           x,
		Y:           y,
		Width:       width,
		Height:      height,
		Label:       label,
		NormalColor: mgl32.Vec4{0.2, 0.2, 0.2, 0.8},
		HoverColor:  mgl32.Vec4{0.3, 0.3, 0.3, 0.8},
		PressColor:  mgl32.Vec4{0.1, 0.1, 0.1, 0.8},
		TextColor:   mgl32.Vec3{1.0, 1.0, 1.0},
		TextScale:   1.0,
		onClick:     onClick,
	}
	ui.buttons = append(ui.buttons, button)
	return button
}

// Update updates the UI state based on cursor position and mouse buttons
func (ui *UIManager) Update(window *glfw.Window) {
	// Get cursor position
	ui.cursorX, ui.cursorY = window.GetCursorPos()

	// Check mouse button state
	wasPressed := ui.mousePressed
	ui.mousePressed = window.GetMouseButton(glfw.MouseButtonLeft) == glfw.Press
	clicked := !ui.mousePressed && wasPressed // Mouse was released

	// Update all buttons
	for _, button := range ui.buttons {
		// Check if cursor is over this button
		button.isHovered = float64(button.X) <= ui.cursorX &&
			ui.cursorX <= float64(button.X+button.Width) &&
			float64(button.Y) <= ui.cursorY &&
			ui.cursorY <= float64(button.Y+button.Height)

		// Handle clicking
		if button.isHovered && clicked && button.onClick != nil {
			button.onClick()
		}

		// Update button press state
		button.isPressed = button.isHovered && ui.mousePressed
	}
}

// Draw renders all UI elements
func (ui *UIManager) Draw() {
	// Enable blending for transparent UI elements
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)

	// Draw all buttons
	for _, button := range ui.buttons {
		ui.drawButton(button)
	}

	gl.Disable(gl.BLEND)
}

// drawButton renders a single button
func (ui *UIManager) drawButton(button *UIButton) {
	// Choose color based on button state
	var color mgl32.Vec4
	if button.isPressed {
		color = button.PressColor
	} else if button.isHovered {
		color = button.HoverColor
	} else {
		color = button.NormalColor
	}

	// Draw button background
	ui.drawRectangle(button.X, button.Y, button.Width, button.Height, color)

	// Get text width for centering (approximately)
	// Note: glfont doesn't have a Metrics function, so we estimate
	//textWidth := float32(len(button.Label)) * 8 * button.TextScale // Rough estimate

	// Center text horizontally and vertically
	//textX := button.X + (button.Width-textWidth)/2
	//textY := button.Y + button.Height/2

	// Draw button text
	//ui.font.SetColor(button.TextColor[0], button.TextColor[1], button.TextColor[2], 1.0)
	//ui.font.Printf(textX, textY, button.TextScale, button.Label)
}

// drawRectangle draws a colored rectangle
func (ui *UIManager) drawRectangle(x, y, width, height float32, color mgl32.Vec4) {
	gl.UseProgram(ui.uiProgram)

	// Set color uniform
	colorUniform := gl.GetUniformLocation(ui.uiProgram, gl.Str("color\x00"))
	gl.Uniform4fv(colorUniform, 1, &color[0])

	// Set projection uniform (orthographic projection)
	projection := mgl32.Ortho(0, float32(ui.windowWidth), float32(ui.windowHeight), 0, -1, 1)
	projectionUniform := gl.GetUniformLocation(ui.uiProgram, gl.Str("projection\x00"))
	gl.UniformMatrix4fv(projectionUniform, 1, false, &projection[0])

	// Create vertices for rectangle
	vertices := []float32{
		x, y, // Bottom left
		x + width, y, // Bottom right
		x + width, y + height, // Top right

		x, y, // Bottom left
		x + width, y + height, // Top right
		x, y + height, // Top left
	}

	// Create and bind VAO
	var vao uint32
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)
	defer gl.DeleteVertexArrays(1, &vao)

	// Create and bind VBO
	var vbo uint32
	gl.GenBuffers(1, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(vertices)*4, gl.Ptr(vertices), gl.STATIC_DRAW)
	defer gl.DeleteBuffers(1, &vbo)

	// Set vertex attributes
	vertAttrib := uint32(gl.GetAttribLocation(ui.uiProgram, gl.Str("position\x00")))
	gl.EnableVertexAttribArray(vertAttrib)
	gl.VertexAttribPointerWithOffset(vertAttrib, 2, gl.FLOAT, false, 2*4, 0)

	// Draw rectangle
	gl.DrawArrays(gl.TRIANGLES, 0, 6)

	// Cleanup
	gl.DisableVertexAttribArray(vertAttrib)
}

// DrawText draws text at the specified position
func (ui *UIManager) DrawText(text string, x, y float32, scale float32, color mgl32.Vec3) {
	//ui.font.SetColor(color[0], color[1], color[2], 1.0)
	//ui.font.Printf(x, y, scale, text)
}

// DrawTextFormatted draws formatted text using the Printf-style formatting
func (ui *UIManager) DrawTextFormatted(x, y float32, scale float32, color mgl32.Vec3, format string, args ...interface{}) {
	//ui.font.SetColor(color[0], color[1], color[2], 1.0)
	//ui.font.Printf(x, y, scale, format, args...)
}

// Cleanup releases resources
func (ui *UIManager) Cleanup() {
	// Note: glfont doesn't have a Release method, so we just delete the shader program
	gl.DeleteProgram(ui.uiProgram)

	// The font texture will be automatically cleaned up by OpenGL when the context is destroyed
}

// Shader for UI elements (rectangles)
//var uiVertexShader = `
//#version 330 core
//layout (location = 0) in vec2 position;
//
//uniform mat4 projection;
//
//void main() {
//    gl_Position = projection * vec4(position, 0.0, 1.0);
//}
//` + "\x00"
//
//var uiFragmentShader = `
//#version 330 core
//out vec4 FragColor;
//
//uniform vec4 color;
//
//void main() {
//    FragColor = color;
//}
//` + "\x00"

// Shader for UI elements (rectangles) For gl Es
var uiVertexShader = `
#version 100
attribute vec2 position;

uniform mat4 projection;

void main() {
    gl_Position = projection * vec4(position, 0.0, 1.0);
}
` + "\x00"

var uiFragmentShader = `
#version 100
precision mediump float;

uniform vec4 color;

void main() {
    gl_FragColor = color;
}
` + "\x00"

var fragmentFontShader = `
#version 100
precision mediump float;

varying vec2 fragTexCoord;
uniform sampler2D tex;
uniform vec4 textColor;

void main() {    
    vec4 sampled = vec4(1.0, 1.0, 1.0, texture2D(tex, fragTexCoord).r);
    gl_FragColor = textColor * sampled;
}` + "\x00"

var vertexFontShader = `
#version 100
attribute vec2 vert;
attribute vec2 vertTexCoord;

uniform vec2 resolution;

varying vec2 fragTexCoord;

void main() {
   // convert the rectangle from pixels to 0.0 to 1.0
   vec2 zeroToOne = vert / resolution;

   // convert from 0->1 to 0->2
   vec2 zeroToTwo = zeroToOne * 2.0;

   // convert from 0->2 to -1->+1 (clipspace)
   vec2 clipSpace = zeroToTwo - 1.0;

   fragTexCoord = vertTexCoord;

   gl_Position = vec4(clipSpace * vec2(1, -1), 0, 1);
}` + "\x00"
