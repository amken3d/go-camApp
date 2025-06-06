package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Update the updateCameraFramesFromProcessed function
func updateCameraFramesFromProcessed() {
	for i := range cameraApp.Cameras {
		camera := &cameraApp.Cameras[i]
		if !camera.Active {
			continue
		}

		// Try to get a processed frame
		select {
		case processedFrame, ok := <-camera.ProcessedFrameChan:
			if !ok {
				continue
			}

			// Update the camera's current frame
			camera.FrameMutex.Lock()
			camera.CurrentFrame = processedFrame
			atomic.StoreInt32(&camera.TextureUpdated, 1)
			camera.LastFrameTime = time.Now()
			camera.FrameMutex.Unlock()

			// Increment frame counter for FPS calculation
			atomic.AddUint64(&camera.FrameCount, 1)

			// Update FPS every second
			updateCameraFPS(camera)

		default:
			// No new frame available, continue
		}
	}
}

// Enhanced processFramesForCamera function - replace the existing one
func processFramesForCamera(camera *CameraInstance) {
	defer close(camera.ProcessedFrameChan)
	log.Printf("Starting frame processing for camera: %s", camera.Info.Name)

	// Check if this is a Raspberry Pi camera
	if strings.HasPrefix(camera.Info.Path, "rpicam:") {
		processRaspberryPiFrames(camera)
		return
	}

	// Handle regular V4L2 cameras
	for camera.Active {
		select {
		case frame, ok := <-camera.FrameChan:
			if !ok {
				return
			}

			// Decode JPEG frame
			img, err := jpeg.Decode(bytes.NewReader(frame))
			if err != nil {
				atomic.AddUint64(&camera.DroppedFrames, 1)
				continue
			}

			// Convert to RGBA
			bounds := img.Bounds()
			rgbaImg := image.NewRGBA(bounds)
			draw.Draw(rgbaImg, bounds, img, bounds.Min, draw.Src)

			// Send processed frame
			select {
			case camera.ProcessedFrameChan <- rgbaImg:
			default:
				atomic.AddUint64(&camera.DroppedFrames, 1)
			}

		case <-time.After(100 * time.Millisecond):
			// Timeout, check if camera is still active
			if !camera.Active {
				return
			}
		}
	}
}

// Add this new function for processing Raspberry Pi frames
func processRaspberryPiFrames(camera *CameraInstance) {
	log.Printf("Starting Raspberry Pi camera processing for: %s", camera.Info.Name)

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

		log.Printf("Starting rpicam-vid command: %v", cmd.Args)

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

		log.Printf("rpicam-vid started successfully for camera: %s", camera.Info.Name)

		// Read MJPEG stream from rpicam-vid in a separate goroutine
		frameChan := make(chan []byte, 10)
		go readRPiMJPEGStream(stdout, frameChan, &camera.Active)

		// Process frames from the RPi camera
		processLoop := true
		frameCount := 0
		for processLoop && camera.Active {
			select {
			case frame, ok := <-frameChan:
				if !ok {
					log.Printf("Frame channel closed for RPi camera")
					processLoop = false
					break
				}

				frameCount++
				if frameCount%30 == 0 {
					//log.Printf("Processed %d frames from RPi camera", frameCount)
				}

				// Decode JPEG frame
				img, err := jpeg.Decode(bytes.NewReader(frame))
				if err != nil {
					log.Printf("Failed to decode JPEG frame: %v", err)
					atomic.AddUint64(&camera.DroppedFrames, 1)
					continue
				}

				// Convert to RGBA
				bounds := img.Bounds()
				rgbaImg := image.NewRGBA(bounds)
				draw.Draw(rgbaImg, bounds, img, bounds.Min, draw.Src)

				// Update last frame time
				camera.LastFrameTime = time.Now()

				// Send processed frame
				select {
				case camera.ProcessedFrameChan <- rgbaImg:
				default:
					atomic.AddUint64(&camera.DroppedFrames, 1)
				}

			case <-time.After(5 * time.Second):
				log.Printf("No frames received from RPi camera in 5 seconds, checking process...")
				// Check if process is still running
				if cmd.Process != nil {
					err = cmd.Process.Signal(syscall.Signal(0))
					if err != nil {
						log.Printf("rpicam-vid process died: %v", err)
						processLoop = false
						break
					}
				}
			}
		}

		log.Printf("Cleaning up rpicam-vid process")
		// Clean up
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait()
		stdout.Close()
		close(frameChan)

		if !camera.Active {
			break
		}

		// Brief pause before restarting
		log.Printf("Restarting rpicam-vid in 1 second...")
		time.Sleep(time.Second)
	}
}

// Enhanced readRPiMJPEGStream with better logging
func readRPiMJPEGStream(reader io.Reader, frames chan<- []byte, active *bool) {
	defer close(frames)
	log.Printf("Starting MJPEG stream reader")

	buffer := make([]byte, 1024*1024) // 1MB buffer
	frameBuffer := bytes.NewBuffer(nil)
	frameCount := 0

	for *active {
		n, err := reader.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from rpicam-vid: %v", err)
			}
			break
		}

		if n == 0 {
			continue
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

			frameCount++
			if frameCount%30 == 0 {
				//log.Printf("Read %d frames from rpicam-vid stream", frameCount)
			}

			// Send frame to channel
			select {
			case frames <- frame:
			default:
				// Channel full, drop frame
				log.Printf("Dropped frame due to full channel")
			}

			// Remove processed frame from buffer
			remaining := data[endIdx:]
			frameBuffer.Reset()
			frameBuffer.Write(remaining)
			data = frameBuffer.Bytes()
		}
	}

	log.Printf("MJPEG stream reader finished, read %d frames total", frameCount)
}
